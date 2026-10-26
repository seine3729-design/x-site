---
name: testing-and-verification
description: 用测试和运行结果证明改动有效。用于实现新逻辑、修复 bug（先写会失败的测试）、修改已有行为、或需要给出「已完成」结论之前。
---

# 测试与验证

## 概述

测试是完成的证据。修 bug 先写能复现它的失败测试；新逻辑先确定期望行为再实现。

## 先确认技术栈的测试命令

| 端 | 测试 | 静态检查 |
| --- | --- | --- |
| service | `./dev.sh go test ./...`（指定包：`./dev.sh go test ./internal/domain/storage/...`） | `./dev.sh go vet ./...` |
| apps | `./dev.sh run apps bun run test`（Vitest，接入后） | `./dev.sh run apps bun run check` |
| admin | `./dev.sh run admin pnpm test`（Vitest，接入后） | `./dev.sh run admin pnpm typecheck` |
| mobile | `./dev.sh flutter test` | `./dev.sh flutter analyze` |

## 修 bug：红 → 绿 → 整理

1. 写一个复现问题的测试，运行，**确认它失败且失败原因正确**。
2. 用最小改动修复，测试变绿。
3. 跑该端全部测试，确认无回归。
4. 在测试名或注释里写清问题场景（例如 `TestSelectLines_AllUnhealthyKeepsBest`）。

## 测试层次

- **单元**（多数）：纯函数与业务规则，表驱动。选线、签名、屏蔽词、令牌、格式化等都应在这一层覆盖。
- **集成**（适量）：handler + 真实 PostgreSQL/Valkey（本地 compose）；API 契约字段。
- **端到端**（少量关键路径）：注册登录、起播、后台保存配置后前台生效。

## 什么算证据

- 测试命令的通过输出
- `curl` 实际响应（状态码 + 关键字段）
- 浏览器 / 模拟器中的实际表现（必要时截图或描述可观察结果）
- 「代码看起来对」「应该可以」不算

## 常见借口

| 借口 | 事实 |
| --- | --- |
| 「改动很小，不用测」 | 小改动引起的回归最难发现。 |
| 「测试后补」 | 后补的测试常常只验证实现，而不是需求。 |
| 「没有测试框架」 | 按上表命令接入，没有就把接入当作本任务的第一个切片。 |

## 红线

- 修 bug 没有先看到失败的测试
- 为了让测试通过而修改测试的断言（除非断言本身错了，并说明原因）
- 跳过失败的测试或加 `skip` 不说明原因

## 验证

- [ ] 新行为/修复都有测试，且曾经失败过
- [ ] 该端全部测试通过
- [ ] 在回复中给出实际运行的命令与结果摘要
