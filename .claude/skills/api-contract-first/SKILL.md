---
name: api-contract-first
description: 先定义接口契约再实现。用于新增或修改任何 HTTP 接口（/api/v1、/api/admin、/open-api）、调整响应字段、错误码、分页方式，或前后端需要并行开发时。
---

# 接口契约优先

## 概述

`service/api/openapi.yaml` 是三端（apps BFF、admin、mobile）共用的唯一契约。先改契约、再写后端、前端按契约接入；契约不一致时以 OpenAPI 为准并修正实现。

## 流程

1. 在 `docs/07` 找到接口（没有就先补到 `docs/07`）。
2. 修改 `service/api/openapi.yaml`：路径、参数、请求体、响应 schema、错误码、是否需要鉴权、缓存策略说明。
3. 后端实现并写 handler 测试，响应与 schema 一致。
4. 前端/APP 按契约接入（admin 用生成的 TS 类型，mobile 用生成的 Dart 模型，生成方式见 `docs/11` X0-5）。
5. 破坏性变更（删字段、改类型、改语义）必须：新增字段并行一段时间 → 客户端迁移 → 再删除；APP 存在旧版本，删除前确认最低支持版本。

## 约定（与 `docs/07` 第 0 节一致）

- 响应包：成功 `{ "code": "OK", "message": "ok", "data": ... }`，失败 `{ "code": "<业务码>", "message", "request_id" }`；业务码见 `docs/07` 第 4 节，新增业务码要同时加到文档。
- 字段 snake_case；时间 ISO 8601 UTC；会话/设备/任务 ID 用 UUIDv7 字符串，内部实体 ID 用数字。
- 列表默认游标分页：`{ list, next_cursor, total }`；只有频道页使用页码分页。
- 路径前缀（`docs/07` 第 0.5 节）：前台（Web、APP）接口一律 `/api/v1/...`，后台接口一律 `/api/admin/...`，不要直接挂在 `/api/` 下；前台不兼容改动开 `/api/v2/...` 并行，后台不带版本号。
- 路径用名词复数、多个单词用连字符：`/api/admin/storage/lines/:id`；动作型操作用子路径：`POST /api/admin/storage/lines/:id/test`。
- 个人数据、含签名地址的接口：`Cache-Control: private, no-store`。
- 写接口支持幂等时接受 `Idempotency-Key`。
- 不在响应里返回内部字段（凭据、哈希、内部错误堆栈）；错误信息给用户能理解的中文，细节进日志并附 `request_id`。

## OpenAPI 片段示例

```yaml
/api/v1/lines:
  get:
    summary: 按客户端 IP 返回线路列表
    parameters:
      - { name: kind, in: query, required: true, schema: { type: string, enum: [video, image] } }
    responses:
      "200":
        description: 成功
        content:
          application/json:
            schema: { $ref: "#/components/schemas/LinesEnvelope" }
      "429": { $ref: "#/components/responses/RateLimited" }
```

## 常见借口

| 借口 | 事实 |
| --- | --- |
| 「先写代码，契约后面补」 | 补出来的契约描述的是实现，不是约定，前端照着写会踩坑。 |
| 「只是加个字段，不用改文档」 | APP 和后台都依赖生成的类型，漏改会导致类型不一致。 |

## 红线

- 接口行为与 `openapi.yaml` 不一致
- 直接删除或改名已上线字段
- 把错误堆栈、SQL、凭据返回给客户端

## 验证

- [ ] `openapi.yaml` 已更新且能通过校验
- [ ] handler 测试断言了状态码、业务码和关键字段
- [ ] `docs/07` 已同步
