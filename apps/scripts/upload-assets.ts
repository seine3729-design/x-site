/**
 * 把前台构建产物发布到 R2：
 *
 *   bun scripts/upload-assets.ts
 *
 * 发布顺序：
 *   1. 先上传 build/client/_app/immutable 下带内容哈希的产物；
 *   2. 全部成功后，最后覆盖 build/client/_app/version.json。
 *
 * immutable 产物文件名里带内容哈希，可以安全地按一年缓存，而且发布时不删远端旧对象 ——
 * 老页面还在引用的 chunk 才不会在发布瞬间 404。version.json 不带哈希，SvelteKit 在配置
 * paths.assets 后会从 CDN 前缀轮询它，因此必须上传到同一前缀并在每次发布时覆盖，不能套用
 * 「对象已存在就跳过」的策略。Cloudflare 要对这个精确路径禁用缓存或使用源站校验。
 *
 * `/service-worker.js`、`/icons/`、`manifest` 不上传：它们要么必须由站点域名直接提供，
 * 要么不属于 SvelteKit 的 CDN 产物路径。
 *
 * 浏览器**直接从 CDN 取**这些产物：
 *
 *   CDN_ASSET_PREFIX = https://<CDN 域名>/<CDN_ASSETS_PREFIX>
 *   immutable 对象键 = <CDN_ASSETS_PREFIX>/_app/immutable/...
 *   版本对象键        = <CDN_ASSETS_PREFIX>/_app/version.json
 *
 * 跳过的 immutable 文件：
 *   *.br / *.gz  预压缩文件在 R2 上是独立对象键，CDN 不会按 Accept-Encoding 在不同键之间选，
 *                上去只会变成「把 Brotli 字节当 JS 发出去」的坑。压缩交给 CDN 边缘。
 *   *.map        公开出去等于把源码送人。
 *
 * 对象上刻意不写 Cache-Control：Bun 的 S3 客户端没有这个选项（`S3Options` 只有 type /
 * contentEncoding / contentDisposition / acl / storageClass）。带内容哈希的产物在 Cloudflare
 * 上按静态扩展名缓存；version.json 必须在 Cloudflare Cache Rules 中按精确路径绕过缓存。
 */
const ROOT = new URL('../', import.meta.url).pathname;
const SOURCE_DIR = `${ROOT}build/client/_app/immutable`;
const SOURCE_PREFIX = `${ROOT}build/client`;
const VERSION_FILE = `${ROOT}build/client/_app/version.json`;
const VERSION_CONTENT_TYPE = 'application/json; charset=utf-8';

const CONTENT_TYPES: Record<string, string> = {
	css: 'text/css; charset=utf-8',
	js: 'text/javascript; charset=utf-8',
	json: VERSION_CONTENT_TYPE,
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

type ObjectWriter<T> = (key: string, body: T, options: { type: string }) => Promise<unknown>;

/** version.json 使用固定 key；前缀两端的斜杠不参与对象键。 */
export function versionObjectKey(prefix: string): string {
	const normalized = prefix.replace(/^\/+|\/+$/g, '');
	return normalized ? `${normalized}/_app/version.json` : '_app/version.json';
}

/**
 * 每次发布都直接覆盖 version.json。这里刻意不接收 exists：旧版本存在恰恰是必须覆盖的场景。
 */
export async function publishVersion<T>(write: ObjectWriter<T>, prefix: string, body: T): Promise<string> {
	const key = versionObjectKey(prefix);
	await write(key, body, { type: VERSION_CONTENT_TYPE });
	return key;
}

function required(name: string): string {
	const value = process.env[name]?.trim();

	if (!value) {
		throw new Error(`缺少环境变量 ${name}`);
	}

	return value;
}

function contentType(path: string): string {
	const extension = path.split('.').pop()?.toLowerCase() ?? '';
	return CONTENT_TYPES[extension] ?? 'application/octet-stream';
}

async function main(): Promise<void> {
	let bucket: string;
	let prefix: string;
	let client: Bun.S3Client;
	try {
		bucket = required('R2_BUCKET');
		prefix = (process.env.CDN_ASSETS_PREFIX ?? 'apps').replace(/^\/+|\/+$/g, '');
		client = new Bun.S3Client({
			endpoint: required('R2_ENDPOINT'),
			bucket,
			accessKeyId: required('R2_ACCESS_KEY_ID'),
			secretAccessKey: required('R2_SECRET_ACCESS_KEY'),
			region: process.env.R2_REGION ?? 'auto'
		});
	} catch (error) {
		console.error(error instanceof Error ? error.message : String(error));
		process.exitCode = 1;
		return;
	}

	const files: string[] = [];

	for await (const entry of new Bun.Glob('**/*').scan({ cwd: SOURCE_DIR, onlyFiles: true, absolute: true })) {
		if (/\.(?:br|gz|map)$/.test(entry)) {
			continue;
		}
		files.push(entry);
	}

	if (files.length === 0) {
		console.error(`没有找到可上传的产物，先跑 bun run build：${SOURCE_DIR}`);
		process.exitCode = 1;
		return;
	}

	console.log(`CDN：${files.length} 个 immutable 产物 → s3://${bucket}/${prefix}/_app/immutable/`);

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

	console.log(`CDN immutable：新增 ${uploaded}，已存在 ${skipped}，失败 ${failed}`);

	if (failed > 0) {
		// 有文件没传上去就不覆盖 version.json，也不让应用启动；避免版本清单先指向不完整的新产物。
		process.exitCode = 1;
		return;
	}

	const versionFile = Bun.file(VERSION_FILE);
	if (!(await versionFile.exists())) {
		console.error(`没有找到版本文件，先跑 bun run build：${VERSION_FILE}`);
		process.exitCode = 1;
		return;
	}

	try {
		const key = await publishVersion((objectKey, body, options) => client.write(objectKey, body, options), prefix, versionFile);
		console.log(`CDN version：已覆盖 s3://${bucket}/${key}`);
	} catch (error) {
		console.error(`上传失败 ${versionObjectKey(prefix)}：${error instanceof Error ? error.message : String(error)}`);
		process.exitCode = 1;
	}
}

if (import.meta.main) {
	await main();
}
