---
name: go-service-conventions
description: service/ Go 后端的编码规范。用于编写或修改 service/ 下任何 Go 代码、SQL 迁移、sqlc 查询、River 任务、中间件时。
---

# Go 后端规范（service/）

## 概述

Go 1.27.3 + Gin 1.12 + pgx v5 + sqlc + goose + Valkey + River。结构与分层以 `docs/03` 第 0 节为准。

## 目录与分层

```
cmd/api        HTTP 进程入口      cmd/worker   River 任务进程
internal/app   依赖装配（手写构造函数，不用反射 DI）
internal/config  配置加载与校验
internal/http/{middleware,public,admin,open}   handler 只做绑定/校验/调用/响应
internal/domain/<域>   业务规则（service.go）+ 仓储接口（repo.go）
internal/store/{pg,cache,blob}   实现：sqlc 生成代码、Valkey、对象存储
db/migrations  goose SQL          db/queries   sqlc 查询
```

- handler 不直接访问 `store`；domain 不依赖 Gin。
- 依赖方向：`http → domain → store接口`，`app` 负责把实现注入。

## 编码规则

- **错误**：领域错误用哨兵值（`errs.ErrNotFound` 等），中间件统一映射业务码；用 `fmt.Errorf("…: %w", err)` 保留链路；不要把内部错误文本返回客户端。
- **context**：每个外部调用（DB、Valkey、HTTP、S3）都传请求 ctx 并设置超时；goroutine 不能脱离 ctx 无限运行。
- **SQL**：查询写在 `db/queries/*.sql` 由 sqlc 生成；动态排序/筛选用白名单映射，值一律占位符；禁止字符串拼接用户输入。
- **迁移**：每个 goose 文件同时写 `Up` 与 `Down`；大表加索引用 `CREATE INDEX CONCURRENTLY`（需 `-- +goose NO TRANSACTION`）。
- **事务**：需要入队的任务在同一事务里 `river.InsertTx`。
- **JSON**：DTO 显式写 `json` tag；对外字段 snake_case。
- **缓存**：读穿缓存用 `internal/store/cache` 的封装（singleflight + TTL 抖动），不要在 handler 里直接写 Valkey 命令。
- **日志**：`slog`，带 `request_id`；不得记录令牌、密码、`X-Enc` 头与加密报文、支付凭据与回调原文、存储凭据、完整 IP。
- **报文加密**：`/api/v1/*` 的加解密只在 `apienc` 中间件；handler 读写明文，不要自己解析 `X-Enc`。新增 `/api/v1` 路由默认就在加密组内；支付回调等第三方调用放 `/callback/*`。
- **错误文案**：`errs` 里的 `message` 是中文兜底，用户端按 `code` 显示 9 种语言；新增业务码同步 `docs/07` 与 `apps`、`mobile` 的文案 JSON。
- **配置**：新增配置项加到 `internal/config`，有默认值或启动时校验失败；同步 `compose.yaml` 与 `docs/11` 第 9.4 节。
- **并发安全的可替换数据**（线路表、屏蔽词、GeoIP 读取器）：用 `atomic.Pointer` 整体替换，不加大锁。

## 测试

- 纯逻辑（选线排序、签名、屏蔽词）：表驱动单元测试，放在同包 `_test.go`。
- handler：`httptest` + 假仓储，断言状态码、业务码、关键字段。
- 涉及 SQL：集成测试连接本地 compose 的 PostgreSQL（`DATABASE_URL`），测试内自建并清理数据；无数据库时 `t.Skip`。
- 命令：`./dev.sh go test ./...`、`./dev.sh go vet ./...`。

## 示例：handler 形态

```go
func (h *SiteHandler) GetConfig(c *gin.Context) {
	cfg, err := h.svc.Get(c.Request.Context())
	if err != nil {
		respond.Error(c, err) // 统一映射业务码
		return
	}
	if respond.NotModified(c, cfg.ETag()) {
		return
	}
	respond.OK(c, cfg)
}
```

## 常见借口

| 借口 | 事实 |
| --- | --- |
| 「这条 SQL 很简单，直接拼字符串」 | 注入漏洞往往出在「简单」的拼接里。 |
| 「先用全局变量，后面再注入」 | 全局状态让测试无法替换依赖。 |
| 「错误直接 panic 方便」 | panic 只用于程序员错误，业务错误要返回。 |

## 红线

- handler 里写 SQL 或 Valkey 命令
- 无超时的外部调用
- 迁移没有 `Down`
- 日志或响应中出现凭据、令牌、密码、支付回调原文

## 验证

- [ ] `./dev.sh go vet ./...` 与 `./dev.sh go test ./...` 通过
- [ ] `gofmt -l` 无输出
- [ ] 接口改动已 `curl` 实测，并与 `openapi.yaml` 一致
