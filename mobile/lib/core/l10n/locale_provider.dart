import 'package:flutter/widgets.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

/// 用户手动选择的语言；null 表示跟随系统。设置页（M2-2）接入后持久化到本地偏好。
class LocaleOverride extends Notifier<Locale?> {
  @override
  Locale? build() => null;

  void set(Locale? locale) => state = locale;
}

final localeOverrideProvider = NotifierProvider<LocaleOverride, Locale?>(LocaleOverride.new);
