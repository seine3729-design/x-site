# Tracker Frontend - 前端埋点SDK与后台数据展示

本项目包含两个主要部分：

1. **SDK** (`/sdk`) - 前端埋点数据采集 SDK
2. **后台管理系统** (`/src`) - 基于 React + Vite 的数据展示平台

### 环境要求

- Node.js >= 16.0.0
- pnpm (推荐)

### 安装依赖

```bash
# 安装主项目依赖
pnpm install

# 安装 SDK 依赖
cd sdk
pnpm install
cd ..
```

### 开发环境

#### 启动后台管理系统

```bash
# 使用开发环境配置启动
pnpm dev
```

访问地址：http://localhost:5173

#### 开发 SDK

```bash
cd sdk
pnpm dev
```

### 生产构建

#### 构建后台管理系统

```bash
# 生产环境打包
pnpm build

# 开发环境打包
pnpm build:dev
```

构建产物输出到 `dist/` 目录

#### 构建 SDK

```bash
cd sdk

# 生产环境打包
pnpm build:prod

# 开发环境打包
pnpm build:dev
```

构建产物输出到 `sdk/dist/` 目录

## 项目结构

```
tracking-frontend/
├── sdk/                          # 数据采集 SDK
│   ├── src/                      # SDK 源码
│   ├── proto/                    # Protobuf 协议定义
│   ├── package.json              # SDK 依赖配置
│   ├── tsup.config.ts            # SDK 构建配置
│   ├── .env.development          # SDK 开发环境配置
│   └── .env.production           # SDK 生产环境配置
│
├── src/                          # 后台管理系统源码
│   ├── assets/                   # 静态资源
│   ├── components/               # 公共组件
│   │   ├── ui/                   # UI 基础组件 (shadcn/ui)
│   │   ├── data-table/           # 数据表格组件
│   │   └── ...
│   ├── features/                 # 功能模块
│   │   ├── analytics/            # 数据分析
│   │   ├── dashboard/            # 仪表盘
│   │   ├── visit-analysis/       # 访问分析
│   │   ├── visitor-analysis/     # 访客分析
│   │   ├── traffic-analysis/     # 流量分析
│   │   ├── source-analysis/      # 来源分析
│   │   ├── error-logs/           # 错误日志
│   │   ├── merchants/            # 商户管理
│   │   ├── roles/                # 角色管理
│   │   ├── menus/                # 菜单管理
│   │   └── auth/                 # 认证相关
│   ├── routes/                   # 路由配置
│   ├── hooks/                    # 自定义 Hooks
│   ├── lib/                      # 工具库
│   ├── stores/                   # 状态管理 (Zustand)
│   ├── context/                  # React Context
│   ├── styles/                   # 全局样式
│   └── main.tsx                  # 应用入口
│
├── public/                       # 公共静态资源
├── .env.development              # 开发环境变量
├── .env.production               # 生产环境变量
├── .env.example                  # 环境变量示例
├── package.json                  # 项目依赖配置
├── vite.config.ts                # Vite 构建配置
├── tsconfig.json                 # TypeScript 配置
└── README.md                     # 项目说明文档
```

## 环境配置

### 后台管理系统环境变量

复制 `.env.example` 到 `.env.development` 或 `.env.production`：

```bash
# 环境
VITE_APP_ENV=development

# 接口地址
VITE_API_BASE_URL=http://localhost:3000/api
```

### SDK 环境变量

在 `sdk/.env.development` 或 `sdk/.env.production` 中配置：

```bash
# SDK 相关配置
TRACKER_ENV=development
TRACKER_DEBUG=true
```

## 技术栈

### 后台管理系统

- **框架**: React 19 + TypeScript
- **构建工具**: Vite 7
- **路由**: TanStack Router
- **状态管理**: Zustand + TanStack Query
- **UI 组件**: shadcn/ui + Radix UI
- **样式**: TailwindCSS 4
- **图表**: Tremor + Recharts
- **表单**: React Hook Form + Zod
- **认证**: Clerk

### SDK

- **语言**: TypeScript
- **构建工具**: tsup
- **数据序列化**: Protobuf
- **打包格式**: UMD + ESM

## 其他命令

```bash
# 代码检查
pnpm lint

# 代码格式化
pnpm format

# 代码格式检查
pnpm format:check
```

