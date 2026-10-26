-- +goose Up
-- 热搜词（docs/06 第 22 节，S3-4 / A3-4，docs/specs/A3-4.md）。word 是规范化后的搜索词（全角转半角、转小写、合并空白），唯一。
-- 搜索次数先在 Valkey 累加，worker 每小时落库；自动收录（auto）的词默认不显示，后台手动加（manual）的词默认显示。
-- 热搜榜 = 显示中的词：置顶的按 pin_sort，其余按搜索次数倒序，共 10 个。置顶的词一定显示。
CREATE TABLE hotsearch (
    id               bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    word             text        NOT NULL UNIQUE CHECK (length(word) BETWEEN 1 AND 50),
    search_count     bigint      NOT NULL DEFAULT 0 CHECK (search_count >= 0),
    miss_count       bigint      NOT NULL DEFAULT 0 CHECK (miss_count >= 0),  -- 搜了但片库没有结果的次数
    pinned           boolean     NOT NULL DEFAULT false,
    pin_sort         int         NOT NULL DEFAULT 0,
    visible          boolean     NOT NULL DEFAULT false,
    source           text        NOT NULL CHECK (source IN ('manual', 'auto')),
    last_searched_at timestamptz,                                              -- 最近一次落库时有搜索的时间（按小时）
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now(),
    CHECK (visible OR NOT pinned)
);
-- 后台列表：置顶的在前，其余按搜索次数倒序
CREATE INDEX hotsearch_list_idx ON hotsearch (pinned DESC, pin_sort, search_count DESC, id DESC);
-- 热搜榜只读显示中的词
CREATE INDEX hotsearch_board_idx ON hotsearch (pinned DESC, pin_sort, search_count DESC, id) WHERE visible;

-- +goose Down
DROP TABLE hotsearch;
