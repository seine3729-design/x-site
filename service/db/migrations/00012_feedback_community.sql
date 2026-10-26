-- +goose Up
-- 反馈社区（feedback/docs/03，feedback/docs/adr/0008）：与短剧业务同库不同表，统一 fb_ 前缀。
-- 社区用户（fb_user）与后台管理员（admin_user）是两套身份：运营靠 RBAC 权限点管理社区，不需要社区账号。
-- 账号只有用户名 + 密码，不收集邮箱与手机号；找回靠一次性恢复码（feedback/docs/adr/0007）。

-- 社区用户。handle 大小写不敏感唯一（handle_norm = lower(handle)）。
CREATE TABLE fb_user (
    id                 bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    handle             text        NOT NULL CHECK (handle ~ '^[A-Za-z0-9_-]{3,32}$'),
    handle_norm        text        NOT NULL UNIQUE CHECK (handle_norm = lower(handle)),
    password_hash      text        NOT NULL CHECK (password_hash <> ''),   -- Argon2id PHC 串
    recovery_code_hash text        NOT NULL CHECK (recovery_code_hash <> ''), -- 一次性恢复码的哈希，只写不读
    nickname           text        NOT NULL DEFAULT '' CHECK (length(nickname) <= 40),
    bio                text        NOT NULL DEFAULT '' CHECK (length(bio) <= 300),
    show_level         boolean     NOT NULL DEFAULT true,                  -- 是否公开贡献等级与署名
    status             text        NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'restricted', 'suspended')),
    session_version    int         NOT NULL DEFAULT 1,                     -- 改密、重置、封禁时 +1，旧会话立即失效
    points             int         NOT NULL DEFAULT 0 CHECK (points >= 0), -- 贡献值缓存，可由 fb_contribution 重算
    created_at         timestamptz NOT NULL DEFAULT now(),
    updated_at         timestamptz NOT NULL DEFAULT now()
);

-- 内测项目。只有平台管理员能创建（feedback/docs/adr/0001）。
CREATE TABLE fb_project (
    id          bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    slug        text        NOT NULL UNIQUE CHECK (slug ~ '^[a-z0-9][a-z0-9-]{1,47}$'),
    key_prefix  text        NOT NULL UNIQUE CHECK (key_prefix ~ '^[A-Z][A-Z0-9]{1,7}$'), -- 反馈编号前缀，如 APP-1024
    name        text        NOT NULL CHECK (length(name) BETWEEN 1 AND 60),
    summary     text        NOT NULL DEFAULT '' CHECK (length(summary) <= 200),
    description text        NOT NULL DEFAULT '' CHECK (length(description) <= 10000),
    cover       text        NOT NULL DEFAULT '',
    try_url     text        NOT NULL DEFAULT '',                           -- 「去试用」地址，只允许 https
    status      text        NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'recruiting', 'active', 'paused', 'archived')),
    visibility  text        NOT NULL DEFAULT 'public' CHECK (visibility IN ('public', 'members')),
    join_mode   text        NOT NULL DEFAULT 'invite_only' CHECK (join_mode IN ('open', 'application', 'invite_only', 'closed')),
    next_number bigint      NOT NULL DEFAULT 1 CHECK (next_number >= 1),   -- 项目内反馈编号计数器，事务内分配
    sort        int         NOT NULL DEFAULT 0,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX fb_project_list_idx ON fb_project (status, sort DESC, id DESC);

-- 项目成员。owner / maintainer / operator 属于项目团队，member 是测试成员。
CREATE TABLE fb_project_member (
    project_id bigint      NOT NULL REFERENCES fb_project (id) ON DELETE CASCADE,
    user_id    bigint      NOT NULL REFERENCES fb_user (id) ON DELETE CASCADE,
    role       text        NOT NULL DEFAULT 'member' CHECK (role IN ('member', 'operator', 'maintainer', 'owner')),
    status     text        NOT NULL DEFAULT 'active' CHECK (status IN ('pending', 'active', 'removed')),
    joined_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (project_id, user_id)
);
CREATE INDEX fb_project_member_user_idx ON fb_project_member (user_id, project_id);

-- 邀请码。明文只在创建时返回一次，库里只存哈希；code_hint 是前 4 位，供运营辨认。
CREATE TABLE fb_invite (
    id         bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    code_hash  text        NOT NULL UNIQUE,
    code_hint  text        NOT NULL DEFAULT '' CHECK (length(code_hint) <= 8),
    project_id bigint      REFERENCES fb_project (id) ON DELETE CASCADE,   -- 为空表示只注册、不自动加入项目
    role       text        NOT NULL DEFAULT 'member' CHECK (role IN ('member', 'operator', 'maintainer')),
    max_uses   int         NOT NULL DEFAULT 1 CHECK (max_uses BETWEEN 1 AND 10000),
    used_count int         NOT NULL DEFAULT 0 CHECK (used_count >= 0),
    note       text        NOT NULL DEFAULT '' CHECK (length(note) <= 100),
    expires_at timestamptz,
    revoked_at timestamptz,
    created_by bigint      NOT NULL REFERENCES admin_user (id) ON DELETE RESTRICT,
    created_at timestamptz NOT NULL DEFAULT now(),
    CHECK (used_count <= max_uses)
);
CREATE INDEX fb_invite_list_idx ON fb_invite (created_at DESC, id DESC);

-- 邀请核销记录：一个用户对同一邀请只能核销一次。
CREATE TABLE fb_invite_redemption (
    invite_id  bigint      NOT NULL REFERENCES fb_invite (id) ON DELETE CASCADE,
    user_id    bigint      NOT NULL REFERENCES fb_user (id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (invite_id, user_id)
);

-- 项目动态：公告、测试任务、上线小结。只有项目团队能发（成员不能发讨论帖）。
CREATE TABLE fb_post (
    id           bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    project_id   bigint      NOT NULL REFERENCES fb_project (id) ON DELETE CASCADE,
    kind         text        NOT NULL CHECK (kind IN ('announcement', 'test_task', 'changelog')),
    title        text        NOT NULL CHECK (length(title) BETWEEN 1 AND 120),
    body         text        NOT NULL DEFAULT '' CHECK (length(body) <= 50000), -- 受限 Markdown 源文
    status       text        NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'published', 'hidden')),
    visibility   text        NOT NULL DEFAULT 'public' CHECK (visibility IN ('public', 'members')),
    pinned       boolean     NOT NULL DEFAULT false,
    published_at timestamptz,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    CHECK (status <> 'published' OR published_at IS NOT NULL)
);
CREATE INDEX fb_post_feed_idx ON fb_post (status, pinned DESC, published_at DESC, id DESC);
CREATE INDEX fb_post_project_idx ON fb_post (project_id, status, published_at DESC, id DESC);

-- 反馈。number 在项目内连续，展示为 <key_prefix>-<number>。
CREATE TABLE fb_feedback (
    id               bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    project_id       bigint      NOT NULL REFERENCES fb_project (id) ON DELETE CASCADE,
    number           bigint      NOT NULL CHECK (number >= 1),
    author_id        bigint      REFERENCES fb_user (id) ON DELETE SET NULL, -- 注销后置空，保留反馈与状态历史
    kind             text        NOT NULL CHECK (kind IN ('bug', 'experience', 'feature', 'content', 'access', 'other')),
    title            text        NOT NULL CHECK (length(title) BETWEEN 4 AND 160),
    body             text        NOT NULL CHECK (length(body) BETWEEN 1 AND 20000),
    env              text        NOT NULL DEFAULT '' CHECK (length(env) <= 500), -- 机型/浏览器/网络等上下文，不含精确位置
    visibility       text        NOT NULL DEFAULT 'public' CHECK (visibility IN ('public', 'members', 'private')),
    status           text        NOT NULL DEFAULT 'submitted' CHECK (status IN ('submitted', 'triaged', 'planned', 'in_progress', 'verifying', 'shipped', 'closed', 'duplicate', 'declined')),
    priority         text        NOT NULL DEFAULT 'unset' CHECK (priority IN ('critical', 'high', 'medium', 'low', 'unset')),
    duplicate_of     bigint      REFERENCES fb_feedback (id) ON DELETE SET NULL,
    vote_count       int         NOT NULL DEFAULT 0 CHECK (vote_count >= 0),
    version          int         NOT NULL DEFAULT 1,                       -- 乐观锁，后台保存时比对
    last_activity_at timestamptz NOT NULL DEFAULT now(),
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now(),
    UNIQUE (project_id, number),
    -- duplicate 必须指向主反馈，其他状态不能带 duplicate_of（外键置空后由业务改状态）
    CHECK ((status = 'duplicate') = (duplicate_of IS NOT NULL)),
    CHECK (duplicate_of IS NULL OR duplicate_of <> id)
);
CREATE INDEX fb_feedback_project_idx ON fb_feedback (project_id, status, last_activity_at DESC, id DESC);
CREATE INDEX fb_feedback_public_idx ON fb_feedback (visibility, last_activity_at DESC, id DESC);
CREATE INDEX fb_feedback_author_idx ON fb_feedback (author_id, created_at DESC, id DESC);
CREATE INDEX fb_feedback_triage_idx ON fb_feedback (status, created_at, id) WHERE status = 'submitted';

-- 状态历史：只追加，不更新。note 是公开说明，internal_note 只给后台。
CREATE TABLE fb_feedback_event (
    id            bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    feedback_id   bigint      NOT NULL REFERENCES fb_feedback (id) ON DELETE CASCADE,
    from_status   text        NOT NULL DEFAULT '',
    to_status     text        NOT NULL CHECK (to_status <> ''),
    note          text        NOT NULL DEFAULT '' CHECK (length(note) <= 1000),
    internal_note text        NOT NULL DEFAULT '' CHECK (length(internal_note) <= 1000),
    actor_admin   bigint      REFERENCES admin_user (id) ON DELETE SET NULL,
    created_at    timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX fb_feedback_event_idx ON fb_feedback_event (feedback_id, id);

-- 投票：一个用户对一条反馈只能一票。
CREATE TABLE fb_vote (
    feedback_id bigint      NOT NULL REFERENCES fb_feedback (id) ON DELETE CASCADE,
    user_id     bigint      NOT NULL REFERENCES fb_user (id) ON DELETE CASCADE,
    created_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (feedback_id, user_id)
);

-- 贡献事件：只追加，撤销写反向分（feedback/docs/adr/0005，非货币化）。
CREATE TABLE fb_contribution (
    id           bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id      bigint      NOT NULL REFERENCES fb_user (id) ON DELETE CASCADE,
    project_id   bigint      REFERENCES fb_project (id) ON DELETE SET NULL,
    feedback_id  bigint      REFERENCES fb_feedback (id) ON DELETE SET NULL,
    kind         text        NOT NULL CHECK (kind IN ('triaged', 'shipped', 'helped', 'topic', 'manual', 'reverse')),
    points       int         NOT NULL,
    reason       text        NOT NULL DEFAULT '' CHECK (length(reason) <= 200),
    source_key   text        NOT NULL UNIQUE,                              -- 自动事件的幂等键，如 shipped:123
    created_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX fb_contribution_user_idx ON fb_contribution (user_id, created_at DESC, id DESC);

-- ---------- 运营后台菜单与权限点（与 admin/src/config/navigation.ts 一致） ----------
INSERT INTO admin_menu (kind, code, title, icon, sort, builtin)
VALUES ('group', 'feedback', '反馈社区', 'MessagesSquare', 65, true)
ON CONFLICT (code) DO NOTHING;

INSERT INTO admin_menu (parent_id, kind, code, title, path, icon, sort, builtin)
SELECT g.id, 'page', p.code, p.title, p.path, p.icon, p.sort, true
FROM (VALUES
    ('fb_projects', '内测项目', '/feedback/projects', 'Box',            10),
    ('fb_issues',   '反馈分诊', '/feedback/issues',   'Inbox',          20),
    ('fb_posts',    '项目动态', '/feedback/posts',    'Megaphone',      30),
    ('fb_invites',  '邀请码',   '/feedback/invites',  'Ticket',         40),
    ('fb_users',    '社区成员', '/feedback/users',    'Users',          50)
) AS p (code, title, path, icon, sort)
JOIN admin_menu g ON g.code = 'feedback' AND g.kind = 'group'
ON CONFLICT (code) DO NOTHING;

INSERT INTO admin_permission (code, menu_id, name, sensitive, sort)
SELECT m.code || v.suffix, m.id, v.name, false, v.sort
FROM admin_menu m
CROSS JOIN (VALUES ('.view', '查看', 0), ('.edit', '修改', 1)) AS v (suffix, name, sort)
WHERE m.code IN ('fb_projects', 'fb_issues', 'fb_posts', 'fb_invites', 'fb_users')
ON CONFLICT (code) DO NOTHING;

-- +goose Down
-- 权限点随菜单级联删除，角色授权随权限点级联删除。
DELETE FROM admin_menu WHERE code IN ('fb_projects', 'fb_issues', 'fb_posts', 'fb_invites', 'fb_users') AND kind = 'page';
DELETE FROM admin_menu WHERE code = 'feedback' AND kind = 'group';

DROP TABLE fb_contribution;
DROP TABLE fb_vote;
DROP TABLE fb_feedback_event;
DROP TABLE fb_feedback;
DROP TABLE fb_post;
DROP TABLE fb_invite_redemption;
DROP TABLE fb_invite;
DROP TABLE fb_project_member;
DROP TABLE fb_project;
DROP TABLE fb_user;
