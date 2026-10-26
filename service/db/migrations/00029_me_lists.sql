-- +goose Up
-- 注册用户的观看历史、收藏、点赞、通知（docs/02 第 5.2 / 5.3 / 5.9 节，S2-3）。
-- 只存注册用户；游客的历史 / 收藏 / 点赞存本机，登录后经 /me/local-merge 并入（S2-10）。

-- watch_history：续播断点，每人每剧一行。高频上报先写 Valkey、worker 每分钟批量 UPSERT 落库。
CREATE TABLE watch_history (
    user_id        bigint      NOT NULL REFERENCES "user" (id) ON DELETE CASCADE,
    drama_id       bigint      NOT NULL REFERENCES drama (id) ON DELETE CASCADE,
    episode_id     bigint      NOT NULL,
    episode_number int         NOT NULL,
    position_sec   int         NOT NULL DEFAULT 0,
    duration_sec   int         NOT NULL DEFAULT 0,
    updated_at     timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, drama_id)
);
-- 续播列表按最近更新倒序（docs/02 第 5 节索引清单）
CREATE INDEX watch_history_user_idx ON watch_history (user_id, updated_at DESC);

-- follow：收藏（追剧）。
CREATE TABLE follow (
    user_id    bigint      NOT NULL REFERENCES "user" (id) ON DELETE CASCADE,
    drama_id   bigint      NOT NULL REFERENCES drama (id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, drama_id)
);
CREATE INDEX follow_user_idx ON follow (user_id, created_at DESC);

-- like：点赞剧 / 分集。target_id 按 target_type 多态（不建外键）。like 是 SQL 保留字，查询用双引号。
CREATE TABLE "like" (
    user_id     bigint      NOT NULL REFERENCES "user" (id) ON DELETE CASCADE,
    target_type text        NOT NULL CHECK (target_type IN ('drama', 'episode')),
    target_id   bigint      NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, target_type, target_id)
);
CREATE INDEX like_user_idx ON "like" (user_id, created_at DESC);

-- notification：站内信。来源（评论被回复 S2-5、追更 S3-9）后续接入；S2-3 只建表 + 列表 / 全部已读读端点。
CREATE TABLE notification (
    id         bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    user_id    bigint      NOT NULL REFERENCES "user" (id) ON DELETE CASCADE,
    type       text        NOT NULL CHECK (type IN ('reply', 'like', 'update')),
    payload    jsonb       NOT NULL DEFAULT '{}',
    is_read    boolean     NOT NULL DEFAULT false,
    created_at timestamptz NOT NULL DEFAULT now()
);
-- 列表（倒序）与未读数（部分索引）
CREATE INDEX notification_user_idx ON notification (user_id, created_at DESC);
CREATE INDEX notification_unread_idx ON notification (user_id) WHERE is_read = false;

-- +goose Down
DROP TABLE notification;
DROP TABLE "like";
DROP TABLE follow;
DROP TABLE watch_history;
