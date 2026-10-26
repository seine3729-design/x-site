---
name: flutter-app-conventions
description: mobile/ APP（Flutter 3.47 + Dart 3.13 + Riverpod 3）的编码规范。用于编写或修改 mobile/ 下的页面、状态、网络层、播放器、本地存储、平台配置时。界面工作同时读 ui-ux-pro-max。
---

# APP 规范（mobile/）

## 概述

Flutter 3.47 / Dart 3.13 + Riverpod 3 + go_router 18 + dio 5。结构见 `docs/05` A 节。

## 目录（feature-first）

```
lib/app/            MaterialApp.router、主题、路由表
lib/core/net/       dio 实例、拦截器、域名池
lib/core/config/    构建期配置（--dart-define）
lib/core/security/  安全存储、证书锁定
lib/features/<功能>/{data,domain,presentation}
test/               与 lib/ 同结构
```

## 规则

- **状态**：Riverpod；页面数据用 `FutureProvider` / `AsyncNotifier`，用 `AsyncValue.when` 处理加载/错误/空态；高频更新（进度条）用 `select` 精细订阅。
- **路由**：go_router；底部 Tab 用 `StatefulShellRoute`；年龄门、登录守卫放 `redirect`。
- **网络**：只用 `core/net` 提供的 dio 实例；不要在页面里新建 dio 或 `HttpClient`。域名来自域名池，不硬编码。
- **配置**：环境、种子域名等用 `--dart-define`，读取集中在 `core/config/env.dart`。
- **存储**：令牌与设备密钥只用 `flutter_secure_storage`；普通偏好用 shared_preferences；不把个人数据写入日志。
- **播放**：刷剧流控制器池最多 3 个，离屏立即暂停、超出池立即 dispose。
- **性能**：列表用 builder + 固定高度；图片按显示尺寸解码；重计算放 `Isolate.run`；避免在滚动项中使用 `Opacity`/`saveLayer`。
- **const**：能 `const` 的构造都写 `const`（lint 会检查）。
- **多语言**：界面文案只写进 `assets/i18n/*.json`（9 种语言，先加 `zh-CN.json`），用 `AppStrings.of(context).t()` / `.error(code)`；布局用 `EdgeInsetsDirectional` 等方向无关写法（阿拉伯语 RTL）。见 `docs/05` A.6。
- **报文加密**：请求只走 `core/net` 的 dio（已含 `EncryptionInterceptor`）；正式包必须 `--dart-define=API_ENC_KEYS`。

## 测试

- 纯逻辑（域名池切换、线路排序、签名）：单元测试。
- 页面：widget 测试用 `ProviderScope(overrides: ...)` 替换网络依赖。
- 命令：`./dev.sh flutter analyze`、`./dev.sh flutter test`（在 mobile/ 执行本机 Flutter；低于 3.47 时脚本会提示升级）。

## 红线

- 页面中直接 new dio / 硬编码域名
- 令牌存 shared_preferences
- 不处理 `AsyncValue` 的错误态
- 刷剧流不回收播放器控制器

## 验证

- [ ] `flutter analyze` 无问题，`flutter test` 通过
- [ ] 在 Android 模拟器或 iOS 模拟器实际运行过改动页面
