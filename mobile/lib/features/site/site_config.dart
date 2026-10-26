import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/net/api_client.dart';
import '../../core/providers.dart';

/// 站点配置（与 service/api/openapi.yaml 的 SiteConfig 一致，只取 APP 用到的字段）。
class SiteConfig {
  const SiteConfig({required this.version, required this.siteName, required this.slogan, required this.features});

  factory SiteConfig.fromJson(Map<String, dynamic> json) {
    final site = (json['site'] as Map<String, dynamic>?) ?? const {};
    final features = (json['features'] as Map<String, dynamic>?) ?? const {};
    return SiteConfig(
      version: (json['version'] as num?)?.toInt() ?? 0,
      // 后端未配置站点名时为 null，界面用当前语言的默认名
      siteName: site['site_name'] as String?,
      slogan: site['slogan'] as String? ?? '',
      features: {for (final e in features.entries) e.key: e.value == true},
    );
  }

  final int version;
  final String? siteName;
  final String slogan;
  final Map<String, bool> features;

  bool feature(String key) => features[key] ?? false;
}

/// 启动时拉取站点配置（ETag 缓存与本地持久化在 M1-1 中加入）。
final siteConfigProvider = FutureProvider<SiteConfig>((ref) {
  final dio = ref.watch(dioProvider);
  return getData(dio, '/api/v1/site-config', (data) => SiteConfig.fromJson(data! as Map<String, dynamic>));
});
