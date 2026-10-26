-- +goose Up
-- 排行干预（docs/06 第 21 节，S3-5 / A3-4，docs/specs/A3-4.md）。
-- 人工调整按榜单存：置顶（排在最前，多部置顶之间按分数）、不让上榜（只影响该榜）、加分（负数为减分，直接加到分数上）。
-- 改动要重新排名才生效：重算任务把结果写进新的 Valkey 有序集合后原子 RENAME。设置类数据，不进回收站；删剧时一起删除。
CREATE TABLE rank_override (
    board      text        NOT NULL CHECK (board IN ('drama_week', 'drama_month')),
    drama_id   bigint      NOT NULL REFERENCES drama (id) ON DELETE CASCADE,
    pinned     boolean     NOT NULL DEFAULT false,
    blocked    boolean     NOT NULL DEFAULT false,
    boost      bigint      NOT NULL DEFAULT 0 CHECK (boost BETWEEN -1000000000 AND 1000000000),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (board, drama_id),
    CHECK (NOT (pinned AND blocked)),        -- 置顶与不让上榜互斥
    CHECK (pinned OR blocked OR boost <> 0)  -- 没有任何调整的不保存
);
-- 删剧时按剧级联删除
CREATE INDEX rank_override_drama_idx ON rank_override (drama_id);

-- 计分系数（docs/02 第 3.6 节 site_config.ranking）：播放 1、追剧 3、点赞 2、看完 2。只给后台读，不下发前台，因此不改 version。
ALTER TABLE site_config ADD COLUMN ranking jsonb NOT NULL DEFAULT '{"play": 1, "follow": 3, "like": 2, "finish": 2}';

-- +goose Down
ALTER TABLE site_config DROP COLUMN ranking;
DROP TABLE rank_override;
