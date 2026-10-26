import type { NextConfig } from 'next'

/**
 * 反馈社区 H5/PC 前台（feedback/docs/06 第 1.1 节）。
 *
 * 浏览器只与本服务同源通信，由本服务的服务端带 X-Service-Token 转发到 Go
 * （命中 service 侧的 isInternalSSR 明文豁免，见 feedback/docs/adr/0008），
 * 因此这里不需要配置任何后端地址给客户端，也不暴露服务令牌。
 */
const config: NextConfig = {
  reactStrictMode: true,
  poweredByHeader: false,
  /*
   * 容器部署产物（feedback/docs/12 第 7 节）。
   *
   * standalone 把运行时真正用到的文件连同裁剪过的 node_modules 一起放到 .next/standalone，
   * 运行镜像里不再需要完整依赖树。没有它的话生产镜像得带上整个 node_modules。
   * 注意：standalone **不包含** .next/static 与 public，镜像构建时要自己拷进去
   * （见 feedback/Dockerfile）—— 这是 Next.js 的既定行为，不是漏拷。
   *
   * 刻意只在镜像构建时打开（Dockerfile 里设 NEXT_OUTPUT=standalone），不是无条件：
   * `next start` 不支持 standalone，会直接警告并且不服务请求。package.json 的 start 脚本、
   * scripts/check-js-budget.mjs（要起服务抓真实 HTML）和 CI 都依赖 `next start`，
   * 无条件打开会把这三处一起弄坏，而症状只是「服务起来了但请求不通」，很难归因。
   */
  output: process.env.NEXT_OUTPUT === 'standalone' ? 'standalone' : undefined,
  /*
   * 静态产物的 CDN 前缀，由构建期 NEXT_ASSET_PREFIX 注入（deploy/prod/scripts/cdn-upload.sh 上传同一份文件）。
   *
   * 只影响 /_next/static/* 的取用地址；页面 HTML、RSC 流、Server Actions、public/ 下的文件
   * 仍走本站同源，因为它们要么随请求变化、要么带 Cookie。
   *
   * 留空时一切同源，和接 CDN 之前完全一样。
   */
  assetPrefix: process.env.NEXT_ASSET_PREFIX?.replace(/\/$/, '') || undefined,
  /*
   * Source map 全部关掉。
   *
   * 浏览器端本来就没有（这个默认值是 false，写出来是为了不被将来某次改动悄悄打开——
   * 一旦打开，完整源码就随产物发给浏览器了）。
   *
   * 服务端那份默认是开的，实测 147 个 .map、32.2 MB。它不发给浏览器，
   * 但占构建时间和镜像体积。代价是线上服务端报错的堆栈不再映射回 TS 源码，
   * 只能看到编译后的位置 —— 排查线上问题时如果需要，临时打开重新构建一次即可。
   */
  productionBrowserSourceMaps: false,
  experimental: {
    serverSourceMaps: false,
  },
  /*
   * Cache Components（Next.js 16 的 PPR 模型）。
   *
   * 开它是为了让首页真正静态化。没有它做不到：顶栏在 layout 里读会话，
   * 传统模式下 layout 一碰 cookies() 整条路由就是动态的，页面自己怎么写都没用。
   * 有了 PPR，读 Cookie 的部分用 Suspense 一包，Next.js 就能把静态外壳
   * 预渲染成 HTML，只留那一小块在运行时流式填充。
   *
   * 代价要说清楚：这个开关会把**所有**路由的默认行为翻过来 ——
   * 数据获取默认是动态的，想静态就得显式 `use cache`。因此那些本来就该
   * 按访问者实时渲染的页面（列表、详情、我的、设置…）都显式标了
   * `export const instant = false`，表示「这页就是阻塞渲染，别试图预渲染」。
   * 这不是退让，是把原本隐式的行为写明。
   *
   * partialPrefetching 必须一起给值，否则 Next.js 会警告；新项目两个都开。
   */
  cacheComponents: true,
  partialPrefetching: true,
  /*
   * 压缩策略（feedback/docs/06 第 11 节）：
   *
   * 静态产物由 scripts/precompress.mjs 在构建后压成 .br 与 .gz，边缘层直接送，
   * 优先 Brotli、回退 Gzip（nginx 配置见 deploy/edge/feedback.conf）。
   *
   * 这里的 compress 管的是另一半：SSR 出来的 HTML 没法预压缩，
   * 边缘层没接或没开压缩时，由 Next.js 自己 gzip 兜住。它默认就是 true，
   * 显式写出来是因为「已经有预压缩了，这个是不是可以关」会被问到 —— 不能关。
   */
  compress: true,
  // 仅放行品牌 Logo 的固定 HTTPS 资源，避免开放任意远程图片域名。
  images: {
    remotePatterns: [
      {
        protocol: 'https',
        hostname: 'cdn.duanju66.cc',
        pathname: '/static/logo.webp',
      },
    ],
  },
  async headers() {
    return [
      {
        source: '/:path*',
        headers: [
          { key: 'X-Content-Type-Options', value: 'nosniff' },
          { key: 'Referrer-Policy', value: 'strict-origin-when-cross-origin' },
          // 这个站不需要这些能力，默认全部关掉
          { key: 'Permissions-Policy', value: 'camera=(), microphone=(), geolocation=()' },
        ],
      },
      {
        /*
         * public/ 下的静态文件没有内容哈希，Next.js 不会给它们加长缓存。
         * 这里显式给一天 + stale-while-revalidate：图标这类东西改动极少，
         * 但又不能像带哈希的产物那样标 immutable。
         * （.next/static 由 Next.js 自己加 immutable，不要在这里重复覆盖。）
         */
        source: '/:file*.(svg|png|ico|txt|xml)',
        headers: [
          { key: 'Cache-Control', value: 'public, max-age=86400, stale-while-revalidate=604800' },
        ],
      },
      {
        /*
         * Service Worker 本身绝不能被缓存。
         *
         * 浏览器对 SW 脚本有特殊的更新检查，但中间的 CDN 不一定懂这件事：
         * 一旦 /sw.js 被边缘缓存住，用户会一直拿到旧的 Service Worker，
         * 而它又控制着静态资源缓存 —— 等于站点卡在某个旧版本上，且很难排查。
         */
        source: '/sw.js',
        headers: [
          { key: 'Cache-Control', value: 'no-cache, no-store, must-revalidate' },
          { key: 'Content-Type', value: 'application/javascript; charset=utf-8' },
        ],
      },
    ]
  },
}

export default config
