-- +goose Up
-- 内容域（docs/02 第 2 节、第 8 节）：剧集、分集、演员、标签、筛选维度与取值、专题及关联表。
-- 资讯（article，S3-7）不做，没有对应的表。

-- 中文搜索用 pg_bigm（二元切分，2 个字的词也能命中）。扩展由超级用户在建库时装好（deploy/local/postgres/init），
-- 这里只确认存在；线上缺少扩展时迁移直接失败。
CREATE EXTENSION IF NOT EXISTS pg_bigm;

-- 把别名数组拼成一个字符串，供 pg_bigm 索引与搜索查询共用（查询须写成 aliases_text(aliases) LIKE …才能用上索引）。
-- array_to_string 声明为 STABLE，不能直接出现在索引表达式里；对 text[] 它的结果是确定的，所以包一层 IMMUTABLE。
CREATE FUNCTION aliases_text(text[]) RETURNS text
    LANGUAGE sql IMMUTABLE STRICT PARALLEL SAFE
    RETURN array_to_string($1, ' ');

-- ---------- 剧集 ----------
-- 状态机：draft ──上架──► published ──下架──► offline；published 且 publish_at > now() 为定时未到点，前台不可见。
-- 删除进回收站（deleted_at），30 天后物理删除，届时分集与关联行随之级联删除。
CREATE TABLE drama (
    id              bigint       GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    title           text         NOT NULL CHECK (title <> ''),
    aliases         text[]       NOT NULL DEFAULT '{}',
    slug            text         UNIQUE,
    summary         text         NOT NULL DEFAULT '',
    cover_portrait  text         NOT NULL DEFAULT '',
    cover_landscape text         NOT NULL DEFAULT '',
    badge           text         NOT NULL DEFAULT '',
    score           numeric(2,1) CHECK (score >= 0),
    year            int          CHECK (year BETWEEN 1900 AND 2100),
    status          text         NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'published', 'offline')),
    publish_at      timestamptz,
    is_finished     boolean      NOT NULL DEFAULT false,
    sort_weight     int          NOT NULL DEFAULT 0,
    view_base       bigint       NOT NULL DEFAULT 0 CHECK (view_base >= 0),
    view_count      bigint       NOT NULL DEFAULT 0 CHECK (view_count >= 0),
    -- 冗余列：hot_score 由定时任务按 view_base + view_count 刷新；latest_episode 为前台可见的最大集号（「更新至 N 集」）
    hot_score       bigint       NOT NULL DEFAULT 0,
    latest_episode  int          NOT NULL DEFAULT 0 CHECK (latest_episode >= 0),
    seo_title       text         NOT NULL DEFAULT '',
    seo_desc        text         NOT NULL DEFAULT '',
    source          text         NOT NULL DEFAULT '',
    created_at      timestamptz  NOT NULL DEFAULT now(),
    updated_at      timestamptz  NOT NULL DEFAULT now(),
    deleted_at      timestamptz
);
-- 三种排序的游标分页：WHERE status = $1 AND (hot_score, id) < ($2, $3) ORDER BY hot_score DESC, id DESC
CREATE INDEX drama_status_hot_idx       ON drama (status, hot_score DESC, id DESC);
CREATE INDEX drama_status_recommend_idx ON drama (status, sort_weight DESC, id DESC);
CREATE INDEX drama_status_publish_idx   ON drama (status, publish_at DESC, id DESC);
CREATE INDEX drama_title_bigm_idx       ON drama USING gin (title gin_bigm_ops);
CREATE INDEX drama_aliases_bigm_idx     ON drama USING gin (aliases_text(aliases) gin_bigm_ops);

-- ---------- 分集 ----------
-- video_object_id 指向 media_object（docs/12 第 7 节），该表随 S1-6a 建立，外键与索引届时补上。
CREATE TABLE episode (
    id              bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    drama_id        bigint      NOT NULL REFERENCES drama (id) ON DELETE CASCADE,
    number          int         NOT NULL CHECK (number > 0),
    title           text        NOT NULL DEFAULT '',
    video_object_id bigint,
    cover           text        NOT NULL DEFAULT '',
    duration_sec    int         NOT NULL DEFAULT 0 CHECK (duration_sec >= 0),
    access          text        NOT NULL DEFAULT 'free' CHECK (access IN ('free', 'login', 'vip')),
    trial_sec       int         NOT NULL DEFAULT 0 CHECK (trial_sec >= 0),
    media_status    text        NOT NULL DEFAULT 'pending' CHECK (media_status IN ('pending', 'transcoding', 'ready', 'failed')),
    renditions      jsonb       NOT NULL DEFAULT '[]' CHECK (jsonb_typeof(renditions) = 'array'),
    -- 当前 HLS 内容密钥版本，密钥本身在 media_key；0 表示尚未生成
    key_ver         int         NOT NULL DEFAULT 0 CHECK (key_ver >= 0),
    source_hash     text        NOT NULL DEFAULT '',
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    UNIQUE (drama_id, number)
);

-- ---------- 演员 ----------
-- 删除演员不可恢复，并从所有剧的演员表中移除。
CREATE TABLE actor (
    id           bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name         text        NOT NULL CHECK (name <> ''),
    aliases      text[]      NOT NULL DEFAULT '{}',
    slug         text        NOT NULL UNIQUE,
    bio          text        NOT NULL DEFAULT '',
    avatar       text        NOT NULL DEFAULT '',
    status       text        NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'inactive')),
    follow_count bigint      NOT NULL DEFAULT 0 CHECK (follow_count >= 0),
    created_at   timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE drama_actor (
    drama_id bigint NOT NULL REFERENCES drama (id) ON DELETE CASCADE,
    actor_id bigint NOT NULL REFERENCES actor (id) ON DELETE CASCADE,
    role     text   NOT NULL CHECK (role IN ('lead', 'support')),
    sort     int    NOT NULL DEFAULT 0,
    PRIMARY KEY (drama_id, actor_id)
);
CREATE INDEX drama_actor_actor_idx ON drama_actor (actor_id, drama_id);

-- ---------- 标签 ----------
-- is_hidden：敏感标签，过年龄门才显示、不被搜索引擎收录。status=hidden：不在前台显示。
-- 合并为软合并：被并入的标签记 merged_into_id，前台按目标标签展示。
CREATE TABLE tag (
    id             bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name           text        NOT NULL CHECK (name <> ''),
    slug           text        NOT NULL UNIQUE,
    is_hidden      boolean     NOT NULL DEFAULT false,
    status         text        NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'hidden')),
    merged_into_id bigint      REFERENCES tag (id),
    created_at     timestamptz NOT NULL DEFAULT now(),
    CHECK (merged_into_id <> id)
);

CREATE TABLE drama_tag (
    drama_id bigint NOT NULL REFERENCES drama (id) ON DELETE CASCADE,
    tag_id   bigint NOT NULL REFERENCES tag (id) ON DELETE CASCADE,
    PRIMARY KEY (drama_id, tag_id)
);
CREATE INDEX drama_tag_tag_idx ON drama_tag (tag_id, drama_id);

-- ---------- 筛选维度与取值 ----------
CREATE TABLE taxonomy (
    id              bigint  GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name            text    NOT NULL CHECK (name <> ''),
    slug            text    NOT NULL UNIQUE,
    multi_select    boolean NOT NULL DEFAULT false,
    show_in_explore boolean NOT NULL DEFAULT true,
    status          text    NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'inactive')),
    sort            int     NOT NULL DEFAULT 0
);

-- slug 在维度内唯一（探索页按「维度 slug + 取值 slug」筛选）；设为频道的取值 slug 全站唯一（频道页 /c/:slug）。
-- code 供区块与专题规则引用，填写时全站唯一。停用的取值前台筛选条不显示、频道页 404。
CREATE TABLE taxonomy_term (
    id          bigint  GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    taxonomy_id bigint  NOT NULL REFERENCES taxonomy (id) ON DELETE CASCADE,
    name        text    NOT NULL CHECK (name <> ''),
    slug        text    NOT NULL,
    status      text    NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'inactive')),
    is_channel  boolean NOT NULL DEFAULT false,
    code        text    NOT NULL DEFAULT '',
    sort        int     NOT NULL DEFAULT 0,
    UNIQUE (taxonomy_id, slug)
);
CREATE UNIQUE INDEX taxonomy_term_channel_slug_key ON taxonomy_term (slug) WHERE is_channel;
CREATE UNIQUE INDEX taxonomy_term_code_key ON taxonomy_term (code) WHERE code <> '';

-- 单选维度每部剧只能有一个取值，由业务层在保存时替换。
CREATE TABLE drama_term (
    drama_id bigint NOT NULL REFERENCES drama (id) ON DELETE CASCADE,
    term_id  bigint NOT NULL REFERENCES taxonomy_term (id) ON DELETE CASCADE,
    PRIMARY KEY (drama_id, term_id)
);
CREATE INDEX drama_term_term_idx ON drama_term (term_id, drama_id);

-- ---------- 专题 ----------
-- source_type=manual：手选剧（topic_drama，保存时整份替换）；auto：按 rule_json 规则自动选剧。
CREATE TABLE topic (
    id            bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    title         text        NOT NULL CHECK (title <> ''),
    slug          text        NOT NULL UNIQUE,
    summary       text        NOT NULL DEFAULT '',
    status        text        NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'published')),
    source_type   text        NOT NULL DEFAULT 'manual' CHECK (source_type IN ('manual', 'auto')),
    rule_json     jsonb       NOT NULL DEFAULT '{}' CHECK (jsonb_typeof(rule_json) = 'object'),
    faq_json      jsonb       NOT NULL DEFAULT '[]' CHECK (jsonb_typeof(faq_json) = 'array'),
    list_in_index boolean     NOT NULL DEFAULT true,
    created_at    timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE topic_drama (
    topic_id bigint NOT NULL REFERENCES topic (id) ON DELETE CASCADE,
    drama_id bigint NOT NULL REFERENCES drama (id) ON DELETE CASCADE,
    sort     int    NOT NULL DEFAULT 0,
    PRIMARY KEY (topic_id, drama_id)
);
CREATE INDEX topic_drama_drama_idx ON topic_drama (drama_id);

-- +goose Down
-- pg_bigm 扩展由建库脚本安装、可能被其他对象使用，回滚时保留。
DROP TABLE topic_drama;
DROP TABLE topic;
DROP TABLE drama_term;
DROP TABLE taxonomy_term;
DROP TABLE taxonomy;
DROP TABLE drama_tag;
DROP TABLE tag;
DROP TABLE drama_actor;
DROP TABLE actor;
DROP TABLE episode;
DROP TABLE drama;
DROP FUNCTION aliases_text(text[]);
