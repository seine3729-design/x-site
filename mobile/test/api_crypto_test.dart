import 'dart:convert';
import 'dart:io';
import 'dart:typed_data';

import 'package:flutter_test/flutter_test.dart';
import 'package:xsite_app/core/security/api_crypto.dart';
import 'package:xsite_app/core/security/hpke.dart';

Uint8List hex(String s) =>
    Uint8List.fromList([for (var i = 0; i < s.length; i += 2) int.parse(s.substring(i, i + 2), radix: 16)]);

String toHex(List<int> b) => b.map((x) => x.toRadixString(16).padLeft(2, '0')).join();

void main() {
  group('HPKE 发送方', () {
    // RFC 9180 官方测试向量：mode_base, DHKEM(X25519, HKDF-SHA256), HKDF-SHA256, AES-256-GCM
    // （与 Go、Web 测试使用同一组）
    test('与 RFC 9180 测试向量逐字节一致', () async {
      final sender = await HpkeSender.setup(
        hex('430f4b9859665145a6b1ba274024487bd66f03a2dd577d7753c68d7d7d00c00c'),
        hex('4f6465206f6e2061204772656369616e2055726e'),
        fixedEphemeralKey: hex('179d4b53b6365c45b600c4163b61d95cbc2f4d9e36f1695558dce265ab8bab11'),
      );
      expect(toHex(sender.enc), '6c93e09869df3402d7bf231bf540fadd35cd56be14f97178f0954db94b7fc256');
      final pt = hex('4265617574792069732074727574682c20747275746820626561757479');
      expect(
        toHex(await sender.seal(hex('436f756e742d30'), pt)),
        'e5d84cd531cfb583096e7cfa9641bd3079cf3a91cda813c52deb5f512be9931980a41de125a925cdad859d5b7a',
      );
      expect(
        toHex(await sender.seal(hex('436f756e742d31'), pt)),
        '2c43aff25343fdbff864506f0818b9d87df84ea01b1a2144d23b4d40c26bf655fdf197fe40297a8aebeed5cc2d',
      );
      expect(
        toHex(await sender.export(const [], 32)),
        'ded6cffafaea6b812cbf3e241e88332adbc077aca81512914213810ee291770a',
      );
    });
  });

  group('三端互通样本', () {
    // 与 service/internal/crypto/apienc/testdata、apps/src/lib/crypto/testdata 下的文件内容相同
    final fixture = jsonDecode(File('test/fixtures/interop_v1.json').readAsStringSync()) as Map<String, dynamic>;
    final key = ServerKey(fixture['kid'] as String, ApiCrypto.fromB64(fixture['public_key'] as String));

    Future<SealedRequest> sealFixture({bool withBody = true}) => ApiCrypto.seal(
      key,
      fixture['method'] as String,
      fixture['request_target'] as String,
      fixture['ts'] as int,
      withBody ? utf8.encode(fixture['request_plaintext'] as String) : null,
      fixedEphemeralKey: hex(fixture['ephemeral_private_key_hex'] as String),
    );

    test('请求封装与样本逐字节一致（Go 已验证能打开同一份）', () async {
      final sealed = await sealFixture();
      expect(sealed.header, fixture['x_enc']);
      expect(toHex(sealed.body!), fixture['request_body_hex']);
    });

    test('样本响应可解密，篡改后失败', () async {
      final sealed = await sealFixture(withBody: false);
      final body = hex(fixture['response_body_hex'] as String);
      expect(utf8.decode(await ApiCrypto.openResponse(sealed.responseKey, body)), fixture['response_plaintext']);
      body[body.length - 1] ^= 1;
      await expectLater(ApiCrypto.openResponse(sealed.responseKey, body), throwsA(anything));
    });
  });

  test('ServerKey.parse 解析 --dart-define 格式', () {
    final k = ServerKey.parse('k1:WG3tRh_GTBPRCIpKwC7ggF0LChSHFhVs3bLWWmV5OkY');
    expect(k.kid, 'k1');
    expect(k.publicKey, hasLength(32));
    expect(() => ServerKey.parse('no-colon'), throwsFormatException);
  });
}
