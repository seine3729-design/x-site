import 'dart:convert';

import 'package:dio/dio.dart';

import '../security/api_crypto.dart';

/// 服务端公钥来源（docs/10 第 5.3 节）。
///
/// 正式包必须用 `--dart-define=API_ENC_KEYS=kid:公钥[,kid2:公钥2]` 预置公钥（当前 + 下一把），
/// 中间人无法替换；只有开发环境允许从 /api/v1/crypto/keys 在线获取。
class ServerKeyStore {
  ServerKeyStore({required List<ServerKey> pinned, Future<ServerKey> Function()? fetchRemote})
    : _pinned = List.unmodifiable(pinned),
      _fetchRemote = fetchRemote; // ignore: prefer_initializing_formals 命名参数不能用私有名

  final List<ServerKey> _pinned;
  final Future<ServerKey> Function()? _fetchRemote;
  int _index = 0;
  Future<ServerKey>? _remote;

  Future<ServerKey> current() {
    if (_pinned.isNotEmpty) return Future.value(_pinned[_index]);
    final fetch = _fetchRemote;
    if (fetch == null) {
      return Future.error(StateError('未配置 API_ENC_KEYS，且当前环境不允许在线获取公钥'));
    }
    return _remote ??= fetch().catchError((Object e) {
      _remote = null;
      throw e;
    });
  }

  /// 服务端返回 ENC_KEY_UNKNOWN：换下一把预置公钥，或重新在线获取。返回 false 表示没有可换的了。
  bool rotate() {
    if (_pinned.isNotEmpty) {
      if (_index + 1 >= _pinned.length) return false;
      _index++;
      return true;
    }
    _remote = null;
    return _fetchRemote != null;
  }
}

/// 加密拦截器：请求体加密、响应体解密；协议错误（公钥轮换、时间偏差）自动处理后重试一次。
///
/// 须放在域名选择之后（请求目标参与 AAD）、失败切换之前（切换域名重发时会重新加密）。
class EncryptionInterceptor extends Interceptor {
  EncryptionInterceptor(this._keys, this._dio, {DateTime Function()? now}) : _now = now ?? DateTime.now;

  final ServerKeyStore _keys;
  final Dio _dio;
  final DateTime Function() _now;
  int _clockOffsetMs = 0;

  /// 置为 true 的请求不加密（公钥接口）。
  static const skipKey = 'xsite_enc_skip';
  static const _plainKey = 'xsite_enc_plain';
  static const _respKey = 'xsite_enc_resp_key';
  static const _retriedKey = 'xsite_enc_retried';

  @override
  Future<void> onRequest(RequestOptions options, RequestInterceptorHandler handler) async {
    if (options.extra[skipKey] == true) return handler.next(options);
    try {
      // 重发（失败切换、协议错误重试）时从原始明文重新加密，避免二次加密
      final plain = options.extra.containsKey(_plainKey) ? options.extra[_plainKey] : options.data;
      options.extra[_plainKey] = plain;
      final bytes = switch (plain) {
        null => null,
        final List<int> b => b,
        final String s => utf8.encode(s),
        _ => utf8.encode(jsonEncode(plain)),
      };
      final uri = options.uri;
      final target = uri.hasQuery ? '${uri.path}?${uri.query}' : uri.path;
      final ts = (_now().millisecondsSinceEpoch + _clockOffsetMs) ~/ 1000;
      final sealed = await ApiCrypto.seal(await _keys.current(), options.method, target, ts, bytes);

      options.headers['X-Enc'] = sealed.header;
      if (sealed.body != null) {
        options.data = sealed.body;
        options.contentType = ApiCrypto.contentType;
        options.headers['X-Enc-Type'] = 'application/json';
      } else {
        options.data = null;
      }
      options.responseType = ResponseType.bytes;
      options.extra[_respKey] = sealed.responseKey;
      handler.next(options);
    } catch (e, st) {
      handler.reject(DioException(requestOptions: options, error: e, stackTrace: st, message: '请求加密失败'));
    }
  }

  @override
  Future<void> onResponse(Response<dynamic> response, ResponseInterceptorHandler handler) async {
    try {
      response.data = await _decode(response);
      handler.next(response);
    } catch (e, st) {
      handler.reject(
        DioException(requestOptions: response.requestOptions, response: response, error: e, stackTrace: st, message: '响应解密失败'),
      );
    }
  }

  @override
  Future<void> onError(DioException err, ErrorInterceptorHandler handler) async {
    final response = err.response;
    if (response == null || err.requestOptions.extra[_respKey] == null) return handler.next(err);
    try {
      // 业务错误（401/404/…）也是加密的；解密前的协议错误是明文 JSON
      response.data = await _decode(response);
    } catch (_) {
      return handler.next(err);
    }
    final code = response.data is Map ? (response.data as Map)['code'] : null;
    final options = err.requestOptions;
    if ((code == 'ENC_KEY_UNKNOWN' || code == 'ENC_TIME_SKEW') && options.extra[_retriedKey] != true) {
      if (code == 'ENC_KEY_UNKNOWN' && !_keys.rotate()) return handler.next(err);
      if (code == 'ENC_TIME_SKEW') _syncClock(response);
      options.extra[_retriedKey] = true;
      try {
        return handler.resolve(await _dio.fetch<dynamic>(options));
      } on DioException catch (e) {
        return handler.next(e);
      }
    }
    handler.next(err);
  }

  void _syncClock(Response<dynamic> response) {
    final date = response.headers.value('date');
    if (date == null) return;
    try {
      _clockOffsetMs = HttpDate.parse(date).millisecondsSinceEpoch - _now().millisecondsSinceEpoch;
    } on FormatException {
      // 忽略无法解析的 Date
    }
  }

  Future<Object?> _decode(Response<dynamic> response) async {
    final key = response.requestOptions.extra[_respKey] as List<int>?;
    final data = response.data;
    if (key == null || data is! List<int>) return data;
    if (data.isEmpty) return null; // 204 / 304
    final contentType = response.headers.value(Headers.contentTypeHeader) ?? '';
    final List<int> plain;
    final String realType;
    if (contentType == ApiCrypto.contentType) {
      plain = await ApiCrypto.openResponse(key, data);
      realType = response.headers.value('x-enc-type') ?? '';
    } else {
      plain = data;
      realType = contentType;
    }
    return realType.startsWith('application/json') ? jsonDecode(utf8.decode(plain)) : plain;
  }
}

/// dio 的 HttpDate 在 dart:io 中；这里只解析 RFC 7231 的 IMF-fixdate，避免页面层依赖 dart:io。
abstract final class HttpDate {
  static const _months = {
    'Jan': 1, 'Feb': 2, 'Mar': 3, 'Apr': 4, 'May': 5, 'Jun': 6, //
    'Jul': 7, 'Aug': 8, 'Sep': 9, 'Oct': 10, 'Nov': 11, 'Dec': 12,
  };

  /// `Wed, 21 Oct 2026 12:00:00 GMT`
  static DateTime parse(String value) {
    final m = RegExp(r'^\w{3}, (\d{2}) (\w{3}) (\d{4}) (\d{2}):(\d{2}):(\d{2}) GMT$').firstMatch(value.trim());
    final month = m == null ? null : _months[m[2]];
    if (m == null || month == null) throw FormatException('无法解析 Date 头', value);
    return DateTime.utc(int.parse(m[3]!), month, int.parse(m[1]!), int.parse(m[4]!), int.parse(m[5]!), int.parse(m[6]!));
  }
}
