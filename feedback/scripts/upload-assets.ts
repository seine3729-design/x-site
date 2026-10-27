/**
 * 把 .next/static（全部带内容哈希）上传到 R2。
 *
 *   bun scripts/upload-assets.ts
 *
 * 浏览器**直接从 CDN 取**这些文件：构建期 `NEXT_ASSET_PREFIX` 变成 Next.js 的 `assetPrefix`，
 * 页面里引用的就是 `<CDN 域名>/<前缀>/_next/static/...`。所以对象键必须和那个前缀严格对应：
 *
 *   NEXT_ASSET_PREFIX = https://<CDN 域名>/<CDN_ASSETS_PREFIX>   （不带末尾斜杠）
 *   对象键            = <CDN_ASSETS_PREFIX>/_next/static/...
 *
 * 只传 .next/static，刻意不传这些：
 *   页面 HTML / RSC 流 / Server Actions  随请求变化、带 Cookie，`assetPrefix` 也不管它们。
 *   public/      没有内容哈希，而且 /sw.js 必须是 no-store（next.config.ts 里已设好响应头），
 *                交给 Next.js 自己发更不容易出错。
 *   *.br / *.gz  预压缩文件在 R2 上是独立对象键，CDN 不会按 Accept-Encoding 在不同键之间选，
 *                上去只会变成「把 Brotli 字节当 JS 发出去」的坑。压缩交给 CDN 边缘。
 *   *.map        公开出去等于把源码送人（next.config.ts 已经关掉了浏览器端 sourcemap，
 *                这里的过滤是第二道）。
 *
 * **不删远端旧对象**：老页面还在引用的 chunk 要留着，否则发布瞬间就 404。
 * 清理靠 R2 的生命周期规则（deploy/prod/scripts/r2-setup.sh 里 feedback/ 前缀 90 天过期）。
 *
 * 前台的 Service Worker（public/sw.js）已经认 CDN 跨源资源（`ASSET_ORIGINS`，接受 `cors`、
 * 仍挡 `opaque`）。换 CDN 域名时那里的 `ASSET_ORIGINS` 与 `VERSION` 要一起改。
 *
 * 写法与 apps/scripts/upload-assets.ts、admin/scripts/upload-assets.ts 相同；三端的产物布局
 * 和不传的文件各不一样，而且 Docker 构建上下文互相看不到，所以各自一份。
 */
/*
 * 这个脚本只在 feedback/Dockerfile 的 assets-uploader 阶段（Bun 镜像）里跑，而 feedback/ 自己的
 * 运行时是 Node、tsconfig 里没有 @types/bun。而 tsconfig 的 include 覆盖了全仓库的 .ts，
 * `next build` 会连这个文件一起类型检查，所以在这里声明用到的那几个 Bun API。
 *
 * 刻意不改 feedback/tsconfig.json 去 exclude 它：那等于以后谁都不再检查这个文件，
 * 而它是发布链路上的一环，写错了只会在部署时才暴露。
 */
declare const Bun: {
	file(path: string): unknown;
	S3Client: new (options: {
		endpoint: string;
		bucket: string;
		accessKeyId: string;
		secretAccessKey: string;
		region?: string;
	}) => {
		exists(key: string): Promise<boolean>;
		write(key: string, data: unknown, options?: { type?: string }): Promise<number>;
	};
	Glob: new (pattern: string) => {
		scan(options: { cwd: string; onlyFiles?: boolean; absolute?: boolean }): AsyncIterable<string>;
	};
};

const ROOT = new URL('../', import.meta.url).pathname;
const SOURCE_DIR = `${ROOT}.next/static`;

const CONTENT_TYPES: Record<string, string> = {
	css: 'text/css; charset=utf-8',
	js: 'text/javascript; charset=utf-8',
	json: 'application/json; charset=utf-8',
	svg: 'image/svg+xml',
	png: 'image/png',
	webp: 'image/webp',
	avif: 'image/avif',
	jpg: 'image/jpeg',
	jpeg: 'image/jpeg',
	gif: 'image/gif',
	ico: 'image/x-icon',
	woff: 'font/woff',
	woff2: 'font/woff2',
	ttf: 'font/ttf',
	wasm: 'application/wasm',
	txt: 'text/plain; charset=utf-8'
};

function required(name: string): string {
	const value = process.env[name]?.trim();

	if (!value) {
		console.error(`缺少环境变量 ${name}`);
		process.exit(1);
	}

	return value;
}

function contentType(path: string): string {
	const extension = path.split('.').pop()?.toLowerCase() ?? '';
	return CONTENT_TYPES[extension] ?? 'application/octet-stream';
}

const bucket = required('R2_BUCKET');
const prefix = (process.env.CDN_ASSETS_PREFIX ?? 'feedback').replace(/^\/+|\/+$/g, '');
const client = new Bun.S3Client({
	endpoint: required('R2_ENDPOINT'),
	bucket,
	accessKeyId: required('R2_ACCESS_KEY_ID'),
	secretAccessKey: required('R2_SECRET_ACCESS_KEY'),
	region: process.env.R2_REGION ?? 'auto'
});

const files: string[] = [];

for await (const entry of new Bun.Glob('**/*').scan({ cwd: SOURCE_DIR, onlyFiles: true })) {
	if (/\.(?:br|gz|map)$/.test(entry)) {
		continue;
	}
	files.push(entry);
}

if (files.length === 0) {
	console.error(`没有找到可上传的产物，先跑 bun run build：${SOURCE_DIR}`);
	process.exit(1);
}

console.log(`CDN：${files.length} 个产物 → s3://${bucket}/${prefix}/_next/static/`);

let uploaded = 0;
let skipped = 0;
let failed = 0;
const CONCURRENCY = 8;
const queue = [...files];

async function worker() {
	for (;;) {
		const relative = queue.pop();

		if (!relative) {
			return;
		}

		// 对象键 = CDN_ASSETS_PREFIX + 浏览器请求的路径（Next.js 固定是 /_next/static/…）
		const key = `${prefix}/_next/static/${relative}`;

		try {
			// 文件名带内容哈希，已存在就是同一份内容，重发只是浪费带宽
			if (await client.exists(key)) {
				skipped += 1;
				continue;
			}

			await client.write(key, Bun.file(`${SOURCE_DIR}/${relative}`), { type: contentType(relative) });
			uploaded += 1;
		} catch (error) {
			failed += 1;
			console.error(`上传失败 ${key}：${error instanceof Error ? error.message : String(error)}`);
		}
	}
}

await Promise.all(Array.from({ length: CONCURRENCY }, worker));

console.log(`CDN：新增 ${uploaded}，已存在 ${skipped}，失败 ${failed}`);

if (failed > 0) {
	// 有文件没传上去就让这一步失败：compose 里 feedback 依赖它成功完成，
	// 否则会出现「页面已经发布，但 CDN 上缺它要的 chunk」
	process.exit(1);
}

// 顶层 await 要求这个文件是模块，而它没有 import / export
export {};
