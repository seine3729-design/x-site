-- +goose Up
-- 资源机 FTP（docs/02 第 4.2 节、docs/06 第 31 节，S1-6）。
-- 账号密码不入库：cred_ref 是凭据引用，worker 从环境变量 FTP_<cred_ref>_USER / FTP_<cred_ref>_PASSWORD 读取
-- （线上由密钥管理注入，docs/10 第 1 节）。资源机属于设置类数据，删除不进回收站。
CREATE TABLE ftp_server (
    id         bigint      GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name       text        NOT NULL CHECK (length(name) BETWEEN 1 AND 64),
    host       text        NOT NULL CHECK (length(host) BETWEEN 1 AND 253),
    port       int         NOT NULL DEFAULT 21 CHECK (port BETWEEN 1 AND 65535),
    -- none 不加密 / explicit 显式 TLS（AUTH TLS）/ implicit 隐式 TLS；非开发环境只允许 explicit（docs/10 第 2 节，业务层校验）
    tls        text        NOT NULL DEFAULT 'explicit' CHECK (tls IN ('none', 'explicit', 'implicit')),
    -- 目前只支持被动模式（业务层校验）；主动模式需要资源机回连 worker
    passive    boolean     NOT NULL DEFAULT true,
    cred_ref   text        NOT NULL CHECK (cred_ref ~ '^[A-Z][A-Z0-9_]{0,31}$'),
    status     text        NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'inactive')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE ftp_server;
