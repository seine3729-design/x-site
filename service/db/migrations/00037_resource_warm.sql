-- +goose Up
-- 资源预热（S4-5，docs/02 第 4.4 节、docs/03 第 9.5 节、docs/06 第 10 节）：
-- 由 worker 预先拉取清单与前 N 个切片，让 CDN 边缘缓存住，减少真实观众的回源等待。
--
-- 相比 docs/02 第 4.4 节的原始字段，这里多了几列，理由分别是：
--   episodes_total / episodes_done  进度条要有分母。原字段只有「热好多少个」，没有「一共多少个」
--   heartbeat_at                    worker 崩了之后没人把 status 改掉，靠心跳超时才能判定为 interrupted
--   stop_requested                  「停止」是运营点的，worker 在另一个进程里；用列传信号比 Valkey 可靠
--                                   （worker 重启后信号还在），也省得为一个布尔值引入一条新的通信通道
--   admin_id / error                谁发起的、为什么中断，排查时要看
--   finished_at                     原字段只有 elapsed；存结束时刻而不是时长，运行中也能算出已跑多久
CREATE TABLE resource_warm_job (
    id       bigint NOT NULL GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    -- NULL 表示全部剧（docs/02 写的是「drama_id 或 all」）。剧被删除后保留这条记录，置空即可
    drama_id bigint REFERENCES drama (id) ON DELETE SET NULL,
    mode     text   NOT NULL CHECK (mode IN ('playlist_only', 'with_segments')),
    -- 每个清晰度拉前几个切片（with_segments 时有效）
    segments_per_quality int     NOT NULL CHECK (segments_per_quality BETWEEN 1 AND 20),
    -- 同时热几集
    concurrency          int     NOT NULL CHECK (concurrency BETWEEN 1 AND 16),
    include_unpublished  boolean NOT NULL DEFAULT false,
    status               text    NOT NULL CHECK (status IN ('running', 'done', 'stopped', 'interrupted')),

    -- ---------- 进度 ----------
    episodes_total int    NOT NULL DEFAULT 0 CHECK (episodes_total >= 0),
    episodes_done  int    NOT NULL DEFAULT 0 CHECK (episodes_done >= 0),
    -- warmed 是热好的资源数（清单与切片各算一个），failed 是拿不到的
    warmed        bigint NOT NULL DEFAULT 0 CHECK (warmed >= 0),
    failed        bigint NOT NULL DEFAULT 0 CHECK (failed >= 0),
    fetched       bigint NOT NULL DEFAULT 0 CHECK (fetched >= 0),
    traffic_bytes bigint NOT NULL DEFAULT 0 CHECK (traffic_bytes >= 0),
    -- 当前在热什么，给运营看进展（如「第 12 集 · 720p」）
    current text NOT NULL DEFAULT '',

    stop_requested boolean     NOT NULL DEFAULT false,
    error          text        NOT NULL DEFAULT '',
    -- 发起人。不加外键：任务记录要比账号活得久（同 config_backup.admin_id 的取舍）
    admin_id     bigint      NOT NULL,
    started_at   timestamptz NOT NULL DEFAULT now(),
    heartbeat_at timestamptz NOT NULL DEFAULT now(),
    finished_at  timestamptz
);

-- 同一时间仅一轮（docs/02 第 4.4 节）。索引表达式恒为 true，配上部分条件后全表最多一行 status='running'。
-- 和 transcode_job_active_key（00007）同一个套路，只是那个按分集、这个是全局单例。
CREATE UNIQUE INDEX resource_warm_single_running
    ON resource_warm_job ((status = 'running')) WHERE status = 'running';
-- 历史记录倒序翻页
CREATE INDEX resource_warm_recent_idx ON resource_warm_job (id DESC);

-- +goose Down
DROP TABLE resource_warm_job;
