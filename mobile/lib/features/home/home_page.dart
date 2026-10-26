import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/l10n/app_strings.dart';
import '../site/site_config.dart';

/// 首页：区块编排在 M1-2 中实现，这里先展示站点配置，验证网络链路。
class HomePage extends ConsumerWidget {
  const HomePage({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final config = ref.watch(siteConfigProvider);
    final s = AppStrings.of(context);
    return Scaffold(
      appBar: AppBar(title: Text(config.value?.siteName ?? s.t('tab.home'))),
      body: Center(
        child: config.when(
          data: (c) => Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              Text(c.slogan, style: Theme.of(context).textTheme.titleMedium),
              const SizedBox(height: 8),
              Text(s.t('home.configVersion', {'version': c.version})),
            ],
          ),
          loading: () => const CircularProgressIndicator(),
          error: (error, _) => Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              Text(s.t('home.connectFailed')),
              const SizedBox(height: 8),
              FilledButton(
                onPressed: () => ref.invalidate(siteConfigProvider),
                child: Text(s.t('common.retry')),
              ),
            ],
          ),
        ),
      ),
    );
  }
}
