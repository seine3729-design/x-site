import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:xsite_app/app/app.dart';
import 'package:xsite_app/core/l10n/locale_provider.dart';
import 'package:xsite_app/features/site/site_config.dart';

/// 测试环境默认是 en_US，这里固定为简体中文，断言中文文案
class _Chinese extends LocaleOverride {
  @override
  Locale? build() => const Locale('zh');
}

void main() {
  testWidgets('首页显示站点配置，底部有 5 个 Tab', (tester) async {
    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          localeOverrideProvider.overrideWith(_Chinese.new),
          siteConfigProvider.overrideWith(
            (ref) async => const SiteConfig(version: 3, siteName: '短剧', slogan: '随时随地', features: {}),
          ),
        ],
        child: const XsiteApp(),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.text('随时随地'), findsOneWidget);
    expect(find.text('站点配置 v3'), findsOneWidget);
    expect(find.byType(NavigationDestination), findsNWidgets(5));

    await tester.tap(find.text('刷剧'));
    await tester.pumpAndSettle();
    expect(find.textContaining('M1-4'), findsOneWidget);
  });

  testWidgets('配置加载失败时显示重试', (tester) async {
    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          localeOverrideProvider.overrideWith(_Chinese.new),
          siteConfigProvider.overrideWith((ref) => Future<SiteConfig>.error(Exception('down'))),
        ],
        child: const XsiteApp(),
      ),
    );
    await tester.pumpAndSettle();

    expect(find.text('连接服务失败'), findsOneWidget);
    expect(find.text('重试'), findsOneWidget);
  });
}
