package geoip

import (
	"errors"
	"log/slog"
	"net/netip"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

// Resolver 把 IP 解析成地区（docs/12 第 4.2 节）：
//   - 查询结果按 IPv4 /24、IPv6 /48 前缀缓存在进程内（同一网段的用户共用一次解析）
//   - 数据库文件更新后原子替换内存中的读取器（atomic.Pointer），不中断服务、不加锁
//   - 内网与保留地址直接判为未知，不查库
//
// 零值不可用，用 NewResolver 构造。多 goroutine 并发安全。
type Resolver struct {
	db    atomic.Pointer[XDB]
	cache *prefixCache
	// path 与 loadedAt 用于热更新：文件 mtime 或大小变化时重新加载。
	path     string
	mu       sync.Mutex
	modTime  time.Time
	fileSize int64
}

// cacheCapacity 是前缀缓存的条数上限（docs/12：10 万条）。
const cacheCapacity = 100_000

// cacheTTL 是缓存条目的有效期（docs/12：1 小时）。数据库换版后旧条目最多存活这么久。
const cacheTTL = time.Hour

// NewResolver 加载 xdb 文件并建好缓存。
func NewResolver(path string) (*Resolver, error) {
	db, err := OpenXDB(path)
	if err != nil {
		return nil, err
	}
	r := &Resolver{path: path, cache: newPrefixCache(cacheCapacity, cacheTTL)}
	r.db.Store(db)
	r.rememberFileState()
	return r, nil
}

// NewResolverFromDB 用已加载的数据库构造（测试与内存数据用）。
func NewResolverFromDB(db *XDB) *Resolver {
	r := &Resolver{cache: newPrefixCache(cacheCapacity, cacheTTL)}
	r.db.Store(db)
	return r
}

// Lookup 解析一个 IP 字符串。无法解析、内网地址、不在库中时返回零值 Region（调用方按未知地区处理）。
func (r *Resolver) Lookup(ip string) Region {
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return Region{}
	}
	return r.LookupAddr(addr)
}

// LookupAddr 解析一个地址。
func (r *Resolver) LookupAddr(addr netip.Addr) Region {
	addr = addr.Unmap()
	// 内网、回环、链路本地等不查库（本地开发、内网回源都会走到这里）
	if !addr.IsValid() || addr.IsLoopback() || addr.IsPrivate() || addr.IsLinkLocalUnicast() ||
		addr.IsUnspecified() || addr.IsMulticast() {
		return Region{}
	}
	key, ok := prefixKey(addr)
	if !ok {
		return Region{}
	}
	if got, hit := r.cache.get(key); hit {
		return got
	}

	db := r.db.Load()
	if db == nil {
		return Region{}
	}
	raw, err := db.Lookup(addr)
	if err != nil {
		// 不在库中也缓存零值：避免同一网段的每个请求都去二分查找一次
		if errors.Is(err, ErrNotFound) {
			r.cache.put(key, Region{})
			return Region{}
		}
		slog.Warn("GeoIP 解析出错", "err", err)
		return Region{}
	}
	region := ParseRegion(raw)
	r.cache.put(key, region)
	return region
}

// Stats 是解析器的运行状态（供健康检查与后台展示）。
type Stats struct {
	Loaded    bool      `json:"loaded"`     // 是否已加载数据库
	Segments  int       `json:"segments"`   // 索引条数
	CreatedAt time.Time `json:"created_at"` // 数据库生成时间
	CacheSize int       `json:"cache_size"` // 缓存条数
	Hits      uint64    `json:"hits"`
	Misses    uint64    `json:"misses"`
}

// Stats 返回当前状态。
func (r *Resolver) Stats() Stats {
	s := Stats{CacheSize: r.cache.len()}
	s.Hits, s.Misses = r.cache.counters()
	if db := r.db.Load(); db != nil {
		s.Loaded = true
		s.Segments = db.Segments()
		if db.CreatedAt() > 0 {
			s.CreatedAt = time.Unix(int64(db.CreatedAt()), 0).UTC()
		}
	}
	return s
}

// Reload 在文件发生变化（mtime 或大小）时重新加载并原子替换，同时清空缓存。
// 返回是否真的替换了。文件没变或加载失败时保留当前数据库 —— 宁可用旧数据，也不要因为一次坏文件失去解析能力。
func (r *Resolver) Reload() (bool, error) {
	if r.path == "" {
		return false, nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	st, err := os.Stat(r.path)
	if err != nil {
		return false, err
	}
	if st.ModTime().Equal(r.modTime) && st.Size() == r.fileSize {
		return false, nil
	}
	db, err := OpenXDB(r.path)
	if err != nil {
		return false, err
	}
	r.db.Store(db)
	r.cache.clear()
	r.modTime, r.fileSize = st.ModTime(), st.Size()
	slog.Info("GeoIP 数据库已热更新", "path", r.path, "segments", db.Segments(),
		"created_at", time.Unix(int64(db.CreatedAt()), 0).UTC().Format(time.RFC3339))
	return true, nil
}

func (r *Resolver) rememberFileState() {
	if st, err := os.Stat(r.path); err == nil {
		r.modTime, r.fileSize = st.ModTime(), st.Size()
	}
}

// prefixKey 把地址归一到缓存键：IPv4 按 /24、IPv6 按 /48（docs/12 第 4.2 节）。
// 同一网段的用户共用一条缓存，既提高命中率，也避免缓存里留下完整 IP（隐私：IP 只用于本次匹配）。
func prefixKey(addr netip.Addr) (uint64, bool) {
	switch {
	case addr.Is4():
		b := addr.As4()
		// 高位标记 4，低位放 /24 前缀
		return 4<<56 | uint64(b[0])<<16 | uint64(b[1])<<8 | uint64(b[2]), true
	case addr.Is6():
		b := addr.As16()
		// /48 = 前 6 字节
		var v uint64
		for i := range 6 {
			v = v<<8 | uint64(b[i])
		}
		return 6<<56 | v, true
	}
	return 0, false
}

// ---------- 前缀缓存 ----------

type cacheEntry struct {
	region  Region
	expires time.Time
}

// prefixCache 是带 TTL 的定容缓存。容量满时整体清空而不是逐条淘汰：
// 解析结果的访问分布很集中（热门网段反复命中），整体清空的实现只有几行、没有链表维护开销，
// 偶尔的一次清空代价远小于每次查询都维护 LRU 链表。
type prefixCache struct {
	mu     sync.RWMutex
	m      map[uint64]cacheEntry
	cap    int
	ttl    time.Duration
	hits   atomic.Uint64
	misses atomic.Uint64
}

func newPrefixCache(capacity int, ttl time.Duration) *prefixCache {
	return &prefixCache{m: make(map[uint64]cacheEntry, capacity/8), cap: capacity, ttl: ttl}
}

func (c *prefixCache) get(key uint64) (Region, bool) {
	c.mu.RLock()
	e, ok := c.m[key]
	c.mu.RUnlock()
	if !ok || time.Now().After(e.expires) {
		c.misses.Add(1)
		return Region{}, false
	}
	c.hits.Add(1)
	return e.region, true
}

func (c *prefixCache) put(key uint64, region Region) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.m) >= c.cap {
		c.m = make(map[uint64]cacheEntry, c.cap/8)
	}
	c.m[key] = cacheEntry{region: region, expires: time.Now().Add(c.ttl)}
}

func (c *prefixCache) clear() {
	c.mu.Lock()
	c.m = make(map[uint64]cacheEntry, c.cap/8)
	c.mu.Unlock()
}

func (c *prefixCache) len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.m)
}

func (c *prefixCache) counters() (hits, misses uint64) {
	return c.hits.Load(), c.misses.Load()
}
