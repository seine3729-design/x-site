import 'package:flutter/material.dart';

import '../core/l10n/app_strings.dart';

/// 尚未实现的 Tab 页，标注对应任务编号（docs/11）。
class PlaceholderPage extends StatelessWidget {
  const PlaceholderPage({super.key, required this.titleKey, required this.task});

  final String titleKey;
  final String task;

  @override
  Widget build(BuildContext context) {
    final s = AppStrings.of(context);
    final title = s.t(titleKey);
    return Scaffold(
      appBar: AppBar(title: Text(title)),
      body: Center(child: Text(s.t('placeholder.building', {'title': title, 'task': task}))),
    );
  }
}
