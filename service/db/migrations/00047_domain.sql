-- +goose Up
-- 域名与证书（docs/02 第 6.4 节、docs/06 第 38 节、docs/09 第 2、4 节，A4-4 / S4-8）。
-- 入口域名池、签名域名清单的发布历史、按天汇总的域名健康。
-- 证书不入库：面向用户的入口证书由部署层 ACME（certbot）自动签发（docs/09 第 2.2 节），
-- 后台只维护到期日 cert_expires_at 并在到期前告警；「手动生成证书」生成的私钥一次性下载、不落库。

-- 入口域名池：域名、角色、权重、适用地区、状态、所属 CDN / DNS 服务商、证书到期日。
CREATE TABLE domain_entry (
    id              bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    host            text        NOT NULL UNIQUE CHECK (length(host) BETWEEN 1 AND 253),
    -- web_primary Web 主入口 / web_backup 备用 / nav 永久导航 / api API 域名 / media 媒体域名 / admin 后台
    role            text        NOT NULL CHECK (role IN ('web_primary', 'web_backup', 'nav', 'api', 'media', 'admin')),
    weight          int         NOT NULL DEFAULT 100 CHECK (weight BETWEEN 1 AND 100),
    region          text        NOT NULL DEFAULT '*',  -- ISO 3166-1 alpha-2 或 *（全部地区）
    cdn             text        NOT NULL DEFAULT '' CHECK (length(cdn) <= 64),
    dns_provider    text        NOT NULL DEFAULT '' CHECK (length(dns_provider) <= 64),
    cert_expires_at timestamptz,  -- 证书到期日；没录入 / 上传过证书时为空
    -- active 启用 / watch 观察（仍在清单里但排在后面）/ disabled 停用
    status          text        NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'watch', 'disabled')),
    note            text        NOT NULL DEFAULT '' CHECK (length(note) <= 200),
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX domain_entry_role_idx ON domain_entry (role, status, weight DESC, id);

-- 签名域名清单的发布历史（docs/09 第 4.1 节）。payload 是被签名的清单正文（含 v，不含 sig），
-- signature 是离线签发工具（docs/09：service/cmd/keytool）签出的 Ed25519 签名的 base64 —— 后台不保存签名私钥。
-- version 即清单里的 v：由应用层取「当前最大版本 + 1」显式赋值（不用自增 IDENTITY），
-- 因为 v 被写进被签名的 payload、客户端只接受不小于本地版本的清单，发布时要校验 payload.v 与分配的版本一致。
-- 每次发布（含回滚历史版本的内容）都是一个更大的新版本。
CREATE TABLE domain_list_release (
    version      bigint      PRIMARY KEY CHECK (version > 0),
    payload      jsonb       NOT NULL,
    signature    text        NOT NULL DEFAULT '' CHECK (length(signature) <= 2048),
    note         text        NOT NULL DEFAULT '' CHECK (length(note) <= 200),
    -- 发布者；账号删除后记录仍要保留（清单比账号活得久），故置空而非级联删除
    published_by bigint      REFERENCES admin_user (id) ON DELETE SET NULL,
    published_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX domain_list_release_version_idx ON domain_list_release (version DESC);

-- 域名健康日表（docs/02 第 6.4 节、docs/06 第 38 节健康看板）。
-- probe_* 来自服务端 / 拨测探测，client_failover_count 与 fail_reasons 来自 APP / Web 的切换上报
-- （docs/09 第 4.2 节 {from,to,reason}）；上报端点在用户端抗封锁链路接入前，这张表没有写入源、看板为空。
CREATE TABLE domain_health_daily (
    day                   date   NOT NULL,
    host                  text   NOT NULL,
    region                text   NOT NULL DEFAULT '*',
    probe_ok              bigint NOT NULL DEFAULT 0 CHECK (probe_ok >= 0),
    probe_fail            bigint NOT NULL DEFAULT 0 CHECK (probe_fail >= 0),
    client_failover_count bigint NOT NULL DEFAULT 0 CHECK (client_failover_count >= 0),
    -- 失败原因分布：{"dns":..,"tls":..,"timeout":..,"http5xx":..}
    fail_reasons          jsonb  NOT NULL DEFAULT '{}'::jsonb,
    PRIMARY KEY (day, host, region)
);
CREATE INDEX domain_health_daily_day_idx ON domain_health_daily (day DESC);

-- +goose Down
DROP TABLE domain_health_daily;
DROP TABLE domain_list_release;
DROP TABLE domain_entry;
