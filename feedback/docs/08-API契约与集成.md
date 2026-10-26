# 08 · API 契约与集成

> 状态：语义草案，OpenAPI 尚未创建  
> 最后核对：2026-10-23  
> 权威范围：HTTP 语义、资源模型、错误、分页、缓存、幂等与端点索引  
> 机器权威源：实现阶段创建的 `feedback/api/openapi.yaml`

## 1. 契约优先流程

1. 在本文件确认资源语义、权限和错误场景；
2. 在 `api/openapi.yaml` 定义路径、参数、请求/响应 schema、错误与安全方案；
3. 运行 OpenAPI 校验并生成 Web/后台类型；
4. 后端按契约实现并做响应 schema 测试；
5. 前端只通过生成类型/客户端调用；
6. CI 检查生成物与契约是否同步；
7. 兼容性变化先扩展字段，迁移客户端，再删除旧字段。

Markdown 不重复完整 schema，只解释业务语义和索引。OpenAPI 尚未创建前，不得宣称接口契约完成。

## 2. 路径与版本提议

| 命名空间 | 用途 | 版本策略 |
| --- | --- | --- |
| `/api/v1/feedback/*` | 用户 Web/未来客户端 | 不兼容变更开 `/api/v2` 并行 |
| `/api/admin/feedback/*` | 运营后台 | 后台与服务同步发布，首期不带版本 |
| `/open-api/feedback` | 站外网页提交反馈（`adr/0009`） | 新增字段向后兼容；删字段要先通知接入方 |
| `/api/v1/health`、`/internal/ready` | 进程存活 / 接流量就绪 | 由 `service/` 统一提供 |

前缀已采纳（`adr/0006`、`adr/0008`、`04` 第 9 节）：前台 `/api/v1/feedback/*`、后台 `/api/admin/feedback/*`，不提供 `/api/integrations/*`。

**注意多出来的 `feedback` 这一段**：反馈社区与短剧平台共用一个后端（`adr/0008`），`/api/v1/topics` 这类路径已经被短剧业务占用，所以社区的全部端点都收在 `/feedback` 子路径下。本文件下面的端点表已按实际路径书写；设计阶段写过的 `/api/v1/topics`、`/api/admin/feedback`（不带子路径）等形式已作废。

路径使用复数名词和连字符；动作只在无法用资源状态表达时使用子路径，例如 `/feedback/issues/:id/triage`。Next.js 自身的 `app/api/*` Route Handler 只用于会话与转发，不是业务契约的一部分，不写入 OpenAPI。

**权威契约是 `service/api/openapi.yaml`**（0.24.0 起）。本文件给语义与索引；两边不一致时以 OpenAPI 为准，并且该文件与 Gin 实际注册的路由保持双向零差异。

路径使用复数名词和连字符；动作只在无法用资源状态表达时使用子路径，例如 `/feedback/:id/merge`。Next.js 自身的 `app/api/*` Route Handler 只用于会话与转发，不是业务契约的一部分，不写入 OpenAPI。

## 3. 通用格式

### 3.1 成功响应

单资源：

```json
{
  "code": "OK",
  "message": "ok",
  "data": {
    "id": "019a...",
    "version": 3
  }
}
```

列表：

```json
{
  "code": "OK",
  "message": "ok",
  "data": {
    "list": [],
    "next_cursor": null,
    "total": 0
  }
}
```

`total` 对高成本查询可省略，但必须在 schema 明确，而不是随机缺失。创建返回 `201` 和资源；成功删除使用 `204` 时无响应包。

### 3.2 错误响应

```json
{
  "code": "VALIDATION_FAILED",
  "message": "请检查填写内容",
  "request_id": "019a...",
  "details": {
    "fields": [
      {
        "path": "steps_to_reproduce",
        "code": "REQUIRED",
        "message": "Bug 反馈需要填写复现步骤"
      }
    ]
  }
}
```

生产错误不返回堆栈、SQL、内部路径、内部备注、策略表达式或依赖原始错误。`message` 可面向用户，客户端主要按稳定 `code` 处理。

### 3.3 命名与基础类型

- JSON 字段使用 `snake_case`；
- 时间为 ISO 8601 UTC，如 `2026-10-23T08:30:00Z`；
- 业务主键为 UUID 字符串，反馈展示编号为 `APP-1024`；
- 金额类若未来出现使用最小单位整数 + 币种，MVP 无支付；
- URL 只返回 HTTPS 或站内相对路径；
- 可空与缺省语义必须在 schema 区分；
- 枚举新增值视为兼容扩展，客户端必须有未知值兜底。

## 4. 认证、会话与 CSRF

浏览器使用 `Secure; HttpOnly; SameSite=Lax` 的宿主限定会话 Cookie，不把长效 token 放 `localStorage`。Next.js 服务端持有服务令牌并在转发时附加，客户端 bundle 中不出现任何令牌。

- 登录成功轮换会话 ID；
- 写请求验证 `Origin`，并使用同步 token 或同等 CSRF 防护；
- 登录、注册、邀请核销、恢复、上传和搜索都有独立限流；
- 后台高敏操作要求 `X-Reauth-Token` 或短期重新认证会话；
- 401 表示需认证，403 表示已认证但无动作权限；为防资源枚举，私密资源无权读取可统一 404；
- 响应不得返回会话 token；会话列表只给设备类别、粗略地点、时间和是否当前设备。

OpenAPI 至少定义 `cookieAuth`、`csrfHeader` 和后台重新认证安全方案。

## 5. 幂等、并发与条件请求

### 5.1 幂等

以下 POST 接受 `Idempotency-Key`：创建反馈、评论、议题回答、邀请核销、批量任务。键长度 16–128，仅允许可打印安全字符，按用户 + 路由隔离，有效期建议 24 小时。

相同键和相同规范化请求返回首次结果；相同键不同请求返回 `409 IDEMPOTENCY_MISMATCH`。

投票、关注使用 `PUT` 设置目标状态：

```json
{ "active": true }
```

### 5.2 乐观锁

可编辑资源返回 `version` 与可选 ETag。更新请求必须带 `If-Match` 或请求体 `version`（最终只保留一种）；不匹配返回：

```json
{
  "code": "VERSION_CONFLICT",
  "message": "内容已被其他人更新，请刷新后比较再保存",
  "request_id": "...",
  "details": { "current_version": 4 }
}
```

客户端不能自动覆盖，后台批量任务逐项记录冲突。

## 6. 分页、筛选与排序

用户持续列表使用游标：

- 请求：`?limit=20&cursor=...`，`limit` 默认 20，最大 100；
- 响应：`list / next_cursor / total`；
- 游标不透明、有完整性保护、绑定排序与筛选，修改条件后不能复用；
- 稳定排序总以 `id` 作为最后一列。

后台需要跳页的报表可使用 `page / page_size`，必须在端点文档显式声明。

反馈通用筛选：`project_id`、`type`、`status`（可多值）、`tag_id`、`author_id`、`assignee_id`、`visibility`、`updated_after`。排序白名单：`updated / newest / votes`；后台增加 `oldest_unanswered / priority`。

未知筛选、排序或超范围页大小返回 422，不静默忽略。

## 7. 缓存与隐私

| 响应 | 建议缓存 |
| --- | --- |
| 公开项目/动态/反馈列表 | `public, max-age=0, s-maxage=60, stale-while-revalidate=300`，按实际 CDN 调整 |
| 公开详情 | 短共享缓存 + ETag；更新/隐藏后主动失效 |
| `me`、通知、会话 | `private, no-store` |
| 成员/私密反馈及附件授权 | `private, no-store` |
| 后台全部接口 | `private, no-store` |
| 静态派生图 | 内容哈希 URL，长期 immutable；访问权限必须与可见性匹配 |

不能通过 `Vary: Cookie` 把所有会话页面误存共享缓存。私密页面 HTML、错误响应和 Open Graph 元数据同样 `no-store`。

## 8. 用户端端点索引

以下为首版候选，权限和 schema 在 OpenAPI 中逐项写明。

### 8.1 身份与账号

| 方法 | 路径 | 说明 | 访问 |
| --- | --- | --- | --- |
| POST | `/api/v1/auth/register` | 用用户名 + 密码 + 邀请码注册，成功即登录；**不返回任何找回凭据**（`adr/0011`） | 公开、限流 |
| POST | `/api/v1/auth/login` | 登录并轮换会话 | 公开、限流 |

| POST | `/api/v1/me/password` | 用当前密码改密 | 登录 + CSRF |

| POST | `/api/v1/auth/logout` | 注销当前会话 | 登录 + CSRF |
| GET | `/api/v1/me` | 当前身份、项目摘要、权限能力 | 登录 |
| PATCH | `/api/v1/me/profile` | 修改公开资料 | 登录 + 乐观锁 |
| GET | `/api/v1/me/sessions` | 会话设备列表 | 登录、no-store |
| DELETE | `/api/v1/me/sessions/:id` | 撤销某会话 | 登录 + CSRF |
| POST | `/api/v1/me/export` | 创建数据导出任务 | 登录 + 重认证 |
| POST | `/api/v1/me/deletion-request` | 申请注销/删除 | 登录 + 重认证 |

**没有 `/auth/recover`，也没有 `/me/recovery-code`**（`adr/0011`，2026-10-25 删除）。
唯一的改密入口是 `/me/password`，必须提供当前密码。register 与 login 的失败响应
必须同形同耗时，不泄露用户名是否存在。没有邮箱相关端点。

实际落地的路径都带 `feedback` 段（`/api/v1/feedback/auth/register` 等），
权威定义见 `service/api/openapi.yaml`；本表是早期设计稿，路径前缀未同步。

### 8.2 项目与成员

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/api/v1/projects` | 可见项目列表 |
| GET | `/api/v1/projects/:slug` | 项目详情与当前用户关系 |
| POST | `/api/v1/projects/:id/join` | 申请/加入开放项目 |
| DELETE | `/api/v1/projects/:id/membership` | 退出项目（所有者受限） |
| POST | `/api/v1/invitations/redeem` | 核销邀请并加入项目 |

### 8.3 动态与版本

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/api/v1/posts` | 按项目/类型获取可见动态 |
| GET | `/api/v1/posts/:id` | 动态详情 |
| GET | `/api/v1/projects/:id/releases` | 公开发布版本 |
| GET | `/api/v1/projects/:id/releases/:release_id` | 版本与上线反馈 |

### 8.4 反馈

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/api/v1/feedback` | 权限过滤后的列表/搜索 |
| POST | `/api/v1/feedback` | 创建反馈，要求幂等键 |
| GET | `/api/v1/feedback/:key` | 详情、能力和公开时间线 |
| PATCH | `/api/v1/feedback/:id` | 作者在允许阶段编辑，带版本 |
| POST | `/api/v1/feedback/:id/supplements` | 追加证据，要求幂等键 |
| POST | `/api/v1/feedback/similar` | 项目内相似项建议，不含私密泄露 |
| PUT | `/api/v1/feedback/:id/vote` | 设置投票状态 |
| PUT | `/api/v1/feedback/:id/subscription` | 设置关注状态 |
| GET | `/api/v1/feedback/:id/comments` | 可见公开评论 |
| POST | `/api/v1/feedback/:id/comments` | 创建评论/回复，要求幂等键 |
| PATCH | `/api/v1/comments/:id` | 作者限时编辑 |
| DELETE | `/api/v1/comments/:id` | 作者撤回或按规则隐藏 |
| POST | `/api/v1/reports` | 举报内容，要求幂等键 |

### 8.5 议题与通知

已实现（迁移 `00016`）：

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/api/v1/feedback/topics` | 可见议题列表；征集中优先、置顶优先。`status` 只接受 `open` / `closed` |
| GET | `/api/v1/feedback/topics/:id` | 议题详情；`can_answer` 表示当前访问者能否回答 |
| GET | `/api/v1/feedback/topics/:id/answers` | 回答列表；精选优先、再按「有道理」数 |
| POST | `/api/v1/feedback/topics/:id/answers` | 发布回答或回复（登录） |
| PUT | `/api/v1/feedback/answers/:id/helpful` | 设置/取消「有道理」（登录，按人去重） |

首期议题**只做自由回答**：没有单选/多选题型，因此也没有「可见结果」这种聚合视图。原先提议的
`PUT /api/v1/topics/:id/response`（一人一条、截止前可改）不实现——参考站的实际形态是多条回答 + 「有道理」+ 一层回复，用回答列表表达。

回答的署名用**内测编号**（`tester_no`，从 1001 起）加昵称，不下发社区用户 ID。只支持一层回复：回复一条「回复」时，服务端把它挂到同一个顶层回答下。

尚未实现（仍是提议）：

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/api/v1/notifications` | 当前用户通知，no-store |
| PUT | `/api/v1/notifications/:id/read` | 设置已读 |
| POST | `/api/v1/notifications/read-all` | 按筛选批量已读，幂等 |
| GET/PATCH | `/api/v1/me/notification-preferences` | 通知偏好（只有站内渠道） |
| GET | `/api/v1/projects/:id/leaderboard` | 项目贡献榜，登录可见，排除隐藏等级的用户 |
| GET | `/api/v1/me/contributions` | 自己的贡献事件与当前等级 |

### 8.6 上传

尚未实现（仍是提议）：

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| POST | `/api/v1/uploads` | 创建受限上传意图，返回短期上传授权 |
| POST | `/api/v1/uploads/:id/complete` | 声明上传完成并进入扫描 |
| GET | `/api/v1/uploads/:id` | 查询处理状态 |
| DELETE | `/api/v1/uploads/:id` | 删除未绑定上传 |

服务端绑定附件时再次检查所有权、用途、项目、扫描状态和过期时间。

### 8.7 开放提交接口（站外网页）

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| POST | `/open-api/feedback` | 站外网页提交一条反馈，返回展示编号 |

这是给**我们不控制的网页**调用的唯一端点，鉴权与跨域方案见 `adr/0009`。与 `/api/v1/feedback/*` 的区别：

| 维度 | `/api/v1/feedback/*` | `/open-api/feedback` |
| --- | --- | --- |
| 报文加密 | 走（H5 前台经 SSR 转发命中明文豁免） | 不走 |
| 回源鉴权 | 要 `X-Edge-Secret` 或 `X-Service-Token` | 豁免 |
| 身份 | `Authorization: Bearer`（社区会话） | `X-Api-Key`（后台生成的密钥明文） |
| 跨域 | 同源，不需要 | 回显 `Origin`，**不**带 `Access-Control-Allow-Credentials` |
| 能做什么 | 读 + 写 | 只写一条反馈 |

要点：

- **项目由密钥决定**，请求体里不能指定；未知字段一律 400。
- 密钥可配 `allowed_origins` 白名单；**配了白名单时，不带 `Origin` 的请求一律拒绝**，否则白名单可以靠去掉这个头绕过。需要服务端直连的接入方把白名单留空。
- 配额两层：按密钥每分钟 60 条 + 每日 `daily_limit` 条，另有按来源 IP 每分钟 120 条。超限返回 429 并带 `Retry-After`。
- 落库可见性取密钥的 `default_visibility`（默认 `members`）：先进后台分诊，不让站外内容直接出现在前台。
- `page_url` 入库前丢弃查询串与片段；`contact` 只在 `/api/admin` 响应里出现。
- 密钥不存在、已停用、已作废统一返回同一个 `SIGNATURE_INVALID`，不让调用方据此探测密钥是否存在。

接入示例（站外页面）：

```js
await fetch("https://<站点>/open-api/feedback", {
  method: "POST",
  headers: { "Content-Type": "application/json", "X-Api-Key": "fbk_..." },
  body: JSON.stringify({
    kind: "competitor",          // bug / experience / feature / content / access / competitor / other
    title: "竞品上了一键导出",
    body: "他们在详情页右上角加了导出按钮。",
    page_url: location.href,     // 查询串与片段会被服务端丢弃
    contact: "",                 // 选填
  }),
});
```

## 9. 运营端端点索引

### 9.1 当前管理员与项目

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/api/admin/me` | 身份、项目范围、权限与菜单 |
| POST | `/api/admin/reauth` | 创建短期高敏操作凭证 |
| GET/POST | `/api/admin/projects` | 项目列表/创建 |
| GET/PATCH | `/api/admin/projects/:id` | 项目详情/编辑 |
| POST | `/api/admin/projects/:id/archive` | 归档，需原因与重认证 |
| GET/POST | `/api/admin/projects/:id/members` | 成员列表/邀请或添加 |
| PATCH/DELETE | `/api/admin/projects/:id/members/:user_id` | 改角色/移除 |
| POST | `/api/admin/projects/:id/transfer-ownership` | 转移所有权，高敏 |

### 9.2 反馈运营

已实现（权限点 `fb_issues.view` / `.edit`）：

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/api/admin/feedback/issues` | 全量列表；可按项目、状态、类型、优先级、可见性、`source` 筛选 |
| GET | `/api/admin/feedback/issues/overview` | 按状态的数量汇总 |
| GET | `/api/admin/feedback/issues/:id` | 用户内容 + 含内部备注的完整状态历史 + `contact` |
| POST | `/api/admin/feedback/issues/:id/triage` | 一次调用完成分诊：状态流转 + 优先级 + 可见性 + 公开说明 + 内部备注，带乐观锁 |

分诊合并成一个端点：状态、优先级、可见性、说明在 UI 上本来就在同一个弹窗里一起提交，拆成
`transitions` / `classification` / `internal-comments` 多个端点只会让一次操作产生多条审计与多次乐观锁冲突。

尚未实现（仍是提议）：负责人与协作者（`assignment`）、合并与解除合并的专用端点（当前靠 `triage` 填
`duplicate_of`）、批量任务（`bulk-jobs`）、脱敏导出（`exports`）。

### 9.3 内容、议题与密钥

已实现：

| 方法 | 路径 | 权限点 | 说明 |
| --- | --- | --- | --- |
| GET/POST | `/api/admin/feedback/posts` | `fb_posts` | 动态列表/新建 |
| PATCH/DELETE | `/api/admin/feedback/posts/:id` | `fb_posts` | 整体保存/删除 |
| GET/POST | `/api/admin/feedback/topics` | `fb_topics` | 议题列表（含草稿与归档）/新建 |
| GET/PATCH/DELETE | `/api/admin/feedback/topics/:id` | `fb_topics` | 详情/整体保存/删除（已有回答的只能归档） |
| GET | `/api/admin/feedback/topics/:id/answers` | `fb_topics` | 回答列表，含已隐藏 |
| POST | `/api/admin/feedback/answers/:id/featured` | `fb_topics` | 标记/取消精选 |
| POST | `/api/admin/feedback/answers/:id/status` | `fb_topics` | 隐藏/恢复一条回答，议题回答数同步重算 |
| GET/POST | `/api/admin/feedback/api-keys` | `fb_api_keys` | 开放提交密钥列表/生成（明文只返回一次） |
| PATCH | `/api/admin/feedback/api-keys/:id` | `fb_api_keys` | 整体保存（不能换项目） |
| POST | `/api/admin/feedback/api-keys/:id/revoke` | `fb_api_keys` | 作废，立即生效 |

议题与动态的发布状态直接写在整体保存里（`status` 字段），不另开 `publish` 动作端点：
草稿与发布的差别只是一个状态值加一次发布时间写入，运营在同一个表单里切换。

尚未实现（仍是提议）：`/api/admin/releases`、`/api/admin/tags`。

### 9.4 治理、用户与系统

- `/api/admin/reports`、`/:id/actions`；
- `/api/admin/appeals`、`/:id/decision`；
- `/api/admin/users`、`/:id/restrictions`、`/:id/sessions/revoke`；
- `/api/admin/invitations`；
- `/api/admin/contribution-events`（手调高敏，只追加）；
- `/api/admin/roles`、`/api/admin/permissions`；
- `/api/admin/audit-logs`；
- `/api/admin/settings`。

所有列表、计数、导出和批量动作使用同一项目作用域授权，不仅保护详情端点。

## 10. 内部事件（不对外投递）

外部集成与 Webhook 出站已明确排除在范围外（`00` 第 4.2 节），因此没有订阅管理、签名密钥和投递重试接口。

以下事件只在后端进程内使用，由 River 同事务入队并驱动站内通知、搜索索引与贡献记账：

`feedback.created`、`feedback.triaged`、`feedback.status_changed`、`feedback.merged`、`comment.created`、`release.published`、`moderation.action_taken`、`contribution.recorded`。

约定：

- 事件带唯一 ID、版本和发生时间，payload 只放实体 ID 与必要标量，不放私密正文；
- 消费者按事件 ID 幂等；
- 事件结构变化向后兼容，回滚旧版本 Worker 时不能产生无限死信；
- 不因为“以后可能要开放”就提前实现对外投递。

## 11. 限流提议

| 动作 | 初始策略（每账号，另有 IP 风险桶） |
| --- | --- |
| 登录 | 5 次/15 分钟，渐进挑战，不做永久锁死 |
| 注册/邀请核销 | 5 次/小时 |
| 创建反馈 | 10 次/小时/项目 |
| 评论 | 30 次/10 分钟 |
| 投票/关注 | 120 次/分钟，幂等状态写 |
| 相似搜索 | 30 次/分钟 |
| 上传 | 30 个/小时，总字节配额 |
| 举报 | 10 次/小时 |
| 后台导出 | 5 个/小时 |

数值均为 `[提议]`，需压测和真实滥用数据调整。429 返回 `Retry-After`，不能只给模糊错误。

## 12. 兼容性与弃用

- 新增可选字段、端点和枚举通常兼容；客户端对未知字段/枚举要兜底；
- 改类型、语义、必填、删除/改名字段为不兼容；
- 不兼容流程：新增并行字段 → 双写/兼容读 → 客户端迁移与遥测确认 → 标记弃用 → 新大版本删除；
- 响应字段不因权限随机换类型；不可见字段应省略或固定为 null，并在 schema 明确；
- OpenAPI `info.version`、变更记录和生成物在同一提交更新；
- 契约测试必须覆盖成功、未认证、无权、校验失败、冲突、限流和依赖失败。

## 13. 已定与验收

已定（2026-10-23，恢复码部分已于 2026-10-25 作废）：前缀 `/api/v1` 与 `/api/admin`；
身份为用户名 + 密码（**无恢复码**，`adr/0011`）；会话走 HttpOnly Cookie，由 Next.js 服务端转发；
用户列表用游标分页、后台报表可用页码；无外部集成与邮件端点。

待确认：后台是否独立域名（影响 Cookie 域与 CORS）、附件是否直传对象存储还是经 Go 代理、分页 `total` 在哪些高成本列表省略。

本设计通过标准：端点覆盖 P0 流程；每个写操作有认证/权限/幂等/并发语义；私密缓存和错误不泄露；OpenAPI 创建任务已在 `11` 中列为代码前置依赖。