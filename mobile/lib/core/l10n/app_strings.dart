import 'dart:convert';

import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart';
import 'package:flutter/widgets.dart';

/// APP 支持的语言（docs/05 A.6），顺序即默认优先级：设备语言不在列表中时用第一项（简体中文）。
/// 运营后台只用中文，与这里无关。
const supportedLocales = <Locale>[
  Locale('zh'),
  Locale('en'),
  Locale('es'),
  Locale('vi'),
  Locale('ja'),
  Locale('ko'),
  Locale('th'),
  Locale('hi'),
  Locale('ar'),
];

/// 文案文件名（与 Web 的语言标签一致：`assets/i18n/<tag>.json`）。
String localeTag(Locale locale) =>
    locale.languageCode == 'zh' ? 'zh-CN' : locale.languageCode;

/// 界面文案。文案放在 assets/i18n/*.json（与 Web 同一格式，后续可由 CDN 下发覆盖），不编译进 Dart 代码。
class AppStrings {
  AppStrings(
    this.locale,
    Map<String, String> messages,
    Map<String, String> fallback,
  ) : _messages = messages,
      _fallback = fallback;

  final Locale locale;
  final Map<String, String> _messages;
  final Map<String, String> _fallback;

  static const delegate = _AppStringsDelegate();

  static AppStrings of(BuildContext context) {
    final strings = Localizations.of<AppStrings>(context, AppStrings);
    assert(strings != null, 'MaterialApp 需要注册 AppStrings.delegate');
    return strings!;
  }

  /// 取文案并替换 {name} 占位符。当前语言缺键时回退简体中文，再缺则显示键名（便于发现问题）。
  String t(String key, [Map<String, Object> params = const {}]) {
    final template = _messages[key] ?? _fallback[key] ?? key;
    if (params.isEmpty) return template;
    return template.replaceAllMapped(RegExp(r'\{(\w+)\}'), (m) {
      final value = params[m[1]];
      return value == null ? m[0]! : '$value';
    });
  }

  /// 业务码 → 当前语言提示（docs/07 第 4 节）。后端 message 只作兜底，界面一律按 code 显示。
  String error(String code) {
    if (code.startsWith('ENC_')) return t('error.ENCRYPTION');
    final key = 'error.$code';
    return _messages.containsKey(key) || _fallback.containsKey(key)
        ? t(key)
        : t('error.INTERNAL');
  }

  /// 从资源包读取一种语言的文案。Localizations 会保留加载结果，这里不再走 AssetBundle 的缓存，
  /// 避免切换语言后旧语言文案常驻内存。
  static Future<Map<String, String>> loadCatalog(
    AssetBundle bundle,
    String tag,
  ) async {
    final text = bundle is CachingAssetBundle
        ? await bundle.loadString('assets/i18n/$tag.json', cache: false)
        : await bundle.loadString('assets/i18n/$tag.json');
    final raw = jsonDecode(text) as Map<String, dynamic>;
    return raw.map((k, v) => MapEntry(k, v as String));
  }
}

class _AppStringsDelegate extends LocalizationsDelegate<AppStrings> {
  const _AppStringsDelegate();

  @override
  bool isSupported(Locale locale) =>
      supportedLocales.any((l) => l.languageCode == locale.languageCode);

  @override
  Future<AppStrings> load(Locale locale) async {
    final tag = localeTag(locale);
    final fallback = await AppStrings.loadCatalog(rootBundle, 'zh-CN');
    final messages = tag == 'zh-CN'
        ? fallback
        : await AppStrings.loadCatalog(rootBundle, tag);
    return SynchronousFuture(AppStrings(locale, messages, fallback));
  }

  @override
  bool shouldReload(_AppStringsDelegate old) => false;
}
