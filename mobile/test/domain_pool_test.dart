import 'package:flutter_test/flutter_test.dart';
import 'package:xsite_app/core/config/env.dart';
import 'package:xsite_app/core/net/domain_pool.dart';

void main() {
  final a = Uri.parse('https://a.example.net');
  final b = Uri.parse('https://b.example.org');
  final c = Uri.parse('https://c.example.dev');

  group('DomainPool', () {
    test('失败次数未到阈值时不切换', () {
      final pool = DomainPool([a, b], failureThreshold: 3);
      expect(pool.markFailure(a), isNull);
      expect(pool.markFailure(a), isNull);
      expect(pool.current, a);
    });

    test('达到阈值后切换到下一个域名', () {
      final pool = DomainPool([a, b, c], failureThreshold: 2);
      pool.markFailure(a);
      expect(pool.markFailure(a), b);
      expect(pool.current, b);
    });

    test('成功会清零失败计数', () {
      final pool = DomainPool([a, b], failureThreshold: 2);
      pool.markFailure(a);
      pool.markSuccess(a);
      expect(pool.markFailure(a), isNull);
    });

    test('跳过冷却中的域名', () {
      var now = DateTime(2026, 10, 21, 12);
      final pool = DomainPool([a, b, c], failureThreshold: 1, now: () => now);
      expect(pool.markFailure(a), b);
      expect(pool.markFailure(b), c);
      // a、b 都在冷却，c 再失败时只能回到最早恢复的 a
      expect(pool.markFailure(c), a);
      now = now.add(const Duration(minutes: 6));
      expect(pool.markFailure(a), b);
    });

    test('证书异常立即切换', () {
      final pool = DomainPool([a, b]);
      expect(pool.switchNow(), b);
    });
  });

  group('Env.parseHosts', () {
    test('忽略非法地址，生产环境只接受 https', () {
      expect(Env.parseHosts('http://10.0.2.2:8080, ,not a url'), [Uri.parse('http://10.0.2.2:8080')]);
      expect(Env.parseHosts('http://x.com,https://y.com', requireHttps: true), [Uri.parse('https://y.com')]);
    });
  });
}
