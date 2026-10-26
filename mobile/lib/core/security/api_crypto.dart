import 'dart:convert';
import 'dart:typed_data';

import 'package:cryptography/cryptography.dart';

import 'hpke.dart';

/// 用户端接口报文加密 v1（docs/10 第 5.3 节，与 service/internal/crypto/apienc 一致）。
class ApiCrypto {
  ApiCrypto._();

  static const contentType = 'application/vnd.xsite.enc';
  static const info = 'xsite-api/v1';
  static const responseLabel = 'xsite-api/v1 response';

  static final _aes = AesGcm.with256bits();

  /// AAD：方法、请求目标（路径 + 查询串，按发出的原样）与时间戳。
  static Uint8List requestAad(String method, String requestTarget, int ts) =>
      utf8.encode('$info\n${method.toUpperCase()}\n$requestTarget\n$ts');

  /// 封装一次请求。[plaintext] 为空表示没有请求体。
  static Future<SealedRequest> seal(
    ServerKey key,
    String method,
    String requestTarget,
    int ts,
    List<int>? plaintext, {
    List<int>? fixedEphemeralKey,
  }) async {
    final sender = await HpkeSender.setup(key.publicKey, utf8.encode(info), fixedEphemeralKey: fixedEphemeralKey);
    final aad = requestAad(method, requestTarget, ts);
    // tag：空明文的 Seal，让没有请求体的 GET 也能校验路径、查询串与时间戳
    final tag = await sender.seal(aad, const []);
    final body = plaintext == null || plaintext.isEmpty ? null : await sender.seal(aad, plaintext);
    final responseKey = await sender.export(utf8.encode(responseLabel), 32);
    final header = ['1', key.kid, '$ts', _b64(sender.enc), _b64(tag)].join('.');
    return SealedRequest(header, body, responseKey);
  }

  /// 解密响应体：nonce(12) || AES-256-GCM 密文 || tag(16)。失败抛出 [SecretBoxAuthenticationError]。
  static Future<Uint8List> openResponse(List<int> responseKey, List<int> data) async {
    if (data.length < 12 + 16) {
      throw const FormatException('加密响应过短');
    }
    final box = SecretBox.fromConcatenation(data, nonceLength: 12, macLength: 16, copy: true);
    final plain = await _aes.decrypt(box, secretKey: SecretKey(responseKey), aad: utf8.encode(responseLabel));
    return Uint8List.fromList(plain);
  }

  static String _b64(List<int> bytes) => base64Url.encode(bytes).replaceAll('=', '');

  static Uint8List fromB64(String value) => base64Url.decode(base64Url.normalize(value));
}

/// 服务端加密公钥。
class ServerKey {
  const ServerKey(this.kid, this.publicKey);

  /// 解析 `kid:base64url公钥`（`--dart-define=API_ENC_KEYS` 的单项格式）。
  factory ServerKey.parse(String spec) {
    final i = spec.indexOf(':');
    if (i <= 0) throw FormatException('API_ENC_KEYS 格式应为 kid:公钥', spec);
    return ServerKey(spec.substring(0, i).trim(), ApiCrypto.fromB64(spec.substring(i + 1).trim()));
  }

  final String kid;
  final List<int> publicKey;
}

class SealedRequest {
  const SealedRequest(this.header, this.body, this.responseKey);

  final String header;
  final Uint8List? body;
  final Uint8List responseKey;
}
