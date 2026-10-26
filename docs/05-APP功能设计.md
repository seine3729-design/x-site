# 05 · APP 功能设计（mobile/）

目标：把 Web 用户端能力搬到原生应用，**强化竖屏沉浸式刷剧、本地续播、消息推送、域名容灾**。APP 复用后端 `/api/v1/*` 接口（全部报文加密），不新增业务后端；界面支持 9 种语言（A.6）。

技术栈：**Flutter 3.47 + Dart 3.13 + Riverpod 3 + go_router 18 + dio 5（native adapter）+ drift + video_player（ExoPlayer/AVPlayer）+ Shorebird**。原 React Native + Expo 方案废弃。

---

## A. 工程与技术方案

### A.1 版本与构建要求（2026-10）

| 项 | 基线 | 备注 |
| --- | --- | --- |
| Flutter | 3.47.x stable | Impeller 默认（iOS/Android） |
| Dart | 3.13.x | 主构造函数、`new`/`factory` 简写构造 |
| 设计系统 | `material_ui` 1.x / `cupertino_ui` 1.x | 从 SDK 解耦的独立包；`dart fix --apply --code=migrate_design_widgets` 迁移 |
| Android | Gradle 8.14、AGP 8.11.1、Kotlin 2.2.20、Java 17；`minSdk 24` | 3.47 起为硬性要求 |
| iOS | Xcode 26+，Swift Package Manager；最低 iOS 15 | 准备 Xcode 27 |
| 热更新 | Shorebird（已支持 3.47.x） | 仅修补 Dart 代码 |

### A.2 依赖选型

| 用途 | 包 | 说明 |
| --- | --- | --- |
| 状态 | `flutter_riverpod` 3 + `riverpod_generator` | `@riverpod` 代码生成；`AsyncValue` 统一加载/错误态 |
| 路由 | `go_router` 18 | `StatefulShellRoute` 做底部 Tab；`redirect` 处理年龄门与登录守卫；深链 |
| 网络 | `dio` 5 + `native_dio_adapter` | Android Cronet、iOS URLSession，获得 HTTP/2/3、Brotli |
| 模型 | `freezed` 3 + `json_serializable` | 不可变模型、`sealed` 联合类型 |
| 本地库 | `drift`（SQLite） | 历史、追剧、离线事件队列；后台 Isolate 执行 |
| 安全存储 | `flutter_secure_storage` | 令牌、设备密钥存 Keychain / Android Keystore |
| 报文加密 | `cryptography` 2.9 | HPKE 发送方（X25519、HKDF、AES-256-GCM），见 A.4、`10` 第 5.3 节 |
| 多语言 | `flutter_localizations`（SDK） | Material/Cupertino 组件的 9 种语言与 RTL；界面文案用运行时 JSON（A.6） |
| 播放 | `video_player` 2.14+ | Android ExoPlayer（Media3）、iOS AVPlayer；备选 `fvp`/`media_kit` |
| 图片 | `cached_network_image` | 磁盘缓存上限 300 MB，LRU 淘汰 |
| 推送 | APNs / FCM / 国内厂商通道 | 国内 Android 需厂商通道或聚合推送 |
| 安全 | `freerasp` + Play Integrity / App Attest | 运行时检测 + 服务端校验证明 |
| 监控 | Sentry 或 Crashlytics | 崩溃、ANR、启动耗时 |
| 测试 | `flutter_test`、`integration_test`、`patrol` | 启动→播放端到端 |

### A.3 目录结构（feature-first）

```
mobile/lib/
├─ main.dart / bootstrap.dart   # 启动：完整性检测、配置拉取、域名选择
├─ app/                         # MaterialApp.router、主题、路由表
├─ core/
│  ├─ net/                      # dio、拦截器（含 encryption_interceptor）、域名池、签名
│  ├─ l10n/                     # AppStrings（JSON 文案加载）、语言选择
│  ├─ security/                 # hpke / api_crypto（报文加密）、证书锁定、RASP、密钥存储
│  ├─ storage/                  # drift 数据库
│  ├─ player/                   # 控制器池、预加载、测速
│  └─ telemetry/                # 埋点队列
└─ features/<feature>/{data,domain,presentation}
   # home shua explore detail play search rank topic account me messages community ads share
```

### A.4 网络层拦截器链

```
DomainSelector → CommonHeaders（含 Accept-Language）→ AuthInterceptor（401 单飞刷新）
→ SignInterceptor（时间戳+nonce+HMAC）→ EncryptionInterceptor（HPKE 报文加密）→ RetryInterceptor（幂等请求退避）
→ FailoverInterceptor（连接/证书异常或连续 5xx → 切换域名重试一次）→ Telemetry
```

- 已实现：DomainSelector、CommonHeaders、EncryptionInterceptor、FailoverInterceptor（`lib/core/net/`）。
- **EncryptionInterceptor**：请求体加密、响应解密为 JSON（业务错误的响应体也是密文，同样解密）；`ENC_KEY_UNKNOWN` 时换下一把预置公钥、`ENC_TIME_SKEW` 时按响应 `Date` 校准时钟，各重试一次；切换域名重发时从原始明文重新加密。
- **公钥**：正式包必须 `--dart-define=API_ENC_KEYS=kid:公钥[,kid2:公钥2]` 预置（当前 + 下一把，中间人无法替换）；只有 `ENV=development` 允许从 `/api/v1/crypto/keys` 在线获取。服务端轮换公钥前，先发带新公钥的版本并等其覆盖到最低支持版本。
- 签名（SignInterceptor，M5-3）对网络上的密文计算，服务端先验签再解密。
- 联调：`cd mobile && dart run tool/e2e_encryption.dart http://localhost:8080`。

### A.5 构建与发布

- 构建：`flutter build appbundle|ipa --release --obfuscate --split-debug-info=build/symbols`；符号文件上传崩溃平台，不随包分发。
- Android 开启 R8 压缩与资源裁剪；按 ABI 拆包（`--split-per-abi`）用于官网下载包。
- 渠道：App Store、Google Play、国内应用商店、官网 APK。官网 APK 用同一签名证书，并在官网公布证书 SHA-256 指纹。
- 分发合规：iOS 只通过 App Store / TestFlight 分发，不使用企业证书或「超级签」向公众分发（违反 Apple 协议，证书会被吊销）。各渠道包内容与审核时一致，不做审核期与上线后行为不同的处理。
- Shorebird 补丁只用于缺陷修复和小幅调整，不改变应用主要功能；补丁签名校验开启。

### A.6 多语言

- 语言与 Web 相同：简体中文（默认）、英语、西班牙语、越南语、日语、韩语、泰语、印地语、阿拉伯语（从右到左）。
- 默认跟随系统语言；系统语言不在列表中时用简体中文。「我的 → 设置 → 语言」可手动选择（`localeOverrideProvider`，设置页在 M2-2 接入并持久化）。
- 文案放 `assets/i18n/<标签>.json`，格式与 Web 相同（扁平键、`{name}` 占位符，标签 `zh-CN`/`en`/…）。由 `AppStrings` 委托在运行时加载，不编译进 Dart 代码，因此可以从 CDN 更新：
  - 用法：`AppStrings.of(context).t('tab.home')`、`.t('home.configVersion', {'version': 3})`、`.error(code)`（业务码 → 当前语言）。
  - 当前语言缺键回退简体中文，再缺显示键名；单元测试保证 9 种语言键齐全、占位符一致，并扫描源码确认用到的键都存在。
  - `flutter_localizations` 提供系统组件文案与阿拉伯语 RTL；布局用 `EdgeInsetsDirectional`、`AlignmentDirectional`，方向性图标按 `Directionality` 镜像。
  - 请求头 `Accept-Language` 带当前界面语言。
- CDN 更新（与 Web 同一套发布方式，`04` A.4.3，路径 `i18n/app/`）：启动与回到前台（距上次检查超过 10 分钟）时带 ETag 取 `manifest.json`；只下载当前语言与简体中文中哈希变化的文件，校验通过后写入应用目录，下次进入页面生效；内置文案始终保留为兜底。M0-8 实现。

---

## B. 信息架构（底部 Tab）

| Tab | 对应 | 说明 |
| --- | --- | --- |
| 首页 | 区块编排 | 与 Web 一致的区块流，原生卡片；右上角消息铃铛（未读角标） |
| 刷剧 | 沉浸式连播 | APP 主打，全屏竖屏流 |
| 福利 | 福利中心 | 签到、转盘、奖券、任务、返利、商城、晒图（`13` 第 3 节） |
| 发现 | 探索筛选 | 维度/标签/状态/上新 + 排序 |
| 我的 | 个人中心 | 资料、等级、钱包、历史、收藏、点赞、消息、设置（`13` 第 2 节） |

- 底栏最多 5 项：加入「福利」后，「消息」移到首页铃铛与「我的」入口（推送点击仍直达消息页）。

Tab 可由后台 `menu(position=mobile_tabbar)` 远程配置（图标、徽标、可见性、排序），启动时随站点配置拉取，本地缓存上一版兜底。

---

## C. 核心模块

### C.1 启动与身份
1. 冷启动读取本地缓存的站点配置与上次可用域名，立即渲染首页骨架（不等网络）。
2. 并行执行：RASP 初始化、域名健康探测（见 `09` 第 4 节）、`GET /api/v1/site-config`（ETag 条件请求）。
3. 年龄确认门（首启或到期）。
4. 游客身份：无令牌时 `POST /api/v1/auth/guest`，首次携带平台完整性证明；令牌存安全存储。
5. `GET /api/v1/me/state`。
- 目标：冷启动到首页可交互 ≤ 1.5s（中端机），首帧不依赖网络。

### C.2 沉浸式刷剧（核心体验）
- `PageView.builder` 竖向分页；上滑换剧、左滑下一集、单击暂停、双击点赞 [推断]、长按设置、按住 2x。
- 右侧操作列：赞、评论（底部抽屉）、追剧、分享、选集、剧详情。
- **控制器池**：同时最多 3 个 `VideoPlayerController`（上一条/当前/下一条），离开窗口立即 `pause` 并在超出池时 `dispose`，避免低端机解码器耗尽导致崩溃。
- **预加载**：当前视频起播后再初始化下一条（取清单 + 首切片），弱网下只预取清单。
- 边界提示「松手看下一部 / 没有更多」。
- 进度每 15 秒及暂停/切集/进后台时写入本地 drift，再批量上报 `/api/v1/me/history`。

### C.3 播放器
- HLS 多清晰度（自动 + 各档）、倍速、线路切换与测速（原生 Range 请求，线路由后端按 IP 下发，手选记本地 7 天，质量上报 `/api/v1/lines/report`，见 `12` 第 6 节）、断点续播、连播倒计时、跳过片段、试看/登录墙/VIP 墙。
- **加密播放**：清单与 AES-128 内容密钥只能经加密接口获取，原生播放器（ExoPlayer/AVPlayer）不能直接请求。APP 在本机起回环代理（只监听 `127.0.0.1`、随机端口、每次播放会话一个随机令牌）：播放器请求 `http://127.0.0.1:<端口>/<令牌>/index.m3u8`，代理经加密接口取回清单、把密钥地址改写到代理、切片地址保持 CDN 原地址；离开播放页即关闭。切片仍是 AES-128 密文，由播放器自己解密。M1-3 实现。
- 播放令牌过期前 60 秒静默重新取源并无缝切换。
- 后台播放/画中画：默认关闭，按产品与版权策略开启 [推断]。
- 弹幕：`CustomPainter` 单层绘制，轨道复用；滑动中暂停弹幕绘制。
- 截屏/录屏：Android 播放页可按配置设置 `FLAG_SECURE`；iOS 只能检测录屏状态并提示，不能阻止。

### C.4 内容浏览
- 首页区块、详情、频道、标签、专题、排行、搜索（联想 + 热搜 + 历史），与 Web 功能对齐。
- 列表用 `SliverList` + 固定高度项（`itemExtent`/`prototypeItem`），图片按显示尺寸解码（`memCacheWidth`）。
- 广告位同 Web（`/api/v1/ads`），曝光以可见 50% 且 1 秒为准。

### C.5 社区
- 评论（排序/回复/点赞/表情/删除/举报/审核中态）、弹幕、举报，与 Web 一致；受功能开关、屏蔽词、先审后显、禁言约束。

### C.6 账号
- 注册 / 登录 / 改密，规则同 `03` 第 2 节；**没有找回密码**：注册页醒目提示并要求勾选「我已知晓忘记密码无法找回」，登录页「忘记密码？」说明只能重新注册、会员与充值金币可凭订单号联系客服迁移；连续失败后展示人机验证。
- 个人中心（规则见 `13` 第 2 节）：资料卡（头像框、昵称、等级、会员、简介）、资产条（金币、优惠券、成长值）、继续观看、观看历史、收藏（追剧）、我的点赞、消息、我的订单、邀请好友、账号设置（头像、昵称、简介、修改密码）、语言、**登录设备管理**。
- 游客数据：历史、收藏、点赞存 drift 本机表（`local_history`、`local_favorites`、`local_likes`），登录或注册后调用一次 `POST /api/v1/me/local-merge` 并入账号。
- 可选：系统生物识别解锁「我的」页（本地能力，不上传生物信息）。

### C.7 消息与推送
- 站内消息三类 + 未读角标 + 全部已读。
- 推送：追剧更新、被回复/点赞；启动后上报推送令牌到 `POST /api/v1/devices/push-token`，令牌变化即更新；用户可在设置中按类别关闭。
- Android 13+ 运行时申请通知权限；iOS 在用户产生追剧行为后再请求授权，提高授权率。

### C.8 分享与增长
- 系统分享、复制链接、生成海报（`RepaintBoundary` 截图 + 二维码）。
- 深链：Universal Links / App Links 打开对应剧集；域名配置的 `apple-app-site-association` 与 `assetlinks.json` 需覆盖域名池中所有入口域名。
- 注册墙漏斗同 Web；APP 内不展示 App 下载浮窗。

---

### C.9 会员与支付

- 套餐、规则同 `03` 第 12 节（天 / 周 / 月 / 季 / 年卡）；游客须先注册；购买前提示忘记密码无法找回。
- **按分发渠道区分支付方式**（`--dart-define=DIST_CHANNEL=official|appstore|googleplay`）：

| 渠道包 | 可用支付 | 说明 |
| --- | --- | --- |
| 官网 APK | 支付宝 APP 支付、微信 APP 支付、USDT | 调起支付宝 SDK / 微信 OpenSDK；USDT 展示地址、金额、二维码与倒计时 |
| App Store | Apple 内购，或不在 APP 内售卖 | 应用内销售数字内容须用内购（App Store 审核指南 3.1.1），不能调起支付宝、微信、USDT，也不能引导到外部购买（个别地区另有政策，上架前按当地规则评估） |
| Google Play | Google Play 结算，或不在 APP 内售卖 | 同上，按 Play 付款政策；部分地区允许替代结算，需单独申请 |

- 商店包选「不在 APP 内售卖」时：只展示会员状态与已购权益，不出现价格与购买按钮（「阅读器类」应用模式），用户在网页购买后登录即生效。用内购时，服务端校验收据（Apple App Store Server API / Google Play Developer API）后按同一流程发放会员，订单渠道记为 `iap_apple` / `iap_google`。具体选哪种需产品确认。
- 支付结果以服务端为准：从支付 SDK 回到 APP 后进入「支付确认中」并轮询订单状态。

### C.10 福利中心

- 页面与规则同 `13` 第 3 节；数据一次 `GET /api/v1/welfare/home`。
- 转盘：`CustomPainter` 绘制，按服务端返回的 `slot_index` 停格；系统「减少动态效果 / 移除动画」开启时直接显示结果；中奖时轻触觉反馈（可在设置关闭）。
- 推广返利：App Links / Universal Links 打开 `/r/<码>` 时把邀请码记在本机，注册时提交；海报用 `RepaintBoundary` 生成；不读取剪贴板做归因。
- 签到提醒：用户主动开启后用本地通知或推送提醒。
- 评论晒图：系统相册选择器（不申请整个相册权限）、本机压缩后直传；元数据在服务端统一去除。
- 商店包：不提供评分有奖、下载其他应用有奖与第三方任务墙；启用充值金币时只走商店内购（C.9）。
- 激励视频换转盘次数为 P4，需接入广告 SDK，并在隐私政策与权限说明中写明。

## D. APP 独有能力

| 能力 | 说明 |
| --- | --- |
| 热更新 | Shorebird 推送 Dart 补丁；配置、开关走远程配置；界面文案经 CDN 更新（A.6），都不需要发版 |
| 远程配置 | 启动拉 `site_config` + 菜单 + 功能开关，本地缓存兜底 |
| 多域名容灾 | 内置种子域名 + 签名域名清单 + 健康探测自动切换（`09` 第 4 节） |
| 防 DNS 劫持 | API 域名优先通过 DoH/HTTPDNS 解析，失败回退系统 DNS（`09` 第 3 节） |
| 本地离线 | 历史/追剧/设置/待上报事件本地存储；离线视频下载为可选增量，需版权与合规评估 [推断] |
| 端侧加固 | 代码混淆、证书锁定、RASP、完整性证明、请求签名（`10` 第 6 节） |
| 手势体验 | 竖屏滑动、长按加速、音量/亮度手势 |

---

## E. 与 Web 的功能映射

| 功能 | Web | APP |
| --- | --- | --- |
| 沉浸式刷剧 | `/shua` 页 | 一级 Tab |
| PWA 安装 | 添加主屏幕 | 不涉及 |
| App 下载浮窗 | 有 | 无 |
| 回家的路 | 页面 | 内置域名容灾 + 页面入口 |
| 推送 | 站内消息 | 站内消息 + 系统推送 |
| 分享海报 | Canvas | `RepaintBoundary` |
| 主题 | 浅/深 | 浅/深 + 跟随系统 |
| 登录态 | HttpOnly Cookie | 安全存储 + Bearer |
| 语言 | URL 前缀（`/en/…`），首访按浏览器语言跳转 | 跟随系统，可手动切换 |
| 报文加密公钥 | 运行时从 `/api/v1/crypto/keys` 获取 | 构建期预置（`API_ENC_KEYS`） |
| 视频 | hls.js + MSE，`<video>` 为 `blob:` 地址 | 原生播放器 + 本机回环代理 |
| 会员支付 | 支付宝 / 微信 / USDT（网页） | 官网包同 Web；商店包内购或只登录查看 |

---

## F. 非功能性

- **性能**（详见 `08` 第 6 节）：刷剧滑动 60fps（120Hz 设备尽量 120fps），首帧 ≤ 1.0s（预加载命中）/ ≤ 1.8s（未命中）；内存峰值中端机 ≤ 400 MB；安装包 Android（单 ABI）≤ 25 MB。
- **弱网**：自动降码率/切线；请求超时连接 5s、读取 10s；失败三分类埋点同 Web。
- **合规**：年龄确认门保留；隐私政策首启弹窗同意后才初始化统计与推送 SDK；权限按需申请；各商店按其内容分级要求填报。
- **埋点**：复用 `/api/v1/events`，补充设备型号、系统版本、渠道包、推送到达/点击、域名切换事件；本地队列最多 500 条，满后丢最旧的。

---

## G. 落地阶段

1. **P0**：工程骨架与拦截器链（含报文加密）、9 种语言、启动/身份/年龄门、域名池与证书锁定、首页、详情、播放器（HLS + 选线 + 续播 + 回环代理）、沉浸式刷剧、登录注册、我的。
2. **P1**：会员与支付（按渠道包）、个人中心（历史、收藏、点赞、账号设置）、评论弹幕、搜索、探索、排行、专题、消息、分享海报、广告位、RASP 与完整性证明接入、文案 CDN 更新。
3. **P1.5（M6）**：福利中心（钱包、等级、签到、任务、转盘、奖券、返利、商城、晒图），见 `13` 第 10 节。
4. **P2**：推送（含厂商通道）、Shorebird、深链、离线能力评估。
