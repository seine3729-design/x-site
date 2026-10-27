/**
 * 把后台产物里带内容哈希的那棵子树（dist/assets）上传到 R2。
 *
 *   bun scripts/upload-assets.ts
 *
 * 浏览器**直接从 CDN 取**这些文件：构建期 `ADMIN_ASSET_BASE` 写进 index.html 的就是
 * `<CDN 域名>/<前缀>/assets/...`（Vite 的 `base`）。所以对象键必须和那个前缀严格对应：
 *
 *   ADMIN_ASSET_BASE = https://<CDN 域名>/<CDN_ASSETS_PREFIX>/     （末尾斜杠是 Vite 要的）
 *   对象键           = <CDN_ASSETS_PREFIX>/assets/...
 *
 * 只传 assets/，刻意不传这三样：
 *   index.html   必须由后台域名自己发。会话是 HttpOnly Cookie，后端还要校验 Origin 白名单
 *                与 CSRF，页面和 /api/admin 必须同源（docs/03 第 2.3 节）。
 *   images/      只有两个 favicon、合计 8 KB，而且 index.html 里的图标直接写的是品牌 CDN
 *                的完整地址，没有引用它们。留在容器里当兜底，不值得为它单独配缓存策略。
 *   *.br / *.gz  预压缩文件在 R2 上是独立对象键，CDN 不会按 Accept-Encoding 在不同键之间选，
 *                上去只会变成「把 Brotli 字节当 JS 发出去」的坑。压缩交给 CDN 边缘。
 *   *.map        公开出去等于把源码送人（admin/Dockerfile 在构建阶段就把它们挪出 dist 了，
 *                这里的过滤是第二道）。
 *
 * **不删远端旧对象**：老页面还在引用的 chunk 要留着，否则发布瞬间就 404。
 * 清理靠 R2 的生命周期规则（deploy/prod/scripts/r2-setup.sh 里 admin/ 前缀 90 天过期）。
 *
 * 写法与 apps/scripts/upload-assets.ts、feedback/scripts/upload-assets.ts 相同；三端的产物布局
 * 和不传的文件各不一样，而且 Docker 构建上下文互相看不到，所以各自一份。
 */
const ROOT = new URL('../', import.meta.url).pathname;
const SOURCE_DIR = `${ROOT}dist/assets`;
const SOURCE_PREFIX = `${ROOT}dist`;

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
const prefix = (process.env.CDN_ASSETS_PREFIX ?? 'admin').replace(/^\/+|\/+$/g, '');
const client = new Bun.S3Client({
	endpoint: required('R2_ENDPOINT'),
	bucket,
	accessKeyId: required('R2_ACCESS_KEY_ID'),
	secretAccessKey: required('R2_SECRET_ACCESS_KEY'),
	region: process.env.R2_REGION ?? 'auto'
});

const files: string[] = [];

for await (const entry of new Bun.Glob('**/*').scan({ cwd: SOURCE_DIR, onlyFiles: true, absolute: true })) {
	if (/\.(?:br|gz|map)$/.test(entry)) {
		continue;
	}
	files.push(entry);
}

if (files.length === 0) {
	console.error(`没有找到可上传的产物，先跑 pnpm build：${SOURCE_DIR}`);
	process.exit(1);
}

console.log(`CDN：${files.length} 个产物 → s3://${bucket}/${prefix}/assets/`);

let uploaded = 0;
let skipped = 0;
let failed = 0;
const CONCURRENCY = 8;
const queue = [...files];

async function worker() {
	for (;;) {
		const local = queue.pop();

		if (!local) {
			return;
		}

		// 对象键 = CDN_ASSETS_PREFIX + 产物在 dist 下的相对路径
		const key = `${prefix}${local.slice(SOURCE_PREFIX.length)}`;

		try {
			// 文件名带内容哈希，已存在就是同一份内容，重发只是浪费带宽
			if (await client.exists(key)) {
				skipped += 1;
				continue;
			}

			await client.write(key, Bun.file(local), { type: contentType(local) });
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
	// 有文件没传上去就让这一步失败：compose 里 admin 依赖它成功完成，
	// 否则会出现「页面已经发布，但 CDN 上缺它要的 chunk」
	process.exit(1);
}
