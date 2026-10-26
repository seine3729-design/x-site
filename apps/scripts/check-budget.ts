// 首屏体积预算检查（docs/08 第 1 节：Web 首屏 JS ≤ 90 KB br）。先 bun run build，再 bun run budget。
// 按 SvelteKit 构建清单统计每个页面路由首次访问要加载的 JS（入口 + 布局 + 页面节点及其静态依赖，
// 即 HTML 里 modulepreload 的那一组）与内联样式；体积取 adapter 预压缩的 .br 文件，没有时现场压缩。
import { existsSync, readFileSync, statSync } from 'node:fs';
import { resolve } from 'node:path';
import { brotliCompressSync, constants } from 'node:zlib';

const JS_BUDGET = 90 * 1024;
const root = resolve(import.meta.dirname, '..');
const manifestPath = resolve(root, 'build/server/manifest.js');

if (!existsSync(manifestPath)) {
	console.error('找不到 build/server/manifest.js，请先执行 bun run build');
	process.exit(1);
}

type ServerNode = { imports?: string[]; inline_styles?: () => Record<string, string> };
type Route = { id: string; page: { layouts: (number | undefined)[]; leaf: number } | null };
const { manifest } = (await import(manifestPath)) as {
	manifest: { client: { imports: string[] }; nodes: (() => Promise<ServerNode>)[]; routes: Route[] };
};

const br = (bytes: Uint8Array | string) =>
	brotliCompressSync(bytes, { params: { [constants.BROTLI_PARAM_QUALITY]: 11 } }).byteLength;

/** 客户端文件压缩后的体积：优先用构建时生成的 .br。 */
function brSize(file: string): { raw: number; br: number } {
	const path = resolve(root, 'build/client', file);
	const raw = statSync(path).size;
	return { raw, br: existsSync(`${path}.br`) ? statSync(`${path}.br`).size : br(readFileSync(path)) };
}

const kb = (n: number) => `${(n / 1024).toFixed(1)} KB`;
let over = false;

for (const route of manifest.routes) {
	if (!route.page) continue;
	const files = new Set(manifest.client.imports);
	let css = '';
	for (const index of [...route.page.layouts, route.page.leaf]) {
		if (index === undefined) continue;
		const node = await manifest.nodes[index]!();
		for (const f of node.imports ?? []) files.add(f);
		for (const text of Object.values(node.inline_styles?.() ?? {})) css += text;
	}
	let raw = 0;
	let compressed = 0;
	for (const f of files) {
		const s = brSize(f);
		raw += s.raw;
		compressed += s.br;
	}
	const ok = compressed <= JS_BUDGET;
	over ||= !ok;
	console.log(
		`${ok ? '✓' : '✗'} ${route.id}：JS ${files.size} 个文件 ${kb(raw)}，br ${kb(compressed)}（预算 ${kb(JS_BUDGET)}）；` +
			`内联样式 ${kb(css.length)}，br ${kb(br(css))}`
	);
}

process.exit(over ? 1 : 0);
