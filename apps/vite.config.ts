import adapter from '@sveltejs/adapter-bun';
import { sveltekit } from '@sveltejs/kit/vite';
import tailwindcss from '@tailwindcss/vite';
import { defineConfig } from 'vite';

const port = 3001;

function assetPrefix(): '' | `http://${string}` | `https://${string}` {
	const cdn = process.env.CDN_ASSET_PREFIX?.replace(/\/$/, '') ?? '';

	if (!cdn) {
		return '';
	}

	if (cdn.startsWith('https://') || cdn.startsWith('http://')) {
		return cdn as `http://${string}` | `https://${string}`;
	}

	throw new Error(`CDN_ASSET_PREFIX 必须以 http:// 或 https:// 开头：${cdn}`);
}

export default defineConfig({
	build: {
		/*
		 * 浏览器基线（docs/04 A.6）。显式写出来，不吃 Vite 默认值——默认值会随 Vite 大版本漂移
		 * （7 起是 baseline-widely-available ≈ Safari 16），升级一次就可能把一批老设备挡在门外。
		 *
		 * 这里刻意比 CSS 的下限（Tailwind 4 要求 Safari 16.4 / Chrome 111）再低一档：
		 * 样式降级了页面还能看，脚本一旦有语法错就整页不能交互，所以 JS 往低处编。
		 * 不引入 @vitejs/plugin-legacy：它会额外产出一套 nomodule 包与 core-js，
		 * 和首屏 90 KB 的预算冲突，而真正的兜底是 SSR 出的 HTML 本身可读可点（渐进增强）。
		 */
		target: ['chrome100', 'edge100', 'firefox100', 'safari15.4']
	},
	server: {
		port,
		strictPort: true
	},
	preview: {
		port,
		strictPort: true
	},
	plugins: [
		tailwindcss(),
		sveltekit({
			adapter: adapter({
				precompress: true,
				serverOptions: {
					port
				}
			}),
			// 环境文件由 scripts/with-env.ts 载入。这里指到空目录，避免 Vite 再按 mode 读一遍 .env.*。
			env: {
				dir: 'config/vite-env'
			},
			paths: {
				assets: assetPrefix()
			},
			serviceWorker: {
				register: false
			},
			// 小于这个长度的样式直接写进 HTML，首屏不用再等一次样式请求。
			inlineStyleThreshold: 12_000,
			compilerOptions: {
				runes: ({ filename }) =>
					filename.split(/[/\\]/).includes('node_modules') ? undefined : true
			}
		})
	]
});
