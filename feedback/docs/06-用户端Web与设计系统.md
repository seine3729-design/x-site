# 06 · 用户端 Web 与设计系统

> 状态：设计方向已定，实现栈已采纳（Next.js 16.4 + React 19.3 + Bun 1.4 + Tailwind 4.3 + shadcn/ui）  
> 最后核对：2026-10-23  
> 权威范围：用户端页面行为、视觉语言、组件、响应式、无障碍与前端质量  
> 视觉前提：参考站仅用于产品模式分析；未获得可靠视觉截图，不做像素复刻。

## 1. 体验目标

三个关键词：**可信、清晰、有回应**。

- 可信：反馈状态、官方身份、修改记录和可见性明确；
- 清晰：内容优先，层级少，用户随时知道自己在哪个项目、下一步做什么；
- 有回应：提交、状态变化、错误和异步处理都给及时、可恢复的反馈。

避免：娱乐化无限信息流、过度渐变/玻璃效果、夸张动效、只显示数字不解释含义、把复杂表单一次铺满。

## 1.1 Next.js 落地约定

| 主题 | 约定 |
| --- | --- |
| 路由 | App Router；`app/(site)` 为用户端，`app/(admin)` 为运营后台，各自独立 `layout.tsx` 与中间件策略 |
| 渲染 | 公开页面默认 Server Component + SSR；私密/个人页面 `no-store`；仅交互组件加 `"use client"` |
| 数据读取 | Server Component 内调用 `src/lib/server/api.ts`（带服务令牌、超时、traceparent）；客户端不直接拿服务令牌 |
| 数据写入 | 表单优先 Server Action，保证无 JS 时可提交；需要乐观更新的（投票、关注）用客户端 `fetch` + `useOptimistic` |
| 缓存 | 公开列表用标签化缓存并在服务端事件后 `revalidateTag`；任何含会话结果的响应禁止共享缓存 |
| 会话 | HttpOnly Cookie，由 Next.js 服务端读取并转发；`middleware.ts` 只做粗粒度跳转，授权仍在 Go 侧 |
| 校验 | zod schema 放 `src/features/<域>/schema.ts`，Server Action 与客户端表单共用 |
| 类型 | 只用 `src/lib/api/schema.ts`（由 `openapi.yaml` 生成）的类型，经 `src/lib/api/types.ts` 改名后使用，不手写接口类型。重新生成：`bun run gen:api` |
| 文案 | 全部进 `messages/zh-CN.json`，组件内不硬编码；首发单语言，不做语言切换与 RTL |
| 样式 | Tailwind 4.3，`@theme` 定义三层 token；组件不写裸十六进制色值 |
| 组件 | shadcn/ui 组件进仓库后按本文件规范改造（placeholder、等宽等高、焦点态） |
| 包管理与脚本 | Bun 1.4（`bun install`、`bun run dev/build/test`）；**运行时是 Node 24**，见下方说明 |
| 测试 | 单元用 Vitest，E2E 用 Playwright |

落地时与原计划的两处偏离（2026-10-24，均已在代码中生效）：

| 项 | 原计划 | 实际 | 原因 |
| --- | --- | --- | --- |
| TypeScript | 7.0.2 | **6.0.3** | `typescript-eslint` 明确不支持 TS 7.0（加载即报错），`openapi-typescript` 的 peer 也停在 TS 5。用 TS 7 就要同时放弃 lint 与契约类型生成，代价远大于收益。`apps/` 也是 6.0.3，前台两端一致 |
| 运行时 | Bun | **Node 24 + Bun 只做包管理** | Next.js 16 的 Turbopack 是跑在 Node 上的原生模块，用 Bun 运行时跑 `next dev` 风险不必要。compose 里 `feedback` 服务用 `node:24-bookworm-slim` 镜像 + 全局安装 bun |

设计 token 的对比度不靠眼睛判断：`bun run check:contrast` 会解析 `globals.css` 的三层 token，
按 WCAG 2.2 计算 62 组配对（浅色 + 深色各 31 组），低于 4.5:1（正文）或 3:1（控件边界、状态图标）就失败。

## 2. 视觉方向

采用低风险的 **Minimalism + Swiss grid**：大面积中性背景、清楚的排版网格、有限的强调色和轻量卡片边界。紫蓝色表达共创与识别，绿色只用于明确的正向完成/主要加入动作，状态色不作为唯一信息。

### 2.1 原始色 token（已落地，见 `src/app/globals.css`）

2026-10-24 重做。两条原则：

- **品牌色压低饱和**。原来提议的 `#7c3aed` 饱和度太高，在大面积按钮和徽标上会盖住内容，
  整体观感偏「工具」。改成 `#5b5bd6` 一系，并把品牌色的出现频率降到最低，
  让层级和留白承担质感。
- **中性色不用纯灰**。纯灰在屏幕上显得平而廉价。整条 `ink` 阶梯带一点蓝（色相 ~250），
  和品牌色同源，深浅两套主题切换时不会出现「换了一套灰」的割裂感。

| Token | 值 | 用途 |
| --- | --- | --- |
| `brand.700` | `#4a4ac0` | 品牌文字、填充 hover |
| `brand.600` | `#5b5bd6` | 主品牌、焦点环（浅色） |
| `brand.400` | `#9a9af0` | 深色下的品牌填充与焦点环 |
| `brand.300` | `#b9b9f6` | 深色下的品牌文字 |
| `brand.50` | `#f4f4fe` | 淡品牌背景 |
| `ink.950` | `#0c0c12` | 深色页面背景、浅色填充上的文字 |
| `ink.900` | `#1a1a24` | 浅色正文（不用 950：纯黑在白底上发虚） |
| `ink.600` | `#55556a` | 次要正文 |
| `ink.500` | `#6e6e82` | 辅助文字（算出来的：要在白底上 ≥4.5:1） |
| `ink.200` | `#e8e8ee` | 边界、凹陷面 |
| `ink.100` | `#f4f4f7` | 抬升面 / hover 底 |
| `ink.50` | `#fbfbfd` | 浅色页面背景 |
| `grass.700` | `#15803d` | 正向文字与填充（600 配白字只有 3.3:1） |
| `grass.600` | `#16a34a` | 成功状态图标 |
| `flame.700` | `#c62a2f` | 错误/破坏操作的文字与填充 |
| `honey.700` | `#ab5d00` | 警告文字 |
| `sky.700` | `#2f4fb5` | 信息文字 |

实现时组件不得直接使用十六进制值；原始值映射到语义 token，再映射组件 token。
几个「算出来而不是挑出来」的值（`ink.500`、两套 `control-border`）在 globals.css 里有注释说明约束条件，
改动前先跑 `bun run check:contrast`。

### 2.2 三层 token

```text
Primitive              Semantic                 Component
purple.600      →      color.primary     →      button.primary.background
slate.50        →      color.canvas      →      page.background
slate.200       →      color.border      →      card.border
red.600         →      color.danger      →      alert.error.icon
space.4         →      space.control-x   →      input.padding-inline
radius.3        →      radius.surface    →      card.radius
```

语义 token 至少包括：`canvas / surface / surface-raised / surface-sunken / surface-overlay /
fg / fg-muted / fg-subtle / fg-on-fill / border / border-strong / control-border /
primary / accent / info / success / warning / danger / ring / scrim`。
深色模式只重映射语义层，组件层不写独立颜色。

**背景分四档，`surface-raised` 和 `surface-overlay` 不能共用一个 token。**
这是实现时踩出来的：浅色下「浮起来」只能是纯白 + 阴影，而 hover 底色只能往深走；
深色下反过来 —— 浮层必须比卡片更亮，hover 底也更亮。两种主题里这两个角色的走向相反。

| Token | 浅色 | 深色 | 用途 |
| --- | --- | --- | --- |
| `canvas` | `#fbfbfd` | `#0c0c12` | 页面底 |
| `surface` | `#ffffff` | `#16161d` | 卡片 |
| `surface-raised` | `#f4f4f7` | `#1e1e27` | hover 底、徽标底、内嵌信息块 |
| `surface-sunken` | `#e8e8ee` | `#08080c` | 凹陷区（骨架屏、分段控件轨道） |
| `surface-overlay` | `#ffffff` | `#22222c` | 浮层：下拉、弹窗、菜单、toast |

### 2.3 字体

中文优先使用系统字体栈，避免下载字体阻塞首屏：

```css
font-family: ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont,
  "Segoe UI", "PingFang SC", "Hiragino Sans GB", "Microsoft YaHei", sans-serif;
```

可为拉丁标题选用品牌字体，但首期不依赖不含中文字形的 Rubik/Nunito。代码、反馈编号和数据列使用系统等宽字体，数字启用 tabular figures。

### 2.4 排版、间距与形状

- 字号：12 / 14 / 16 / 18 / 24 / 32 / 40；正文移动端至少 16px；
- 行高：正文 1.6，标题 1.2–1.35；桌面正文行长 60–75 个英文字符等宽，中文主栏约 32–42 字；
- 间距：4px 基准，常用 4 / 8 / 12 / 16 / 24 / 32 / 48 / 64；
- 圆角：输入/按钮 8px，卡片 12px，标签 999px；不要每个容器都套卡片；
- 阴影：默认无或极轻，主要用背景与边界分层；悬浮层使用统一 elevation token；
- 图标：使用同一套 SVG 线性图标，功能图标不使用 Emoji。

## 3. 页面布局

### 3.1 全局壳

- 顶部栏高度约 64px：Logo/工作名、一级导航、项目切换/搜索、通知、头像；
- 移动端：顶部只保留上下文与必要动作，底部导航 5 项；
- 桌面内容最大宽度约 1200px；内容流主栏 680–760px，辅助栏 280–320px；
- 固定元素为内容预留空间，不能遮住最后一条评论或表单按钮；
- 全站提供跳到主要内容链接和可见键盘焦点。

### 3.2 首页

顺序：

1. 简洁 Hero：一句价值说明 + “浏览项目”主动作 + “了解如何参与”次动作；
2. 当前重点：最多 3 条置顶测试任务/上线动态；
3. 活跃项目：2–6 个卡片，说明状态、测试重点和加入方式；
4. 最近更新的高质量反馈：显示项目、类型、状态、标题、更新时间；
5. 本周议题：一个强上下文模块，不轮播；
6. 参与说明与底部 CTA。

Hero 不做自动轮播和视差。动态列表首屏不无限滚动，使用“查看更多”进入专门页面。

### 3.3 项目主页

- 项目头：Logo、名称、状态、简述、加入/去体验主动作；
- 项目内导航：`概览 / 动态 / 反馈 / 议题`；
- 概览优先显示“这轮请测试什么”和反馈提交入口；
- 暂停/归档项目显示持久提示，不只禁用按钮；
- 私密项目未加入时只显示允许公开的信息和加入方式。

### 3.4 反馈列表

每行/卡片显示：反馈编号、类型、状态、标题、项目、作者（按隐私）、投票、评论、更新时间、最多 2 个标签。

- 标题优先换行，不在移动端截成无法理解的半句；
- 状态 Badge 同时有文字和可选图标；
- 桌面筛选条常驻，移动端使用带已选数量的筛选抽屉；
- 筛选 chip 集合先换行，空间不足时用可操作的“+N”展开，不静默隐藏；
- 加载更多后浏览器返回需恢复位置；列表 ≥ 50 可评估虚拟化，但不能破坏搜索引擎与键盘。

### 3.5 反馈详情

桌面主栏：编号/状态 → 标题/元信息 → 正文与上下文 → 附件 → 补充 → 时间线 → 评论。辅助栏：投票/关注、负责人、版本、标签、可见性说明。

移动端辅助信息移到正文后或底部 Sheet，投票/关注保持可见但不遮内容。

状态时间线：

- 默认展示公开状态变化和官方说明；
- 内部备注永不下发到用户端；
- 合并、撤回、隐藏有专门解释与去向；
- `planned` 旁明确“已进入计划，不代表固定发布日期”。

### 3.6 提交反馈

采用单页渐进表单而非强制多步骤：

1. 项目和类型（从项目进入时预选）；
2. 标题和说明；
3. 根据类型展开复现/环境字段；
4. 图片上传；
5. 相似项建议；
6. 可见性、隐私提醒和提交。

规则：

- 每个输入有可见 Label 和中文 placeholder，placeholder 不能替代 Label；
- 输入、下拉框、日期控件在同一表单中等宽等高；
- 错误在失焦或提交后显示，字段下说明原因和修复方式；
- 多字段错误提供顶部可聚焦摘要，并链接到字段；
- 本地自动保存非敏感草稿，显示最后保存时间；
- 离开未保存私密草稿前确认；
- 上传区支持按钮选择与拖放，拖放必须有键盘替代；
- 成功后进入详情页，明确编号、当前状态与预计下一步。

示例 placeholder：

- 标题：“请用一句话描述问题，例如：提交图片后一直显示处理中”；
- 复现步骤：“请按发生顺序填写，每一步单独一行”；
- 页面路径：“请输入发生问题的页面地址，不要粘贴包含登录凭据的链接”；
- 期望结果：“请说明你原本希望发生什么”。

## 4. 核心组件规范

| 组件 | 必备状态 | 特别要求 |
| --- | --- | --- |
| `Button` | default/hover/pressed/focus/loading/disabled | 每屏一个主 CTA；加载时保留宽度并禁重复提交 |
| `Input/Textarea` | default/focus/error/disabled/read-only | Label、描述、错误与 `aria-describedby` 关联 |
| `Select/Combobox` | closed/open/searching/empty/error | 完整键盘操作；表单中与输入等宽等高 |
| `Badge` | status/type/role/tag | 不只靠颜色；关键文本不省略 |
| `FeedbackCard` | normal/hover/focus/unread/private/hidden | 整卡可点击时内部动作不能形成嵌套交互冲突 |
| `Timeline` | current/completed/terminal | 屏幕阅读器按有序列表读出完整状态句 |
| `AttachmentGrid` | uploading/scanning/clean/rejected/error | 预留比例避免 CLS；失败可单张重试/删除 |
| `VoteButton` | off/on/loading/disabled | `aria-pressed`；即时反馈，失败回滚并说明 |
| `FollowButton` | off/on/loading | 文案明确“关注/已关注”，不能只用铃铛颜色 |
| `CommentComposer` | idle/draft/submitting/error | 字数、权限、草稿与附件状态清晰 |
| `FilterBar` | default/active/mobile-sheet | URL 驱动；清空动作可见；焦点返回触发按钮 |
| `Dialog/Sheet` | opening/open/closing | 焦点锁定、Esc、关闭按钮、未保存确认 |
| `Toast` | success/info/error | 不抢焦点；3–5 秒普通提示；关键错误持久展示 |
| `EmptyState` | first-use/filtered/permission | 解释原因并只给一个主要下一步 |

组件必须使用语义 token，不在页面复制私有样式。第三次真实重复出现后再抽象业务组件。

## 5. 状态与反馈文案

- 状态名使用用户能理解的中文，内部枚举不直接显示；
- 错误格式为“发生了什么 + 可能原因 + 下一步”，避免“操作失败”；
- 成功提示说明结果，如“反馈 APP-1024 已提交，我们会在完成分诊后通知你”；
- 权限错误不责备用户，给出登录、加入项目或联系项目方路径；
- 空状态不伪造社交证明，不使用虚构数量或评价；
- 时间同时提供相对时间和可访问的绝对时间；
- 所有用户端文案放入 locale 文件，首发暂定 `zh-CN`，组件内不散落硬编码。

## 6. 响应式规则

- Mobile-first；主要检查 320、375、640、768、1024、1280、1440px；
- 320px 不出现整页横向滚动；数据表确需横向滚动时显示边缘提示与固定关键列；
- 主操作触控目标至少 44×44px，目标间距至少 8px；
- 正文在 200% 缩放下不丢信息和操作；
- 移动底栏不超过 5 项且图标配文字；
- hover 只做增强，核心功能用点击/键盘完成；
- 横屏和软键盘弹出时提交按钮不遮输入。

## 7. 无障碍基线

目标：WCAG 2.2 AA。

- 普通文本对比度 ≥ 4.5:1，大文本与非文字控件边界 ≥ 3:1；
- 可见焦点环，焦点不被粘性头部/底栏遮挡；
- 完整键盘路径：导航、筛选、投票、关注、上传、评论、弹窗；
- 页面标题与 H1 唯一，标题层级连续；
- 图像有语义 alt；截图描述允许作者补充，装饰图 `alt=""`；
- 动态计数以完整句子在适当 live region 更新，不移动焦点；
- 状态变化不是只靠颜色；
- 登录允许密码管理器、粘贴和自动填充，不设置认知测试障碍；
- 自动运动可暂停，`prefers-reduced-motion` 下直接显示最终状态；
- 表单失败后聚焦错误摘要/首个错误，成功后焦点进入确认标题。

## 8. 动效

动效强度低：

- hover/press 150–200ms，仅 opacity/transform，位移不超过 2px；
- 弹窗/Sheet 用简短进出场，退出短于进入；
- 状态更新时间线用颜色、图标和文本变化，避免庆祝动画干扰；
- 不动画 width/height/top/left，不用滚动视差；
- 动画可中断，快速重复操作最终状态由数据决定，不依赖 `animationend`。

## 9. 深浅主题

已定（2026-10-24）：支持系统主题并允许用户切换（跟随系统 / 浅色 / 深色三选）。

实现要点：主题由 `src/components/theme/theme-script.tsx` 的同步内联脚本在首帧之前确定，
不会闪色；切换器用 `useSyncExternalStore` 读 `localStorage`，不在 effect 里 setState。
深色不是反色——背景分四档（第 2.2 节）、边框与状态色单独给值，
并由 `bun run check:contrast` 分别验证（当前 80 组配对全达标）。

内联脚本写两个东西，各有用途，不能合并：

- `.dark` 类 → **最终外观**，CSS 变量按它切换；
- `html[data-theme]` → **用户选的模式**（`system` / `light` / `dark`）。

为什么要第二个：跟随系统且系统是深色时，最终外观和「选了深色」完全一样，单靠 `.dark` 分不出来。
切换器要高亮「跟随系统」还是「深色」就得知道这个区别，而它必须在首帧就可用，
否则高亮位置会在 hydration 之后跳一下。分段控件的选中态因此是纯 CSS 驱动的
（规则在 `globals.css`，选择器形如 `html[data-theme='dark'] .theme-seg[data-mode='dark']`）。

切换器用**三段分段控件**而不是下拉菜单：下拉的触发器只能显示一个图标，
选「跟随系统」时它显示一个显示器图标，用户看不出这跟明暗有关系（这是实际反馈）。
三段并排把所有选项和当前状态一次摊开，不点开就知道这里能切主题。

### 9.1 内联脚本不够，还需要 ThemeSync

内联脚本只在**新文档**加载时跑一次。客户端路由导航不是新文档，而 React 在导航时会
重新 reconcile root layout 的 `<html>`，把脚本运行时加上的 `.dark` 与 `data-theme`
一起抹掉（服务端渲染的 `<html>` 上本来就没有这两个标记）。表现是**跳转之后页面突然变回浅色**。

这个 bug 是开启 Cache Components（`adr/0010`）之后才暴露的：以前未登录访问 `/settings`
是 HTTP 307，浏览器层面跳转 = 新文档，脚本会重跑；现在那些路由是部分预渲染，
redirect 走在流式内容里 = 客户端导航，脚本不再执行。

所以 `src/components/theme/theme-sync.tsx` 监听 `usePathname()`，导航后比对
`<html>` 上的标记与 localStorage，不一致就补回去。它挂在 **root layout** 而不是
`SiteShell` 里 —— `/offline` 不在 `(site)` 分组下，挂在壳里会漏掉它。

共享逻辑在 `src/components/theme/apply.ts`（`readTheme` / `applyTheme` / `syncThemeIfLost`），
内联脚本、切换器、同步组件三处用同一套规则，避免各写一遍后不一致。

回归检查点：**设为深色后，未登录访问 `/settings`，落到 `/login` 时必须仍是深色**。
这条光看 HTTP 状态码测不出来（现在是 200 + 流式 redirect），必须用真实浏览器看最终落点。

- 主题在首屏渲染前确定，避免闪烁；
- 深色不是简单反色，背景层级、边界、状态色分别验证对比；
- 用户生成截图保持原始呈现，周围使用中性边框；
- 主题偏好本地保存；登录后是否跨设备同步为 P1。

## 10. 前端数据与错误策略

- 路由参数和筛选是页面状态权威源；
- 服务端首屏提供权限裁剪数据，客户端水合不重复泄露请求；
- 投票/关注可乐观更新，失败必须回滚并保留可重试动作；
- 创建、状态改变、评论使用幂等请求；
- 401 保存安全草稿并登录回跳，403 显示权限路径，409 显示最新资源并允许比较，429 显示重试时间；
- 网络中断不清空已加载内容，敏感操作不离线伪成功；
- 用户端错误不展示后端堆栈，只展示请求编号。

## 11. 性能预算

| 指标 | 预算 | 当前实测 |
| --- | --- | --- |
| 公开首页 LCP（P75，移动） | ≤ 2.5s | 待线上采集 |
| INP（P75） | ≤ 200ms | 待线上采集 |
| CLS（P75） | ≤ 0.1，项目目标 ≤ 0.05 | 待线上采集 |
| 首页 TTFB（本机 `next start`） | — | **2–6ms**，命中预渲染（见 11.5） |
| 首屏关键路径 JS（br 后） | ≤ 180 KB | 首页 137.6 KB，最重路由 `/feedback` 169.2 KB |
| ↳ 其中框架地板 | 97.5 KB（不可控） | react-dom 60.0 + Next 客户端运行时 37.5 |
| ↳ 其中应用代码 | ≤ 80 KB | 首页 40.1 KB，最重 71.7 KB |
| 首屏图片 | 明确尺寸、响应式 AVIF/WebP；非首屏懒加载 | 当前无首屏图片 |
| 输入反馈 | 视觉响应 ≤ 100ms | — |

Source map 全部关闭（`next.config.ts`）：浏览器端本来就没有（默认 false，显式写出来防止被误开，
一旦开启完整源码会随产物发给浏览器）；服务端那份实测 147 个文件 / 32.2 MB，关掉后为 0。
代价是线上服务端报错的堆栈不再映射回 TS 源码 —— 真需要时临时打开重新构建一次即可。

验证命令：`bun run build` 后启动服务，再跑 `BASE=http://localhost:3000 bun run check:budget`。
超预算时脚本按非 0 退出。

### 11.1 首屏 JS 预算为什么从 120 KiB 调到 180 KB

原定 120 KiB 是在选定实现栈之前写的，Next.js 16 + React 19 的基线建立后发现它不可达：
react-dom 60.0 KB + Next.js 客户端运行时 37.5 KB = **97.5 KB（br）是框架地板**，
一行业务代码都不写也得付。120 KiB 的预算只剩 22.5 KB 给应用代码，
而一个带导航、主题切换、账号菜单、筛选表单的站点做不到。

所以预算拆成两项：总量用来防线上回归，**应用代码那一项才是真正能控制的**。
提预算之前先确认不是引入了不必要的客户端依赖。

量的是「关键路径」—— HTML 里会阻塞 hydration 的 script。刻意排除两类：

- 带 `nomodule` 的 polyfill chunk（34.3 KB）：支持 ES module 的浏览器不会下载它，
  算进来会凭空多出 34 KB；
- hydration 之后才按需取的 chunk：它们不阻塞可交互。

### 11.2 已做的瘦身

| 改动 | 效果（br，首页关键路径） |
| --- | --- |
| 账号菜单（Radix DropdownMenu + floating-ui）懒加载 | 163.8 → 137.4 KB（−26.4 KB）；未登录访客完全不下载 |
| Toast（sonner）懒加载 | 170.7 → 164.1 KB（−6.6 KB），总下载量 +1.7 KB |
| 外观切换器从下拉菜单改为分段控件 | 164.1 → 163.8 KB（−0.3 KB，主要目的是可发现性，不是体积） |

`/feedback` 只降到 169.0 KB，因为它用 Radix Select 与 Dialog，floating-ui 仍在关键路径上。
下一步可做：移动端筛选抽屉（Dialog）懒加载，预计再省 5–8 KB —— 桌面端根本不用它。

大型编辑器、图表、附件预览按路由懒加载。字体优先系统字体，不为装饰预加载多种字重。

### 11.3 压缩：Brotli 优先，Gzip 回退

静态产物在构建后预压缩，边缘层直接送现成文件，不在请求时压：

```bash
bun run build          # = next build && node scripts/precompress.mjs
```

实测（`.next/static` + `public`，26 个文件）：

| | 大小 | 相对原始 |
| --- | --- | --- |
| 原始 | 1045.8 KB | — |
| Brotli（`.br`） | 281.2 KB | 省 73.1% |
| Gzip（`.gz`） | 322.3 KB | 省 69.2% |

Brotli 比 Gzip 再小 41.1 KB（约 13%），所以边缘按 `Accept-Encoding` 优先给 `br`，
只有不支持的客户端才回退 `gzip`。nginx 配置见 `deploy/edge/feedback.conf`。

几条容易踩的：

- 预压缩用 Brotli quality 11 + 窗口 24。运行时压缩做不到这个档位（CPU 划不来），
  但静态产物带内容哈希、永不变更，这个代价只付一次。
- 预压缩**只覆盖静态产物**。SSR 出来的 HTML 每次都不一样，没法预压缩，
  由边缘实时压（`brotli_comp_level 5`），`next.config.ts` 的 `compress: true`
  是没有边缘层时的兜底，不能关。
- nginx 反代必须 `proxy_set_header Accept-Encoding "";`。不清掉的话
  Next.js 会先按浏览器的 Accept-Encoding 自己 gzip，nginx 看到
  `Content-Encoding` 就不再处理，**HTML 永远拿不到 Brotli**。
- 预压缩跳过 `.map`（只给排错用）、图片与字体（本身已是压缩格式），
  以及小于 1 KB 的文件（压完常常更大）。
- 送预压缩文件的 location 必须加 `Vary: Accept-Encoding`，
  否则 CDN 会把 br 版本串给不支持的客户端。

### 11.4 Service Worker：缓存静态产物 + 断网兜底

`public/sw.js`，手写约 90 行，没有引入 `next-pwa` / `serwist` 之类的生成器 ——
策略只有两条规则，自己写比配一个黑盒更可控。

| 请求 | 策略 |
| --- | --- |
| `/_next/static/*` | cache-first。带内容哈希、永不变更、不含用户数据 |
| 导航请求 | **network-only**，失败时回退预缓存的 `/offline` |
| 其余（`/api`、Server Action、RSC 载荷） | 不拦截 |

**绝不缓存 HTML。这不是保守，是正确性要求。** 这个站的页面内容按访问者过滤
（顶栏账号、我的反馈、项目可见范围），而 Cache Storage 是按源共享、不区分登录状态的。
缓存导航响应就意味着同一台设备换账号后可能看到上一个账号的页面。

`/offline` 刻意不放在 `(site)` 分组下：那个布局的顶栏要读会话，一来断网时取不到，
二来预缓存一个带账号信息的 HTML 正好犯上面那条。它只用 root layout，整页零请求、零用户数据。
页面里的「重新连接」用原生 `<a>` 而不是 `next/link`：客户端路由在断网时取不到 RSC 载荷，
点了只会卡住，必须触发一次真实的整页加载。

**没有 web app manifest，这是刻意的。** 浏览器判定「可安装」的前提之一就是存在 manifest；
没有它，地址栏不会出现安装图标、也不会弹安装横幅。PWA 的性能收益全部来自
Service Worker，和 manifest 无关 —— 所以「要 PWA 的快，不要 PWA 的安装提示」
这两件事是可以同时满足的。

两个容易踩的点：

- `/sw.js` 必须 `Cache-Control: no-cache`（已配在 `next.config.ts`）。浏览器对 SW 脚本
  有特殊的更新检查，但中间的 CDN 不一定懂 —— 一旦 `/sw.js` 被边缘缓存住，
  用户会一直拿到旧 SW，而它又控制着静态资源缓存，等于站点卡在旧版本上且极难排查。
- 只在生产注册。开发模式下 SW 会缓存住静态产物、和 Turbopack 热更新打架，
  表现为「改了代码页面不变」。

验证（`/tmp/pptr/pwa.mjs` 那类脚本）：**离线回退必须真的把服务器停掉来测**。
puppeteer 的 `setOfflineMode` 走 CDP 网络模拟，它不拦 Service Worker 自己那条网络栈，
SW 里的 `fetch` 照样能通，测出来是假的通过。

### 11.5 首页静态化（Cache Components / PPR）

首页是 `◐ Partial Prerender`：静态 HTML 外壳在构建时生成，
只有依赖身份的几小块在运行时流式填充。实测 TTFB 2–6ms。

**为什么必须开 `cacheComponents`。** 顶栏在 layout 里读会话，传统 App Router 下
layout 一碰 `cookies()` 整条路由就是动态的，页面自己怎么写都没用 ——
这是首页做不成静态页的根因。PPR 才允许「静态外壳 + 动态洞」。

改动分三层：

1. **数据层** `lib/api/public-queries.ts`：公开视角的读接口，带 `'use cache'`。
   它调 `callPublicService`，和 `queries.ts` 的区别只有一个但很关键 —— **不带访问者身份**。
   两个限制都是必须的：
   - 不读 Cookie：`use cache` 内部禁止访问请求上下文；更要紧的是缓存按源共享，
     带上身份等于把某个人的可见范围发给所有人。
   - 不转发 `user-agent` / `x-forwarded-for` / `x-request-id`：这些头会进 fetch 的
     Data Cache key，带上 UA 的话每种浏览器版本各占一份缓存，命中率基本为零。
2. **布局层** `SiteHeader` 不再是 async，读 Cookie 的部分收进 `AccountSlot` + `Suspense`。
   占位给了 `min-w-16 sm:min-w-32`：占位 / 未登录（登录+注册）/ 已登录（头像+昵称）
   三种状态宽度不一致的话，流式替换那一刻左边的「提交反馈」按钮会被推一下，就是一次实打实的 CLS。
3. **页面层** 首页的 Hero 次要按钮与底部 CTA 各自收进 `Suspense`；
   时间基准用 `cachedNow()`（同样 `use cache`，档位和数据一致）。
   配成同一档不是巧合：相对时间是用基准减数据里的时间戳算的，两者取自同一个缓存时刻才自洽。

**换来的变化（需要知道）：首页列表只展示公开内容。** 「仅成员可见」的项目与反馈不在首页出现，
要看它们去 `/projects`、`/me` —— 那两页仍按访问者身份实时渲染。对一个对外的橱窗页
这是正确的，而且更安全：静态内容不可能泄露私密数据。

**其余路由显式标了 `export const instant = false`**（13 个）。`cacheComponents` 把全局默认
翻了过来 —— 数据获取默认动态、想静态要显式 `use cache`，所以那些本来就该按访问者实时渲染的页面
（列表、详情、我的、设置…）要声明「这页就是阻塞渲染，别试图预渲染」。这不是退让，
是把原本隐式的行为写明。顺带收益：`/guide`、`/privacy`、`/terms`、`/recover`
不依赖任何请求上下文，自动变成了部分预渲染。

**构建期需要能回源。** 首页的数据在构建时就要取一次，所以 CI 需要 `BFF_SERVICE_TOKEN`
且后端可达。取不到时不会让构建失败（`public-queries.ts` 的 `orEmpty` 兜住并打日志），
但那次构建产出的首页是空状态，要等运行时缓存过期才补上数据。

两个已知取舍：

- 账号槽的流式内容靠 React 注入 `<template>` 实现，**禁用 JS 时顶栏不会出现登录入口**。
  这个站本身依赖 JS（表单是 Server Action + 客户端组件），且底部导航与页面内 CTA 都有登录入口，
  所以接受。
- 首页整页响应带 `Cache-Control: private, no-cache`（因为含 postponed 部分），
  边缘不缓存整页。静态外壳的收益来自不等后端，不是来自边缘缓存。

## 12. 页面验收清单

每个用户页面交付前检查：

- 正常、加载、空、错误、无权限、会话过期、离线状态；
- 320/375/768/1024/1440px 和 200% 缩放；
- 仅键盘完整走通，焦点顺序与返回正确；
- 屏幕阅读器读出页面名、状态、错误和动态结果；
- 浅色/深色（若启用）对比度；
- 减少动态模式；
- 慢网、超时、重复提交和 409 冲突；
- 私密内容不进入 HTML 元数据、缓存、埋点或错误日志；
- Lighthouse/axe 自动检查无严重问题，关键路径人工验证。

## 13. 已定与待确认

已定（2026-10-23）：

- 实现栈：Next.js 16.4 App Router + React 19.3 + Bun 1.4 + Tailwind 4.3 + Radix UI（shadcn 那一套组件结构）；
- TypeScript **6.0.3**，不是本文早前写的 7.0.2 —— `typescript-eslint` 加载 TS 7 即抛错，
  `openapi-typescript` 的 peer 也还停在 TS 5，详见 `adr/0006`；
- 语言：只做简体中文，文案集中在 `messages/zh-CN.json`，不做语言切换与 RTL；
- 贡献榜：登录可见，匿名访客不可见（`00` 第 8.2 节）；
- 讨论动态：成员不能发布，只有项目团队能发动态；
- 注册入口：必须邀请码，登录页与注册页需说明这一点。

已定（2026-10-24）：

- 产品名：**短剧66**；
- **深色模式首期就做**。不是浅色的反色：两套主题各自给值、各自验证对比度
  （`bun run check:contrast`，当前 80 组配对全达标）。用户的选择（跟随系统 / 浅色 / 深色）
  由 `ThemeScript` 在首帧之前写进 `html[data-theme]`，所以不会闪色、切换器高亮也不会跳；
- 品牌色改为低饱和紫蓝（`#5b5bd6` 系），中性阶梯带冷色温（色相 ~250）而不用纯灰。
  原来的 `#7c3aed` 饱和度在大面积按钮和徽标上会盖住内容；
- 下拉与弹窗统一用 Radix（`components/ui/select.tsx`、`sheet.tsx`），
  不再用原生 `<select>` —— 它的箭头只能靠背景图硬编码颜色，主题 token 一改就对不上，
  选项列表也跟不了主题。

已定（2026-10-25，详见 `adr/0010`）：

- **开启 Cache Components（PPR）**，首页做成 `◐ Partial Prerender`，TTFB 2–6ms。
  代价是首页列表只展示公开内容，其余 13 个依赖请求上下文的路由显式标 `instant = false`（第 11.5 节）；
- **Service Worker 只缓存静态产物 + 断网回退 `/offline`，绝不缓存 HTML**（第 11.4 节）；
- **不提供 web app manifest**：没有它浏览器就不认为站点可安装，不会出现安装图标与安装横幅。
  PWA 的性能收益来自 Service Worker，与 manifest 无关；
- **Source map 全部关闭**，浏览器端与服务端都不生成（第 11 节）。

待确认：

- Logo 与品牌语气是否需要专门的视觉稿；
- 是否需要高保真视觉稿作为实现前置验收；
已定（2026-10-25，详见 `adr/0011`）：

- **恢复码与「忘记密码」已删除**。上一版这里列的 A/B/C 三个方案，用户选了 A
  （照原话删，不要替代方案），并明确确认代价：忘记密码即永久失去账号。
  `/recover` 路由、`RecoveryCodePanel`、注册页的知情确认勾选框、登录页的「忘记密码」入口
  全部移除；后端删了 `POST /api/v1/feedback/auth/recover`（契约 0.41.0）。
  唯一的改密入口是设置页，必须提供当前密码。
- **注册表单字段顺序：用户名 → 密码 → 昵称 → 邀请码**。邀请码从第一位挪到最后：
  它通常是复制粘贴来的，带邀请链接进来时还会预填，放开头会让人以为要再填一次。
- 注册页保留一条**静态告知**（不是勾选框）：说明这个社区没有找回密码的功能。
  用户要删的是那道必须勾选的确认，不是「密码丢了找不回」这个事实 ——
  后果不可逆的事情不能藏起来。
- **骨架屏**：见下文第 14 节。

## 14. 骨架屏

组件在 `src/components/ui/skeleton.tsx`，按路由接在 `loading.tsx` 上。

| 组件 | 对应的真实内容 |
| --- | --- |
| `SkeletonRegion` | 一组骨架的外壳，挂 `role="status"` + `aria-busy` |
| `SkeletonText` | 段落（末行默认 60% 宽，照抄真实段落的观感） |
| `SkeletonPageHeader` | 列表页的「大标题 + 一行说明」，可选右侧主操作 |
| `SkeletonFilters` | 搜索框 + 若干下拉（下拉 `hidden sm:flex`，小屏不出现） |
| `SkeletonList` | `FeedbackCard` 那种一行一条的紧凑列表 |
| `SkeletonCardGrid` | 项目卡 / 动态卡的多列网格 |
| `SkeletonArticle` | 反馈详情、议题详情的正文区 |

已接入：`/feedback`、`/projects`、`/topics`、`/me`、`/feedback/[slug]/[number]`、
`/topics/[id]`、`/projects/[slug]`、`/updates/[id]`。

### 14.1 三条必须守的规则

**骨架尺寸要和最终内容一致。** 骨架比内容矮，内容到位时页面往下跳一截，
那是实打实的 CLS，比不放骨架更糟。所以每个组件的高度都是按对应真实组件量出来的，
`loading.tsx` 的容器 `gap` / `py` 也必须和 `page.tsx` 的 `Container` 逐字一致。
改真实组件时要一起改骨架。

**`aria-hidden` 挂在色块上，`role="status"` 挂在容器上。** 反过来做的话，
屏幕阅读器会把十几个空 div 逐个念出来。所以 `Skeleton` 一律 `aria-hidden`，
由 `SkeletonRegion` 统一报告一次「XX 正在加载」。

**行数按首屏可见量给，不要铺满一页。** `/feedback` 一页 20 条，但骨架只给 5 行 ——
铺 20 个灰块会让人以为内容已经加载完了。

### 14.2 顺带拿到的性能收益

`loading.tsx` 会自动包一层 Suspense，这让原本完全动态（`ƒ`）的几个路由变成了
**部分预渲染（`◐`）**：

| 路由 | 加骨架前 | 加骨架后 |
| --- | --- | --- |
| `/feedback` | `ƒ` 动态 | `◐` 静态外壳（含骨架）+ 流式内容 |
| `/projects` | `ƒ` | `◐` |
| `/topics` | `ƒ` | `◐` |
| `/me` | `ƒ` | `◐` |
| `/feedback/new` | `ƒ` | `◐` |

也就是说服务端返回的**第一个字节就带骨架**，不用等后端查询。
实测 `/feedback` 的静态 HTML 里有 114 个骨架色块与 `role="status"` 标签。
骨架屏在这里不只是视觉补偿，它顺带把这些页面的首字节时间拉到了和首页同一档。

### 14.3 怎么验证骨架

**不要用网络限速。** RSC 的流式内容和页面在同一个响应里，限速会把两者一起拖慢，
骨架该消失的时候照样消失，只是整体变慢 —— 截图时机根本卡不准，实测四个路由里
只能偶然抓到一个。

**用禁用 JS。** 关掉 JS 后，服务端返回的静态外壳（就是骨架）无法被流式内容替换，
它会原样留在画面上，而这恰好就是用户在真实慢网下看到的第一屏。
断言骨架区域的色块数与高度即可（参考脚本做法：`[role="status"][aria-busy="true"]`）。
