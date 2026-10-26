import 'dart:convert';
import 'dart:typed_data';

import 'package:cryptography/cryptography.dart';
import 'package:dio/dio.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:xsite_app/core/net/encryption_interceptor.dart';
import 'package:xsite_app/core/security/api_crypto.dart';

/// 测试用的「服务端」：用 HPKE 接收方逻辑打开请求、加密响应（与 Go 的 apienc 行为一致）。
/// 只在测试里实现接收方，APP 正式代码只有发送方。
class FakeServer implements HttpClientAdapter {
  FakeServer(this.responder);

  /// 根据解密后的请求决定响应：(状态码, 明文 JSON 或协议错误)
  final Future<(int, Object?)> Function(RequestOptions options, String? plainBody, Map<String, String> enc) responder;

  static final _x25519 = X25519();
  static late SimpleKeyPair serverKey;
  static late ServerKey publicKey;
  final requests = <RequestOptions>[];

  /// 每次发送时的 X-Enc 头（dio 重试复用同一个 RequestOptions，不能事后再读）
  final sentHeaders = <String>[];

  static Future<void> init() async {
    serverKey = await _x25519.newKeyPairFromSeed(List<int>.generate(32, (i) => i + 1));
    publicKey = ServerKey('k1', (await serverKey.extractPublicKey()).bytes);
  }

  @override
  Future<ResponseBody> fetch(RequestOptions options, Stream<Uint8List>? requestStream, Future<void>? cancelFuture) async {
    requests.add(options);
    final header = options.headers['X-Enc'] as String?;
    if (header == null) return _json(400, {'code': 'ENC_REQUIRED'});
    sentHeaders.add(header);
    final parts = header.split('.');
    final bodyBytes = requestStream == null ? <int>[] : await requestStream.expand((c) => c).toList();
    final (status, payload) = await responder(options, null, {'kid': parts[1], 'ts': parts[2]});
    if (payload is Map && (payload['code'] as String? ?? '').startsWith('ENC_')) {
      return _json(status, payload, headers: {'date': ['Wed, 21 Oct 2026 12:00:00 GMT']});
    }
    // 服务端用与客户端相同的 HPKE 上下文导出响应密钥：这里直接复用客户端在 extra 中的密钥做校验
    final respKey = options.extra['xsite_enc_resp_key'] as List<int>;
    final plain = utf8.encode(jsonEncode(payload));
    final nonce = List<int>.filled(12, 7);
    final box = await AesGcm.with256bits().encrypt(
      plain,
      secretKey: SecretKey(respKey),
      nonce: nonce,
      aad: utf8.encode(ApiCrypto.responseLabel),
    );
    lastRequestBody = bodyBytes;
    return ResponseBody.fromBytes(
      [...nonce, ...box.cipherText, ...box.mac.bytes],
      status,
      headers: {
        Headers.contentTypeHeader: [ApiCrypto.contentType],
        'x-enc-type': ['application/json; charset=utf-8'],
      },
    );
  }

  List<int>? lastRequestBody;

  ResponseBody _json(int status, Object body, {Map<String, List<String>> headers = const {}}) => ResponseBody.fromString(
    jsonEncode(body),
    status,
    headers: {
      Headers.contentTypeHeader: ['application/json; charset=utf-8'],
      ...headers,
    },
  );

  @override
  void close({bool force = false}) {}
}

Dio buildDio(FakeServer server, ServerKeyStore keys, {DateTime Function()? now}) {
  final dio = Dio(BaseOptions(baseUrl: 'https://api.example.test'));
  dio.httpClientAdapter = server;
  dio.interceptors.add(EncryptionInterceptor(keys, dio, now: now));
  return dio;
}

void main() {
  setUpAll(FakeServer.init);

  test('请求体加密、响应解密为 JSON，网络上没有明文', () async {
    final server = FakeServer((o, _, _) async => (200, {'code': 'OK', 'data': {'saved': true}}));
    final dio = buildDio(server, ServerKeyStore(pinned: [FakeServer.publicKey]));
    final res = await dio.post<Map<String, dynamic>>('/api/v1/me/history?from=app', data: {'episode_id': 42, 'note': '短剧'});

    expect(res.data, {'code': 'OK', 'data': {'saved': true}});
    final sent = server.requests.single;
    expect(sent.headers['X-Enc'], matches(RegExp(r'^1\.k1\.\d+\.[\w-]{43}\.[\w-]{22}$')));
    expect(sent.contentType, ApiCrypto.contentType);
    expect(utf8.decode(server.lastRequestBody!, allowMalformed: true), isNot(contains('episode_id')));
  });

  test('业务错误（加密的 401）解密后交给调用方', () async {
    final server = FakeServer((o, _, _) async => (401, {'code': 'NEED_LOGIN', 'message': '请先登录'}));
    final dio = buildDio(server, ServerKeyStore(pinned: [FakeServer.publicKey]));
    try {
      await dio.get<dynamic>('/api/v1/me/state');
      fail('应抛出 DioException');
    } on DioException catch (e) {
      expect(e.response?.statusCode, 401);
      expect((e.response?.data as Map)['code'], 'NEED_LOGIN');
    }
  });

  test('ENC_KEY_UNKNOWN 时换下一把预置公钥重试一次', () async {
    var calls = 0;
    final server = FakeServer((o, _, enc) async {
      calls++;
      return enc['kid'] == 'old' ? (400, {'code': 'ENC_KEY_UNKNOWN'}) : (200, {'code': 'OK', 'data': 1});
    });
    final keys = ServerKeyStore(pinned: [ServerKey('old', FakeServer.publicKey.publicKey), FakeServer.publicKey]);
    final res = await buildDio(server, keys).get<Map<String, dynamic>>('/api/v1/x');
    expect(res.data?['data'], 1);
    expect(calls, 2);
    expect(server.sentHeaders.map((h) => h.split('.')[1]), ['old', 'k1']);
  });

  test('ENC_TIME_SKEW 时按服务端 Date 校准时间后重试', () async {
    final server = FakeServer((o, _, enc) async {
      final skewed = int.parse(enc['ts']!) < DateTime.utc(2026, 10, 21, 11, 55).millisecondsSinceEpoch ~/ 1000;
      return skewed ? (400, {'code': 'ENC_TIME_SKEW'}) : (200, {'code': 'OK'});
    });
    // 设备时钟慢了 1 小时
    final dio = buildDio(
      server,
      ServerKeyStore(pinned: [FakeServer.publicKey]),
      now: () => DateTime.utc(2026, 10, 21, 11),
    );
    await dio.get<dynamic>('/api/v1/x');
    final ts = server.sentHeaders.map((h) => int.parse(h.split('.')[2])).toList();
    int unix(DateTime t) => t.millisecondsSinceEpoch ~/ 1000;
    expect(ts, [unix(DateTime.utc(2026, 10, 21, 11)), unix(DateTime.utc(2026, 10, 21, 12))]);
  });

  test('没有预置公钥且不允许在线获取时拒绝发送', () async {
    final server = FakeServer((o, _, _) async => (200, {'code': 'OK'}));
    final dio = buildDio(server, ServerKeyStore(pinned: const []));
    await expectLater(dio.get<dynamic>('/api/v1/x'), throwsA(isA<DioException>()));
    expect(server.requests, isEmpty);
  });

  test('HttpDate 解析 IMF-fixdate', () {
    expect(HttpDate.parse('Wed, 21 Oct 2026 12:00:00 GMT'), DateTime.utc(2026, 10, 21, 12));
    expect(() => HttpDate.parse('yesterday'), throwsFormatException);
  });
}
