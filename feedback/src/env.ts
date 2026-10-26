import 'server-only'

/**
 * 服务端环境变量的唯一入口（feedback/docs/06 第 1.1 节）。
 *
 * 规则：
 * - 缺少必需变量时**启动即报错**，不静默用危险默认值；
 * - 这些值只在服务端读取，任何一个都不能进客户端 bundle，所以本模块标记 server-only。
 *
 * 必需变量用 getter 而不是在模块求值时就读。原因：`next build` 的
 * 「Collecting page data」阶段会 import 到这里（经由 SiteHeader），但构建期
 * 并不回源，不该要求提供运行时密钥 —— 否则 CI 就得给一个假 token 才能构建，
 * 那种假值一旦漏进部署比缺值更难查。
 *
 * 「启动即报错」的语义由 src/instrumentation.ts 保住：它在服务启动时读一次，
 * 配置缺失当场失败，而不是等第一个用户请求进来才失败。
 */
function required(name: string): string {
  const value = process.env[name]
  if (!value) {
    throw new Error(`缺少环境变量 ${name}（见仓库根目录 .env.example）`)
  }
  return value
}

function optional(name: string, fallback: string): string {
  return process.env[name] || fallback
}

export const env = {
  /** Go 后端的内网地址，只在服务端使用。 */
  get serviceInternalUrl(): string {
    return optional('SERVICE_INTERNAL_URL', 'http://service:8080')
  },
  /**
   * 回源服务令牌。带上它的请求在 service 侧命中 isInternalSSR 豁免，可以走明文
   * （adr/0008）；它绝不能出现在浏览器里。
   */
  get serviceToken(): string {
    return required('BFF_SERVICE_TOKEN')
  },
  /** 后端请求超时，毫秒。 */
  get timeoutMs(): number {
    return Number(optional('SERVICE_TIMEOUT_MS', '8000'))
  },
  /** 生产环境下会话 Cookie 带 Secure；本地走 http://localhost 不能带。 */
  isProduction: process.env.NODE_ENV === 'production',
}
