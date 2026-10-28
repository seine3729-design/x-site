package geoip

import (
	"fmt"
	"math/rand/v2"
	"net/netip"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"testing"
	"time"
)

func TestResolver_Lookup(t *testing.T) {
	r := NewResolverFromDB(buildTestDB(t))

	cases := []struct {
		ip   string
		want Region
	}{
		{"1.0.0.1", Region{Country: "CN", Province: "广东", City: "深圳市", ISP: "电信"}},
		{"2.2.1.1", Region{Country: "CN", Province: "新疆", City: "乌鲁木齐市", ISP: "移动"}},
		{"8.8.8.8", Region{Country: "US", ISP: "谷歌"}},
		// 不在库中 → 零值（调用方按未知地区 XX 处理）
		{"5.5.5.5", Region{}},
		// 非法输入不 panic
		{"not-an-ip", Region{}},
		{"", Region{}},
	}
	for _, c := range cases {
		if got := r.Lookup(c.ip); got != c.want {
			t.Errorf("Lookup(%q)=%+v，期望 %+v", c.ip, got, c.want)
		}
	}
}

// 内网、回环等地址不查库（本地开发与内网回源都会走到这里，docs/12 第 4.2 节：记为 XX）。
func TestResolver_SkipsPrivateAddresses(t *testing.T) {
	r := NewResolverFromDB(buildTestDB(t))
	for _, ip := range []string{
		"127.0.0.1", "10.0.0.1", "192.168.1.1", "172.16.0.1",
		"169.254.1.1", "0.0.0.0", "224.0.0.1", "::1", "fc00::1",
	} {
		if got := r.Lookup(ip); got != (Region{}) {
			t.Errorf("%s 应判为未知地区，得到 %+v", ip, got)
		}
	}
	// 没有查库，所以缓存里不该有条目
	if n := r.cache.len(); n != 0 {
		t.Errorf("内网地址不该进缓存，缓存里有 %d 条", n)
	}
}

func TestResolver_CachesByPrefix(t *testing.T) {
	r := NewResolverFromDB(buildTestDB(t))

	// 同一个 /24 内的不同地址共用一条缓存
	r.Lookup("1.0.0.1")
	hits1, misses1 := r.cache.counters()
	r.Lookup("1.0.0.2")
	r.Lookup("1.0.0.200")
	hits2, misses2 := r.cache.counters()

	if misses2 != misses1 {
		t.Errorf("同 /24 的后续查询不该再 miss（%d → %d）", misses1, misses2)
	}
	if hits2 != hits1+2 {
		t.Errorf("应命中缓存 2 次（%d → %d）", hits1, hits2)
	}
	if n := r.cache.len(); n != 1 {
		t.Errorf("同 /24 应只占一条缓存，得到 %d", n)
	}

	// 不同 /24 各占一条
	r.Lookup("2.2.1.1")
	if n := r.cache.len(); n != 2 {
		t.Errorf("不同 /24 应各占一条，得到 %d", n)
	}
}

// 不在库中的结果也要缓存，否则同一网段的每个请求都要白跑一次二分查找。
func TestResolver_CachesMisses(t *testing.T) {
	r := NewResolverFromDB(buildTestDB(t))
	r.Lookup("99.99.99.1")
	if n := r.cache.len(); n != 1 {
		t.Fatalf("未命中的结果也应缓存，缓存里有 %d 条", n)
	}
	_, missesBefore := r.cache.counters()
	r.Lookup("99.99.99.2")
	_, missesAfter := r.cache.counters()
	if missesAfter != missesBefore {
		t.Error("同网段的第二次查询应命中缓存")
	}
}

func TestPrefixKey(t *testing.T) {
	k := func(s string) uint64 {
		key, ok := prefixKey(mustAddr(t, s))
		if !ok {
			t.Fatalf("%s 应能生成缓存键", s)
		}
		return key
	}
	// IPv4 按 /24
	if k("1.2.3.4") != k("1.2.3.200") {
		t.Error("同 /24 应得到相同的键")
	}
	if k("1.2.3.4") == k("1.2.4.4") {
		t.Error("不同 /24 应得到不同的键")
	}
	// IPv6 按 /48
	if k("2001:db8:1::1") != k("2001:db8:1:ffff::2") {
		t.Error("同 /48 应得到相同的键")
	}
	if k("2001:db8:1::1") == k("2001:db8:2::1") {
		t.Error("不同 /48 应得到不同的键")
	}
	// v4 与 v6 的键空间不重叠
	if k("1.2.3.4") == k("102:304::") {
		t.Error("v4 与 v6 的键不该碰撞")
	}
}

// 热更新：文件变化后原子替换，且清掉旧缓存。
func TestResolver_Reload(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ip2region.xdb")

	first, err := BuildXDB([]Entry{
		{mustAddr(t, "1.0.0.0"), mustAddr(t, "1.0.0.255"), "中国|0|广东省|深圳市|电信"},
	}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, first, 0o600); err != nil {
		t.Fatal(err)
	}

	r, err := NewResolver(path)
	if err != nil {
		t.Fatalf("加载失败：%v", err)
	}
	if got := r.Lookup("1.0.0.1"); got.Province != "广东" {
		t.Fatalf("初次解析错误：%+v", got)
	}
	if r.cache.len() != 1 {
		t.Fatal("应有一条缓存")
	}

	// 文件没变：不重载
	changed, err := r.Reload()
	if err != nil {
		t.Fatalf("Reload 出错：%v", err)
	}
	if changed {
		t.Error("文件没变时不该重载")
	}

	// 换一份数据：同一个网段改成别的省份
	second, err := BuildXDB([]Entry{
		{mustAddr(t, "1.0.0.0"), mustAddr(t, "1.0.0.255"), "中国|0|浙江省|杭州市|联通"},
	}, 2)
	if err != nil {
		t.Fatal(err)
	}
	// mtime 要有可见变化
	if err := os.WriteFile(path, second, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, time.Now().Add(time.Hour), time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}

	changed, err = r.Reload()
	if err != nil {
		t.Fatalf("Reload 出错：%v", err)
	}
	if !changed {
		t.Fatal("文件变化后应重载")
	}
	// 缓存被清掉，新数据生效
	if got := r.Lookup("1.0.0.1"); got.Province != "浙江" || got.ISP != "联通" {
		t.Errorf("热更新后应返回新数据，得到 %+v", got)
	}
	if s := r.Stats(); !s.Loaded || s.CreatedAt.Unix() != 2 {
		t.Errorf("状态应反映新数据库：%+v", s)
	}
}

// 坏文件不能破坏已有的解析能力：宁可继续用旧数据。
func TestResolver_ReloadKeepsOldDBOnBadFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ip2region.xdb")
	good, err := BuildXDB([]Entry{
		{mustAddr(t, "1.0.0.0"), mustAddr(t, "1.0.0.255"), "中国|0|广东省|深圳市|电信"},
	}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, good, 0o600); err != nil {
		t.Fatal(err)
	}
	r, err := NewResolver(path)
	if err != nil {
		t.Fatal(err)
	}

	// 写入一个损坏的文件
	if err := os.WriteFile(path, []byte("garbage"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, time.Now().Add(time.Hour), time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if changed, err := r.Reload(); changed || err == nil {
		t.Error("坏文件应报错且不替换")
	}
	// 旧数据仍可用
	if got := r.Lookup("1.0.0.1"); got.Province != "广东" {
		t.Errorf("坏文件不该影响已加载的数据，得到 %+v", got)
	}
}

func TestResolver_ConcurrentLookupAndReload(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ip2region.xdb")
	raw, err := BuildXDB(sampleEntries(t), 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	r, err := NewResolver(path)
	if err != nil {
		t.Fatal(err)
	}

	// 并发查询 + 并发重载：靠 -race 抓数据竞争
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 500 {
				r.Lookup("1.0.0.1")
				r.Lookup("8.8.8.8")
				r.Lookup("99.1.2.3")
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := range 5 {
			_ = os.Chtimes(path, time.Now().Add(time.Duration(i)*time.Hour), time.Now().Add(time.Duration(i)*time.Hour))
			_, _ = r.Reload()
		}
	}()
	wg.Wait()

	if got := r.Lookup("1.0.0.1"); got.Province != "广东" {
		t.Errorf("并发后数据应完好，得到 %+v", got)
	}
}

func TestPrefixCache_EvictsWhenFull(t *testing.T) {
	c := newPrefixCache(10, time.Hour)
	for i := range 15 {
		c.put(uint64(i), Region{Country: "CN"})
	}
	// 容量满时整体清空，所以条数不会超过上限
	if n := c.len(); n > 10 {
		t.Errorf("缓存条数 %d 超过上限 10", n)
	}
}

func TestPrefixCache_Expires(t *testing.T) {
	c := newPrefixCache(10, time.Millisecond)
	c.put(1, Region{Country: "CN"})
	if _, ok := c.get(1); !ok {
		t.Fatal("刚写入应能读到")
	}
	time.Sleep(5 * time.Millisecond)
	if _, ok := c.get(1); ok {
		t.Error("过期后不该读到")
	}
}

// ---------- 性能（docs/12 第 4.2 节：10 万次查询 P99 < 20µs，单次解析 < 5µs）----------

// buildLargeDB 造一个接近真实规模的数据库（约 5 万段）。
func buildLargeDB(t testing.TB) *XDB {
	t.Helper()
	const segments = 50_000
	entries := make([]Entry, 0, segments)
	// 每段占 64 个地址，连续铺开
	for i := range segments {
		base := uint32(i) * 64
		// 跳过 0.x 段，避免与保留地址混淆
		base += 1 << 24
		start := netip.AddrFrom4([4]byte{byte(base >> 24), byte(base >> 16), byte(base >> 8), byte(base)})
		e := base + 63
		end := netip.AddrFrom4([4]byte{byte(e >> 24), byte(e >> 16), byte(e >> 8), byte(e)})
		entries = append(entries, Entry{start, end, fmt.Sprintf("中国|0|省%d|市%d|电信", i%34, i%300)})
	}
	raw, err := BuildXDB(entries, 1)
	if err != nil {
		t.Fatalf("生成大数据库失败：%v", err)
	}
	db, err := NewXDB(raw)
	if err != nil {
		t.Fatalf("读取大数据库失败：%v", err)
	}
	return db
}

// TestResolver_LookupLatency 验收 docs/12 的性能预算：10 万次查询的 P99。
// 用 XSITE_PERF=1 开启（默认跳过，避免在慢机器或并行跑测试时误报）。
func TestResolver_LookupLatency(t *testing.T) {
	if os.Getenv("XSITE_PERF") == "" {
		t.Skip("设置 XSITE_PERF=1 运行性能验收")
	}
	db := buildLargeDB(t)
	r := NewResolverFromDB(db)

	const n = 100_000
	rng := rand.New(rand.NewPCG(1, 2))
	addrs := make([]netip.Addr, n)
	for i := range addrs {
		// 在数据库覆盖范围内随机取地址，并故意让部分落在同一 /24（模拟真实的热点分布）
		base := uint32(1<<24) + uint32(rng.IntN(50_000*64))
		addrs[i] = netip.AddrFrom4([4]byte{byte(base >> 24), byte(base >> 16), byte(base >> 8), byte(base)})
	}

	lat := make([]time.Duration, n)
	for i, a := range addrs {
		start := time.Now()
		r.LookupAddr(a)
		lat[i] = time.Since(start)
	}
	sort.Slice(lat, func(i, j int) bool { return lat[i] < lat[j] })
	p50, p99 := lat[n/2], lat[n*99/100]
	stats := r.Stats()
	t.Logf("%d 次查询：P50=%v P99=%v（%d 段，缓存 %d 条，命中 %d / 未命中 %d）",
		n, p50, p99, db.Segments(), stats.CacheSize, stats.Hits, stats.Misses)

	if p99 > 20*time.Microsecond {
		t.Errorf("P99=%v 超过预算 20µs", p99)
	}
}

func BenchmarkResolver_LookupCached(b *testing.B) {
	r := NewResolverFromDB(buildLargeDB(b))
	addr := netip.AddrFrom4([4]byte{1, 0, 0, 1})
	r.LookupAddr(addr) // 预热缓存
	b.ResetTimer()
	for range b.N {
		r.LookupAddr(addr)
	}
}

func BenchmarkXDB_LookupUncached(b *testing.B) {
	db := buildLargeDB(b)
	rng := rand.New(rand.NewPCG(1, 2))
	addrs := make([]netip.Addr, 4096)
	for i := range addrs {
		base := uint32(1<<24) + uint32(rng.IntN(50_000*64))
		addrs[i] = netip.AddrFrom4([4]byte{byte(base >> 24), byte(base >> 16), byte(base >> 8), byte(base)})
	}
	b.ResetTimer()
	for i := range b.N {
		_, _ = db.Lookup(addrs[i%len(addrs)])
	}
}

// 没配 GEOIP_XDB_PATH 时 NewStorageResolver 的第二个返回值是 nil *Resolver，
// 装配方会把它装进 jobs.GeoReloader 接口交给周期任务 —— 那是「非 nil 接口里装着 nil 指针」，
// 接口侧的 r == nil 守卫失效。所以 Reload 必须对 nil 接收者安全，否则任务每轮都 panic。
func TestReloadIsNilReceiverSafe(t *testing.T) {
	_, reloader, err := NewStorageResolver("")
	if err != nil {
		t.Fatalf("空路径不该报错：%v", err)
	}
	if reloader != nil {
		t.Fatalf("空路径应返回 nil *Resolver，得到 %v", reloader)
	}
	changed, err := reloader.Reload()
	if changed || err != nil {
		t.Fatalf("nil 解析器的 Reload 应返回 (false, nil)，得到 (%v, %v)", changed, err)
	}
}

// 守住上面那个坑的成因：typed nil 装进接口后 != nil，所以不能只靠接口侧的 nil 判断。
func TestNilResolverInInterfaceIsNotNil(t *testing.T) {
	_, reloader, _ := NewStorageResolver("")
	var asInterface interface{ Reload() (bool, error) } = reloader
	if asInterface == nil {
		t.Fatal("如果这条断言失败，说明 Go 改了 typed nil 的语义，装配处的守卫可以简化")
	}
}
