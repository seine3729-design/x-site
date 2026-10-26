-- +goose Up
-- 资源体检（S4-5，docs/02 第 4.3 节、docs/03 第 9.4 节、docs/06 第 9 节）：
-- 批量探测剧集的视频产物与封面是否可达，结果存这张表供运营后台查看。
--
-- 相比 docs/02 第 4.3 节的原始字段，这里多了三列，理由分别是：
--   field      一部剧有竖封面、横封面，一集有封面、首帧图与视频，只有 file_type（video/cover）分不出是哪个，
--              运营看到「封面打不开」却不知道该去改哪个字段
--   line_id    视频与封面现在都走存储线路（S1-6），同一个对象键经不同线路的可达性可能不同；
--              记下这次用的是哪条线路，结果才解释得通（线路本身的健康在「选线预览与健康」页，不在这里重复）
--   elapsed_ms / message  只有状态码不足以区分「连不上」的原因（DNS、超时、被出站策略拦截）
--
-- url 存不含查询串的地址：HMAC 令牌与 S3 预签名都带密钥派生值与过期时间，
-- 落库就等于把签名材料留在表里、还会显示在后台页面上。探测时按线路现算签名。
CREATE TABLE resource_check (
    id          bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    drama_id    bigint      NOT NULL REFERENCES drama (id) ON DELETE CASCADE,
    -- 剧封面的记录没有分集
    episode_id  bigint      REFERENCES episode (id) ON DELETE CASCADE,
    file_type   text        NOT NULL CHECK (file_type IN ('video', 'cover')),
    field       text        NOT NULL CHECK (field IN ('cover_portrait', 'cover_landscape', 'episode_cover', 'episode_poster', 'video')),
    url         text        NOT NULL DEFAULT '',
    line_id     bigint      REFERENCES storage_line (id) ON DELETE SET NULL,
    result      text        NOT NULL CHECK (result IN ('ok', 'not_found', 'forbidden', 'server_error', 'unopenable', 'unreachable')),
    status_code int         NOT NULL DEFAULT 0 CHECK (status_code >= 0),
    elapsed_ms  int         NOT NULL DEFAULT 0 CHECK (elapsed_ms >= 0),
    message     text        NOT NULL DEFAULT '',
    checked_at  timestamptz NOT NULL DEFAULT now()
);

-- 一个资源只留最后一次的结果：重复体检走 UPSERT，表不会随体检次数无限膨胀。
-- episode_id 可空，NULL 在唯一索引里互不相等，所以用 coalesce 折成 0。
CREATE UNIQUE INDEX resource_check_target_key
    ON resource_check (drama_id, coalesce(episode_id, 0), file_type, field);
-- 「只看打不开」是最常用的筛选
CREATE INDEX resource_check_result_idx ON resource_check (result, checked_at DESC);
CREATE INDEX resource_check_drama_idx ON resource_check (drama_id, id);

-- +goose Down
DROP TABLE resource_check;
