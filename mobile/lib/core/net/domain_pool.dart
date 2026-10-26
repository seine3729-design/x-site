/// API 域名池：记录当前使用的域名，失败时切换到下一个健康域名（docs/09 第 4.2 节）。
///
/// 规则：单个域名连续失败 [failureThreshold] 次进入 [cooldown] 冷却；
/// 全部域名都在冷却时，返回最早恢复的那个，保证总有地址可用。
/// 签名域名清单的下载与校验在任务 M5-1 中接入，这里只负责选择与切换。
class DomainPool {
  DomainPool(
    List<Uri> hosts, {
    this.failureThreshold = 3,
    this.cooldown = const Duration(minutes: 5),
    DateTime Function()? now,
  }) : assert(hosts.isNotEmpty, '至少需要一个 API 地址'),
       _hosts = List.unmodifiable(hosts),
       _now = now ?? DateTime.now;

  final List<Uri> _hosts;
  final int failureThreshold;
  final Duration cooldown;
  final DateTime Function() _now;

  final Map<Uri, int> _failures = {};
  final Map<Uri, DateTime> _coolingUntil = {};
  int _current = 0;

  List<Uri> get hosts => _hosts;

  /// 当前使用的地址。
  Uri get current => _hosts[_current];

  /// 请求成功：清零该域名的失败计数。
  void markSuccess(Uri host) {
    _failures.remove(host);
    _coolingUntil.remove(host);
  }

  /// 请求失败（连接失败、TLS 失败、超时、连续 5xx）。返回切换后的地址；未切换时返回 null。
  Uri? markFailure(Uri host) {
    final count = (_failures[host] ?? 0) + 1;
    _failures[host] = count;
    if (count < failureThreshold && host == current) {
      return null;
    }
    if (count >= failureThreshold) {
      _coolingUntil[host] = _now().add(cooldown);
    }
    if (host != current) return null;
    return _switchToNext();
  }

  /// 立即切换（例如证书锁定校验失败，疑似中间人）。
  Uri switchNow() {
    _coolingUntil[current] = _now().add(cooldown);
    return _switchToNext();
  }

  Uri _switchToNext() {
    final now = _now();
    for (var step = 1; step <= _hosts.length; step++) {
      final index = (_current + step) % _hosts.length;
      final until = _coolingUntil[_hosts[index]];
      if (until == null || !until.isAfter(now)) {
        _current = index;
        return current;
      }
    }
    // 全部在冷却：选最早恢复的；恢复时间相同时按轮转顺序取下一个，当前域名排在最后
    int? best;
    for (var step = 1; step <= _hosts.length; step++) {
      final index = (_current + step) % _hosts.length;
      if (best == null ||
          _coolingUntil[_hosts[index]]!.isBefore(
            _coolingUntil[_hosts[best]]!,
          )) {
        best = index;
      }
    }
    _current = best!;
    return current;
  }
}
