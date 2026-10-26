-- +goose Up
-- 媒体转封装（S1-5，docs/specs/S1-5.md）：媒体对象、内容密钥、转封装任务；分集补首帧图与视频对象外键。
-- media_object 按 docs/12 第 7 节建立（原计划随 S1-6a）；副本 media_replica 依赖存储源，仍随 S1-6a 建立。

-- ---------- 媒体对象 ----------
-- kind=hls_dir：key 为一集 HLS 产物的目录（主清单 master.m3u8、各档清单、s/ 下的切片），sha256 为主清单内容的哈希。
-- 分集删除后对象记录保留（episode_id 置空），由清理任务删除存储中的文件（S1-6f）。
CREATE TABLE media_object (
    id         bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    kind       text        NOT NULL CHECK (kind IN ('hls_dir', 'file')),
    key        text        NOT NULL UNIQUE CHECK (key <> ''),
    size_bytes bigint      NOT NULL DEFAULT 0 CHECK (size_bytes >= 0),
    sha256     text        NOT NULL DEFAULT '',
    episode_id bigint      REFERENCES episode (id) ON DELETE SET NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX media_object_episode_idx ON media_object (episode_id);

-- 分集当前使用的视频对象；被引用的媒体对象不能删除
ALTER TABLE episode
    ADD CONSTRAINT episode_video_object_id_fkey FOREIGN KEY (video_object_id) REFERENCES media_object (id);
CREATE INDEX episode_video_object_idx ON episode (video_object_id);
-- 首帧图（图片桶中的相对 key），转封装生成，播放器出画面前显示
ALTER TABLE episode ADD COLUMN poster text NOT NULL DEFAULT '';

-- ---------- 内容密钥 ----------
-- 每次转封装生成一把新的 AES-128 内容密钥，key_ver 按分集递增；episode.key_ver 指向当前产物使用的版本，
-- 旧版本保留到旧产物清理（播放中的客户端仍可能请求）。wrapped_key 是 KEK 包装后的密文（docs/10 第 7 节）。
CREATE TABLE media_key (
    episode_id  bigint      NOT NULL REFERENCES episode (id) ON DELETE CASCADE,
    key_ver     int         NOT NULL CHECK (key_ver > 0),
    wrapped_key bytea       NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (episode_id, key_ver)
);

-- ---------- 转封装任务 ----------
-- 一次入队一行（重试同一行，重新提交是新行）。状态：queued（排队或等待重试）→ running → succeeded / skipped / failed。
-- skipped：源文件与当前产物相同（source_hash 一致），没有重新转码。
CREATE TABLE transcode_job (
    id               uuid        PRIMARY KEY,
    episode_id       bigint      NOT NULL REFERENCES episode (id) ON DELETE CASCADE,
    source_key       text        NOT NULL CHECK (source_key <> ''),
    force            boolean     NOT NULL DEFAULT false,
    status           text        NOT NULL DEFAULT 'queued' CHECK (status IN ('queued', 'running', 'succeeded', 'skipped', 'failed')),
    progress         int         NOT NULL DEFAULT 0 CHECK (progress BETWEEN 0 AND 100),
    attempts         int         NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    error            text        NOT NULL DEFAULT '',
    source_hash      text        NOT NULL DEFAULT '',
    output_object_id bigint      REFERENCES media_object (id) ON DELETE SET NULL,
    key_ver          int         NOT NULL DEFAULT 0 CHECK (key_ver >= 0),
    renditions       jsonb       NOT NULL DEFAULT '[]' CHECK (jsonb_typeof(renditions) = 'array'),
    poster           text        NOT NULL DEFAULT '',
    duration_sec     int         NOT NULL DEFAULT 0 CHECK (duration_sec >= 0),
    created_at       timestamptz NOT NULL DEFAULT now(),
    started_at       timestamptz,
    finished_at      timestamptz,
    updated_at       timestamptz NOT NULL DEFAULT now()
);
-- 同一集同时最多一个排队或进行中的任务
CREATE UNIQUE INDEX transcode_job_active_key ON transcode_job (episode_id) WHERE status IN ('queued', 'running');
CREATE INDEX transcode_job_episode_idx ON transcode_job (episode_id, created_at DESC);
CREATE INDEX transcode_job_status_idx ON transcode_job (status, created_at DESC);

-- 「更新至 N 集」改为只算可播放（转封装完成）的分集（docs/03 第 3.1 节）
UPDATE drama d
SET latest_episode = coalesce((SELECT max(e.number) FROM episode e WHERE e.drama_id = d.id AND e.media_status = 'ready'), 0);

-- +goose Down
-- 回到 00006：媒体表删除后分集不再有视频，媒体字段恢复为未转封装；「更新至 N 集」恢复为全部分集的最大集号。
DROP TABLE transcode_job;
DROP TABLE media_key;
ALTER TABLE episode DROP COLUMN poster;
DROP INDEX episode_video_object_idx;
ALTER TABLE episode DROP CONSTRAINT episode_video_object_id_fkey;
UPDATE episode
SET video_object_id = NULL, media_status = 'pending', renditions = '[]', key_ver = 0, source_hash = ''
WHERE video_object_id IS NOT NULL OR media_status <> 'pending' OR key_ver <> 0 OR source_hash <> '';
DROP TABLE media_object;
UPDATE drama d
SET latest_episode = coalesce((SELECT max(e.number) FROM episode e WHERE e.drama_id = d.id), 0);
