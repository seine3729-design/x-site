-- 表情包 / 表情（S2-5，docs/02 第 5.8 节，docs/03 第 5.4 节）。前台 /api/v1/emojis 下发，后台维护。
-- code 全局唯一（评论文本里的表情码，如 [微笑]）；删表情包级联删其表情。

-- name: ListEmojiPacks :many
SELECT id, name, sort, created_at FROM emoji_pack ORDER BY sort, id;

-- name: ListEmojis :many
SELECT id, pack_id, code, image, sort, created_at FROM emoji ORDER BY pack_id, sort, id;

-- name: CreateEmojiPack :one
INSERT INTO emoji_pack (name, sort) VALUES (@name, @sort)
RETURNING id, name, sort, created_at;

-- name: UpdateEmojiPack :one
UPDATE emoji_pack SET name = @name, sort = @sort WHERE id = @id
RETURNING id, name, sort, created_at;

-- name: DeleteEmojiPack :one
DELETE FROM emoji_pack WHERE id = @id
RETURNING id, name, sort, created_at;

-- name: CreateEmoji :one
INSERT INTO emoji (pack_id, code, image, sort) VALUES (@pack_id, @code, @image, @sort)
RETURNING id, pack_id, code, image, sort, created_at;

-- name: UpdateEmoji :one
UPDATE emoji SET code = @code, image = @image, sort = @sort WHERE id = @id
RETURNING id, pack_id, code, image, sort, created_at;

-- name: DeleteEmoji :one
DELETE FROM emoji WHERE id = @id
RETURNING id, pack_id, code, image, sort, created_at;
