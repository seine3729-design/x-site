#!/usr/bin/env bash
# 首次初始化数据目录时执行一次（docker-entrypoint-initdb.d）。
# 创建业务库、应用账号、只读账号和常用扩展。表结构由 service 的 goose 迁移负责，这里不建表。
set -euo pipefail

: "${XSITE_DB:?缺少 XSITE_DB}"
: "${XSITE_APP_USER:?缺少 XSITE_APP_USER}"
: "${XSITE_APP_PASSWORD:?缺少 XSITE_APP_PASSWORD}"
: "${XSITE_RO_USER:?缺少 XSITE_RO_USER}"
: "${XSITE_RO_PASSWORD:?缺少 XSITE_RO_PASSWORD}"

# 用 psql 变量传值，避免把密码直接拼进 SQL 字符串
psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname postgres \
  -v db="$XSITE_DB" \
  -v app_user="$XSITE_APP_USER" -v app_pass="$XSITE_APP_PASSWORD" \
  -v ro_user="$XSITE_RO_USER" -v ro_pass="$XSITE_RO_PASSWORD" <<'SQL'
CREATE ROLE :"app_user" LOGIN PASSWORD :'app_pass';
CREATE ROLE :"ro_user" LOGIN PASSWORD :'ro_pass';
CREATE DATABASE :"db" OWNER :"app_user" ENCODING 'UTF8' TEMPLATE template0;
SQL

psql -v ON_ERROR_STOP=1 --username "$POSTGRES_USER" --dbname "$XSITE_DB" \
  -v db="$XSITE_DB" -v app_user="$XSITE_APP_USER" -v ro_user="$XSITE_RO_USER" <<'SQL'
-- 扩展需要超级用户创建，提前装好，迁移里只写 CREATE EXTENSION IF NOT EXISTS
CREATE EXTENSION IF NOT EXISTS pg_bigm;
CREATE EXTENSION IF NOT EXISTS pg_trgm;
CREATE EXTENSION IF NOT EXISTS btree_gin;
CREATE EXTENSION IF NOT EXISTS pgcrypto;
CREATE EXTENSION IF NOT EXISTS pg_stat_statements;

-- 收回 public 模式的默认建表权限，只有应用账号能建表
REVOKE CREATE ON SCHEMA public FROM PUBLIC;
GRANT USAGE, CREATE ON SCHEMA public TO :"app_user";

-- 只读账号：看板、排障用
GRANT CONNECT ON DATABASE :"db" TO :"ro_user";
GRANT USAGE ON SCHEMA public TO :"ro_user";
ALTER DEFAULT PRIVILEGES FOR ROLE :"app_user" IN SCHEMA public GRANT SELECT ON TABLES TO :"ro_user";
SQL
