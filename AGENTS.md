# AGENTS.md

本文件约束所有在本仓库工作的 AI 编码助手（Claude Code、Kiro、Cursor、Codex、Copilot 等）。

## 动手之前必须做的事

1. **先读技能总入口**：`.claude/skills/project-skills-index/SKILL.md`。
2. **按总入口的路由表，读完本次任务适用的全部技能**（`.claude/skills/<技能名>/SKILL.md`）。例如：
   - 改 Go 后端接口 → `incremental-implementation`、`api-contract-first`、`go-service-conventions`、`testing-and-verification`
   - 做 Web 页面 → `incremental-implementation`、`sveltekit-web-conventions`、`ui-ux-pro-max`、`ui-styling`
   - 做后台页面 → `incremental-implementation`、`react-admin-conventions`、`ui-styling`
   - 做 APP 页面 → `incremental-implementation`、`flutter-app-conventions`、`ui-ux-pro-max`
   - 涉及鉴权/密钥/存储凭据 → 额外读 `security-checklist`
3. **读对应文档章节**（`docs/` 下，文档地图见技能总入口）。
4. 在回复开头用一行写明本次参考的技能，例如：`参考技能：incremental-implementation、go-service-conventions、api-contract-first`。

没有完成以上步骤，不要开始写代码或修改文档。

## 仓库速览

| 目录 | 内容 | 技术栈 |
| --- | --- | --- |
| `service/` | 后端 | Go 1.27.3 + Gin 1.12 + pgx/sqlc + goose + Valkey + River |
| `apps/` | Web 前台 | Bun 1.4 + SvelteKit 3 + Svelte 5 + Tailwind 4 |
| `admin/` | 运营后台 | React 19.3 + Vite 8 + TanStack + shadcn |
| `mobile/` | APP | Flutter 3.47 + Riverpod 3 + go_router + dio |
| `docs/` | 设计文档（00–12） | — |
| `deploy/local/` | 本地开发镜像 | — |
| `.claude/skills/` | 开发规范与设计技能 | — |

## 常用命令（仓库根目录）

```bash
./dev.sh up                 # 启动本地全栈（Docker）
./dev.sh go test ./...      # 在容器内运行 Go 命令（Go 1.27.3）
./dev.sh run apps bun run check
./dev.sh run admin pnpm typecheck
./dev.sh flutter analyze    # 在 mobile/ 运行本机 Flutter（基线 3.47）
./dev.sh help
```

## 不可违反的规则

- 回复、注释、文档使用中文。
- 不提交、不推送，除非用户明确要求。
- 不把 `.env`、密钥、存储凭据写入代码、日志或提交。
- 改接口必须同步 `service/api/openapi.yaml` 与 `docs/07`。
- 用户端（`apps/`、`mobile/`）界面文案只写进 i18n JSON（9 种语言），不在代码里写死；浏览器 / APP 调 `/api/v1/*` 只用加密客户端（`docs/10` 第 5.3 节）。`admin/` 只用中文、不加密。
- 完成前满足 `.claude/skills/project-skills-index/references/definition-of-done.md`，并给出实际运行的验证命令与结果。
