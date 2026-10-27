/**
 * 把前台构建产物里带内容哈希的那棵子树（build/client/_app/immutable）上传到 R2。
 *
 *   bun scripts/upload-assets.ts
 *
 * 为什么只传这一棵子树：
 *   - 文件名里带内容哈希，内容一变文件名必然变，可以安全地按一年 immutable 缓存，
 *     而且**发布时不删远端旧对象** —— 老页面还在引用的 chunk 才不会在发布瞬间 404。
 *   - `_app/version.json` 是客户端轮询用来发现新版本的，必须每次拿到最新的，留在应用容器上发。
 *   - `service-worker.js`、`icons/`、`manifest` 同理，要么不能长缓存，要么必须由站点域名直接发。
 *
 * 浏览器**直接从 CDN 取**这些产物：页面里写的就是 `<CDN_ASSET_PREFIX>/_app/immutable/...`
 * （构建期由 SvelteKit 的 `paths.assets` 决定）。所以这里的对象键必须和那个前缀严格对应：
 *
 *   CDN_ASSET_PREFIX = https://<CDN 域名>/<CDN_ASSETS_PREFIX>
 *   对象键           = <CDN_ASSETS_PREFIX>/_app/immutable/...
 *
 * 跳过的文件：
 *   *.br / *.gz  预压缩文件在 R2 上是独立对象键，CDN 不会按 Accept-Encoding 在不同键之间选，
 *                上去只会变成「把 Brotli 字节当 JS 发出去」的坑。压缩交给 CDN 边缘。
 *   *.map        公开出去等于把源码送人。
 *
 * 对象上刻意不写 Cache-Control：Bun 的 S3 客户端没有这个选项（`S3Options` 只有 type /
 * contentEncoding / contentDisposition / acl / storageClass）。带内容哈希的产物在 Cloudflare
 * 上按静态扩展名缓存即可；需要精确控制时在 R2 的 Cache Rules 里按 `apps/` 前缀配。
 */
const ROOT = new URL('../', import.meta.url).pathname;
const SOURCE_DIR = `${ROOT}build/client/_app/immutable`;
const SOURCE_PREFIX = `${ROOT}build/client`;

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
const prefix = (process.env.CDN_ASSETS_PREFIX ?? 'apps').replace(/^\/+|\/+$/g, '');
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
	console.error(`没有找到可上传的产物，先跑 bun run build：${SOURCE_DIR}`);
	process.exit(1);
}

console.log(`CDN：${files.length} 个产物 → s3://${bucket}/${prefix}/_app/immutable/`);

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

		// 对象键 = CDN_ASSETS_PREFIX + 产物在 build/client 下的相对路径
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
	// 有文件没传上去就让这一步失败：compose 里 apps 依赖它成功完成，
	// 否则会出现「应用已经发布，但 CDN 上缺它要的 chunk」
	process.exit(1);
}
