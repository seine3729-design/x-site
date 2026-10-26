// 报文加密联调：用 APP 同一份拦截器代码直连本地 Go 后端（./dev.sh up 之后）。
//
//   cd mobile && dart run tool/e2e_encryption.dart http://localhost:8080
//
// 检查：在线取公钥（开发环境）、加密 GET 解出站点配置、网络上是密文、业务错误解密后可读。
import 'dart:io';

import 'package:dio/dio.dart';
import 'package:xsite_app/core/net/encryption_interceptor.dart';
import 'package:xsite_app/core/security/api_crypto.dart';

Future<void> main(List<String> args) async {
  final base = args.isEmpty ? 'http://localhost:8080' : args.first;
  final dio = Dio(
    BaseOptions(baseUrl: base, validateStatus: (s) => s != null && s < 400),
  );
  final wire = <String>[];
  dio.interceptors.add(
    InterceptorsWrapper(
      onResponse: (r, h) {
        wire.add('${r.statusCode} ${r.headers.value('content-type')}');
        h.next(r);
      },
    ),
  );

  Future<ServerKey> fetchKey() async {
    final r = await Dio(
      BaseOptions(baseUrl: base),
    ).get<Map<String, dynamic>>('/api/v1/crypto/keys');
    final data = r.data!['data'] as Map<String, dynamic>;
    final k = (data['keys'] as List<dynamic>)
        .cast<Map<String, dynamic>>()
        .firstWhere((k) => k['kid'] == data['current']);
    return ServerKey(
      k['kid'] as String,
      ApiCrypto.fromB64(k['public_key'] as String),
    );
  }

  // 排在记录器之后：响应先经过记录器（看到网络上的原始密文），再由加密拦截器解密
  dio.interceptors.add(
    EncryptionInterceptor(
      ServerKeyStore(pinned: const [], fetchRemote: fetchKey),
      dio,
    ),
  );

  var failed = false;
  void check(String name, bool ok, [String detail = '']) {
    stdout.writeln(
      '${ok ? '✓' : '✗'} $name${detail.isEmpty ? '' : ' — $detail'}',
    );
    failed |= !ok;
  }

  final res = await dio.get<Map<String, dynamic>>('/api/v1/site-config?from=app');
  final version = (res.data?['data'] as Map<String, dynamic>?)?['version'];
  check(
    '加密 GET /api/v1/site-config 解出站点配置',
    res.data?['code'] == 'OK',
    'version=$version',
  );
  check('网络上的响应是密文', wire.last.contains(ApiCrypto.contentType), wire.last);

  try {
    await dio.get<dynamic>('/api/v1/not-a-route');
    check('未匹配路由应返回 404', false);
  } on DioException catch (e) {
    check('未匹配路由返回 404', e.response?.statusCode == 404, '${e.response?.data}');
  }
  exit(failed ? 1 : 0);
}
