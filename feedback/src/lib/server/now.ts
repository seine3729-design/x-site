import 'server-only'
import { cache } from 'react'
import { cacheLife } from 'next/cache'
import { connection } from 'next/server'

/**
 * 本次请求的时间基准。
 *
 * 两个理由不直接在组件里写 Date.now()：
 * - 渲染过程必须是纯的（React 的 lint 规则会直接拦住渲染期的 Date.now()）；
 * - 同一次渲染里所有「3 分钟前」要用同一个基准，否则页面上下两处相对时间可能差一秒，
 *   水合时也会和服务端结果不一致。
 *
 * 用 cache 包一层：一次请求内多次调用拿到同一个值。
 *
 * `await connection()` 是给 Cache Components 看的声明：这个值只有真正来了请求
 * 才能确定。不写的话 Next.js 在生成静态外壳时会撞上 `Date.now()` 并报
 * 「encountered the unstable value」—— 它没猜错，当前时刻确实不能被预渲染进 HTML。
 * 想要可以进静态页的时间基准，用下面的 cachedNow()。
 */
export const requestNow = cache(async (): Promise<number> => {
  await connection()
  return Date.now()
})

/**
 * 静态页用的时间基准。
 *
 * `requestNow()` 读的是当前时刻，这在预渲染里属于运行时数据，会把整页拖成动态。
 * 这个版本进缓存，档位和 public-queries 的数据一致（minutes）。
 *
 * 配成同一档不是巧合：首页的「3 分钟前」是用这个基准减去数据里的时间戳算的，
 * 两者取自同一个缓存时刻，算出来才是自洽的。如果基准实时、数据来自几分钟前的缓存，
 * 偏差会直接显示在页面上。
 *
 * 代价是相对时间最多滞后一个缓存周期 —— 对「几分钟前 / 几天前」这种粒度无影响。
 */
export async function cachedNow(): Promise<number> {
  'use cache'
  cacheLife('minutes')
  return Date.now()
}
