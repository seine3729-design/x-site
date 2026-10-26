# 0010 · 开启 Cache Components 做首页静态化
状态：已采纳  
日期：2026-10-25  
决策人：用户（2026-10-25：「首页SSG 静态生成 一定要确保加载速度很快」「减少首屏 JavaScript，避免整个页面客户端化」）  
适用范围：`feedback/` 前台的渲染模式、数据读取分层、路由段配置  
取代/被取代：不取代任何 ADR；细化 `0008-实现位置复用现有工程` 里前台与后端的读取方式

## 背景与问题

要求是首页静态生成、加载要快。

直接的做法是给首页加 `revalidate` 走 ISR，但做不到：**顶栏在 layout 里读会话**
（`SiteHeader` → `getCurrentUser()` → `cookies()`）。传统 App Router 下 layout 一碰
`cookies()`，整条路由就是动态的，页面自己怎么写都没用。构建产物里 21 个路由全是 `ƒ (Dynamic)`，
首页也在其中。

绕开它的几种思路都有硬伤：

- 把顶栏的用户信息改成客户端获取 —— 多一次请求、顶栏会从「未登录」闪成「已登录」，
  而且和同一轮需求里的「减少首屏 JavaScript」相矛盾。
- 让首页不走 `(site)` 布局 —— 首页就没有统一顶栏了。
- 只给数据加缓存、不追求真静态 —— TTFB 会降，但首页仍是 `ƒ`，不满足「静态生成」。

## 决策

开启 Next.js 16 的 Cache Components（PPR 模型）：

```ts
cacheComponents: true,
partialPrefetching: true,   // 不给值 Next.js 会警告；新项目两个都开
```

配套三层改动：

| 层 | 改动 |
| --- | --- |
| 数据 | 新增 `lib/api/public-queries.ts`：公开视角、带 `'use cache'`，走新的 `callPublicService`（不带身份、不转发请求头） |
| 布局 | `SiteHeader` 不再 async，读 Cookie 的部分收进 `AccountSlot` + `Suspense`，占位与两种真实状态等宽 |
| 页面 | 首页 Hero 次要按钮、底部 CTA 各自 `Suspense`；时间基准改用 `cachedNow()`；其余 13 个依赖请求上下文的路由显式标 `export const instant = false` |

结果：首页 `◐ Partial Prerender`，TTFB 2–6ms（本机 `next start`，响应头 `x-nextjs-prerender: 1`）。
顺带 `/guide`、`/privacy`、`/terms`、`/recover` 也变成部分预渲染。

### 两条不能破的线

**公开查询绝不带访问者身份。** `callPublicService` 不读 Cookie、不转发
`user-agent` / `x-forwarded-for` / `x-request-id`。前者是因为缓存按源共享，带上身份等于把
某个人的可见范围发给所有人；后者是因为这些头会进 fetch 的 Data Cache key，
带上 UA 的话每种浏览器版本各占一份缓存，命中率基本为零。

**带个人数据的接口不许搬进 `public-queries.ts`。** 判断标准：同一个地址对不同人
返回不同内容的，就不能放进去。`queries.ts` 保持原样（带令牌、`no-store`）。

## 备选方案与放弃原因

| 方案 | 放弃原因 |
| --- | --- |
| 只给首页数据加 `revalidate`，不开 PPR | layout 读 Cookie，首页仍是 `ƒ (Dynamic)`，不满足「静态生成」。而且 `forwardHeaders()` 会把 UA 写进 Data Cache key，缓存几乎不命中 |
| 顶栏用户信息改客户端获取 | 多一次请求 + 顶栏状态闪烁；增加首屏 JS，与同轮需求冲突 |
| 首页脱离 `(site)` 布局 | 丢掉统一顶栏，用户体验倒退 |
| 用 `next-pwa` / `serwist` 生成 Service Worker | 本轮 SW 策略只有两条规则（静态资源 cache-first、导航 network-only），自己写 90 行比配一个黑盒更可控，也更容易保证「绝不缓存 HTML」这条 |
| 提供 `display: "browser"` 的 manifest | 要「不显示安装提示」，最稳的是根本不提供 manifest。性能收益来自 SW，与 manifest 无关 |
| 导航高亮改纯 CSS，把 `nav-links`/`bottom-nav` 变成服务端组件 | 能省约 3–6 KB，但 `aria-current` 是属性、CSS 改不了，屏幕阅读器会失去「当前位置」。`docs/06` 第 7 节明确要求 `aria-current`，不值得换 |
| 移动端筛选抽屉懒加载 | 实测关键路径只少 1.1 KB —— Radix Dialog 的大头是 dismissable-layer 与 focus-scope，同页的 Radix Select 也用它们，拆不出去。已回滚，保持直接 import |

## 后果

**好的：**

- 首页 HTML 不等后端，TTFB 从「6 次并行回源」降到 2–6ms；
- 静态内容不可能泄露私密数据（公开视角本身就是最小可见集）；
- 原本隐式的「这页必须实时渲染」变成了代码里显式的 `instant = false`。

**要承担的：**

- **首页列表只展示公开内容。** 「仅成员可见」的项目与反馈不在首页出现，
  要看它们去 `/projects`、`/me`。对一个对外橱窗页这是正确的，但和改动前的行为不同。
- **构建期需要能回源。** 首页数据在构建时就要取一次，CI 需要 `BFF_SERVICE_TOKEN`
  且后端可达。取不到不会让构建失败（`orEmpty` 兜住并打日志），但那次产出的首页是空状态，
  要等运行时缓存过期才补上。
- **禁用 JS 时顶栏没有登录入口。** 账号槽的流式内容靠 React 注入 `<template>` 实现。
  这个站本身依赖 JS，且底部导航与页面内 CTA 都有登录入口，所以接受。
- `cacheComponents` 翻转了全局默认（数据获取默认动态）。新增页面必须想清楚
  走 `use cache` 还是 `instant = false`，否则构建会直接报错 —— 报错比静默降级好，
  但新人会撞上一次。
- 相对时间最多滞后一个缓存周期（minutes 档）。对「几分钟前 / 几天前」的粒度无影响。

## 验证

- `bun run build`：首页 `◐`，`/offline` 与 `/_not-found` 为 `○`；
- 响应头 `x-nextjs-prerender: 1` + `x-nextjs-postponed: 1`；
- `BASE=... bun run check:budget`：首页关键路径 137.6 KB（br），预算 180 KB；
- PWA 15 项断言全通过，含**真的停掉服务器**后导航落到预缓存的 `/offline`；
- 交互回归 28 项、浅深色 × 双断点 28 个组合 0 溢出 0 错误。
