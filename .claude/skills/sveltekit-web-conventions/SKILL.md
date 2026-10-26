---
name: sveltekit-web-conventions
description: apps/ 前台（Bun + SvelteKit 3 + Svelte 5）的编码规范。用于编写或修改 apps/ 下的页面、组件、BFF 接口、hooks、Service Worker、样式时。界面工作同时读 ui-ux-pro-max 与 ui-styling。
---

# Web 前台规范（apps/）

## 概述

Bun 1.4 + SvelteKit 3 + Svelte 5（Runes）+ Vite 8 + Tailwind 4 + TypeScript 6。工程方案见 `docs/04` A 节。

## SvelteKit 3 要点（与 2.x 不同）

- 配置在 `vite.config.ts` 的 `sveltekit({...})` 中，没有 `svelte.config.js`。
- 共享代码导入用 `#lib/...`，**必须写扩展名**：`import { api } from '#lib/server/api.ts'`。
- 环境变量在 `src/env.ts` 用 `defineEnvVars` 声明；服务端读 `$app/env/private`，公开变量读 `$app/env/public`。新增变量先在 `env.ts` 声明并给出校验。
- 服务端专用代码放 `src/lib/server/`，不得被客户端代码导入。
- Remote Functions 与 Async Svelte 仍是实验特性，**不要使用**；数据用 `+page.server.ts` / `+layout.server.ts` 的 `load` 和 `+server.ts`。

## Svelte 5 写法

- 组件状态用 `$state`、派生用 `$derived`、副作用用 `$effect`（只用于同步外部系统，不用来派生数据）、属性用 `$props()`。
- 跨组件共享状态放 `*.svelte.ts` 模块，导出带 `$state` 的类或对象。
- 事件用属性写法 `onclick={...}`，不用 `on:click`。
- 用户内容一律文本插值，**禁止 `{@html}`**；后台富文本已清洗，仍需经 DOMPurify 后在专用组件中渲染。

## 多语言与加密
- 界面文案只写进 `src/lib/i18n/messages/*.json`（先加 `zh-CN.json`，再补齐其他 8 种），组件里用 `getI18n().t()`；业务错误用 `i18n.error(code)`，不直接显示后端 `message`。站内链接用 `i18n.path()` 加语言前缀。样式用逻辑属性（`ms-`/`me-`/`start-`/`end-`），保证阿拉伯语 RTL。
- 浏览器调用 `/api/v1/*` 只用 `#lib/crypto/api-client.ts` 的 `secureApi`；播放用 `#lib/player/secure-hls.ts`；图片用 `#lib/media/BlobImage.svelte`。
- 规则与 CDN 发布见 `docs/04` A.4–A.5。

## BFF 规则

- 浏览器只请求同源 `/api/v1/*`；BFF 通过 `#lib/server/api.ts` 调后端（带 `BFF_SERVICE_TOKEN`、`x-request-id`、客户端 IP）。
- BFF 不连数据库、不持有存储凭据或签名密钥。
- 令牌只放 HttpOnly Cookie；不要在 `localStorage` 存任何身份信息（本机偏好如手选线路可以存）。
- 公共页面 `load` 返回的数据不能包含个人信息，否则边缘缓存会串号；个人数据在客户端二次请求。

## 性能与体验

- hls.js、二维码、海报、评论抽屉等重模块用动态 `import()`。
- 图片固定宽高或 `aspect-ratio`，首屏前 4 张 `fetchpriority="high"`，其余 `loading="lazy"`。
- 列表超过 200 项启用虚拟滚动；刷剧流同时最多 3 个 `<video>`。
- 预算见 `performance-budget`。

## 测试

- 纯逻辑（线路排序展示、格式化、BFF 错误映射、加密、文案校验）：Vitest，`./dev.sh run apps bun run test`（`vitest.config.ts` 不加载 SvelteKit 插件）。
- 关键页面：Playwright（首页、详情、播放起播、登录）。
- 命令：`./dev.sh run apps bun run check`、`./dev.sh run apps bun run lint`、`./dev.sh run apps bun run build`。

## 红线

- 使用 `{@html}` 渲染用户内容
- 客户端代码导入 `#lib/server/*` 或 `$app/env/private`
- 在公共页面 SSR 数据中混入登录用户信息
- 硬编码后端地址或密钥

## 验证

- [ ] `bun run check` 与 `bun run lint` 通过
- [ ] 页面在浏览器中实际打开，控制台无错误
- [ ] 新环境变量已在 `env.ts` 声明并写入 `.env.example` 说明
