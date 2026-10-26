-- 屏蔽词（S2-4，docs/02 第 5.7 节）。word 存规范化后的形式；改处理办法=先删再加（word 唯一）。
-- group 是 SQL 保留字，一律双引号。LIKE 模式由领域层转义并加 %；narg 为 NULL 表示不按该项筛选。

-- name: ListBlockwords :many
SELECT id, word, "group", action, created_at
FROM blockword
WHERE (sqlc.narg(pattern)::text IS NULL OR word LIKE sqlc.narg(pattern))
  AND (sqlc.narg(grp)::text IS NULL OR "group" = sqlc.narg(grp))
  AND (sqlc.narg(action)::text IS NULL OR action = sqlc.narg(action))
ORDER BY id DESC
LIMIT sqlc.arg(page_limit) OFFSET sqlc.arg(page_offset);

-- name: CountBlockwords :one
SELECT count(*)
FROM blockword
WHERE (sqlc.narg(pattern)::text IS NULL OR word LIKE sqlc.narg(pattern))
  AND (sqlc.narg(grp)::text IS NULL OR "group" = sqlc.narg(grp))
  AND (sqlc.narg(action)::text IS NULL OR action = sqlc.narg(action));

-- name: CreateBlockword :one
-- word 已由领域层规范化；重复 word 由唯一键冲突映射为 409。
INSERT INTO blockword (word, "group", action) VALUES (@word, @grp, @action)
RETURNING id, word, "group", action, created_at;

-- name: DeleteBlockword :one
DELETE FROM blockword WHERE id = $1
RETURNING id, word, "group", action, created_at;

-- name: ListAllBlockwords :many
-- 加载全部屏蔽词以构建 Aho-Corasick 自动机（进程启动与词表变更时调用）。
SELECT word, "group", action FROM blockword ORDER BY id;

-- name: ListBlockwordGroups :many
-- 已有的分组（去重，供后台筛选下拉）。
SELECT DISTINCT "group" FROM blockword WHERE "group" <> '' ORDER BY "group";
