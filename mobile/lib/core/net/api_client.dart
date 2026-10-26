import 'dart:io';
import 'dart:math';

import 'package:dio/dio.dart';

import '../config/env.dart';
import '../security/api_crypto.dart';
import 'domain_pool.dart';
import 'encryption_interceptor.dart';

/// 后端业务错误（docs/07 第 4 节）。statusCode 为 0 表示网络错误。
class ApiException implements Exception {
  ApiException(this.statusCode, this.code, this.message, {this.requestId});

  final int statusCode;
  final String code;
  final String message;
  final String? requestId;

  bool get isUnauthorized => statusCode == 401;

  @override
  String toString() =>
      'ApiException($statusCode, $code, $message, request_id=$requestId)';
}

/// 创建全局唯一的 dio 实例。页面不要自行创建 dio。
///
/// [languageTag] 返回当前界面语言（如 `ja`、`zh-CN`），随 Accept-Language 发给后端。
Dio createDio(
  Env env,
  DomainPool pool, {
  required String Function() languageTag,
  ServerKeyStore? keys,
}) {
  final dio = Dio(
    BaseOptions(
      connectTimeout: const Duration(seconds: 5),
      receiveTimeout: const Duration(seconds: 10),
      responseType: ResponseType.json,
    ),
  );
  dio.interceptors.addAll([
    _DomainInterceptor(pool),
    _CommonHeadersInterceptor(env, languageTag),
    // 须在域名选择之后（请求目标参与 AAD）、失败切换之前（换域名重发时重新加密）
    EncryptionInterceptor(keys ?? defaultKeyStore(env, dio), dio),
    _FailoverInterceptor(pool, dio),
  ]);
  return dio;
}

/// 预置公钥优先；只有开发环境允许在线获取（docs/10 第 5.3 节）。
ServerKeyStore defaultKeyStore(Env env, Dio dio) {
  return ServerKeyStore(
    pinned: env.apiEncKeys,
    fetchRemote: env.isDevelopment ? () => fetchServerKey(dio) : null,
  );
}

/// GET /api/v1/crypto/keys（明文接口），取当前公钥。
Future<ServerKey> fetchServerKey(Dio dio) async {
  final response = await dio.get<Map<String, dynamic>>(
    '/api/v1/crypto/keys',
    options: Options(extra: {EncryptionInterceptor.skipKey: true}),
  );
  final data = response.data?['data'] as Map<String, dynamic>?;
  final current = data?['current'];
  for (final key in (data?['keys'] as List<dynamic>? ?? const [])) {
    final k = key as Map<String, dynamic>;
    if (k['kid'] == current) {
      return ServerKey(
        k['kid'] as String,
        ApiCrypto.fromB64(k['public_key'] as String),
      );
    }
  }
  throw ApiException(
    response.statusCode ?? 0,
    'ENC_KEYS_UNAVAILABLE',
    '无法获取加密公钥',
  );
}

/// 解包 `{ code, message, data }`；非 OK 时抛出 [ApiException]。
Future<T> getData<T>(
  Dio dio,
  String path,
  T Function(Object? data) decode,
) async {
  try {
    final response = await dio.get<Map<String, dynamic>>(path);
    return decode(_unwrap(response));
  } on DioException catch (e) {
    throw _toApiException(e);
  }
}

Object? _unwrap(Response<Map<String, dynamic>> response) {
  final body = response.data;
  if (body == null || body['code'] != 'OK') {
    throw ApiException(
      response.statusCode ?? 0,
      body?['code'] as String? ?? 'INTERNAL',
      body?['message'] as String? ?? '服务暂不可用',
      requestId: body?['request_id'] as String?,
    );
  }
  return body['data'];
}

ApiException _toApiException(DioException e) {
  final body = e.response?.data;
  if (body is Map<String, dynamic>) {
    return ApiException(
      e.response?.statusCode ?? 0,
      body['code'] as String? ?? 'INTERNAL',
      body['message'] as String? ?? '服务暂不可用',
      requestId: body['request_id'] as String?,
    );
  }
  return ApiException(e.response?.statusCode ?? 0, 'NETWORK', '网络异常，请稍后重试');
}

/// 把相对路径拼到当前域名上。
class _DomainInterceptor extends Interceptor {
  _DomainInterceptor(this.pool);

  final DomainPool pool;

  @override
  void onRequest(RequestOptions options, RequestInterceptorHandler handler) {
    options.baseUrl = pool.current.toString();
    handler.next(options);
  }

  @override
  void onResponse(
    Response<dynamic> response,
    ResponseInterceptorHandler handler,
  ) {
    pool.markSuccess(Uri.parse(response.requestOptions.baseUrl));
    handler.next(response);
  }
}

/// 公共请求头（docs/07 第 0.2 节）。签名头在 M5-3 中加入。
class _CommonHeadersInterceptor extends Interceptor {
  _CommonHeadersInterceptor(this.env, this.languageTag);

  final Env env;
  final String Function() languageTag;
  final Random _random = Random.secure();

  @override
  void onRequest(RequestOptions options, RequestInterceptorHandler handler) {
    options.headers['X-Request-Id'] = _requestId();
    options.headers['X-App-Version'] = env.appVersion;
    options.headers['X-Platform'] = Platform.operatingSystem;
    options.headers['Accept-Language'] = languageTag();
    handler.next(options);
  }

  String _requestId() {
    final bytes = List<int>.generate(16, (_) => _random.nextInt(256));
    return bytes.map((b) => b.toRadixString(16).padLeft(2, '0')).join();
  }
}

/// 连接失败、超时、证书错误或 5xx 时记录失败；发生切换则用新域名重试一次。
class _FailoverInterceptor extends Interceptor {
  _FailoverInterceptor(this.pool, this.dio);

  final DomainPool pool;
  final Dio dio;

  static const _retriedKey = 'xsite_failover_retried';

  @override
  Future<void> onError(
    DioException err,
    ErrorInterceptorHandler handler,
  ) async {
    final options = err.requestOptions;
    if (!_isDomainFailure(err) || options.extra[_retriedKey] == true) {
      return handler.next(err);
    }
    final switched = pool.markFailure(Uri.parse(options.baseUrl));
    if (switched == null) return handler.next(err);

    options.extra[_retriedKey] = true;
    try {
      handler.resolve(await dio.fetch<dynamic>(options));
    } on DioException catch (e) {
      handler.next(e);
    }
  }

  bool _isDomainFailure(DioException err) {
    switch (err.type) {
      case DioExceptionType.connectionError:
      case DioExceptionType.connectionTimeout:
      case DioExceptionType.badCertificate:
        return true;
      case DioExceptionType.badResponse:
        return (err.response?.statusCode ?? 0) >= 500;
      default:
        return false;
    }
  }
}
