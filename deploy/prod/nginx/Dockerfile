# 边缘层：TLS 终止、HTTP→HTTPS、按域名分流、Brotli/Gzip 压缩、ACME 挑战。
#
# 为什么用 alpine 基础镜像而不是官方 nginx 镜像：需要 ngx_brotli。官方镜像不带这个模块，
# 自己编译要和镜像里的 nginx 版本严格对齐；Alpine 的 nginx 与 nginx-mod-http-brotli 来自同一个
# 仓库同一个版本，装上就能用，不用编译（deploy/edge/feedback.conf 里记的「需要编译进 nginx」
# 指的是官方包的情况）。
FROM alpine:3.22

RUN apk add --no-cache \
      nginx \
      nginx-mod-http-brotli \
      gettext \
      openssl \
      curl \
      tzdata \
 && rm -f /etc/nginx/http.d/default.conf

COPY nginx.conf /etc/nginx/nginx.conf
COPY snippets/ /etc/nginx/snippets/
COPY templates/ /etc/nginx/templates/
COPY entrypoint.sh /usr/local/bin/entrypoint.sh
RUN chmod +x /usr/local/bin/entrypoint.sh

EXPOSE 80 443
HEALTHCHECK --interval=15s --timeout=5s --start-period=10s --retries=5 \
  CMD curl -fsS -o /dev/null http://127.0.0.1/healthz || exit 1
ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]
