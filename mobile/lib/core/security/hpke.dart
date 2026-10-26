import 'dart:convert';
import 'dart:typed_data';

import 'package:cryptography/cryptography.dart';

/// HPKE（RFC 9180）发送方，base 模式，套件固定为
/// DHKEM(X25519, HKDF-SHA256) + HKDF-SHA256 + AES-256-GCM，与服务端 Go 标准库 crypto/hpke、
/// Web 端 apps/src/lib/crypto/hpke.ts 一致（docs/10 第 5.3 节）。
///
/// LabeledExtract / LabeledExpand 按 RFC 5869 用 HMAC-SHA256 实现。
class HpkeSender {
  HpkeSender._(this.enc, this._key, this._baseNonce, this._exporterSecret);

  static const _kemId = 0x0020;
  static const _kdfId = 0x0001;
  static const _aeadId = 0x0002;
  static const _nk = 32;
  static const _nn = 12;
  static const _nh = 32;

  static final _hmac = Hmac.sha256();
  static final _aes = AesGcm.with256bits();
  static final _x25519 = X25519();

  static final _v1 = utf8.encode('HPKE-v1');
  static final _suiteKem = _concat([utf8.encode('KEM'), _i2osp(_kemId, 2)]);
  static final _suiteHpke = _concat([utf8.encode('HPKE'), _i2osp(_kemId, 2), _i2osp(_kdfId, 2), _i2osp(_aeadId, 2)]);

  /// 封装后的临时公钥（随请求发给服务端）。
  final Uint8List enc;
  final SecretKey _key;
  final Uint8List _baseNonce;
  final Uint8List _exporterSecret;
  int _seq = 0;

  /// SetupBaseS：向服务端公钥 [recipientPublicKey] 封装。[fixedEphemeralKey] 只用于测试。
  static Future<HpkeSender> setup(List<int> recipientPublicKey, List<int> info, {List<int>? fixedEphemeralKey}) async {
    final eph = fixedEphemeralKey == null
        ? await _x25519.newKeyPair()
        : await _x25519.newKeyPairFromSeed(fixedEphemeralKey);
    final pkE = Uint8List.fromList((await eph.extractPublicKey()).bytes);
    final dhKey = await _x25519.sharedSecretKey(
      keyPair: eph,
      remotePublicKey: SimplePublicKey(recipientPublicKey, type: KeyPairType.x25519),
    );
    final dh = await dhKey.extractBytes();

    final kemContext = _concat([pkE, recipientPublicKey]);
    final eaePrk = await _labeledExtract(_suiteKem, const [], 'eae_prk', dh);
    final sharedSecret = await _labeledExpand(_suiteKem, eaePrk, 'shared_secret', kemContext, _nh);

    final pskIdHash = await _labeledExtract(_suiteHpke, const [], 'psk_id_hash', const []);
    final infoHash = await _labeledExtract(_suiteHpke, const [], 'info_hash', info);
    final ksc = _concat([
      [0],
      pskIdHash,
      infoHash,
    ]);
    final secret = await _labeledExtract(_suiteHpke, sharedSecret, 'secret', const []);
    final key = await _labeledExpand(_suiteHpke, secret, 'key', ksc, _nk);
    final baseNonce = await _labeledExpand(_suiteHpke, secret, 'base_nonce', ksc, _nn);
    final exporterSecret = await _labeledExpand(_suiteHpke, secret, 'exp', ksc, _nh);
    return HpkeSender._(pkE, SecretKey(key), baseNonce, exporterSecret);
  }

  /// 按调用顺序递增序号加密，必须与接收方 Open 的顺序一致。返回 ciphertext || tag。
  Future<Uint8List> seal(List<int> aad, List<int> plaintext) async {
    final nonce = Uint8List.fromList(_baseNonce);
    final seq = _i2osp(_seq, _nn);
    for (var i = 0; i < _nn; i++) {
      nonce[i] ^= seq[i];
    }
    _seq++;
    final box = await _aes.encrypt(plaintext, secretKey: _key, nonce: nonce, aad: aad);
    return _concat([box.cipherText, box.mac.bytes]);
  }

  /// Export：从本次上下文导出密钥（用于解密响应）。
  Future<Uint8List> export(List<int> exporterContext, int length) =>
      _labeledExpand(_suiteHpke, _exporterSecret, 'sec', exporterContext, length);

  static Future<Uint8List> _hmacBytes(List<int> key, List<int> data) async {
    // HKDF-Extract 的空 salt 等价于 HashLen 个 0
    final k = key.isEmpty ? Uint8List(_nh) : key;
    final mac = await _hmac.calculateMac(data, secretKey: SecretKey(k));
    return Uint8List.fromList(mac.bytes);
  }

  static Future<Uint8List> _labeledExtract(List<int> suite, List<int> salt, String label, List<int> ikm) =>
      _hmacBytes(salt, _concat([_v1, suite, utf8.encode(label), ikm]));

  static Future<Uint8List> _labeledExpand(
    List<int> suite,
    List<int> prk,
    String label,
    List<int> info,
    int length,
  ) async {
    final labeledInfo = _concat([_i2osp(length, 2), _v1, suite, utf8.encode(label), info]);
    final out = BytesBuilder(copy: false);
    var block = Uint8List(0);
    for (var counter = 1; out.length < length; counter++) {
      block = await _hmacBytes(
        prk,
        _concat([
          block,
          labeledInfo,
          [counter],
        ]),
      );
      out.add(block);
    }
    return Uint8List.sublistView(out.takeBytes(), 0, length);
  }

  static Uint8List _i2osp(int n, int length) {
    final out = Uint8List(length);
    for (var i = length - 1; i >= 0 && n > 0; i--) {
      out[i] = n & 0xff;
      n >>= 8;
    }
    return out;
  }

  static Uint8List _concat(List<List<int>> parts) {
    final b = BytesBuilder(copy: false);
    for (final p in parts) {
      b.add(p);
    }
    return b.takeBytes();
  }
}
