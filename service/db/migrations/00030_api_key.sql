-- +goose Up
-- 开放平台 API Key（docs/06 第 32 节、docs/07 第 3 节，A4-3）：供采集程序等服务端调用方对接。
--
-- 只存密钥的哈希与开头几位：完整密钥仅在生成时返回一次，之后系统里任何地方都取不回来
-- （后台列表、审计、日志都只能看到 key_hint）。丢了只能重新生成。
--
-- 哈希用 SHA-256 而不是密码哈希（argon2 / bcrypt）：密钥是 32 字节随机串、熵足够高，
-- 不存在字典或暴力破解的空间，而慢哈希会让每个 API 请求都付一次 KDF 的代价。
-- 这与管理员密码的处理不同 —— 那里是人选的口令，必须用慢哈希。
--
-- created_by 不加外键：密钥记录要比管理员账号活得久（账号停用甚至清理后仍要能追溯是谁签发的），
-- 与 config_backup.admin_id 同一个取舍。
CREATE TABLE api_key (
    id           bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    -- 用途（做什么用的）与使用方（给谁用的）
    name         text        NOT NULL CHECK (length(name) BETWEEN 1 AND 60),
    owner        text        NOT NULL DEFAULT '' CHECK (length(owner) <= 60),
    key_hash     text        NOT NULL UNIQUE,
    -- 密钥开头几位，供后台识别与搜索（如 xsk_a1b2c3）
    key_hint     text        NOT NULL CHECK (length(key_hint) BETWEEN 1 AND 16),
    -- 权限范围：drama.push 推送剧集入库，analytics.read 只读运营数据
    scopes       text[]      NOT NULL CHECK (
                     cardinality(scopes) BETWEEN 1 AND 8
                     AND scopes <@ ARRAY['drama.push', 'analytics.read']::text[]),
    status       text        NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'inactive')),
    -- 调用计数与最近调用时间：没有 last_used_at 的密钥在后台显示为「未用过」
    used_count   bigint      NOT NULL DEFAULT 0 CHECK (used_count >= 0),
    last_used_at timestamptz,
    created_by   bigint      NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now()
);

-- 列表按编号倒序翻页
CREATE INDEX api_key_created_idx ON api_key (id DESC);

-- +goose Down
DROP TABLE api_key;
