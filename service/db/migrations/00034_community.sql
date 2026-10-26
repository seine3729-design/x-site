-- +goose Up
-- 社区：评论、弹幕、举报、表情（docs/02 第 5.4–5.6 / 5.8 节，docs/03 第 5 节，S2-5）。
-- 评论 / 弹幕作者多态：注册用户 user_id 或游客 guest_device_id，恰好一个（与 user_session 一致）。
-- 状态机 pending（待审）→ visible（显示）/ hidden（隐藏）。先审后显由 comment_policy 或命中 review 词触发。

-- ---------- 评论 ----------
CREATE TABLE comment (
    id              bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    episode_id      bigint      NOT NULL REFERENCES episode (id) ON DELETE CASCADE,
    user_id         bigint      REFERENCES "user" (id) ON DELETE CASCADE,
    guest_device_id uuid        REFERENCES guest_device (id) ON DELETE CASCADE,
    parent_id       bigint      REFERENCES comment (id) ON DELETE CASCADE,  -- 回复
    content         text        NOT NULL CHECK (content <> '' AND length(content) <= 500),
    image_count     int         NOT NULL DEFAULT 0 CHECK (image_count >= 0),  -- 带图评论随 S6-13/S6-14
    status          text        NOT NULL DEFAULT 'visible' CHECK (status IN ('pending', 'visible', 'hidden')),
    like_count      bigint      NOT NULL DEFAULT 0 CHECK (like_count >= 0),
    created_at      timestamptz NOT NULL DEFAULT now(),
    deleted_at      timestamptz,
    CONSTRAINT comment_author_ck CHECK ((user_id IS NULL) <> (guest_device_id IS NULL))
);
-- 列表（最新）：某集未删评论按时间倒序
CREATE INDEX comment_episode_latest_idx ON comment (episode_id, created_at DESC, id DESC) WHERE deleted_at IS NULL;
-- 列表（最热）：显示中的按点赞数倒序
CREATE INDEX comment_episode_hot_idx ON comment (episode_id, like_count DESC, id DESC) WHERE deleted_at IS NULL AND status = 'visible';
-- 回复计数 / 展开
CREATE INDEX comment_parent_idx ON comment (parent_id) WHERE parent_id IS NOT NULL AND deleted_at IS NULL;
-- 后台审核队列：按状态
CREATE INDEX comment_status_idx ON comment (status, id DESC) WHERE deleted_at IS NULL;

-- 评论点赞（仅注册用户；游客点赞存本机、不计公开数）。
CREATE TABLE comment_like (
    comment_id bigint      NOT NULL REFERENCES comment (id) ON DELETE CASCADE,
    user_id    bigint      NOT NULL REFERENCES "user" (id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (comment_id, user_id)
);

-- ---------- 弹幕 ----------
CREATE TABLE danmaku (
    id              bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    episode_id      bigint      NOT NULL REFERENCES episode (id) ON DELETE CASCADE,
    user_id         bigint      REFERENCES "user" (id) ON DELETE CASCADE,
    guest_device_id uuid        REFERENCES guest_device (id) ON DELETE CASCADE,
    time_ms         int         NOT NULL CHECK (time_ms >= 0),  -- 出现时间（毫秒）
    content         text        NOT NULL CHECK (content <> '' AND length(content) <= 40),
    color           text        NOT NULL DEFAULT 'FFFFFF',       -- RRGGBB，白名单在领域层
    status          text        NOT NULL DEFAULT 'visible' CHECK (status IN ('pending', 'visible', 'hidden')),
    created_at      timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT danmaku_author_ck CHECK ((user_id IS NULL) <> (guest_device_id IS NULL))
);
-- 按 60 秒窗口拉取显示中的弹幕
CREATE INDEX danmaku_episode_time_idx ON danmaku (episode_id, time_ms) WHERE status = 'visible';
-- 后台审核队列：按状态
CREATE INDEX danmaku_status_idx ON danmaku (status, id DESC);

-- ---------- 举报 ----------
-- 举报人多态（注册用户或游客）；24 小时内同设备对同对象只计一次由 Valkey 去重，不建唯一约束。
CREATE TABLE report (
    id                bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    target_type       text        NOT NULL CHECK (target_type IN ('comment', 'danmaku', 'drama', 'comment_image')),
    target_id         bigint      NOT NULL,
    reporter_id       bigint      REFERENCES "user" (id) ON DELETE SET NULL,
    reporter_guest_id uuid        REFERENCES guest_device (id) ON DELETE SET NULL,
    reason            text        NOT NULL CHECK (reason IN ('attack', 'vulgar', 'illegal', 'minor', 'ad', 'spoiler', 'other')),
    detail            text        NOT NULL DEFAULT '' CHECK (length(detail) <= 500),
    status            text        NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'resolved', 'rejected')),
    handle_note       text        NOT NULL DEFAULT '',
    created_at        timestamptz NOT NULL DEFAULT now()
);
-- 后台举报队列：按状态、对象
CREATE INDEX report_status_idx ON report (status, id DESC);
CREATE INDEX report_target_idx ON report (target_type, target_id);

-- ---------- 表情 ----------
CREATE TABLE emoji_pack (
    id         bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name       text        NOT NULL CHECK (name <> ''),
    sort       int         NOT NULL DEFAULT 0,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE emoji (
    id         bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    pack_id    bigint      NOT NULL REFERENCES emoji_pack (id) ON DELETE CASCADE,
    code       text        NOT NULL UNIQUE CHECK (code <> ''),  -- 评论里的表情码，如 [微笑]
    image      text        NOT NULL,
    sort       int         NOT NULL DEFAULT 0,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX emoji_pack_sort_idx ON emoji (pack_id, sort, id);

-- +goose Down
DROP TABLE emoji;
DROP TABLE emoji_pack;
DROP TABLE report;
DROP TABLE danmaku;
DROP TABLE comment_like;
DROP TABLE comment;
