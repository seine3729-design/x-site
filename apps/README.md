# Svelte

`svelte` 是 SvelteKit 应用。包管理和运行时使用 Bun 1.4 或更新版本，样式使用 Tailwind CSS 4，生产构建注册 PWA。本机如果还是 Bun 1.3，`bun run dev` 可以开，`build` 和 `start` 会直接提示升级。

在 `svelte` 目录执行：

```bash
bun install
bun run dev
```

浏览器打开 http://localhost:3001 。`apps` 使用 3000，两边可以同时开。

开发和线上分别读取 `.env.development`、`.env.production`。

```bash
bun run build:dev
bun run build:prod
bun run start
```

## 环境文件

密钥和某一套环境的差异不要写进上面两个文件。本地放 `.env.local`，某一套的差异放 `.env.development.local`、`.env.production.local`。这几个文件不会提交。

加载顺序是环境文件、`.env.local`、再是 `.env.<环境>.local`，后面的覆盖前面的。`scripts/with-env.ts` 负责载入，并强制把 `APP_ENV` 和 `PUBLIC_APP_ENV` 写成当前命令对应的环境。Vite 不再自己读 `.env.*`。

`PUBLIC_APP_ENV` 和 `PUBLIC_SITE_URL` 在构建时写进浏览器脚本。服务端的 `APP_ENV`、配额和 `EDGE_SHARED_SECRET` 在进程启动时读取。`build:dev` 之后要配 `start:dev`，`build` / `build:prod` 之后要配 `start` / `start:prod`。

线上不配置 `PUBLIC_SITE_URL`。需要绝对地址时用当次请求的 Host。静态资源要走 CDN 时，把 `CDN_ASSET_PREFIX` 设成 `https://` 开头的域名。

变量说明见 `.env.example`。

## 请求怎么过

每个请求先经过 `src/hooks.server.ts`：

- 按页面、读接口、写接口、探活和自动化客户端分桶限流。
- 写接口没有 `User-Agent` 时直接拒绝。
- 配置 `EDGE_SHARED_SECRET` 后，只有请求头 `x-edge-secret` 一致，才会采信 CDN 写来的国家头。
- 响应带上 `x-request-id`。生产构建再加上 HSTS 和内容安全策略。

`/api/v1/health` 返回 `{ ok, country, env }`，`Cache-Control` 为 `no-store`。CDN 探活用这个地址。

BFF 只透传前台接口 `/api/v1/*`；`/api/admin/*` 与其他 `/api/*` 在前台域名一律返回 404（`docs/07` 第 0.5 节）。

## 静态产物与 CDN

带内容哈希的产物（`build/client/_app/immutable/**`）由浏览器**直接从 CDN 加载**：页面里写的就是
`<CDN_ASSET_PREFIX>/_app/immutable/...`。SvelteKit 的版本轮询同样使用这个基址，因此构建生成的
`build/client/_app/version.json` 也会发布到 `<CDN_ASSET_PREFIX>/_app/version.json`，并在全部哈希产物成功后最后覆盖。
`CDN_ASSET_PREFIX` 是**构建期**变量，一个值管三处：

| 用在哪 | 作用 |
| --- | --- |
| `vite.config.ts` 的 `paths.assets` | 写进 HTML 与客户端脚本的产物地址 |
| `config/security-headers.ts` | 生产 CSP 的 `script-src` / `style-src` / `font-src` / `img-src` / `connect-src` |
| `scripts/build-sw.ts` | 注入 Service Worker，让它认得出哪些请求是产物 |

留空则产物走同源相对地址，`bun run build` + `bun run start` 可以独立跑起来，本机开发不需要 CDN。

**仍由应用容器同源提供的两样**：`/service-worker.js`（必须同源才有 `/` 作用域）与 `/icons/`。
`_app/version.json` 会随 `paths.assets` 使用 CDN 基址，由发布任务最后覆盖；它不能使用 immutable 长缓存。

CSP 的来源表达式里带路径时，不以 `/` 结尾就是**精确匹配那一个路径**（CSP3 §6.6.2.6），
所以 `https://cdn.example.com/apps` 必须补成 `.../apps/`，否则产物全被自己的 CSP 拦掉。
`cdnExpression()` 负责这件事，别把它简化成「去掉末尾斜杠」。

## PWA

生产构建生成 `/service-worker.js`，缓存名带上构建版本：新版本激活后删掉上一版缓存，并通知已打开的页面刷新。

- 页面导航先用缓存，同时在后台更新；都拿不到时返回预缓存的 `/offline`。
- 产物与 `/icons/` 缓存优先。产物可能是 CDN 绝对地址，所以收录条件是 `basic` 或 `cors`，仍然挡 `opaque`
  （CDN 的 CORS 没配好时就会退化成它：status 恒 0、存了读不出来还占配额）。
- `/api/` 不进缓存。
- 开发服务器不注册 Service Worker。

**源码在 `src/sw/index.ts`，不在 `src/service-worker/`，由 `scripts/build-sw.ts` 自己编译。**
SvelteKit 只要扫到自己管的 SW 入口文件、而 `paths.assets` 又非空，就直接以 `service_worker_assets`
退出；产物要走 CDN 就只能让 SW 脱离它的管辖。两条约束别动：

- **必须在 `vite build` 之前编译、输出到 `static/`**。adapter-bun 的静态路由表是在 `vite build` 时定下的，
  构建之后再往 `build/client/` 里塞文件，文件在磁盘上但 `/service-worker.js` 一律 404。
- 构建版本与产物前缀以 `__SW_VERSION__` / `__SW_ASSET_BASE__` 注入，声明在 `src/sw/global.d.ts`；
  这个目录的全局环境是 WebWorker 而不是 DOM，单独用 `src/sw/tsconfig.json` 检查（`bun run check` 已串上）。

安装图标在 `static/icons/`。站点刻意不提供 web app manifest（`docs/04` D 节）。

## Dokploy 部署

编排文件是 `docker-compose.yml`（本目录），域名 `duanju66.cc`，产物域名 `cdn.duanju66.cc`。
完整上线顺序见 `docs/14-部署上线流程.md`。

1. Dokploy 里新建 **Compose** 服务，Compose Type 选 `Docker Compose`
2. Provider 选 GitHub / Git，分支 `main`，**Compose Path** 填 `./apps/docker-compose.yml`
3. Environment 标签页填下表变量，然后 Deploy

两个服务，发布时按 `depends_on` 顺序跑：

| 服务 | 作用 | 生命周期 |
| --- | --- | --- |
| `assets` | `scripts/upload-assets.ts` 先发布 `build/client/_app/immutable`，成功后覆盖 `_app/version.json` | 一次性，任一步失败都不放流量进来 |
| `apps` | Bun SSR + BFF，监听 3001 | 常驻 |

```
浏览器 ──┬─ /_app/immutable/*  ───────────────────────→ cdn.duanju66.cc ─→ R2
         └─ 页面、/api/v1、/service-worker.js、/icons/ ─→ Traefik ─→ apps:3001 ─→ service:8080
```

| 变量 | 必填 | 说明 |
| --- | --- | --- |
| `SERVICE_INTERNAL_URL` | 是 | BFF 访问 Go 后端的内网地址，例如 `http://xsite-service-abc123:8080`。后端必须在同一张 `dokploy-network` 上 |
| `BFF_SERVICE_TOKEN` | 是 | 回源服务令牌，与 `service` 的同名变量完全一致（≥ 32 字符） |
| `R2_ENDPOINT` | 是 | `https://<账户ID>.r2.cloudflarestorage.com` |
| `R2_BUCKET` | 是 | `duanju666` |
| `R2_ACCESS_KEY_ID` / `R2_SECRET_ACCESS_KEY` | 是 | R2 访问密钥，权限按「只能读写这一个桶」最小化 |
| `EDGE_SHARED_SECRET` | 建议 | 设上之后才校验 `x-edge-secret`，否则任何人都能伪造地区头 |
| `APPS_XFF_DEPTH` | 否 | 默认 `1`：当前 Dokploy + Cloudflare 实测传给应用的 `X-Forwarded-For` 只有 1 段。代理拓扑变化后按实际收到的地址段数调整 |
| `CDN_HOST` | 否 | 默认 `cdn.duanju66.cc` |
| `CDN_ASSETS_PREFIX` | 否 | 默认 `apps`。既是 R2 的对象键前缀、也拼进构建参数 `CDN_ASSET_PREFIX` |
| `IMAGE_BASE_URLS` | 否 | 图片线路域名池，默认 `https://cdn.duanju66.cc`。生产 CSP 的 `img-src` 从这里生成 |
| `MEDIA_CONNECT_ORIGINS` | 否 | 视频切片线路域名池，默认同上。对应 CSP 的 `connect-src` |
| `APPS_DOMAIN` / `APPS_WWW_DOMAIN` | 否 | 默认 `duanju66.cc` / `www.duanju66.cc` |

几个容易踩的点：

- **域名二选一**：要么用文件里的 Traefik 标签（已配好 HTTPS、`www` → 主域名 301、80 → 443、`x-edge-secret` 注入），要么删掉 `labels` 段改用 Dokploy 的 Domains 标签页。两边都配会生成重复 router。
- **R2 的 CORS 必须包含站点域名**：产物是 `<script type="module">`，模块脚本一律按 CORS 模式取，没有 `Access-Control-Allow-Origin` 就一行都跑不起来。`deploy/prod/scripts/r2-setup.sh` 已经把站点域名、别名和 CDN 域名自己都列进白名单，改域名后重跑一次。
- **换 CDN 域名必须重新构建**：`CDN_ASSET_PREFIX` 是构建期写进 HTML、CSP 与 Service Worker 的，改运行期环境变量不生效。
- **`APPS_XFF_DEPTH` 大于实际地址数会 500**：当前 Dokploy + Cloudflare 实测应用收到 1 段 `X-Forwarded-For`，所以设 `1`；设 `2` 会报 `XFF_DEPTH is 2, but only found 1 addresses`。若以后代理链传入多段但深度仍过小，才会把边缘节点 IP 当成客户端、让后端限流失效。设了 `ADDRESS_HEADER` 后不经反向代理直接打容器（没有 `X-Forwarded-For`）也会 500，这是预期的；健康检查走 `/api/v1/health`，不取客户端 IP。
- **`RATE_LIMIT_*`、`APP_ENV`、`CDN_ASSET_PREFIX` 在容器环境变量里设了不生效**：`scripts/with-env.ts` 会用镜像里 `.env.production`（以及构建期写入的 `.env.production.local`）的同名值覆盖。要改配额就改那个文件再重新构建。
- **`assets` 和 `apps` 必须共用同一次构建**（编排里是同一个 `image` + 同一段 `build`）。两次独立构建的内容哈希对不上时，页面会去 CDN 取一个不存在的文件名，而且只在生产复现。
- **上传不删远端旧哈希对象**，老页面引用的 chunk 才不会在发布瞬间 404；`version.json` 是例外，每次发布都在最后覆盖。Cloudflare 对精确路径 `/apps/_app/version.json` 绕过缓存，其他清理由 `r2-setup.sh` 的 `apps/` 前缀 90 天生命周期规则负责。
- **不要给 Traefik 加 compress 中间件**：HTML 由 `src/lib/server/compress.ts` 流式压（br q5），容器里剩下的静态文件带 Brotli 11 预压缩文件，产物的压缩由 CDN 边缘做。再套一层只会降档。
- Traefik 默认只开 HTTP/1.1 与 HTTP/2，HTTP/3 要在 Dokploy 的 Traefik 配置里单独开并放通 443/udp。
