/// <reference types="vitest/config" />
import path from 'node:path'
import babel from '@rolldown/plugin-babel'
import tailwindcss from '@tailwindcss/vite'
import { tanstackRouter } from '@tanstack/router-plugin/vite'
import react, { reactCompilerPreset } from '@vitejs/plugin-react'
import { defineConfig, loadEnv } from 'vite'

/** 资源前缀。整站地址必须带末尾斜杠，否则 Vite 会把它和文件名直接粘在一起。 */
function assetBase(value: string | undefined): string {
  const base = value?.trim()
  if (!base) {
    return '/'
  }
  if (!/^(https?:\/\/|\/)/.test(base)) {
    throw new Error(`ADMIN_ASSET_BASE 必须以 http(s):// 或 / 开头：${base}`)
  }
  return base.endsWith('/') ? base : `${base}/`
}

// https://vite.dev/config/
export default defineConfig(({ mode }) => {
  // 第三个参数为空字符串：同时读取 compose 注入的 VITE_BACKEND_URL
  const env = loadEnv(mode, process.cwd(), '')

  return {
    // 带内容哈希的产物可以放 CDN；index.html 仍由后台域名发出（会话是 HttpOnly Cookie + Origin
    // 白名单 + CSRF，必须和 /api/admin 同源）。留空时一切同源，和接 CDN 之前完全一样。
    // 注意：CDN 要对后台域名回 CORS 头，否则 type=module 的脚本按 CORS 取不回来。
    base: assetBase(env.ADMIN_ASSET_BASE),
    plugins: [
      tanstackRouter({ target: 'react', autoCodeSplitting: true }),
      // React Compiler 必须先于 react() 运行
      babel({ presets: [reactCompilerPreset()] }),
      react(),
      tailwindcss(),
    ],
    resolve: {
      alias: {
        '@': path.resolve(import.meta.dirname, './src'),
      },
    },
    build: {
      target: 'es2022',
      // 生成 sourcemap 但产物中不引用，只上传到错误监控平台
      sourcemap: 'hidden',
    },
    server: {
      proxy: {
        // 后台 API 只走 /api/admin（docs/07 第 0.5 节）；开发代理目标来自环境变量，不写死地址
        '/api/admin': {
          target: env.VITE_BACKEND_URL || 'http://localhost:8080',
          changeOrigin: true,
        },
      },
    },
    test: {
      environment: 'node',
      include: ['src/**/*.test.ts'],
    },
  }
})
