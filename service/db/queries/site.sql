-- 站点编排（S3-1 / S3-2 / A3-1 / A3-2，docs/specs/A3-1.md）：静态页、前台菜单、页面区块。
-- LIKE 模式由领域层转义并加 %；pattern 为 NULL 表示不按文本搜。
-- 「前台菜单」的查询名都带 Site 前缀，与 admin.sql 里「后台菜单」（admin_menu）的同名查询区分。

-- ---------- 静态页（迁移 00024）----------

-- name: ListPages :many
SELECT *
FROM page
WHERE (sqlc.narg(pattern)::text IS NULL OR title LIKE sqlc.narg(pattern) OR slug LIKE sqlc.narg(pattern))
  AND (sqlc.narg(status)::text IS NULL OR status = sqlc.narg(status))
ORDER BY id DESC
LIMIT sqlc.arg(page_limit) OFFSET sqlc.arg(page_offset);

-- name: CountPages :one
SELECT count(*)
FROM page
WHERE (sqlc.narg(pattern)::text IS NULL OR title LIKE sqlc.narg(pattern) OR slug LIKE sqlc.narg(pattern))
  AND (sqlc.narg(status)::text IS NULL OR status = sqlc.narg(status));

-- name: GetPage :one
SELECT * FROM page WHERE id = $1;

-- name: CreatePage :one
INSERT INTO page (title, slug, body, body_format, status, seo_title, seo_desc, is_footer)
VALUES (@title, @slug, @body, @body_format, @status, @seo_title, @seo_desc, @is_footer)
RETURNING *;

-- name: UpdatePage :one
UPDATE page
SET title = @title, slug = @slug, body = @body, body_format = @body_format, status = @status,
    seo_title = @seo_title, seo_desc = @seo_desc, is_footer = @is_footer, updated_at = now()
WHERE id = @id
RETURNING *;

-- name: DeletePage :one
DELETE FROM page WHERE id = $1 RETURNING *;

-- name: GetPublicPage :one
-- 前台 /p/:slug：只有已发布的页。
SELECT * FROM page WHERE slug = @slug AND status = 'published';

-- name: ListFooterPages :many
-- 随站点配置下发给前台页脚「帮助与条款」，按创建顺序。
SELECT id, slug, title FROM page WHERE status = 'published' AND is_footer ORDER BY id LIMIT 20;

-- ---------- 前台菜单（迁移 00027）----------

-- name: ListSiteMenus :many
-- 后台与前台都一次取全部（菜单条数很少）：按位置、顺序、编号排。
SELECT * FROM menu ORDER BY position, sort, id;

-- name: GetSiteMenu :one
SELECT * FROM menu WHERE id = $1;

-- name: CreateSiteMenu :one
-- 新建放在该位置的末尾。
INSERT INTO menu (position, label, link_type, link_value, group_title, icon, tabbar_icon, badge, visible_to, device, new_tab, status, sort)
VALUES (@position, @label, @link_type, @link_value, @group_title, @icon, @tabbar_icon, @badge, @visible_to, @device, @new_tab, @status,
        COALESCE((SELECT max(m2.sort) + 1 FROM menu m2 WHERE m2.position = @position), 0))
RETURNING *;

-- name: UpdateSiteMenu :one
-- 位置改变时放到新位置的末尾（顺序在新位置重新调整）。
UPDATE menu m
SET position = @position, label = @label, link_type = @link_type, link_value = @link_value, group_title = @group_title,
    icon = @icon, tabbar_icon = @tabbar_icon, badge = @badge, visible_to = @visible_to, device = @device,
    new_tab = @new_tab, status = @status, updated_at = now(),
    sort = CASE WHEN m.position = @position THEN m.sort
                ELSE COALESCE((SELECT max(m2.sort) + 1 FROM menu m2 WHERE m2.position = @position), 0) END
WHERE m.id = @id
RETURNING m.*;

-- name: DeleteSiteMenu :one
DELETE FROM menu WHERE id = $1 RETURNING *;

-- name: CountActiveSiteMenusAt :one
-- 某个位置启用中的条数（手机底栏最多 5 项）；@except_id 排除正在编辑的那条。
SELECT count(*) FROM menu WHERE position = @position AND status = 'active' AND id <> @except_id;

-- name: SiteMenuIDsAt :many
SELECT id FROM menu WHERE position = @position ORDER BY sort, id;

-- name: SetSiteMenuSort :exec
-- 按给定顺序重排某个位置（ids 的下标即新顺序）。
UPDATE menu m SET sort = v.ord, updated_at = now()
FROM (SELECT unnest(@ids::bigint[]) AS menu_id, generate_subscripts(@ids::bigint[], 1) AS ord) AS v
WHERE m.id = v.menu_id AND m.position = @position;

-- ---------- 菜单链接目标（查到即存在，visible 表示前台当前可见）----------

-- name: MenuPageTargets :many
SELECT slug, (status = 'published')::boolean AS visible FROM page WHERE slug = ANY(@slugs::text[]);

-- name: MenuChannelTargets :many
-- 只有「设为频道」的取值才有频道页；取消设为频道后这里查不到，按目标不存在处理。
SELECT slug, (status = 'active')::boolean AS visible FROM taxonomy_term WHERE is_channel AND slug = ANY(@slugs::text[]);

-- name: MenuTopicTargets :many
SELECT slug, (status = 'published')::boolean AS visible FROM topic WHERE slug = ANY(@slugs::text[]);

-- name: MenuTagTargets :many
SELECT slug, (status = 'active' AND merged_into_id IS NULL)::boolean AS visible FROM tag WHERE slug = ANY(@slugs::text[]);

-- ---------- 防丢地址（site_config.addresses）----------

-- name: GetSiteAddresses :one
SELECT addresses FROM site_config WHERE id = 1;

-- name: SetSiteAddresses :one
-- 只写这一段；版本号由 platform.SiteConfigService.BumpVersion 统一 +1 并广播失效。
UPDATE site_config SET addresses = @addresses WHERE id = 1
RETURNING addresses;

-- ---------- 页面区块（迁移 00028）----------

-- name: ListBlocks :many
-- 某个页面的全部区块（后台）或启用的区块（前台在领域层过滤），按顺序。
SELECT * FROM block WHERE page = @page ORDER BY sort, id;

-- name: GetBlock :one
SELECT * FROM block WHERE id = $1;

-- name: CreateBlock :one
-- 新建放在该页面的末尾。
INSERT INTO block (page, type, title, subtitle, item_limit, status, more_link, cols_desktop, device, source_type, source_json, custom_html, sort)
VALUES (@page, @type, @title, @subtitle, @item_limit, @status, @more_link, @cols_desktop, @device, @source_type, @source_json, @custom_html,
        COALESCE((SELECT max(b2.sort) + 1 FROM block b2 WHERE b2.page = @page), 0))
RETURNING *;

-- name: UpdateBlock :one
-- 换页面时放到新页面的末尾。
UPDATE block b
SET page = @page, type = @type, title = @title, subtitle = @subtitle, item_limit = @item_limit, status = @status,
    more_link = @more_link, cols_desktop = @cols_desktop, device = @device, source_type = @source_type,
    source_json = @source_json, custom_html = @custom_html, updated_at = now(),
    sort = CASE WHEN b.page = @page THEN b.sort
                ELSE COALESCE((SELECT max(b2.sort) + 1 FROM block b2 WHERE b2.page = @page), 0) END
WHERE b.id = @id
RETURNING b.*;

-- name: DeleteBlock :one
DELETE FROM block WHERE id = $1 RETURNING *;

-- name: BlockIDsAt :many
SELECT id FROM block WHERE page = @page ORDER BY sort, id;

-- name: SetBlockSort :exec
UPDATE block b SET sort = v.ord, updated_at = now()
FROM (SELECT unnest(@ids::bigint[]) AS block_id, generate_subscripts(@ids::bigint[], 1) AS ord) AS v
WHERE b.id = v.block_id AND b.page = @page;

-- name: ListBlockPages :many
-- 后台「选页面」下拉：首页固定在前，频道页取启用的频道取值。
SELECT slug, name FROM taxonomy_term WHERE is_channel AND status = 'active' ORDER BY sort, id LIMIT 200;

-- ---------- 区块的取数与目标校验 ----------

-- name: BlockMissingDramas :many
-- ids 中不存在或在回收站的剧（手选来源的校验）。按传入顺序返回，错误提示里的编号顺序才稳定。
SELECT x.id::bigint FROM unnest(@ids::bigint[]) AS x(id)
WHERE NOT EXISTS (SELECT 1 FROM drama d WHERE d.id = x.id AND d.deleted_at IS NULL)
ORDER BY array_position(@ids::bigint[], x.id);

-- name: BlockDramaCards :many
-- 手选来源：按给定顺序返回前台可见的剧卡片（不可见的跳过）。列与 content_public 的卡片一致。
SELECT d.id, d.title, d.slug, d.cover_portrait, d.cover_landscape, d.badge, d.score, d.year,
       d.is_finished, d.latest_episode, (d.view_base + d.view_count)::bigint AS play_count,
       d.publish_at, d.created_at, d.hot_score, d.sort_weight
FROM drama d
WHERE d.id = ANY(@ids::bigint[])
  AND d.deleted_at IS NULL AND d.status = 'published' AND (d.publish_at IS NULL OR d.publish_at <= now())
ORDER BY array_position(@ids::bigint[], d.id);

-- name: BlockTopic :one
-- 专题来源：取专题本身（只要已发布的），含自动专题的规则。
SELECT id, title, slug, summary, source_type, rule_json FROM topic WHERE id = @id AND status = 'published';

-- name: BlockTopicDramas :many
-- 手选专题的剧（按后台保存的顺序，过滤不可见的）。
SELECT d.id, d.title, d.slug, d.cover_portrait, d.cover_landscape, d.badge, d.score, d.year,
       d.is_finished, d.latest_episode, (d.view_base + d.view_count)::bigint AS play_count,
       d.publish_at, d.created_at, d.hot_score, d.sort_weight
FROM topic_drama td
JOIN drama d ON d.id = td.drama_id
WHERE td.topic_id = @topic_id
  AND d.deleted_at IS NULL AND d.status = 'published' AND (d.publish_at IS NULL OR d.publish_at <= now())
ORDER BY td.sort, td.drama_id
LIMIT sqlc.arg(page_limit);

-- name: BlockTopicCards :many
-- 专题组区块：按给定顺序返回已发布的专题。
SELECT id, title, slug, summary FROM topic
WHERE id = ANY(@ids::bigint[]) AND status = 'published'
ORDER BY array_position(@ids::bigint[], id);

-- name: BlockIndexTopics :many
-- 专题组区块没指定专题时：取列在专题列表页的已发布专题。
SELECT id, title, slug, summary FROM topic
WHERE status = 'published' AND list_in_index
ORDER BY id DESC
LIMIT sqlc.arg(page_limit);

-- name: BlockTagsByIDs :many
-- 标签云区块：按给定顺序返回可用的标签（不含隐藏、已合并的）。
SELECT id, name, slug FROM tag
WHERE id = ANY(@ids::bigint[]) AND status = 'active' AND merged_into_id IS NULL AND NOT is_hidden
ORDER BY array_position(@ids::bigint[], id);

-- name: BlockTopTags :many
-- 标签云区块没指定标签时：按关联剧数取热门标签。
SELECT t.id, t.name, t.slug FROM tag t
WHERE t.status = 'active' AND t.merged_into_id IS NULL AND NOT t.is_hidden
ORDER BY (SELECT count(*) FROM drama_tag dt WHERE dt.tag_id = t.id) DESC, t.id
LIMIT sqlc.arg(page_limit);

-- name: BlockExistingTopicIDs :many
SELECT id FROM topic WHERE id = ANY(@ids::bigint[]);

-- name: BlockExistingTagIDs :many
SELECT id FROM tag WHERE id = ANY(@ids::bigint[]);
