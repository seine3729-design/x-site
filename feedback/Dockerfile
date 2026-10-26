# 反馈社区前台生产镜像（Next.js 16 + React 19）。
# 构建：docker build --build-arg NEXT_ASSET_PREFIX=https://cdn.example.com/feedback -t xsite/feedback:<版本> feedback/
#
# 为什么不是静态导出：next.config.ts 开了 cacheComponents（PPR），列表、详情、我的、设置页都显式
# 标了 instant = false 按访问者渲染，必须有 Node 进程。只有 .next/static 可以放 CDN。
#
# 运行时用 Node 而不是 Bun：Bun 跑 Next.js standalone 还没在本项目实测过
# （feedback/docs/12 第 7 节的 X0-1 待办），Bun 只当包管理器用。
FROM node:24-bookworm-slim AS build
WORKDIR /app
ENV NEXT_TELEMETRY_DISABLED=1

RUN npm i -g bun@1.4.4 --silent

COPY package.json bun.lock ./
RUN bun install --frozen-lockfile

COPY . .

ARG NEXT_ASSET_PREFIX=""
# NEXT_OUTPUT=standalone 只在这里设：next.config.ts 里有说明，无条件打开会让 `next start`
# 与首屏预算检查失效。build 末尾会跑 scripts/precompress.mjs 产出 .br / .gz（Brotli 11）。
RUN NEXT_ASSET_PREFIX="${NEXT_ASSET_PREFIX}" NEXT_OUTPUT=standalone bun run build

# ------------------------------------------------------------------ 运行镜像
FROM node:24-bookworm-slim AS runtime
WORKDIR /app
ENV NODE_ENV=production \
    NEXT_TELEMETRY_DISABLED=1 \
    PORT=3002 \
    HOSTNAME=0.0.0.0

# standalone 不含这两份，必须单独拷（见 next.config.ts 的说明）
COPY --from=build /app/.next/standalone ./
COPY --from=build /app/.next/static ./.next/static
COPY --from=build /app/public ./public

# 把 .next/static 导出给边缘层直接发文件用（compose 里的 feedback-assets 一次性服务会覆盖
# entrypoint 调它）。next start 不会去找构建期压好的 .br，只会现压 gzip。
COPY export-assets.sh /usr/local/bin/export-assets.sh
RUN chmod +x /usr/local/bin/export-assets.sh

USER node
EXPOSE 3002
HEALTHCHECK --interval=10s --timeout=5s --start-period=20s --retries=6 \
  CMD node -e "fetch('http://127.0.0.1:'+process.env.PORT+'/').then(r=>process.exit(r.ok?0:1),()=>process.exit(1))"
CMD ["node", "server.js"]
