/*
 * Service Worker（feedback/docs/06 第 11.4 节）。
 *
 * 职责只有两件：
 *   1. 把带内容哈希的静态产物放进 Cache Storage，重复访问直接本地命中；
 *   2. 断网时导航请求回退到预缓存的 /offline，而不是浏览器的报错页。
 *
 * ──────────────────────────────────────────────────────────────
 * 不缓存 HTML。这不是保守，是正确性要求。
 *
 * 这个站的页面内容随登录用户变化（顶栏的账号、我的反馈、项目可见范围都是
 * 按访问者过滤的，见 src/lib/server/api.ts 的说明）。Cache Storage 是按
 * 源共享的，不区分登录状态 —— 缓存导航响应就意味着同一台设备上换账号登录后，
 * 可能看到上一个账号的页面。所以导航与 /api 一律 network-only。
 * ──────────────────────────────────────────────────────────────
 *
 * 也没有 web app manifest：没有 manifest 就不满足浏览器的可安装条件，
 * 地址栏不会出现安装图标、也不会弹安装横幅。PWA 的性能收益来自这个
 * Service Worker，和 manifest 无关。
 */

// 改这个版本号会让旧缓存在下次激活时被清掉。
const VERSION = 'fb-sw-v2'
const STATIC_CACHE = `${VERSION}-static`
const OFFLINE_URL = '/offline'

/** 只有这些路径前缀会被缓存。它们带内容哈希，内容永不变更，也不含任何用户数据。 */
const CACHEABLE_PREFIXES = ['/_next/static/', '/feedback/_next/static/']

/**
 * 除本站之外，还允许缓存这个源上的静态资源。
 *
 * 带内容哈希的产物由 next.config.ts 的 assetPrefix 指到 CDN，所以它们是**跨源**请求。
 * 原来这里只认同源，CDN 一开，静态缓存就等于没有了 —— 重复访问每次都要重新下载。
 *
 * 这个值和 next.config.ts 读的 NEXT_ASSET_PREFIX、src/app/layout.tsx 的 preconnect
 * 指的是同一个域名。public/ 下的文件不经打包，拿不到环境变量，只能写死；
 * 换 CDN 域名时三处要一起改，并且把上面的 VERSION 加一（让旧缓存失效）。
 */
const ASSET_ORIGINS = ['https://cdn.duanju66.cc']

self.addEventListener('install', (event) => {
  event.waitUntil(
    (async () => {
      const cache = await caches.open(STATIC_CACHE)
      // 离线页必须在装的时候就拿到，否则断网时它自己也取不到。
      // reload 绕过 HTTP 缓存，确保存进去的是当前版本。
      await cache.add(new Request(OFFLINE_URL, { cache: 'reload' }))
    })(),
  )
  // 立即进入 waiting→active。只缓存带哈希的资源，所以不存在新旧版本错配：
  // 旧页面请求的是旧哈希文件名，新 SW 不认识就直接走网络。
  self.skipWaiting()
})

self.addEventListener('activate', (event) => {
  event.waitUntil(
    (async () => {
      const names = await caches.keys()
      await Promise.all(
        names.filter((name) => !name.startsWith(VERSION)).map((name) => caches.delete(name)),
      )
      await self.clients.claim()
    })(),
  )
})

self.addEventListener('fetch', (event) => {
  const { request } = event

  // 写请求一概不管
  if (request.method !== 'GET') return
  const url = new URL(request.url)
  const sameOrigin = url.origin === self.location.origin
  // 本站之外只认静态资源 CDN，其他源（第三方、别的站）一概不碰
  if (!sameOrigin && !ASSET_ORIGINS.includes(url.origin)) return

  // 导航请求：永远走网络（见顶部说明），只在失败时给离线页兜底
  if (sameOrigin && request.mode === 'navigate') {
    event.respondWith(
      (async () => {
        try {
          return await fetch(request)
        } catch {
          const cache = await caches.open(STATIC_CACHE)
          const offline = await cache.match(OFFLINE_URL)
          // 连离线页都没有（SW 刚装好就断网）时，让浏览器显示它自己的错误页
          return offline ?? Response.error()
        }
      })(),
    )
    return
  }

  // 带内容哈希的静态产物：cache-first，命中就不碰网络
  if (CACHEABLE_PREFIXES.some((prefix) => url.pathname.startsWith(prefix))) {
    event.respondWith(
      (async () => {
        const cache = await caches.open(STATIC_CACHE)
        const hit = await cache.match(request)
        if (hit) return hit
        const response = await fetch(request)
        // 只存成功的完整响应。
        // basic 是同源，cors 是跨源且对方回了 Access-Control-Allow-Origin —— 这两种都能读、能存。
        // 刻意排除 opaque（跨源但没有 CORS 头）：它的 status 恒为 0，存进去读出来是不可用的空响应，
        // 还会把配额吃掉。CDN 的 CORS 白名单漏配本站域名时就会退化成 opaque，所以这里必须挡住。
        if (response.ok && (response.type === 'basic' || response.type === 'cors')) {
          cache.put(request, response.clone())
        }
        return response
      })(),
    )
    return
  }

  // 其余（/api、Server Action、RSC 载荷…）不拦截，交给浏览器按原样走网络
})
