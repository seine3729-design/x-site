#!/usr/bin/env bash
# 发布前自检服务器上的 .env。
#
#   用法: ./scripts/check-env.sh
#
# 查三类问题，都是实际会导致「服务起不来」或「安全上等于没做」的：
#   1. 必填项为空        —— docker compose 的 ${X:?} 也能查出来，但报错一次只说一个字段
#   2. 密钥字段重复      —— 最常见的错是生成一个值粘到所有字段，一把泄露等于全部泄露
#   3. production 的硬约束 —— ADMIN_ORIGINS 只能 https、MEDIA_SOURCE_BUCKET 不能等于公开桶等
#
# 刻意不打印任何密钥值，只报字段名。
set -euo pipefail

cd "$(dirname "$0")/.."
ENV_FILE=${1:-.env}
[[ -f $ENV_FILE ]] || { echo "找不到 $ENV_FILE" >&2; exit 1; }

# 取某个键的值（只认行首的 KEY=，值里允许有 =）
val() { sed -n "s/^$1=//p" "$ENV_FILE" | head -1; }

problems=0
warn() { printf '  ✗ %s\n' "$1"; problems=$((problems + 1)); }
note() { printf '  · %s\n' "$1"; }

# ---- 1. 必填项 ----
required=(
  IMAGE_PREFIX APPS_DOMAIN ADMIN_DOMAIN FEEDBACK_DOMAIN CERTBOT_EMAIL
  POSTGRES_PASSWORD XSITE_APP_PASSWORD XSITE_RO_PASSWORD VALKEY_PASSWORD
  DATABASE_URL VALKEY_URL APPS_VALKEY_URL
  S3_ENDPOINT S3_ACCESS_KEY S3_SECRET_KEY S3_BUCKET_MEDIA S3_BUCKET_ASSETS MEDIA_SOURCE_BUCKET
  BFF_SERVICE_TOKEN EDGE_SHARED_SECRET
  API_ENC_KEYS JWT_SIGNING_KEYS PLAY_TOKEN_KEYS MEDIA_KEKS STORAGE_KEKS
  LINE_SIGN_KEYS CONFIG_BACKUP_KEYS ADMIN_TOTP_KEKS
)
echo "必填项："
for k in "${required[@]}"; do
  v=$(val "$k")
  if [[ -z $v ]]; then warn "$k 为空"; fi
done
[[ ${problems} -eq 0 ]] && note "全部已填"

# 没填完比填空更隐蔽：.env.example 里的占位串看起来像个值，compose 也能解析过去，
# 然后在「连不上数据库」「证书邮件发给 example.com」这种地方才暴露。
echo "占位值："
placeholders=('example.com' '填上面的密码' '账户ID' 'ghcr.io/example' 'change-me')
found=0
for ph in "${placeholders[@]}"; do
  while IFS= read -r line; do
    [[ -z ${line} ]] && continue
    warn "${line%%=*} 还是模板里的占位值（含 ${ph}）"
    found=1
  done < <(grep -E "^[A-Z_]+=.*${ph}" "$ENV_FILE" || true)
done
[[ ${found} -eq 0 ]] && note "没有残留的占位值"

# ---- 2. 密钥 ----
echo "密钥："
# `引用:密钥` 形式，去掉引用前缀再比
secret_keys=(
  POSTGRES_PASSWORD XSITE_APP_PASSWORD XSITE_RO_PASSWORD VALKEY_PASSWORD
  BFF_SERVICE_TOKEN EDGE_SHARED_SECRET
  API_ENC_KEYS JWT_SIGNING_KEYS PLAY_TOKEN_KEYS MEDIA_KEKS STORAGE_KEKS
  LINE_SIGN_KEYS CONFIG_BACKUP_KEYS ADMIN_TOTP_KEKS
)
tmp=$(mktemp); trap 'rm -f "$tmp"' EXIT
for k in "${secret_keys[@]}"; do
  v=$(val "$k")
  [[ -z $v ]] && continue
  printf '%s\t%s\n' "${v#*:}" "$k" >> "$tmp"
done
dups=$(cut -f1 "$tmp" | sort | uniq -d)
if [[ -n $dups ]]; then
  while IFS= read -r d; do
    warn "这些字段共用同一个值：$(awk -F'\t' -v d="$d" '$1==d {printf "%s ", $2}' "$tmp")"
  done <<< "$dups"
else
  note "没有重复值"
fi

# 长度：启动校验要求这两项 ≥ 32
for k in BFF_SERVICE_TOKEN EDGE_SHARED_SECRET; do
  v=$(val "$k")
  [[ -n $v && ${#v} -lt 32 ]] && warn "$k 只有 ${#v} 个字符，启动校验要求至少 32"
done
# `引用:密钥` 的密钥部分同样不该太短
for k in API_ENC_KEYS JWT_SIGNING_KEYS PLAY_TOKEN_KEYS MEDIA_KEKS STORAGE_KEKS LINE_SIGN_KEYS CONFIG_BACKUP_KEYS ADMIN_TOTP_KEKS; do
  v=$(val "$k")
  [[ -z $v ]] && continue
  [[ $v != *:* ]] && { warn "$k 必须是 引用:密钥 的形式"; continue; }
  s=${v#*:}
  [[ ${#s} -lt 32 ]] && warn "$k 的密钥部分只有 ${#s} 个字符，建议至少 32（openssl rand -hex 32）"
done

# ---- 3. production 硬约束 ----
echo "production 约束："
admin_domain=$(val ADMIN_DOMAIN)
[[ -n $admin_domain && $admin_domain == http* ]] && warn "ADMIN_DOMAIN 只填域名，不要带 scheme（compose 会拼成 https://）"
src=$(val MEDIA_SOURCE_BUCKET); assets=$(val S3_BUCKET_ASSETS)
[[ -n $src && $src == "$assets" ]] && warn "MEDIA_SOURCE_BUCKET 不能等于公开读的 S3_BUCKET_ASSETS，后端启动校验会拒"
# 0 号库给后端、1 号库给 BFF，共用一个库会让两边的限流计数互相干扰
be=$(val VALKEY_URL); fe=$(val APPS_VALKEY_URL)
[[ -n $be && "$be" == "$fe" ]] && warn "VALKEY_URL 与 APPS_VALKEY_URL 指向同一个库，应分别用 /0 与 /1"
[[ -z $(val TRUSTED_PROXIES) ]] && warn "TRUSTED_PROXIES 为空，后端不会采信边缘层转发的客户端 IP"
# AUTO_MIGRATE / API_ENCRYPTION_REQUIRED / EGRESS_ALLOW_PRIVATE 刻意不在这里查：
# compose.yaml 把 AUTO_MIGRATE 写死成 false，另两个根本不往容器里传，在 .env 里写什么都不生效。
# 加一条「看着在管、其实不管」的检查比没有更糟 —— 会让人以为改 .env 就能改这些行为。
note "其余格式由后端启动时校验（production 拒绝 http 的 ADMIN_ORIGINS、拒绝 AUTO_MIGRATE、拒绝关闭报文加密）"

# ---- 4. 文件权限 ----
echo "文件权限："
# GNU stat 用 -c，BSD（macOS）用 -f；两边都可能跑这个脚本
mode=$(stat -c '%a' "$ENV_FILE" 2>/dev/null || true)
[[ -z ${mode} ]] && mode=$(stat -f '%Lp' "$ENV_FILE" 2>/dev/null || true)
# 变量一律写 ${VAR}：后面紧跟中文标点时，不加花括号会被当成变量名的一部分（实测报 unbound variable）
if [[ -z ${mode} ]]; then
    note "读不到 ${ENV_FILE} 的权限，自己确认是 600"
elif [[ ${mode} == 600 ]]; then
    note "${ENV_FILE} 权限 ${mode}"
else
    warn "${ENV_FILE} 权限是 ${mode}，应为 600：chmod 600 ${ENV_FILE}"
fi

echo
if [[ $problems -eq 0 ]]; then
  echo "自检通过。接下来 docker compose --env-file $ENV_FILE config >/dev/null 做一次结构校验。"
else
  echo "发现 $problems 个问题，修完再发布。"
  exit 1
fi
