import 'dart:ui';

import 'package:dio/dio.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import 'config/env.dart';
import 'l10n/app_strings.dart';
import 'l10n/locale_provider.dart';
import 'net/api_client.dart';
import 'net/domain_pool.dart';

/// 基础依赖。测试中通过 ProviderScope(overrides: ...) 替换。
final envProvider = Provider<Env>((ref) => Env.fromDefines());

final domainPoolProvider = Provider<DomainPool>((ref) => DomainPool(ref.watch(envProvider).seedHosts));

final dioProvider = Provider<Dio>((ref) {
  // 手动选择的语言优先，否则用系统语言；读取时取最新值，切换语言不需要重建 dio
  String languageTag() => localeTag(ref.read(localeOverrideProvider) ?? PlatformDispatcher.instance.locale);
  final dio = createDio(ref.watch(envProvider), ref.watch(domainPoolProvider), languageTag: languageTag);
  ref.onDispose(dio.close);
  return dio;
});
