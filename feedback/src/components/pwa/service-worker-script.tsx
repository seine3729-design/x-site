import { env } from '@/env'

/**
 * 注册 Service Worker。
 *
 * 用内联脚本而不是 client 组件：注册逻辑只有几行，做成组件会多出一个
 * 客户端 chunk，和「减少首屏 JS」的目标相反。脚本是本文件里的字面量，
 * 不拼接任何外部输入，所以 dangerouslySetInnerHTML 在这里是安全的。
 *
 * 等 load 事件之后再注册：注册本身会去下载 /sw.js 并触发预缓存，
 * 放在首屏阶段会和首屏资源抢带宽。
 *
 * 只在生产注册。开发模式下 SW 会缓存住静态产物，和 Turbopack 的热更新打架，
 * 表现为「改了代码页面不变」，很难查。
 */
const script = `
(function () {
  if (!('serviceWorker' in navigator)) return;
  window.addEventListener('load', function () {
    navigator.serviceWorker.register('/sw.js').catch(function () {
      /* 注册失败不影响站点使用，静默处理 */
    });
  });
})();
`

export function ServiceWorkerScript() {
  if (!env.isProduction) return null
  return <script dangerouslySetInnerHTML={{ __html: script }} />
}
