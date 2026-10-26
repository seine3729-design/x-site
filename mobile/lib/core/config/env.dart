import '../security/api_crypto.dart';

/// 构建期配置，统一通过 `--dart-define` 注入（docs/05 A.1、docs/11 第 9.5 节）。
///
/// 本地运行示例：
/// ```
/// flutter run --dart-define=ENV=development \
///   --dart-define=API_SEED_HOSTS=http://10.0.2.2:8080   # Android 模拟器
/// ```
///
/// 正式包还必须注入 `API_ENC_KEYS=kid:公钥[,kid2:公钥2]`（接口加密公钥，docs/10 第 5.3 节）；
/// 开发环境不配置时从 /api/v1/crypto/keys 在线获取。
class Env {
  const Env({required this.name, required this.seedHosts, required this.appVersion, this.apiEncKeys = const []});

  /// 从 `--dart-define` 读取。未配置种子域名时使用本地开发地址。
  factory Env.fromDefines() {
    const name = String.fromEnvironment('ENV', defaultValue: 'development');
    const hosts = String.fromEnvironment('API_SEED_HOSTS', defaultValue: 'http://10.0.2.2:8080');
    const version = String.fromEnvironment('APP_VERSION', defaultValue: '0.1.0');
    const encKeys = String.fromEnvironment('API_ENC_KEYS');
    return Env(name: name, seedHosts: parseHosts(hosts), appVersion: version, apiEncKeys: parseEncKeys(encKeys));
  }

  final String name;

  /// 预置的接口加密公钥，按顺序使用（第一把为当前，其余为轮换备用）。
  final List<ServerKey> apiEncKeys;

  /// 内置种子 API 地址（签名域名清单不可用时的兜底，docs/09 第 4 节）。
  final List<Uri> seedHosts;
  final String appVersion;

  bool get isDevelopment => name == 'development';

  static List<ServerKey> parseEncKeys(String raw) => [
    for (final part in raw.split(','))
      if (part.trim().isNotEmpty) ServerKey.parse(part),
  ];

  /// 解析逗号分隔的地址；生产环境只接受 https。
  static List<Uri> parseHosts(String raw, {bool requireHttps = false}) {
    final hosts = <Uri>[];
    for (final part in raw.split(',')) {
      final uri = Uri.tryParse(part.trim());
      if (uri == null || !uri.hasScheme || uri.host.isEmpty) continue;
      if (requireHttps && uri.scheme != 'https') continue;
      hosts.add(uri);
    }
    return hosts;
  }
}
