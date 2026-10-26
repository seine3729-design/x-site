/**
 * 启动时的配置自检。
 *
 * `register()` 由 Next.js 在服务进程启动时调用一次（构建期不调用）。
 * 这里把 env.ts 里的必需变量全读一遍，缺哪个当场抛错、进程起不来。
 *
 * 为什么需要这一步：env.ts 里的必需变量是 getter，第一次被用到才校验。
 * 没有这个自检的话，漏配 BFF_SERVICE_TOKEN 的实例能正常启动、health 也过，
 * 直到第一个用户访问列表页才 500 —— 那时候排查成本高得多。
 */
export async function register() {
  const { env } = await import('./env')

  // 读一下就够了：getter 内部会校验，缺失时抛出带变量名的错误。
  // void 是为了让「这里刻意只求值不使用」这件事明确，避免被当成无用代码删掉。
  void env.serviceToken

  if (env.isProduction && !process.env.SERVICE_INTERNAL_URL) {
    // 不抛错：默认值 http://service:8080 在容器编排里是对的。
    // 但如果生产不是这个拓扑，这行日志是唯一的线索。
    console.warn('[feedback] 未设置 SERVICE_INTERNAL_URL，回源将使用默认的 http://service:8080')
  }
}
