import 'dart:convert';
import 'dart:io';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:xsite_app/app/app.dart';
import 'package:xsite_app/core/l10n/app_strings.dart';
import 'package:xsite_app/core/l10n/locale_provider.dart';
import 'package:xsite_app/features/site/site_config.dart';

Map<String, String> readCatalog(String tag) =>
    (jsonDecode(File('assets/i18n/$tag.json').readAsStringSync()) as Map<String, dynamic>).cast<String, String>();

List<String> placeholders(String s) => RegExp(r'\{(\w+)\}').allMatches(s).map((m) => m[1]!).toList()..sort();

Widget app(Locale locale) => ProviderScope(
  overrides: [
    localeOverrideProvider.overrideWith(() => _Fixed(locale)),
    siteConfigProvider.overrideWith(
      (ref) async => const SiteConfig(version: 3, siteName: null, slogan: '', features: {}),
    ),
  ],
  child: const XsiteApp(),
);

class _Fixed extends LocaleOverride {
  _Fixed(this.locale);
  final Locale locale;
  @override
  Locale? build() => locale;
}

void main() {
  final tags = supportedLocales.map(localeTag).toList();
  final base = readCatalog('zh-CN');

  test('支持 9 种语言，文件名与 Web 语言标签一致', () {
    expect(tags, ['zh-CN', 'en', 'es', 'vi', 'ja', 'ko', 'th', 'hi', 'ar']);
  });

  test('每种语言键齐全、非空、占位符与中文一致，且不照抄中文', () {
    for (final tag in tags) {
      final catalog = readCatalog(tag);
      expect(catalog.keys.toSet(), base.keys.toSet(), reason: tag);
      for (final e in catalog.entries) {
        expect(e.value.trim(), isNotEmpty, reason: '$tag ${e.key}');
        expect(placeholders(e.value), placeholders(base[e.key]!), reason: '$tag ${e.key}');
        if (tag != 'zh-CN') expect(e.value, isNot(base[e.key]), reason: '$tag ${e.key} 照抄了中文');
      }
    }
  });

  test('代码里用到的文案键都在 JSON 中', () {
    final used = <String>{};
    for (final f in Directory('lib').listSync(recursive: true).whereType<File>().where((f) => f.path.endsWith('.dart'))) {
      final src = f.readAsStringSync();
      used.addAll(RegExp(r"""\.t\('([\w.]+)'""").allMatches(src).map((m) => m[1]!));
      used.addAll(RegExp(r"""Key: '([\w.]+)'""").allMatches(src).map((m) => m[1]!));
      used.addAll(RegExp(r"""AppTab\(\s*'[^']*',\s*'([\w.]+)'""").allMatches(src).map((m) => m[1]!));
    }
    expect(used, isNotEmpty);
    expect(used.difference(base.keys.toSet()), isEmpty);
  });

  testWidgets('英语界面显示英文 Tab 与文案', (tester) async {
    await tester.pumpWidget(app(const Locale('en')));
    await tester.pumpAndSettle();
    expect(find.text('Site config v3'), findsOneWidget);
    expect(find.text('Shorts'), findsOneWidget);
    // 后端未配置站点名时，标题用当前语言的「首页」
    expect(find.widgetWithText(AppBar, 'Home'), findsOneWidget);
  });

  testWidgets('阿拉伯语界面从右到左', (tester) async {
    await tester.pumpWidget(app(const Locale('ar')));
    await tester.pumpAndSettle();
    expect(find.text('إعدادات الموقع v3'), findsOneWidget);
    final ctx = tester.element(find.byType(NavigationBar));
    expect(Directionality.of(ctx), TextDirection.rtl);
    expect(Localizations.localeOf(ctx).languageCode, 'ar');
  });

  testWidgets('业务码按当前语言显示，未知码按服务异常处理', (tester) async {
    await tester.pumpWidget(app(const Locale('ja')));
    await tester.pumpAndSettle();
    final s = AppStrings.of(tester.element(find.byType(NavigationBar)));
    expect(s.error('NEED_LOGIN'), readCatalog('ja')['error.NEED_LOGIN']);
    expect(s.error('ENC_TIME_SKEW'), readCatalog('ja')['error.ENCRYPTION']);
    expect(s.error('SOMETHING_NEW'), readCatalog('ja')['error.INTERNAL']);
  });
}
