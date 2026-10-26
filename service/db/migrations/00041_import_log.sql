-- +goose Up
-- 批量导入（A4-2 / S4-6，docs/06 第 8 节）：CSV 导入剧集的批次记录与查重索引。
--
-- docs/06 第 8 节只要求「仅记正式导入，可搜日期」，所以预览不写这张表。

CREATE TABLE import_log (
    id bigint NOT NULL GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    -- 来源标识（写进 drama.source，docs/02 第 2.1 节「采集批次」），运营填，可空
    source    text NOT NULL DEFAULT '',
    -- 导入的文件名，只用于让运营认出是哪一批
    file_name text NOT NULL DEFAULT '',
    rows_total int NOT NULL DEFAULT 0 CHECK (rows_total >= 0),

    -- ---------- 统计（docs/06 第 8 节）----------
    -- 分集两项在 CSV 场景恒为 0：CSV 的列里没有分集信息（剧名、别名、简介、标签）。
    -- 保留这两列是为了让 FTP 扫描与开放平台推送（S4-6 的另两种来源）共用同一张表与同一套统计口径。
    dramas_created   int NOT NULL DEFAULT 0 CHECK (dramas_created >= 0),
    dramas_updated   int NOT NULL DEFAULT 0 CHECK (dramas_updated >= 0),
    episodes_created int NOT NULL DEFAULT 0 CHECK (episodes_created >= 0),
    episodes_updated int NOT NULL DEFAULT 0 CHECK (episodes_updated >= 0),
    -- 跳过：行本身没法用（剧名为空）或文件内重复
    skipped  int NOT NULL DEFAULT 0 CHECK (skipped >= 0),
    -- 被拒：命中屏蔽词（采集只认 block，docs/02 第 5.7 节）
    rejected int NOT NULL DEFAULT 0 CHECK (rejected >= 0),
    -- 自动新建的标签数
    tags_created int NOT NULL DEFAULT 0 CHECK (tags_created >= 0),

    -- 操作人。不加外键：批次记录要比账号活得久（同 config_backup.admin_id 的取舍）
    admin_id   bigint      NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
-- 「可搜日期」+ 倒序翻页
CREATE INDEX import_log_created_idx ON import_log (created_at DESC, id DESC);

-- ---------- 查重索引 ----------
-- 导入要按「剧名或别名」判断这部剧是否已经存在（docs/11 S4-6「重复不重建」）。
-- 现有的 drama_title_bigm_idx / drama_aliases_bigm_idx 是给模糊搜索用的（pg_bigm），
-- 支撑不了 `title = ANY(...)` 与 `aliases && ...` 这两种精确匹配，所以单独建。
CREATE INDEX drama_title_exact_idx ON drama (title) WHERE deleted_at IS NULL;
CREATE INDEX drama_aliases_gin_idx ON drama USING gin (aliases);

-- +goose Down
DROP INDEX drama_aliases_gin_idx;
DROP INDEX drama_title_exact_idx;
DROP TABLE import_log;
