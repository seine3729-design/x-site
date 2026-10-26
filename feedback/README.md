# 反馈社区 · H5/PC 前台

面向产品团队与内测用户的多项目反馈社区。用户在这里看项目进展、提交问题与建议、跟踪反馈从提交到上线的全过程。

这个目录是**用户端前台**（Next.js）。后端在 `service/`（表前缀 `fb_`），运营后台在 `admin/`，原因见 [`adr/0008`](./docs/adr/0008-实现位置复用现有工程.md)。

> 产品名「共测」是占位，最终名称与 Logo 待确认（`docs/06` 第 13 节）。改名只需改 `messages/zh-CN.json` 的 `brand.name` 与 `src/components/brand/logo.tsx`。

## 技术栈

| 层 | 选型 |
| --- | --- |
| 框架 | Next.js 16.4 App Router + React 19.3 |
| 语言 | TypeScript 6.0.3（不是 7，原因见 `docs/06` 第 1.1 节） |
| 样式 | Tailwind 4.3，三层设计 token 写在 `src/app/globals.css` 的 `@theme` 里 |
| 组件 | Radix UI primitives + 自建组件（shadcn 风格，组件在仓库里可改） |
| 图标 | lucide-react（与 `admin/` 保持一套） |
| 包管理 | Bun 1.4；**运行时是 Node 24** |

## 本地开发

```bash
# 在仓库根目录
./dev.sh up web          # 起前台（含 service 依赖时用 ./dev.sh up）
open http://localhost:3002
```

容器里装的是 linux 原生依赖，宿主机那份用于 typecheck / lint / 测试：

```bash
cd feedback
bun install
bun run typecheck        # tsc --noEmit
bun run lint             # eslint
bun run check:contrast   # 设计 token 的 WCAG 对比度检查，不达标会失败
bun run gen:api          # 由 ../service/api/openapi.yaml 重新生成接口类型
```

## 造测试数据

空库上看不出列表、排序、状态时间线的效果。种子脚本全程走真实接口，
所以编号分配、状态历史、贡献记账、计数字段都是后端真算出来的：

```bash
# 需要一个有 fb_* 权限的后台账号（默认 fbe2e，可用 SEED_ADMIN_USER / SEED_ADMIN_PASS 覆盖）
node feedback/scripts/seed-dev-data.mjs

# 刚造出来的数据时间都是「刚刚」，把它们摊到过去几周
docker compose -f compose.yaml --env-file .env exec -T postgres \
  psql -U xsite_app -d xsite < feedback/scripts/spread-dev-time.sql
```

脚本可重复运行（每步先查已存在就复用）。造出来的社区账号密码统一是 `Dj66-seed-pass-2026!`。

## 目录结构

```text
feedback/
├─ messages/zh-CN.json          所有界面文案（组件里不硬编码）
├─ scripts/check-contrast.mjs   设计 token 的对比度检查
├─ src/
│  ├─ app/
│  │  ├─ layout.tsx             html 壳、主题脚本、Toaster
│  │  ├─ globals.css            三层设计 token + base 样式
│  │  └─ (site)/                用户端页面
│  ├─ components/
│  │  ├─ ui/                    通用组件（Button、Badge、Card…）
│  │  ├─ domain/                业务徽标（状态、类型、可见性…）
│  │  ├─ layout/                页面壳（顶栏、底部导航、页脚）
│  │  ├─ theme/                 深浅主题
│  │  └─ brand/                 Logo
│  ├─ features/<域>/            按业务域分的页面组件与 Server Action
│  ├─ lib/
│  │  ├─ api/schema.ts          由 openapi.yaml 生成，不要手改
│  │  ├─ api/types.ts           契约类型的改名入口
│  │  ├─ api/queries.ts         服务端读接口
│  │  ├─ server/                只能在服务端导入（标了 server-only）
│  │  ├─ i18n.ts                文案访问 + 错误码映射
│  │  └─ format.ts              时间与数字格式
│  └─ env.ts                    环境变量，缺失即启动失败
└─ docs/                        产品与设计文档（00–12 + adr）
```

## 两条不能破的约定

1. **浏览器只跟本服务同源通信**。调 Go 后端一律经由 `src/lib/server/api.ts`，它带 `X-Service-Token`；令牌只存在于服务端进程里，不进客户端 bundle。
2. **组件里不写裸色值、不写硬编码文案**。颜色只用语义 token 生成的 utility（`bg-surface`、`text-fg-muted`…），文案只从 `messages/zh-CN.json` 取。

## 从这里开始

- [文档总览](./docs/README.md)
- [信息架构与核心流程](./docs/02-信息架构与核心流程.md)
- [用户端 Web 与设计系统](./docs/06-用户端Web与设计系统.md)
- [API 契约与集成](./docs/08-API契约与集成.md)
- [开发任务拆解](./docs/11-开发任务拆解.md)
