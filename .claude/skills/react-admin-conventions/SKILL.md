---
name: react-admin-conventions
description: admin/ 运营后台（React 19.3 + Vite 8 + TanStack）的编码规范。用于编写或修改 admin/ 下的路由、页面、表格、表单、请求层、权限显示时。界面工作同时读 ui-styling 与 ui-ux-pro-max。
---

# 运营后台规范（admin/）

## 概述

React 19.3 + React Compiler + Vite 8 + TanStack Router/Query/Table + shadcn + Tailwind 4 + zod 4 + react-hook-form。模块与字段以 `docs/06` 为准。

## 目录

```
src/routes/            TanStack 文件路由（_authenticated/ 下为需登录页面）
src/features/<模块>/   页面组件、columns、表单 schema、queries.ts（该模块的 query/mutation）
src/components/ui/     shadcn 组件（通过 CLI 添加，不手改样式源）
src/components/        通用组件（DataTable、PageHeader、确认弹窗…）
src/lib/http.ts        唯一的请求封装
src/lib/permissions.ts 权限显示判断
src/stores/            Zustand：仅 UI 状态与当前用户资料
```

## 规则

- **请求**：只通过 `src/lib/http.ts`（基于 fetch，`credentials: 'include'`，自动带 CSRF，统一错误映射）；不要引入 axios。
- **服务端状态**：一律 TanStack Query；query key 形如 `['storage', 'lines', params]`，写操作成功后精确 `invalidateQueries`；不要把接口数据复制进 Zustand。
- **令牌**：前端不接触令牌（HttpOnly 会话）；不要读写任何鉴权 Cookie 或 localStorage 令牌。
- **表单**：react-hook-form + zod schema（schema 与接口字段同名）；提交按钮在 pending 时禁用；凭据类字段只写不读，编辑时留空表示不修改。
- **表格**：服务端分页/筛选/排序，条件同步到 URL（`use-table-url-state`）；超过 100 行用 TanStack Virtual。
- **权限**：用 `hasPermission` 控制显示；真正的权限判断在后端。只读角色看到的表单控件要禁用并说明原因。
- **高敏操作**：调用 `/api/admin/reauth` 流程（后端返回 `REAUTH_REQUIRED` 时弹二次验证）。
- **React Compiler**：不要再手写 `useMemo/useCallback/memo` 做常规优化；只有测量证明需要时保留并注明原因。
- **组件库**：新组件通过 `shadcn` CLI 添加；图表用 shadcn charts（Recharts 3）。

## 测试

- 工具函数、权限、表单 schema：Vitest。
- 冒烟：Playwright（登录 → 列表 → 编辑保存）。
- 命令：`./dev.sh run admin pnpm typecheck`、`./dev.sh run admin pnpm lint`、`./dev.sh run admin pnpm build`。

## 红线

- 引入 axios、crypto-js、Tremor 或其他已在 `docs/06` 第 0.1 节移除的依赖
- 前端保存或读取令牌
- 在组件里直接 `fetch` 绕过 `lib/http.ts`
- 凭据字段回显明文

## 验证

- [ ] typecheck、lint、build 通过
- [ ] 页面在浏览器中实际操作过（含无权限角色的显示）
- [ ] 新菜单项与 `docs/06` 一致
