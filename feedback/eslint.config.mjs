// eslint-config-next 16 直接导出一组 flat config（CommonJS，所以走默认导入）
import nextConfig from 'eslint-config-next'

/**
 * Next.js 16 起不再有 `next lint`，直接跑 eslint。
 *
 * 服务端模块（src/lib/server/*、src/env.ts）靠首行的 `import 'server-only'`
 * 在编译期挡住客户端导入，比 lint 规则可靠，所以这里不重复配 no-restricted-imports。
 *
 * @type {import('eslint').Linter.Config[]}
 */
const config = [
  { ignores: ['.next/**', 'node_modules/**', 'next-env.d.ts', 'src/lib/api/schema.ts'] },
  ...nextConfig,
]

export default config
