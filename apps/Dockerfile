# 前台 Web 生产镜像（SvelteKit 3 + @sveltejs/adapter-bun）。
# 构建：docker build --build-arg CDN_ASSET_PREFIX=https://cdn.example.com/apps -t xsite/apps:<版本> apps/
#
# 为什么不是纯静态站：adapter 是 adapter-bun，产物需要 Bun 进程跑 SSR 与 BFF（/api/v1 透传）。
# 只有 build/client/ 是可以放 CDN 的静态产物，由发布流程另外上传（deploy/prod/scripts/cdn-upload.sh）。
#
# 构建期注入的三个值必须写进 .env.production.local 而不是只 export 环境变量：
# scripts/with-env.ts 会用 .env.production 的内容覆盖已有的同名环境变量，而那份已提交的文件里
# CDN_ASSET_PREFIX 是空的。同一个文件留在运行镜像里，保证构建期改写的资源地址与运行期 CSP、
# 页面下发的 cdn 字段取到同一个值（不一致的话生产环境脚本会被自己的 CSP 拦掉）。
FROM oven/bun:1.4.2 AS build
WORKDIR /app

# 依赖层单独缓存：锁文件没变就不重装
COPY package.json bun.lock ./
RUN bun install --frozen-lockfile

COPY . .

# 只有这两个值需要进文件：
#   CDN_ASSET_PREFIX  构建期决定资源地址，运行期决定 CSP 与页面下发的 cdn 字段，两边必须一致
#   PUBLIC_SITE_URL   static: true，构建时就写进浏览器脚本了（线上一般留空，用请求的 Host）
# IMAGE_BASE_URLS、MEDIA_CONNECT_ORIGINS 等是运行期读的，刻意**不**写进来 ——
# 写进来就会盖掉 compose 注入的值，换一条图片线路要重新构建镜像。
#
# 注意 CDN_ASSET_PREFIX 目前必须留空：本站有 Service Worker，SvelteKit 不允许它和
# paths.assets 共存，填了这一项构建会直接失败（apps/.env.production 里有完整说明）。
ARG CDN_ASSET_PREFIX=""
ARG PUBLIC_SITE_URL=""
RUN { \
      echo "CDN_ASSET_PREFIX=${CDN_ASSET_PREFIX}"; \
      echo "PUBLIC_SITE_URL=${PUBLIC_SITE_URL}"; \
    } > .env.production.local \
 && bun run build

# ------------------------------------------------------------------ 运行镜像
# alpine 变体：带 shell 便于排查，体积远小于 debian 版。产物是纯 JS，换 libc 没有影响。
FROM oven/bun:1.4.2-alpine AS runtime
WORKDIR /app
ENV NODE_ENV=production \
    APPS_PORT=3001

# adapter-bun 的产物自带服务端代码，运行时不需要 node_modules。
# package.json / scripts / .env.production* 是 `bun run start` 需要的（见上面的注释）。
COPY --from=build /app/build ./build
COPY --from=build /app/package.json ./package.json
COPY --from=build /app/scripts/with-env.ts ./scripts/with-env.ts
COPY --from=build /app/.env.production ./.env.production
COPY --from=build /app/.env.production.local ./.env.production.local

USER bun
EXPOSE 3001
# BFF 自己实现的 /api/v1/health 只检查本进程，不打后端，适合做容器存活判断
HEALTHCHECK --interval=10s --timeout=3s --start-period=20s --retries=6 \
  CMD bun -e "fetch('http://127.0.0.1:3001/api/v1/health').then(r=>process.exit(r.ok?0:1),()=>process.exit(1))"
CMD ["bun", "run", "start"]
