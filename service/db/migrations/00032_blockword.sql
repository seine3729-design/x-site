-- +goose Up
-- 屏蔽词（docs/02 第 5.7 节，S2-4）。引擎用 Aho-Corasick 自动机进程内常驻（docs/03 第 5.5 节），
-- 词表变更后重建自动机并原子替换、经 Valkey Pub/Sub 通知各实例重建。
-- word 存规范化后的形式（全角转半角、转小写、去零宽字符与常见插入符号），匹配时对文本做同样规范化。
-- group 是分组（如「引流」用于识别联系方式）；action 决定命中后的处理：block 直接拦 / mask 变 *** / review 先送审。
-- 改处理办法 = 先删再加（word 唯一），所以没有更新接口。group 是 SQL 保留字，查询里用双引号。
CREATE TABLE blockword (
    id         bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    word       text        NOT NULL UNIQUE CHECK (word <> '' AND length(word) <= 100),
    "group"    text        NOT NULL DEFAULT '' CHECK (length("group") <= 50),
    action     text        NOT NULL CHECK (action IN ('block', 'mask', 'review')),
    created_at timestamptz NOT NULL DEFAULT now()
);
-- 后台列表按分组筛选、按加入时间倒序
CREATE INDEX blockword_group_idx ON blockword ("group", id DESC);

-- +goose Down
DROP TABLE blockword;
