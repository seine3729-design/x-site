-- +goose Up
-- 存储线路与地区规则（docs/12 第 3、7 节，S1-6b）：一个存储源可挂多条线路（CDN 域名 / 直连 / 自定义域名），
-- 线路按地区规则决定给哪些用户。选线算法、健康分、site.version 联动在 S1-6d。
CREATE TABLE storage_line (
    id            bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    provider_id   bigint      NOT NULL REFERENCES storage_provider (id) ON DELETE RESTRICT,
    -- video 切片与清单 / image 封面等图片
    kind          text        NOT NULL CHECK (kind IN ('video', 'image')),
    name          text        NOT NULL DEFAULT '' CHECK (length(name) <= 64),  -- 空则前端显示「线路 N」
    base_url      text        NOT NULL DEFAULT '',  -- 访问入口；presign 直连存储时留空，由存储源生成
    -- presign 直接给存储预签名 / cdn 经 CDN 域名 / public 公开读（仅封面等）
    access_mode   text        NOT NULL CHECK (access_mode IN ('presign', 'cdn', 'public')),
    -- s3_presign / hmac_token（目录前缀签名）/ cdn_vendor（CDN 厂商鉴权）/ none
    sign_type     text        NOT NULL CHECK (sign_type IN ('s3_presign', 'hmac_token', 'cdn_vendor', 'none')),
    sign_key_ref  text        NOT NULL DEFAULT '',  -- 签名密钥引用（密钥本身在密钥管理中）
    token_ttl     int         NOT NULL DEFAULT 600 CHECK (token_ttl BETWEEN 60 AND 604800),
    priority      int         NOT NULL DEFAULT 0,   -- 数字越大越靠前
    weight        int         NOT NULL DEFAULT 100 CHECK (weight BETWEEN 1 AND 100),  -- 同优先级内的流量权重
    -- rules 按地区规则 / global 兜底（所有地区可用）
    region_scope  text        NOT NULL DEFAULT 'global' CHECK (region_scope IN ('rules', 'global')),
    -- 历史兼容：standard / text_to_image（旧混淆封装）；新内容一律 standard
    video_process text        NOT NULL DEFAULT 'standard' CHECK (video_process IN ('standard', 'text_to_image')),
    probe_path    text        NOT NULL DEFAULT '/_probe/256k.bin',  -- 健康探测文件路径
    -- active / watch（观察中：仍下发但排在后面）/ disabled
    status        text        NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'watch', 'disabled')),
    note          text        NOT NULL DEFAULT '' CHECK (length(note) <= 200),
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX storage_line_provider_idx ON storage_line (provider_id, priority DESC, id);

-- 地区规则：一条线路可有多条规则，命中任意一条即视为「本地区线路」（docs/12 第 3.2 节）。
CREATE TABLE storage_line_rule (
    id       bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    line_id  bigint NOT NULL REFERENCES storage_line (id) ON DELETE CASCADE,
    country  text   NOT NULL DEFAULT '*',  -- ISO 3166-1 alpha-2 或 *
    province text   NOT NULL DEFAULT '*',  -- 仅中国大陆有意义
    isp      text   NOT NULL DEFAULT '*',  -- 运营商：电信 / 联通 / 移动 / *
    asn      text   NOT NULL DEFAULT '',   -- 可选，精确到自治系统
    -- include 命中生效 / exclude 把某运营商排除出这条线路
    effect   text   NOT NULL DEFAULT 'include' CHECK (effect IN ('include', 'exclude')),
    boost    int    NOT NULL DEFAULT 0 CHECK (boost BETWEEN 0 AND 100)  -- 命中后额外加分
);
CREATE INDEX storage_line_rule_line_idx ON storage_line_rule (line_id);

-- +goose Down
DROP TABLE storage_line_rule;
DROP TABLE storage_line;
