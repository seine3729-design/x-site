# 完成标准（Definition of Done）

每个改动在声明「完成」前都必须满足。任务自身的验收标准见 `docs/11` 对应行，两者都满足才算完成。

## 正确性
- [ ] 任务验收标准全部满足
- [ ] 实际运行验证过（接口用 curl / 测试调用，页面在浏览器或模拟器里看过），不只是能编译
- [ ] 新行为有测试，且这些测试在改动前会失败
- [ ] 现有测试全部通过
- [ ] 错误路径和边界情况有处理（空数据、超时、无权限、重复提交）

## 质量
- [ ] 命名能说明意图；注释解释「为什么」而不是「做了什么」
- [ ] 没有调试输出、注释掉的代码、无用代码
- [ ] 改动限定在任务范围内
- [ ] 格式化与 lint 通过

## 集成
- [ ] 与其他端联调过（至少调用一次真实后端或按契约的模拟数据）
- [ ] 数据库迁移有回滚（goose `-- +goose Down`）
- [ ] 新配置项已加入 `.env.example` / `env.ts` / `--dart-define` 说明，且有默认值或启动校验
- [ ] 接口变更已同步 `service/api/openapi.yaml`，向后兼容或已标注破坏性变更

## 文档
- [ ] 行为、接口、配置变化已更新到 `docs/` 对应章节
- [ ] 值得保留的架构决策写了 ADR（`docs/adr/`）

## 上线准备
- [ ] 涉及用户输入、鉴权、密钥、存储凭据的改动已按 `security-checklist` 自查
- [ ] 关键路径有日志 / 指标 / trace
- [ ] 有回滚方式（功能开关、可回滚迁移、可回退版本）

## 各端验证命令

| 端 | 命令（仓库根目录执行） |
| --- | --- |
| service | `./dev.sh go vet ./...`、`./dev.sh go test ./...`、`./dev.sh go build ./...` |
| apps | `./dev.sh run apps bun run check`、`./dev.sh run apps bun run lint`、`./dev.sh run apps bun run build` |
| admin | `./dev.sh run admin pnpm typecheck`、`./dev.sh run admin pnpm lint`、`./dev.sh run admin pnpm build` |
| mobile | `./dev.sh flutter analyze`、`./dev.sh flutter test` |
| 全栈 | `./dev.sh up` 后各服务 healthy，`curl http://localhost:8080/api/v1/health` 返回 200 |
