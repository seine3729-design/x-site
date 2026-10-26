import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import 'app/app.dart';

void main() {
  // 启动顺序（RASP、域名探测、年龄门、游客身份）在 M1-1 中补全，见 docs/05 第 C.1 节
  runApp(const ProviderScope(child: XsiteApp()));
}
