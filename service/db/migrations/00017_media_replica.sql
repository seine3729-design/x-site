-- 媒体副本（S1-6f，docs/12 第 5、7 节）：一个 media_object 在哪些存储源上有副本。
-- 选源只考虑 status='ready' 的存储源，因此镜像尚未复制完成时不会把该线路下发给客户端。
-- +goose Up
CREATE TABLE media_replica (
    object_id      bigint      NOT NULL REFERENCES media_object (id) ON DELETE CASCADE,
    -- 不带 ON DELETE：有副本的存储源删不掉（docs/12 第 5 节第 5 条，需先迁移），由外键报错兜底
    provider_id    bigint      NOT NULL REFERENCES storage_provider (id),
    status         text        NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'ready', 'failed')),
    -- 失败原因（给运营看，不含凭据）；成功后清空
    error          text        NOT NULL DEFAULT '',
    replicated_at  timestamptz,
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (object_id, provider_id)
);

-- 「这个存储源上还有多少副本」：删除与迁移存储源时要用
CREATE INDEX media_replica_provider_idx ON media_replica (provider_id, status);

-- +goose Down
DROP TABLE media_replica;
