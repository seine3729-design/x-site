-- +goose Up
-- 反馈社区第二批（feedback/docs/01 第 11 节、第 7.1 节）：
-- ① 议题区（官方抛运营向问题，用户自由回答，可标精选、可点「有道理」、可一层回复）
-- ② 开放提交接口：其他网站用 API Key 把用户反馈送进来，运营后台统一分诊
-- ③ 内测编号（展示署名用，替代直接暴露用户 ID）
-- ④ 反馈类型补「竞品发现」

-- ---------- 内测编号 ----------
-- 从 1001 起编号，展示为「内测1001」。编号一旦分配不再变化，用户注销也不回收。
CREATE SEQUENCE fb_tester_no_seq START WITH 1001 INCREMENT BY 1;

ALTER TABLE fb_user ADD COLUMN tester_no bigint NOT NULL DEFAULT nextval('fb_tester_no_seq');
ALTER TABLE fb_user ADD CONSTRAINT fb_user_tester_no_key UNIQUE (tester_no);
ALTER SEQUENCE fb_tester_no_seq OWNED BY fb_user.tester_no;

-- ---------- 反馈类型补「竞品发现」，并记录来源 ----------
ALTER TABLE fb_feedback DROP CONSTRAINT fb_feedback_kind_check;
ALTER TABLE fb_feedback ADD CONSTRAINT fb_feedback_kind_check
    CHECK (kind IN ('bug', 'experience', 'feature', 'content', 'access', 'competitor', 'other'));

-- ---------- 开放提交接口的 API Key ----------
-- 明文只在创建时返回一次，库里只存哈希（与邀请码同样的处理）。
CREATE TABLE fb_api_key (
    id                 bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    project_id         bigint      NOT NULL REFERENCES fb_project (id) ON DELETE CASCADE,
    name               text        NOT NULL CHECK (length(name) BETWEEN 1 AND 60),
    key_hash           text        NOT NULL UNIQUE,
    key_hint           text        NOT NULL DEFAULT '' CHECK (length(key_hint) <= 12),
    -- 外部提交默认不公开：先进后台分诊，由运营决定是否转公开
    default_visibility text        NOT NULL DEFAULT 'members' CHECK (default_visibility IN ('public', 'members', 'private')),
    -- 为空表示不校验来源；非空时只接受 Origin 在列表中的浏览器提交
    allowed_origins    text[]      NOT NULL DEFAULT '{}',
    daily_limit        int         NOT NULL DEFAULT 500 CHECK (daily_limit BETWEEN 1 AND 100000),
    status             text        NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'inactive')),
    used_count         bigint      NOT NULL DEFAULT 0 CHECK (used_count >= 0),
    last_used_at       timestamptz,
    revoked_at         timestamptz,
    created_by         bigint      NOT NULL REFERENCES admin_user (id) ON DELETE RESTRICT,
    created_at         timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX fb_api_key_project_idx ON fb_api_key (project_id, id DESC);

-- 反馈来源：community 为社区用户自己提交，open_api 为外部网站经 API Key 送入。
ALTER TABLE fb_feedback ADD COLUMN source     text   NOT NULL DEFAULT 'community'
    CHECK (source IN ('community', 'open_api'));
ALTER TABLE fb_feedback ADD COLUMN api_key_id bigint REFERENCES fb_api_key (id) ON DELETE SET NULL;
-- 提交者自愿留的联系方式（外部提交没有账号时用来回访）；非必填，不做任何校验与外发
ALTER TABLE fb_feedback ADD COLUMN contact    text   NOT NULL DEFAULT '' CHECK (length(contact) <= 120);
-- 出问题的页面地址，只存站内路径或脱敏后的地址
ALTER TABLE fb_feedback ADD COLUMN page_url   text   NOT NULL DEFAULT '' CHECK (length(page_url) <= 500);
-- 外部提交必须带 Key，社区提交必须有作者（注销后 author_id 置空，故只约束 open_api 一侧）
ALTER TABLE fb_feedback ADD CONSTRAINT fb_feedback_source_key_ck
    CHECK (source <> 'open_api' OR api_key_id IS NOT NULL);

CREATE INDEX fb_feedback_source_idx ON fb_feedback (source, created_at DESC, id DESC);

-- ---------- 议题区 ----------
-- 首期只做自由回答（与参考站一致）；单选/多选议题等确认后再加，不预留半成品字段。
CREATE TABLE fb_topic (
    id           bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    project_id   bigint      NOT NULL REFERENCES fb_project (id) ON DELETE CASCADE,
    title        text        NOT NULL CHECK (length(title) BETWEEN 1 AND 160),
    body         text        NOT NULL DEFAULT '' CHECK (length(body) <= 20000),
    -- 回答框的引导语，如「从『大众』的角度想：为什么他们会点进来…」
    answer_hint  text        NOT NULL DEFAULT '' CHECK (length(answer_hint) <= 200),
    status       text        NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'open', 'closed', 'archived')),
    visibility   text        NOT NULL DEFAULT 'public' CHECK (visibility IN ('public', 'members')),
    pinned       boolean     NOT NULL DEFAULT false,
    answer_count int         NOT NULL DEFAULT 0 CHECK (answer_count >= 0),
    ends_at      timestamptz,
    published_at timestamptz,
    created_by   bigint      REFERENCES admin_user (id) ON DELETE SET NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    CHECK (status = 'draft' OR published_at IS NOT NULL)
);
CREATE INDEX fb_topic_feed_idx ON fb_topic (status, pinned DESC, published_at DESC, id DESC);
CREATE INDEX fb_topic_project_idx ON fb_topic (project_id, status, published_at DESC, id DESC);

-- 回答。parent_id 只允许一层回复（回复的回复挂到同一个顶层回答下）。
CREATE TABLE fb_topic_answer (
    id            bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    topic_id      bigint      NOT NULL REFERENCES fb_topic (id) ON DELETE CASCADE,
    parent_id     bigint      REFERENCES fb_topic_answer (id) ON DELETE CASCADE,
    user_id       bigint      REFERENCES fb_user (id) ON DELETE SET NULL,
    body          text        NOT NULL CHECK (length(body) BETWEEN 1 AND 5000),
    featured      boolean     NOT NULL DEFAULT false,   -- 运营标记的「精选」
    helpful_count int         NOT NULL DEFAULT 0 CHECK (helpful_count >= 0),
    status        text        NOT NULL DEFAULT 'visible' CHECK (status IN ('visible', 'hidden')),
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now(),
    CHECK (parent_id IS NULL OR parent_id <> id)
);
CREATE INDEX fb_topic_answer_list_idx ON fb_topic_answer (topic_id, featured DESC, helpful_count DESC, id);
CREATE INDEX fb_topic_answer_parent_idx ON fb_topic_answer (parent_id, id) WHERE parent_id IS NOT NULL;

-- 「有道理」：一个用户对一条回答只能点一次。
CREATE TABLE fb_answer_helpful (
    answer_id  bigint      NOT NULL REFERENCES fb_topic_answer (id) ON DELETE CASCADE,
    user_id    bigint      NOT NULL REFERENCES fb_user (id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (answer_id, user_id)
);

-- ---------- 运营后台菜单与权限点 ----------
INSERT INTO admin_menu (parent_id, kind, code, title, path, icon, sort, builtin)
SELECT g.id, 'page', p.code, p.title, p.path, p.icon, p.sort, true
FROM (VALUES
    ('fb_topics',   '议题区',   '/feedback/topics',   'MessageCircle', 25),
    ('fb_api_keys', '开放接口', '/feedback/api-keys', 'KeyRound',      60)
) AS p (code, title, path, icon, sort)
JOIN admin_menu g ON g.code = 'feedback' AND g.kind = 'group'
ON CONFLICT (code) DO NOTHING;

INSERT INTO admin_permission (code, menu_id, name, sensitive, sort)
SELECT m.code || v.suffix, m.id, v.name, v.sensitive, v.sort
FROM admin_menu m
CROSS JOIN (VALUES ('.view', '查看', false, 0), ('.edit', '修改', false, 1)) AS v (suffix, name, sensitive, sort)
WHERE m.code IN ('fb_topics', 'fb_api_keys')
ON CONFLICT (code) DO NOTHING;

-- +goose Down
DELETE FROM admin_menu WHERE code IN ('fb_topics', 'fb_api_keys') AND kind = 'page';

DROP TABLE fb_answer_helpful;
DROP TABLE fb_topic_answer;
DROP TABLE fb_topic;

ALTER TABLE fb_feedback DROP CONSTRAINT fb_feedback_source_key_ck;
DROP INDEX fb_feedback_source_idx;
ALTER TABLE fb_feedback DROP COLUMN page_url;
ALTER TABLE fb_feedback DROP COLUMN contact;
ALTER TABLE fb_feedback DROP COLUMN api_key_id;
ALTER TABLE fb_feedback DROP COLUMN source;

DROP TABLE fb_api_key;

-- 回滚类型约束：先把「竞品发现」归到「其他」，否则新约束加不上
UPDATE fb_feedback SET kind = 'other' WHERE kind = 'competitor';
ALTER TABLE fb_feedback DROP CONSTRAINT fb_feedback_kind_check;
ALTER TABLE fb_feedback ADD CONSTRAINT fb_feedback_kind_check
    CHECK (kind IN ('bug', 'experience', 'feature', 'content', 'access', 'other'));

ALTER TABLE fb_user DROP CONSTRAINT fb_user_tester_no_key;
ALTER TABLE fb_user DROP COLUMN tester_no;
DROP SEQUENCE IF EXISTS fb_tester_no_seq;
