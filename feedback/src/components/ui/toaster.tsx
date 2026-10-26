'use client'

import dynamic from 'next/dynamic'

/**
 * 全站 Toast 容器。
 *
 * sonner 本体用 dynamic 懒加载：它挂在 root layout 上，直接 import 就会进
 * 首屏关键路径，但首屏一个 toast 都不会显示 —— toast 全是用户提交、投票之后才触发的。
 *
 * ssr: false 是必须的：Toast 容器没有服务端可渲染的内容，
 * 让它参与 SSR 只会多一轮 hydration 比对。
 *
 * 实测代价与收益（首页，Brotli 后）：
 *   关键路径  170.7 KB → 164.1 KB（-6.6 KB，sonner 11.1 KB 挪出去、dynamic 的
 *             loader 代码 4.5 KB 留下）
 *   总下载量  170.7 KB → 172.4 KB（+1.7 KB，多一个 chunk 的固定开销）
 *
 * 也就是说这不省流量，省的是「hydration 之前必须下完的字节」。
 * 之所以认为划算：阻塞 hydration 的字节直接影响可交互时间，
 * 而 sonner 在空闲时补下来，用户第一次触发 toast 时它早就位了。
 */
const SonnerToaster = dynamic(() => import('sonner').then((m) => m.Toaster), {
  ssr: false,
})

export function Toaster() {
  return (
    <SonnerToaster
      position="top-center"
      duration={4000}
      toastOptions={{
        classNames: {
          // 浮层用 surface-overlay：深色下它比卡片亮一档，toast 才不会糊进背景
          toast:
            'rounded-surface border border-border bg-surface-overlay text-fg shadow-popover text-sm',
          description: 'text-fg-muted',
          actionButton: 'rounded-control bg-primary text-fg-on-fill',
          cancelButton: 'rounded-control bg-surface-raised text-fg-muted',
        },
      }}
    />
  )
}
