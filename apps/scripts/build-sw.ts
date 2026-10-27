/**
 * 编译 Service Worker（`src/sw/index.ts` → `static/service-worker.js`）。
 *
 *   bun scripts/build-sw.ts          # 必须在 vite build **之前**跑，package.json 里已经串好
 *
 * 为什么不让 SvelteKit 构建它：SvelteKit 只要扫到自己管的 Service Worker 入口文件，而
 * `config.paths.assets` 又非空，就直接以 `service_worker_assets` 退出
 * （`node_modules/@sveltejs/kit/src/exports/vite/build/service-worker.js`）。而前台的静态产物要走
 * CDN 绝对地址，两者必须二选一 —— 于是把这个文件移出 `src/service-worker/`，自己编译。
 *
 * 为什么输出到 `static/` 而不是 `build/client/`：**adapter-bun 的静态路由表是在 vite build 时定下的**。
 * 构建之后再往 `build/client/` 里塞文件，文件确实在磁盘上，但 `/service-worker.js` 一律 404
 * （实测踩过：页面一切正常、只有 SW 装不上，排查方向很容易跑偏）。放进 `static/` 由 Vite 复制进产物，
 * 既进路由表，也顺带被 adapter 的 precompress 压成 `.br` / `.gz`。
 *
 * 注入两个常量：
 *   __SW_VERSION__      给缓存命名用，每次构建都要变（新版激活时据此删掉上一版的页面缓存）。
 *                       默认取当前时间戳，`SW_VERSION` 可覆盖（需要可复现构建时用）。
 *   __SW_ASSET_BASE__   产物前缀，必须与 SvelteKit 写进 HTML 的那个值逐字相同，否则 SW 预缓存时
 *                       扒不到产物地址、运行时也接不住产物请求。两边读的是同一个 CDN_ASSET_PREFIX
 *                       （由 scripts/with-env.ts 载入同一份环境），所以天然一致。
 */
import { existsSync, mkdirSync, writeFileSync } from 'node:fs';
import { resolve } from 'node:path';

const root = resolve(import.meta.dirname, '..');
const entry = resolve(root, 'src/sw/index.ts');
const outDir = resolve(root, 'static');
const outFile = resolve(outDir, 'service-worker.js');

function fail(message: string): never {
	console.error(`Service Worker 构建失败：${message}`);
	process.exit(1);
}

if (!existsSync(entry)) {
	fail(`找不到入口文件 ${entry}`);
}

const version = process.env.SW_VERSION?.trim() || String(Date.now());

// 与 vite.config.ts 的 assetPrefix() 同一套规范化：去掉末尾斜杠，必须带 scheme
const assetBase = (process.env.CDN_ASSET_PREFIX ?? '').replace(/\/$/, '');

if (assetBase && !/^https?:\/\//.test(assetBase)) {
	fail(`CDN_ASSET_PREFIX 必须以 http:// 或 https:// 开头：${assetBase}`);
}

const result = await Bun.build({
	entrypoints: [entry],
	target: 'browser',
	format: 'esm',
	minify: true,
	define: {
		__SW_VERSION__: JSON.stringify(version),
		__SW_ASSET_BASE__: JSON.stringify(assetBase)
	}
});

if (!result.success || result.outputs.length === 0) {
	for (const log of result.logs) {
		console.error(log);
	}
	fail('编译没有产出文件');
}

const code = await result.outputs[0]!.text();

mkdirSync(outDir, { recursive: true });
writeFileSync(outFile, code);

console.log(
	`Service Worker：${Buffer.byteLength(code)} 字节 → static/service-worker.js` +
		`（版本 ${version}，产物前缀 ${assetBase || '同源'}）`
);
