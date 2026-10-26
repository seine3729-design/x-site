import 'package:flutter/material.dart';
import 'package:go_router/go_router.dart';

import '../core/l10n/app_strings.dart';
import '../features/home/home_page.dart';
import 'placeholder_page.dart';

/// 底部 Tab（docs/05 第 B 节）。Tab 远程配置在 M1-1 中接入。
class AppTab {
  const AppTab(this.path, this.labelKey, this.icon, this.builder);

  final String path;

  /// 文案键（assets/i18n/*.json）
  final String labelKey;
  final IconData icon;
  final WidgetBuilder builder;
}

final tabs = <AppTab>[
  AppTab('/', 'tab.home', Icons.home_outlined, (_) => const HomePage()),
  AppTab(
    '/shua',
    'tab.shua',
    Icons.play_circle_outline,
    (_) => const PlaceholderPage(titleKey: 'tab.shua', task: 'M1-4'),
  ),
  AppTab(
    '/explore',
    'tab.explore',
    Icons.explore_outlined,
    (_) => const PlaceholderPage(titleKey: 'tab.explore', task: 'M3-1'),
  ),
  AppTab(
    '/messages',
    'tab.messages',
    Icons.notifications_none,
    (_) => const PlaceholderPage(titleKey: 'tab.messages', task: 'M2-2'),
  ),
  AppTab(
    '/me',
    'tab.me',
    Icons.person_outline,
    (_) => const PlaceholderPage(titleKey: 'tab.me', task: 'M2-2'),
  ),
];

GoRouter createRouter() {
  return GoRouter(
    initialLocation: '/',
    routes: [
      StatefulShellRoute.indexedStack(
        builder: (context, state, shell) => _TabScaffold(shell: shell),
        branches: [
          for (final tab in tabs)
            StatefulShellBranch(
              routes: [
                GoRoute(
                  path: tab.path,
                  builder: (context, _) => tab.builder(context),
                ),
              ],
            ),
        ],
      ),
    ],
  );
}

class _TabScaffold extends StatelessWidget {
  const _TabScaffold({required this.shell});

  final StatefulNavigationShell shell;

  @override
  Widget build(BuildContext context) {
    final s = AppStrings.of(context);
    return Scaffold(
      body: shell,
      bottomNavigationBar: NavigationBar(
        selectedIndex: shell.currentIndex,
        onDestinationSelected: (index) =>
            shell.goBranch(index, initialLocation: index == shell.currentIndex),
        destinations: [
          for (final tab in tabs)
            NavigationDestination(
              icon: Icon(tab.icon),
              label: s.t(tab.labelKey),
            ),
        ],
      ),
    );
  }
}
