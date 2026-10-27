# 运营后台生产镜像（React 19 + Vite 8，纯静态 SPA）。
# 构建：docker build --build-arg ADMIN_ASSET_BASE=https://cdn.example.com/admin/ -t xsite/admin:<版本> admin/
#
# 后台页面必须和 /api/admin 同源：会话是 HttpOnly Cookie，后端还要校验 Origin 白名单与 CSRF
# （docs/03 第 2.3 节）。所以 index.html 只能由后台域名发出，不能整站搬到 CDN —— 只有带内容哈希的
# assets/ 走 CDN（由 Vite 的 base 决定）。
#
# 这个镜像**不跑 web 服务器**：它只是产物的载体。启动时把 dist 导出到挂载的卷，
# 由边缘层那个带 ngx_brotli 的 nginx 直接发文件（官方 nginx 镜像没有 brotli 模块，
# 再套一层代理还会把构建期压好的 .br 换成运行时现压的低档 Brotli）。
FROM node:24-bookworm-slim AS build
WORKDIR /app
ENV CI=true \
    COREPACK_ENABLE_DOWNLOAD_PROMPT=0

COPY package.json pnpm-lock.yaml ./
RUN corepack enable \
 && corepack prepare pnpm@10.34.7 --activate \
 && pnpm install --frozen-lockfile

COPY . .

# 末尾要带斜杠：Vite 直接把它拼在资源相对路径前面
ARG ADMIN_ASSET_BASE="/"
# build 脚本末尾会跑 scripts/precompress.mjs，产出 .br 与 .gz（Brotli 11）。
# sourcemap 挪出 dist，而不是构建完再从运行镜像里删：
# 删只会加一层白障，文件仍留在下层镜像里，能 docker pull 的人就能把源码翻出来。
# 需要上传错误监控时用 `docker build --target build` 单独取。
RUN ADMIN_ASSET_BASE="${ADMIN_ASSET_BASE}" pnpm build \
 && mkdir -p /sourcemaps \
 && find dist -name '*.map' -exec mv {} /sourcemaps/ \;

# ------------------------------------------------------------------ 页面服务器（Dokploy 路径）
# docker-compose.yml 用这个阶段：后台域名直接由它发 index.html 与兜底的静态文件，
# TLS 和域名由 Traefik 负责。配置与「为什么自己带一层 nginx」见 admin/nginx.conf 的注释。
FROM alpine:3.22 AS serve
RUN apk add --no-cache nginx nginx-mod-http-brotli curl \
 && rm -f /etc/nginx/http.d/default.conf
COPY nginx.conf /etc/nginx/nginx.conf
COPY --from=build /app/dist /srv/admin
EXPOSE 8080
HEALTHCHECK --interval=15s --timeout=3s --start-period=5s --retries=3 \
  CMD curl -fsS -o /dev/null http://127.0.0.1:8080/healthz || exit 1
CMD ["nginx", "-g", "daemon off;"]

# ------------------------------------------------------------------ 产物上 CDN（Dokploy 路径）
# compose 里的 assets 一次性服务。用 Bun 是因为它自带 S3 客户端，不用再装 aws-cli。
FROM oven/bun:1.4.2-alpine AS assets-uploader
WORKDIR /app
COPY --from=build /app/dist ./dist
COPY scripts/upload-assets.ts ./scripts/upload-assets.ts
ENTRYPOINT ["bun", "scripts/upload-assets.ts"]

# ------------------------------------------------------------------ 产物载体（deploy/prod 路径）
# 这个阶段**必须留在最后**：它是不带 --target 构建时的默认目标，
# .github/workflows/deploy.yml 与 deploy/prod/compose.yaml 的 admin-assets 都依赖它。
FROM alpine:3.22 AS runtime
COPY --from=build /app/dist /app/dist
COPY export-assets.sh /usr/local/bin/export-assets.sh
RUN chmod +x /usr/local/bin/export-assets.sh
ENTRYPOINT ["/usr/local/bin/export-assets.sh"]
