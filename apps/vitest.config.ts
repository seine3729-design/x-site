import { fileURLToPath } from 'node:url';
import { defineConfig } from 'vitest/config';

// 单元测试只覆盖纯 TS 模块（加密、语言、播放加载器、会话工具），不加载 SvelteKit 插件：
// 否则每次运行都会按测试环境重写 .svelte-kit/generated，影响正在运行的开发服务器。
// 代价是 $app/* 这些虚拟模块没有解析器，用到的那个在下面指到替身。
export default defineConfig({
	resolve: {
		alias: {
			// 先写更长的路径：alias 按顺序匹配前缀，'$app/env' 会把 '$app/env/private' 一起截走
			'$app/env/private': fileURLToPath(new URL('./config/test-stubs/app-env-private.ts', import.meta.url)),
			'$app/env': fileURLToPath(new URL('./config/test-stubs/app-env.ts', import.meta.url))
		}
	},
	test: {
		include: ['src/**/*.test.ts'],
		environment: 'node'
	}
});
