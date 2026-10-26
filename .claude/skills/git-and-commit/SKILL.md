---
name: git-and-commit
description: 分支、提交与发版规范。用于创建分支、编写提交信息、准备合并请求、打版本号时。只有用户明确要求时才执行提交与推送。
---

# Git 与提交

## 概述

主干开发 + 短分支。提交小而完整，一个提交只做一件事。**只有在用户明确要求时才执行 `git commit` / `git push`。**

## 分支

- 命名：`<类型>/<任务编号>-<简述>`，例如 `feat/S1-6d-line-selection`、`fix/W1-4-line-switch-resume`。
- 从主干拉出，尽快合回（建议不超过 2 天）。

## 提交信息

```
<类型>(<范围>): <中文摘要>

<可选：为什么这样改、影响、验证方式>

Refs: S1-6d
```

- 类型：`feat` `fix` `refactor` `perf` `test` `docs` `build` `ci` `chore`
- 范围：`service` `apps` `admin` `mobile` `docs` `infra`
- 示例：`feat(service): 按客户端 IP 返回排序后的线路列表`

## 规则

- 暂存时逐个指定文件，不用 `git add .`
- 不提交 `.env`、凭据、构建产物、`node_modules`、`.svelte-kit`、`build/`
- 格式化与 lint 通过后再提交；不使用 `--no-verify`
- 不改写已推送的历史；不使用 `--force`（除非用户明确要求）
- 合并请求描述包含：改了什么、怎么验证的、未完成项

## 版本

- APP 与后端接口版本按语义化版本；破坏性接口变更提升主版本并在 `docs/07` 标注。
