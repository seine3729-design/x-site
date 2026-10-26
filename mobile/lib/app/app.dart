import 'package:flutter/material.dart';
import 'package:flutter_localizations/flutter_localizations.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../core/l10n/app_strings.dart';
import '../core/l10n/locale_provider.dart';
import 'router.dart';

class XsiteApp extends ConsumerStatefulWidget {
  const XsiteApp({super.key});

  @override
  ConsumerState<XsiteApp> createState() => _XsiteAppState();
}

class _XsiteAppState extends ConsumerState<XsiteApp> {
  late final GoRouter _router = createRouter();

  @override
  void dispose() {
    _router.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    const seed = Color(0xFFE5484D);
    return MaterialApp.router(
      onGenerateTitle: (context) =>
          AppStrings.of(context).t('site.fallbackName'),
      debugShowCheckedModeBanner: false,
      theme: ThemeData(colorScheme: ColorScheme.fromSeed(seedColor: seed)),
      darkTheme: ThemeData(
        colorScheme: ColorScheme.fromSeed(
          seedColor: seed,
          brightness: Brightness.dark,
        ),
      ),
      // 9 种语言（docs/05 A.6）；阿拉伯语由 GlobalWidgetsLocalizations 切换为从右到左
      locale: ref.watch(localeOverrideProvider),
      supportedLocales: supportedLocales,
      localizationsDelegates: const [
        AppStrings.delegate,
        GlobalMaterialLocalizations.delegate,
        GlobalWidgetsLocalizations.delegate,
        GlobalCupertinoLocalizations.delegate,
      ],
      routerConfig: _router,
    );
  }
}
