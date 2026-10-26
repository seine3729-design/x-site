# Svelte

`svelte` 是 SvelteKit 应用。包管理和运行时使用 Bun 1.4 或更新版本，样式使用 Tailwind CSS 4，生产构建注册 PWA。本机如果还是 Bun 1.3，`bun run dev` 可以开，`build` 和 `start` 会直接提示升级。

在 `svelte` 目录执行：

```bash
bun install
bun run dev
```

浏览器打开 http://localhost:3001 。`apps` 使用 3000，两边可以同时开。

开发、测试、线上分别读取 `.env.development`、`.env.test`、`.env.production`。

```bash
bun run build:dev
bun run build:test
bun run build:prod
bun run start:test
bun run start
```

## 环境文件

密钥和某一套环境的差异不要写进上面三个文件。本地放 `.env.local`，某一套的差异放 `.env.development.local`、`.env.test.local`、`.env.production.local`。这几个文件不会提交。

加载顺序是环境文件、`.env.local`、再是 `.env.<环境>.local`，后面的覆盖前面的。`scripts/with-env.ts` 负责载入，并强制把 `APP_ENV` 和 `PUBLIC_APP_ENV` 写成当前命令对应的环境。Vite 不再自己读 `.env.*`。

`PUBLIC_APP_ENV` 和 `PUBLIC_SITE_URL` 在构建时写进浏览器脚本。服务端的 `APP_ENV`、配额和 `EDGE_SHARED_SECRET` 在进程启动时读取。`build:test` 之后要配 `start:test`，`build` 之后要配 `start`。

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

## PWA

生产构建生成 `/service-worker.js`，缓存名带上 SvelteKit 的构建版本。作用和 `apps` 里写进 `sw.js` 的构建号一样：新版本激活后删掉上一版缓存，并通知已打开的页面刷新。

- 页面导航先用缓存，同时在后台更新。
- `/_app/immutable/` 和 `/icons/` 缓存优先。
- `/api/` 不进缓存。
- 开发服务器不注册 Service Worker。

安装图标在 `static/icons/`。清单是 `/manifest.webmanifest`。
