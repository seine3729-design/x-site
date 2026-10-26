#!/bin/sh
# 证书容器：首次签发 + 定期续期。走 HTTP-01（webroot），挑战文件写进和 nginx 共享的卷。
#
# 三张证书，刻意不合成一张：用户端、后台、反馈社区的域名与入口是分开的（docs/00 第 2 节），
# 合成一张会让任一域名的证书透明日志暴露另外两个。
#
# 失败时退避 1 小时再试，不重试风暴：Let's Encrypt 对「验证失败」有每小时 5 次的限制，
# 容器反复重启硬撞这个限制会把账号锁在外面，比晚一小时拿到证书糟糕得多。
set -eu

: "${APPS_DOMAIN:?必须设置 APPS_DOMAIN}"
: "${ADMIN_DOMAIN:?必须设置 ADMIN_DOMAIN}"
: "${FEEDBACK_DOMAIN:?必须设置 FEEDBACK_DOMAIN}"
: "${CERTBOT_EMAIL:?必须设置 CERTBOT_EMAIL（证书到期提醒邮箱）}"
APPS_ALIASES=${APPS_ALIASES:-}
RENEW_INTERVAL=${CERTBOT_RENEW_INTERVAL:-43200}
WEBROOT=/var/www/certbot

STAGING_ARG=""
if [ "${CERTBOT_STAGING:-0}" = "1" ]; then
    STAGING_ARG="--staging"
    echo "证书：使用 Let's Encrypt staging 环境（证书不受浏览器信任，仅用于演练流程）"
fi

# 把 "a b c" 变成 "-d a -d b -d c"
domain_args() {
    for d in $*; do
        printf -- '-d %s ' "$d"
    done
}

issue() {
    cert_name=$1
    shift
    if [ -s "/etc/letsencrypt/live/$cert_name/fullchain.pem" ]; then
        return 0
    fi
    echo "证书：开始签发 ${cert_name}（$*）"
    # shellcheck disable=SC2046,SC2086
    certbot certonly \
        --webroot --webroot-path "$WEBROOT" \
        --cert-name "$cert_name" \
        $(domain_args "$@") \
        --email "$CERTBOT_EMAIL" \
        --agree-tos --no-eff-email \
        --non-interactive \
        --keep-until-expiring \
        --key-type ecdsa \
        $STAGING_ARG
}

mkdir -p "$WEBROOT"

# 等 nginx 把 80 端口接起来，否则第一次挑战必然失败
i=0
while [ "$i" -lt 30 ]; do
    if wget -q -O /dev/null "http://nginx/healthz" 2>/dev/null; then
        break
    fi
    i=$((i + 1))
    sleep 2
done

while true; do
    failed=0

    issue "$APPS_DOMAIN" "$APPS_DOMAIN" $APPS_ALIASES || failed=1
    issue "$ADMIN_DOMAIN" "$ADMIN_DOMAIN" || failed=1
    issue "$FEEDBACK_DOMAIN" "$FEEDBACK_DOMAIN" || failed=1

    # renew 只处理 30 天内到期的，没到期是空操作
    certbot renew --webroot --webroot-path "$WEBROOT" --non-interactive --no-random-sleep-on-renew || failed=1

    if [ "$failed" = "1" ]; then
        echo "证书：这一轮有失败项（常见原因：域名还没解析到本机、80 端口被挡、DNS 还没生效）。1 小时后重试"
        sleep 3600
        continue
    fi

    sleep "$RENEW_INTERVAL"
done
