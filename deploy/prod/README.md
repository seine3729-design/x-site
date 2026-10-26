# 生产部署（GitHub Actions + Docker + nginx + R2）

> 对应 `docs/11` 的 X0-3（CI 流水线）与 X0-6（容器编排、域名与证书自动化）。
> 最后核对：2026-10-26
>
> **第一次部署请按 [`docs/14-部署上线流程.md`](../../docs/14-部署上线流程.md) 的顺序走**：
> 那份文档是从「代码还在本机」到「三个域名可访问」的操作手册（推 GitHub、Secrets、
> 服务器初始化、DNS、验收清单、排查表）。本文是机制说明与文件清单，用来查「为什么这么设计」。

## 1. 结构

```
                  ┌──────────── Cloudflare R2（桶 duanju666）+ cdn.duanju66.cc ───────────┐
                  │  admin/assets/**          带内容哈希，immutable 一年                  │
                  │  feedback/_next/static/** 同上                                        │
                  │  static/logo.webp 等品牌资源（已有，和媒体资源同桶）                   │
                  └──────────────────────────────────────────────────────────────────────┘
                                   ▲ 浏览器直接取（CORS 白名单 + preconnect）
                                   │
  浏览器 ──► 443（HTTP/2 + HTTP/3）──► nginx（边缘层，容器，带 ngx_brotli）
                       ├─ duanju66.cc            → apps:3001      SSR + BFF（/api/v1 透传到 Go）
                       │    ↑ 这个域名整体挂在 Cloudflare 代理后面，/_app/immutable/* 由边缘缓存
                       ├─ www.duanju66.cc        → 301 跳主域
                       ├─ admin.duanju66.cc      /              ← 边缘层直接发 /srv/admin（卷）
                       │                         /api/admin/*   → service:8080（注入 x-edge-secret）
                       └─ feedback.duanju66.cc   /_next/static/ ← 边缘层直接发 /srv/feedback（卷）
                                                 /              → feedback:3002  Next.js
                                                 /open-api/feedback → service:8080

  私有网络（compose）：service · worker · media-worker · postgres · valkey
  80 端口只做 ACME 挑战与 HTTPS 跳转；443 上 Host 不认识的请求直接 444 断开
```

后台与反馈社区的静态产物通过 compose 卷交给边缘层**直接从磁盘发**，不再多一跳反向代理：
那一层 nginx 带 `ngx_brotli`，能把构建期压好的 Brotli 11 文件原样送出去（官方 nginx 镜像没有这个
模块，经它代理还会把预压缩换成运行时现压的低档 Brotli）。卷由一次性服务 `admin-assets` /
`feedback-assets` 在每次发布时从镜像里导出。

镜像全部由 CI 构建并推到 GHCR，服务器上只需要 `compose.yaml` 与 `.env`：

| 镜像 | 来源 | 内容 |
| --- | --- | --- |
| `service` | `service/Dockerfile`（默认目标） | api + worker + admintool，distroless 非 root |
| `media-worker` | `service/Dockerfile` `--target media-worker` | 带 FFmpeg 的转封装 worker |
| `postgres` | `deploy/local/postgres/` | PG 18 + pg_bigm（与本地开发同一个 Dockerfile） |
| `apps` | `apps/Dockerfile` | Bun 跑 adapter-bun 产物 |
| `admin` | `admin/Dockerfile` | 只装 `dist/`（含 `.br`/`.gz`）的产物载体，启动即导出到卷后退出，不跑服务 |
| `feedback` | `feedback/Dockerfile` | Node 跑 Next.js standalone |
| `edge` | `deploy/prod/nginx/` | Alpine nginx + ngx_brotli + envsubst 模板 |
| `certbot` | `deploy/prod/certbot/` | 签发与续期循环 |

`scripts/` 下的脚本：

| 脚本 | 在哪跑 | 干什么 |
| --- | --- | --- |
| `deploy.sh` | 服务器 | 一次发布：拉镜像 → 迁移 → 起服务 → 重载 nginx → 验活。也是回滚入口 |
| `gen-secrets.sh` | 服务器 | 打印 14 个密钥字段，每个独立随机值。只输出到标准输出，不写文件 |
| `check-env.sh` | 服务器 | 发布前自检 `.env`：空值、密钥重复与长度、production 约束、文件权限 |
| `cdn-upload.sh` | CI | 把一个前端的静态产物同步到 R2 |
| `r2-setup.sh` | 本机，一次性 | 设置桶的 CORS 与生命周期 |
| `update-cloudflare-ips.sh` | 本机 | 刷新 `nginx/snippets/cloudflare-ips.conf` |

## 2. 首次部署

### 2.1 服务器

需要 Docker 与 Docker Compose v2，80/443 对公网开放。

```bash
sudo mkdir -p /opt/xsite && sudo chown "$USER" /opt/xsite
# 先手工放一份配置；之后每次发布由 CI 覆盖 deploy/prod 下除 .env 以外的文件
mkdir -p /opt/xsite/geoip
cp .env.example /opt/xsite/.env   # 从仓库里取
chmod 600 /opt/xsite/.env
# 按 .env 里的注释逐项填值。密钥一律 openssl rand -hex 32，每项单独生成
```

GeoIP：把 ip2region 的 `ip2region.xdb` 放到 `/opt/xsite/geoip/`。没有这个文件时地区一律按未知处理，只有 `region_scope=global` 的兜底线路会命中。

### 2.2 DNS

三个域名的 A 记录指向服务器公网 IP。

`duanju66.cc` 与 `www.duanju66.cc` 走 **Cloudflare 代理（橙云）**：apps 的静态资源靠边缘缓存加速，
不用独立 CDN 域名（第 4 节）。后台与反馈社区可以代理也可以直连，不影响功能。

走代理时要知道的三件事：

- HTTP-01 挑战不受影响（Cloudflare 不会把 `/.well-known/acme-challenge/` 强制跳 HTTPS）；
- SSL/TLS 模式必须选 **Full (strict)**，源站上的 Let's Encrypt 证书正好满足；
- 客户端真实 IP 会变成 Cloudflare 边缘节点 —— `.env` 里 `EDGE_TRUST_CLOUDFLARE=1` 把它还原回来。
  边缘层只信任 `nginx/snippets/cloudflare-ips.conf` 里的网段再取 `CF-Connecting-IP`；
  **不要在没有来源白名单的情况下直接信任这个头**，那等于让任何人伪造客户端 IP。

`cdn.duanju66.cc` 是 R2 的自定义域，由 Cloudflare 直接服务，不经过这台服务器。

### 2.3 GitHub 仓库配置

Secrets：

| 名称 | 用途 |
| --- | --- |
| `DEPLOY_HOST` / `DEPLOY_USER` | SSH 目标 |
| `DEPLOY_SSH_KEY` | 部署私钥（服务器上对应公钥放 `~/.ssh/authorized_keys`） |
| `DEPLOY_KNOWN_HOSTS` | `ssh-keyscan -H <服务器>` 的输出。**必填**，这样才不用 `StrictHostKeyChecking=no` |
| `R2_ACCOUNT_ID` | R2 账户 ID，用于拼 S3 endpoint |
| `R2_ACCESS_KEY_ID` / `R2_SECRET_ACCESS_KEY` | R2 访问密钥（只需要这个桶的读写） |

Variables：

| 名称 | 示例 | 说明 |
| --- | --- | --- |
| `CDN_BASE` | `https://cdn.duanju66.cc` | **不设也行**，默认就是这个值；只在换域名时才需要设 |
| `R2_BUCKET` | `duanju666` | |
| `DEPLOY_PATH` | `/opt/xsite` | |
| `APPS_DOMAIN` / `ADMIN_DOMAIN` / `FEEDBACK_DOMAIN` | | 只用于发布后的对外验活 |

GHCR 不需要额外令牌：CI 用 `GITHUB_TOKEN` 推送，部署时把同一个临时令牌通过 SSH 传给服务器登录，用完 `docker logout`。

### 2.4 R2 一次性设置

```bash
R2_ENDPOINT=https://<账户ID>.r2.cloudflarestorage.com \
R2_BUCKET=duanju666 \
AWS_ACCESS_KEY_ID=... AWS_SECRET_ACCESS_KEY=... \
APPS_DOMAIN=duanju66.cc APPS_ALIASES=www.duanju66.cc \
ADMIN_DOMAIN=admin.duanju66.cc FEEDBACK_DOMAIN=feedback.duanju66.cc \
CDN_DOMAIN=cdn.duanju66.cc \
  ./scripts/r2-setup.sh
```

必须做，不是可选项：三个前端的入口脚本都是 `<script type="module">`，而模块脚本一律按 CORS 模式取。R2 不回 `Access-Control-Allow-Origin` 的话页面一行都跑不起来。

`put-bucket-cors` 是**整体替换**。这个桶同时放媒体资源（`img/`、`video/` 前缀），所以脚本里把 CDN 域名自己也列进白名单了；加新域名时改脚本，不要手工在控制台改，否则下次跑脚本又被覆盖回去。

### 2.5 第一次发布

推主干（或手动触发 Deploy 工作流）。首次会出现：

1. nginx 用自签证书先起来（日志里有「暂用自签证书占位」）；
2. certbot 拿到证书，最多 6 小时后由 nginx 的重载循环换上 —— 不想等就 `docker compose restart nginx`；
3. 这期间「对外验活」步骤会因为证书不受信而失败，这是预期的。

想先演练流程又不占 Let's Encrypt 额度，把 `.env` 的 `CERTBOT_STAGING` 设成 `1`。

首个超级管理员：

```bash
cd /opt/xsite
read -rs -p "密码: " PW && echo
printf '%s\n' "${PW}" | docker compose --env-file .env \
  run --rm -T --entrypoint /app/admintool service create-admin -username root -role superadmin
unset PW
```

## 3. 日常发布与回滚

CI 自动完成；手工等价操作：

```bash
cd /opt/xsite
./scripts/deploy.sh <镜像标签>      # 标签是 git 短 SHA 前 12 位
./scripts/deploy.sh <上一个标签>    # 回滚
```

`deploy.sh` 的顺序不要改：

- **先迁移再起服务**。production 禁用 `AUTO_MIGRATE`（启动校验会拒），库必须先到位。迁移失败就停在这里，旧版本还在跑。
- **最后重载 nginx**。开源版 nginx 只在启动/重载时解析 `upstream` 里的服务名（`server x:1 resolve;` 是 nginx Plus 的功能）。重建过的容器 IP 变了而 nginx 不知道，会一直 502。

数据库迁移走 `admintool migrate`（嵌入式 goose，只升不降）。迁移二进制和 API 二进制来自同一次构建，所以 schema 必然和即将启动的代码配套，不需要另做一个带 goose 的镜像。

## 4. 静态资源、CDN 与压缩

静态资源域名固定为 **`https://cdn.duanju66.cc`**（R2 桶 `duanju666` 的自定义域）。

| 端 | 放 CDN 的部分 | 机制（构建期注入） |
| --- | --- | --- |
| `admin/` | `dist/assets/**`、`dist/images/**` | Vite `base` ← `ADMIN_ASSET_BASE` |
| `feedback/` | `.next/static/**` | Next.js `assetPrefix` ← `NEXT_ASSET_PREFIX` |
| `apps/` | 不用独立 CDN 域名，见下 | — |

三端都**不能**整站搬到 CDN：

- `apps/` 是 `adapter-bun`，SSR 与 BFF 都要 Bun 进程；
- `feedback/` 开了 `cacheComponents`（PPR），列表、详情、我的、设置页显式标了 `instant = false`，必须有 Node 进程；
- `admin/` 虽然是纯静态 SPA，但会话是 HttpOnly Cookie，后端还校验 Origin 白名单与 CSRF，`index.html` 必须和 `/api/admin` 同源。

### apps 用反向代理型 CDN，不用独立资源域名

SvelteKit 不允许 Service Worker 和 `paths.assets` 共存。填了 `CDN_ASSET_PREFIX`，`bun run build` 直接报错退出：

```
SvelteKit error: service_worker_assets
Cannot use service worker alongside `config.paths.assets`
```

`apps/` 有 Service Worker（`docs/04` D 节：离线兜底 + `/_app/immutable/*` 缓存优先），保留它。
所以 **`duanju66.cc` 整体挂到 Cloudflare 代理（橙云）后面**，`/_app/immutable/*` 在同源地址上被
边缘缓存，Service Worker 照常工作。`docs/00` 的架构图本来就是这个形状。

这条路线要做三件事：

1. DNS 上把 `duanju66.cc` 与 `www.duanju66.cc` 的 A 记录切成**代理模式（橙云）**；SSL/TLS 选
   **Full (strict)**（源站上的 Let's Encrypt 证书正好满足）。
2. `.env` 里 `EDGE_TRUST_CLOUDFLARE=1`。否则客户端 IP 全是 Cloudflare 的边缘节点，限流与风控会把
   所有访客当成同一个来源。打开后边缘层只信任 `nginx/snippets/cloudflare-ips.conf` 里的网段，
   并取 `CF-Connecting-IP`；网段会变，升级镜像时顺手跑一次 `./scripts/update-cloudflare-ips.sh` 看 diff。
   顺带：apps 的国家识别读的就是 Cloudflare 的 `CF-IPCountry`，边缘层注入 `x-edge-secret` 后它才采信。
3. Cloudflare 上加两条 Cache Rule：
   - 匹配 `/_app/immutable/*` 与 `/icons/*` → Eligible for cache，Edge TTL 跟随源站头（源站已给
     `immutable` 一年）。
   - HTML **不要缓存**：SSR 页面随登录用户变化（Cloudflare 默认也不缓存 HTML，这条是防止有人
     后来加了「Cache Everything」）。

### 压缩：优先 Brotli，不支持时回退 Gzip

构建期就把静态产物压成 `.br` 与 `.gz`（Brotli 11 / Gzip 9，离线压一次，运行时零 CPU）：

| 端 | 预压缩由谁做 | 运行时由谁发 |
| --- | --- | --- |
| `apps/` | `adapter-bun` 的 `precompress: true` | adapter 自己按 `Accept-Encoding` 送 `.br`/`.gz` |
| `admin/` | `scripts/precompress.mjs`（串在 `pnpm build` 后，**含 `.html`**） | 边缘层 `brotli_static`/`gzip_static` 从卷里直发 |
| `feedback/` | `scripts/precompress.mjs`（串在 `bun run build` 后） | 同上 |

动态内容（SSR 出来的 HTML）没法预压缩，按请求实时压，档位用 5 不用 11（11 的 CPU 代价每个请求都要付
一遍，而体积只小约 12%）：

- **apps 的 HTML 由 apps 自己压**（`src/lib/server/compress.ts`，`node:zlib` 流式、线程池、br 优先）。
  所以边缘层对 apps **刻意不清** `Accept-Encoding` —— 清了的话它的静态文件会退化成「回明文让 nginx 现压」，
  从 Brotli 11 变成 5。
- **feedback 的 HTML 由边缘层压**。Next.js 的 `compress: true` 只会 gzip，也不认预压缩文件，
  所以这里必须把 `Accept-Encoding` 清成空字符串，让它回明文、由 nginx 上 Brotli。
  （要空字符串，不是不写：nginx 看到响应已带 `Content-Encoding` 就不再处理。）

实测的实际传输量（边缘层上用同一个 URL 分别带 `Accept-Encoding: identity / gzip / br`）：

| 内容 | 明文 | Gzip | Brotli | br 比 gzip 再省 |
| --- | --- | --- | --- | --- |
| apps SSR HTML | 108170 | 19624 | 16401 | 16.4% |
| apps 入口 chunk | 548 | 320 | 297 | 7.2% |
| admin `index.html` | 2491 | 978 | 696 | 28.8% |
| admin 主 chunk | 266597 | 83484 | 71710 | 14.1% |
| feedback SSR HTML | 216950 | 34559 | 18291 | 47.1% |
| feedback 最大 chunk | 229627 | 71765 | 61437 | 14.4% |

只送 `Accept-Encoding: gzip` 时全部正确回退 `content-encoding: gzip`，两个都不送时回明文。
后台主 chunk 的 Brotli 响应字节数与磁盘上的 `.br` 完全一致（71710），证明走的是预压缩直发而不是现压。

### 上传规则

`scripts/cdn-upload.sh` 刻意不传三类文件：

- `*.br` / `*.gz` —— 在 R2 上是独立的对象键，CDN 不会按 `Accept-Encoding` 在不同键之间选，传上去只会
  变成「把 Brotli 字节当 JS 发出去」。实测 Cloudflare 边缘自己会压，`content-encoding: br` 正常返回。
  这些文件是给边缘层 `brotli_static` 用的（CDN 挂了或直连源站时走那条路）。
- `*.map` —— `admin` 是 `sourcemap: 'hidden'`，公开出去等于把源码送人。`admin/Dockerfile` 在构建阶段就把
  它们挪到 `/sourcemaps`（不是从运行镜像里删 —— 删只加一层白障，文件仍在下层镜像里）。要上传错误监控时用
  `docker build --target build` 取。
- `index.html` —— 必须由各自的站点域名发出。

也刻意**不加** `--delete`：带内容哈希的旧产物要留着，否则刚发布的瞬间，老页面还在引用的 chunk 就 404 了。
清理交给 `r2-setup.sh` 设的生命周期规则（90 天）。

缓存头分两档：带内容哈希的子树 `max-age=31536000, immutable`，其余 `max-age=300, stale-while-revalidate=86400`。

### 首屏相关的其他措施

- **HTTP/3（QUIC）**：边缘层 `listen 443 quic` + `http3 on`，并下发 `Alt-Svc: h3=":443"`。
  compose 必须同时映射 `443:443/udp`，否则浏览器拿到公告也连不上、静默退回 HTTP/2。
- **preconnect**：`admin/index.html` 与 `feedback/src/app/layout.tsx` 各有两条指向 `cdn.duanju66.cc` 的
  `preconnect`。写两条不是笔误 —— 浏览器的连接池按「是否带 CORS」分开，模块脚本一律按 CORS 模式取，
  而 `<img>`/`<link rel=icon>` 取 Logo 是 no-cors，只写一条另一类请求仍要重新建连接。
- **feedback 的 Service Worker 认 CDN**：`public/sw.js` 原来只缓存同源请求，CDN 一开静态缓存就等于没有了。
  现在它把 `cdn.duanju66.cc` 列入 `ASSET_ORIGINS`，并接受 `response.type === 'cors'`（仍然挡住 `opaque` ——
  那种响应 status 恒为 0，存了读不出来还占配额，CDN 的 CORS 白名单漏配时就会退化成它）。
  换 CDN 域名时 `sw.js`、`layout.tsx`、`NEXT_ASSET_PREFIX` 三处要一起改，并把 `sw.js` 里的 `VERSION` 加一。
- **首屏预算**（CI 里跑，超标直接失败）：apps 最重的路由 `/play/[id]/[n]` 73.8 KB（预算 90 KB）；
  feedback 最重的路由 `/feedback` 173.2 KB（预算 180 KB，余量 6.8 KB）。feedback 的余量已经不多，
  再往客户端加依赖前先跑一次 `bun run check:budget`。

## 5. 证书

`certbot` 容器走 HTTP-01（webroot），挑战文件写进和 nginx 共享的卷。三个域名三张证书，刻意不合成一张 —— 合成会让任一域名的证书透明日志暴露另外两个（`docs/00` 第 2 节要求后台与用户端入口分离）。

鸡生蛋问题的解法：`nginx/entrypoint.sh` 发现没有真证书时先造一张自签的占位，让 nginx 能起来、80 端口能接挑战；拿到真证书后由重载循环（默认 6 小时，`NGINX_RELOAD_INTERVAL` 可调）换上。

失败时 certbot 退避 **1 小时**再试，不重试风暴：Let's Encrypt 对验证失败有每小时 5 次的限制，容器反复重启硬撞这个限制会把账号锁在外面。

常见失败原因（`docker compose logs certbot` 能看到）：域名还没解析到本机、80 端口被云厂商安全组挡了、DNS 还没生效。

## 6. 排查

```bash
cd /opt/xsite
docker compose --env-file .env ps
docker compose --env-file .env logs -f --tail 100 service
docker compose --env-file .env exec service /app/api -healthcheck   # distroless 里没有 curl，用这个
docker compose --env-file .env exec nginx nginx -T | head -50       # 看渲染后的完整配置
docker compose --env-file .env exec postgres psql -U postgres -d xsite
```

| 现象 | 先看什么 |
| --- | --- |
| 整站 502 | nginx 有没有在服务重建后重载过（`nginx -s reload`） |
| 浏览器报证书不受信 | 还在用自签占位，`docker compose logs certbot` |
| 页面能开但脚本全 404 | `CDN_BASE` 和实际上传的前缀对不上；对比 HTML 里的地址和 `aws s3 ls` |
| 控制台报跨域、脚本一个都不执行 | R2 的 CORS 白名单里没有这个域名，重跑 `r2-setup.sh` |
| 后台整站 404 | `admin-assets` 一次性容器没跑成功，卷是空的：`docker compose logs admin-assets`，再 `docker compose exec nginx ls /srv/admin/current` |
| 资源还是 gzip、没有 Brotli | 先确认产物里有 `.br`（`docker compose exec nginx ls /srv/admin/current/assets \| grep -c .br`）；再看是不是哪个 location 误加了 `proxy_set_header Accept-Encoding ""`（apps 那条必须不清、feedback 那条必须清，原因见第 4 节） |
| 限流把正常用户拦住 / 日志里 IP 全一样 | 站点在 Cloudflare 后面但 `EDGE_TRUST_CLOUDFLARE` 不是 1 |
| 后台登录后立刻掉登录态 | `ADMIN_ORIGINS` 和实际域名不一致（必须 `https://`，production 只允许 https） |
| 生产环境封面或视频出不来 | `IMAGE_BASE_URLS` / `MEDIA_CONNECT_ORIGINS` 没跟着后台线路表更新，被 CSP 拦了 |
| 后台资源预热进度条不动 | SSE 被缓冲；确认 `/api/admin/` 的 `proxy_buffering off` 还在 |

## 7. 已知未做

- **多 CDN 与 GSLB**（`docs/11` X5-1）：现在是单边缘单机 + Cloudflare 一家，没有多家 CDN 调度、没有回源 mTLS、没有源站安全组只放行 CDN。
- **HTTP/3 只验到配置层**：`http3 on`、`listen 443 quic` 都在运行中的配置里，容器也在 UDP 443 上监听，`Alt-Svc` 正常下发；但本机没有带 HTTP/3 的 arm64 curl，真实 QUIC 握手没有端到端实测过。上线后用浏览器开发者工具看协议列是不是 `h3` 即可确认。
- **apps 的 Service Worker 与独立 CDN 域名互斥**：现在走反向代理型 CDN 绕开了这个限制。将来真要换独立资源域名，除了去掉 Service Worker，还要改 `apps/src/service-worker/index.ts` 里按同源 `/_app/immutable/` 预缓存的 `assetPattern`（feedback 的 `public/sw.js` 已经按跨源 CDN 改好了，可以照着做）。
- **多实例与蓝绿**：`service` / `apps` / `feedback` 都是单副本，`docker compose up -d` 会有秒级中断。要无缝发布得先上多副本 + 真正的负载均衡。
- **托管数据库**：`compose.yaml` 里自带 postgres 与 valkey，适合单机起步。换托管实例只要改三个连接串（线上必须 `sslmode=verify-full` 与 `rediss://`），其余不动。
- **备份**：没有自动备份。PostgreSQL 的连续归档与定期全量要按 `docs/09` 单独配。
- **可观测**：后端没有 `/metrics` 端点，`docs/11` 要求的仪表盘还没有数据源。
- **apps 的 Service Worker 与 CDN 预缓存**：Service Worker 用 `/_app/immutable/` 的同源路径做预缓存。将来若去掉 Service Worker 改用独立 CDN 域名，这段逻辑要一起改（`apps/src/service-worker/index.ts` 的 `assetPattern`）。
