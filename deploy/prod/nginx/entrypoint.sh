#!/bin/sh
# 边缘层容器入口。
#
# 做三件事：
#   1. 用环境变量渲染 server 块模板（域名、回源密钥不写死在镜像里）
#   2. 保证每个域名都有证书文件可用 —— 没有真证书时放一张自签的占位
#   3. 起 nginx，并每 6 小时重载一次（换上 certbot 续期后的新证书）
#
# 第 2 步是必须的：nginx 的 ssl_certificate 指向不存在的文件会直接启动失败，
# 而证书要等 certbot 通过 80 端口的 ACME 挑战才拿得到 —— 而挑战又要 nginx 先起来。
# 自签占位打破这个死循环：首次启动浏览器会报证书不受信，几十秒后 certbot 签发完成、
# 重载一次就正常了。
set -eu

TEMPLATE_DIR=/etc/nginx/templates
OUT_DIR=/etc/nginx/http.d
CERT_DIR=/etc/nginx/certs
REALIP_DIR=/etc/nginx/realip
LE_DIR=/etc/letsencrypt/live
RELOAD_INTERVAL=${NGINX_RELOAD_INTERVAL:-21600}

: "${APPS_DOMAIN:?必须设置 APPS_DOMAIN}"
: "${ADMIN_DOMAIN:?必须设置 ADMIN_DOMAIN}"
: "${FEEDBACK_DOMAIN:?必须设置 FEEDBACK_DOMAIN}"
: "${EDGE_SHARED_SECRET:?必须设置 EDGE_SHARED_SECRET（与 service 的同名变量一致）}"
APPS_ALIASES=${APPS_ALIASES:-}

export APPS_DOMAIN ADMIN_DOMAIN FEEDBACK_DOMAIN EDGE_SHARED_SECRET APPS_ALIASES

# envsubst 要显式列出替换哪些变量：nginx 配置里 $host、$remote_addr 这类内建变量
# 不能被当成环境变量吃掉。
SUBST='${APPS_DOMAIN} ${APPS_ALIASES} ${ADMIN_DOMAIN} ${FEEDBACK_DOMAIN} ${EDGE_SHARED_SECRET}'

render() {
    mkdir -p "$OUT_DIR"
    rm -f "$OUT_DIR"/*.conf
    for tpl in "$TEMPLATE_DIR"/*.conf.template; do
        name=$(basename "$tpl" .template)
        # 没有配别名就不要这个块：`server_name ;` 是语法错误
        if [ "$name" = "11-apps-alias.conf" ] && [ -z "$APPS_ALIASES" ]; then
            echo "边缘层：APPS_ALIASES 为空，跳过别名跳转块"
            continue
        fi
        envsubst "$SUBST" < "$tpl" > "$OUT_DIR/$name"
    done
    # 渲染结果里有回源密钥，不让别的用户读到
    chmod 600 "$OUT_DIR"/*.conf
}

# 把 certbot 签好的证书链到 nginx 读的位置；没有就生成自签占位。
# 用拷贝而不是软链：certbot 的 live 目录本身是软链，两层软链在容器里换卷时容易断。
link_certs() {
    changed=0
    for domain in "$APPS_DOMAIN" "$ADMIN_DOMAIN" "$FEEDBACK_DOMAIN"; do
        target="$CERT_DIR/$domain"
        mkdir -p "$target"
        if [ -s "$LE_DIR/$domain/fullchain.pem" ] && [ -s "$LE_DIR/$domain/privkey.pem" ]; then
            if ! cmp -s "$LE_DIR/$domain/fullchain.pem" "$target/fullchain.pem"; then
                cp -L "$LE_DIR/$domain/fullchain.pem" "$target/fullchain.pem"
                cp -L "$LE_DIR/$domain/privkey.pem"   "$target/privkey.pem"
                chmod 600 "$target/privkey.pem"
                echo "边缘层：$domain 已启用 Let's Encrypt 证书"
                changed=1
            fi
        elif [ ! -s "$target/fullchain.pem" ]; then
            openssl req -x509 -newkey rsa:2048 -sha256 -days 3650 -nodes \
                -keyout "$target/privkey.pem" -out "$target/fullchain.pem" \
                -subj "/CN=$domain" -addext "subjectAltName=DNS:$domain" >/dev/null 2>&1
            chmod 600 "$target/privkey.pem"
            echo "边缘层：$domain 暂用自签证书占位，等 certbot 签发"
            changed=1
        fi
    done
    return 0
}

# 站点挂在 Cloudflare 代理后面时，把 CF-Connecting-IP 还原成真实客户端 IP。
# nginx 没有条件 include，所以用「往目录里放/不放文件」来开关（nginx.conf 里 include 这个目录）。
realip() {
    mkdir -p "$REALIP_DIR"
    rm -f "$REALIP_DIR"/*.conf
    if [ "${EDGE_TRUST_CLOUDFLARE:-0}" = "1" ]; then
        cp /etc/nginx/snippets/cloudflare-ips.conf "$REALIP_DIR/cloudflare.conf"
        echo "边缘层：已信任 Cloudflare 回源网段，客户端 IP 取自 CF-Connecting-IP"
    else
        echo "边缘层：未信任任何代理，客户端 IP 取自 TCP 连接本身"
    fi
}

mkdir -p /var/www/certbot /var/log/nginx "$CERT_DIR"
# 静态产物的卷可能还没被一次性服务填上（首次启动、或那个服务失败），
# 目录不存在会让 nginx 对后台域名直接 500；先建空目录，让它回 404 更好排查。
mkdir -p /srv/admin/current /srv/feedback/current
render
realip
link_certs
nginx -t

# CI 用：渲染 + 语法检查到这里就够了，不起服务
if [ "${EDGE_CHECK_ONLY:-0}" = "1" ]; then
    echo "边缘层：配置检查通过（EDGE_CHECK_ONLY=1，不启动）"
    exit 0
fi

# 后台循环：定期把新证书换上并重载。reload 是平滑的，不断连接。
(
    while true; do
        sleep "$RELOAD_INTERVAL"
        link_certs || true
        nginx -t >/dev/null 2>&1 && nginx -s reload || echo "边缘层：重载被跳过（配置检查未通过）"
    done
) &

echo "边缘层：启动 nginx（用户端 $APPS_DOMAIN / 后台 $ADMIN_DOMAIN / 反馈 ${FEEDBACK_DOMAIN}）"
exec nginx -g 'daemon off;'
