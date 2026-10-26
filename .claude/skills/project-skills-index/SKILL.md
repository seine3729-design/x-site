---
name: project-skills-index
description: x-site 项目的技能总入口。开始任何会话、接到任何开发/设计/评审任务、或不确定该用哪个技能时首先使用。它决定本次任务必须先读哪些技能与文档，并规定所有任务共用的工作准则。
---

# 项目技能总入口（x-site）

## 概述

本项目是四端短剧平台：`service/`（Go 1.27.3 + Gin）、`apps/`（Bun + SvelteKit 3）、`admin/`（React 19.3 + Vite 8）、`mobile/`（Flutter 3.47）。设计文档在 `docs/`，任务清单在 `docs/11-开发任务拆解.md`。

**规则：写任何代码或文档之前，先按下表选出适用的技能并完整阅读，再开始动手。** 只要有一点可能适用，就读。

> 技能结构参考 [addyosmani/agent-skills](https://github.com/addyosmani/agent-skills)（MIT），内容按本项目的技术栈与文档改写。

## 技能路由

```
任务到达
 ├─ 需求不清 / 新功能 / 改动跨多个模块 ──→ spec-and-task-planning
 ├─ 开始写代码（任何端）──────────────→ incremental-implementation
 │    ├─ 新增或修改接口 ─────────────→ api-contract-first
 │    ├─ service/ Go 代码 ───────────→ go-service-conventions
 │    ├─ apps/ Web 代码 ─────────────→ sveltekit-web-conventions
 │    ├─ admin/ 后台代码 ────────────→ react-admin-conventions
 │    ├─ mobile/ APP 代码 ───────────→ flutter-app-conventions
 │    └─ 任何界面 / 视觉 / 交互 ───────→ ui-ux-pro-max + ui-styling（+ design-system）
 ├─ 写测试 / 证明能用 ──────────────────→ testing-and-verification
 ├─ 出 bug / 构建失败 / 结果不符预期 ────→ debugging-and-recovery
 ├─ 涉及鉴权、密钥、加密、用户输入、存储凭据 → security-checklist
 ├─ 涉及播放链路、列表、缓存、包体积 ─────→ performance-budget
 ├─ 评审代码 / 提交前自查 ───────────────→ code-review
 ├─ 提交、分支、发版 ───────────────────→ git-and-commit
 ├─ 改了行为、接口、配置或做了架构决策 ───→ docs-and-adr
 └─ 品牌、Logo、Banner、幻灯片 ──────────→ brand / design / banner-design / slides
```

已有的设计类技能（`ui-ux-pro-max`、`ui-styling`、`design-system`、`design`、`brand`、`banner-design`、`slides`）负责「长什么样」；上面的工程类技能负责「怎么写、怎么验证」。做界面时两类都要读。

## 文档地图（按任务读对应章节，不要整套通读）

| 主题 | 文档 |
| --- | --- |
| 架构、版本基线、环境变量 | `docs/00` |
| 业务规则、角色、流程 | `docs/01` |
| 表结构 | `docs/02`（存储相关见 `docs/12` 第 7 节） |
| 后端模块、中间件、错误码 | `docs/03`、`docs/07` |
| Web 页面与组件 | `docs/04` |
| APP | `docs/05` |
| 运营后台 | `docs/06` |
| 性能 / 容灾 / 安全 | `docs/08` / `docs/09` / `docs/10` |
| 任务编号、本地环境、命令 | `docs/11` |
| 存储源、线路、按 IP 选线 | `docs/12` |
| 个人中心、福利活动、金币、等级、推广返利 | `docs/13` |
| 架构决策（找回密码、报文加密、多语言、会员支付、金币与福利） | `docs/adr/` |

## 共用工作准则（任何技能下都适用）

1. **先说出假设**：动手前列出「我的假设」，需求不明确时停下来问，不要猜着写。
2. **发现矛盾就停**：文档与代码、文档之间互相矛盾时，指出具体位置并询问以哪个为准。
3. **该反对就反对**：方案有明确问题时直接说明代价并给替代方案；用户坚持时照做。
4. **保持简单**：先写最直接、明显正确的实现；第三次重复出现前不抽象。
5. **只改任务范围内的东西**：顺手看到的问题记下来汇报，不要顺手改。
6. **用证据说完成**：构建、测试、实际运行结果才算证据，「看起来没问题」不算。完成标准见 `references/definition-of-done.md`。
7. **中文沟通**：回复、注释、文档用中文；标识符、命令、路径保持原样。

## 常见借口

| 借口 | 事实 |
| --- | --- |
| 「这个改动太小，不用看技能」 | 小改动最容易破坏约定，读一遍技能只要一分钟。 |
| 「我记得文档怎么写的」 | 文档会更新，以当前文件为准。 |
| 「先写完再补测试」 | 补的测试往往只覆盖已经写对的路径。 |

## 红线

- 没读适用技能就开始写代码
- 在没有证据的情况下宣称「完成」
- 修改了接口或行为却没有更新 `docs/` 与 `service/api/openapi.yaml`
- 把密钥、`.env` 内容写进代码、日志或提交
