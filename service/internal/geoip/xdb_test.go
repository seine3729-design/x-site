package geoip

import (
	"encoding/binary"
	"errors"
	"net/netip"
	"testing"
	"time"
)

func mustAddr(t *testing.T, s string) netip.Addr {
	t.Helper()
	a, err := netip.ParseAddr(s)
	if err != nil {
		t.Fatalf("地址 %q 无法解析：%v", s, err)
	}
	return a
}

// sampleEntries 是一组覆盖多种情形的地区段。
func sampleEntries(t *testing.T) []Entry {
	t.Helper()
	return []Entry{
		{mustAddr(t, "1.0.0.0"), mustAddr(t, "1.0.0.255"), "中国|0|广东省|深圳市|电信"},
		{mustAddr(t, "1.0.1.0"), mustAddr(t, "1.0.3.255"), "中国|0|北京市|北京市|联通"},
		{mustAddr(t, "2.2.0.0"), mustAddr(t, "2.2.255.255"), "中国|0|新疆维吾尔自治区|乌鲁木齐市|移动"},
		{mustAddr(t, "8.8.8.0"), mustAddr(t, "8.8.8.255"), "美国|0|0|0|谷歌"},
		{mustAddr(t, "203.0.113.0"), mustAddr(t, "203.0.113.255"), "日本|0|0|东京|0"},
		// 跨多个 /16 的大段（验证 vector index 的多槽位覆盖）
		{mustAddr(t, "100.0.0.0"), mustAddr(t, "100.3.255.255"), "中国|0|浙江省|杭州市|电信"},
	}
}

func buildTestDB(t *testing.T) *XDB {
	t.Helper()
	raw, err := BuildXDB(sampleEntries(t), uint32(time.Unix(1_700_000_000, 0).Unix()))
	if err != nil {
		t.Fatalf("生成 xdb 失败：%v", err)
	}
	db, err := NewXDB(raw)
	if err != nil {
		t.Fatalf("读取 xdb 失败：%v", err)
	}
	return db
}

func TestXDB_Lookup(t *testing.T) {
	db := buildTestDB(t)
	if db.Segments() != 6 {
		t.Errorf("索引条数=%d，期望 6", db.Segments())
	}

	cases := []struct{ ip, want string }{
		{"1.0.0.1", "中国|0|广东省|深圳市|电信"},
		{"1.0.0.0", "中国|0|广东省|深圳市|电信"},   // 段的起始边界
		{"1.0.0.255", "中国|0|广东省|深圳市|电信"}, // 段的结束边界
		{"1.0.2.77", "中国|0|北京市|北京市|联通"},  // 跨 /24 的段内部
		{"2.2.123.45", "中国|0|新疆维吾尔自治区|乌鲁木齐市|移动"},
		{"8.8.8.8", "美国|0|0|0|谷歌"},
		{"203.0.113.9", "日本|0|0|东京|0"},
		{"100.2.3.4", "中国|0|浙江省|杭州市|电信"}, // 跨多个 /16 的大段
		{"100.0.0.0", "中国|0|浙江省|杭州市|电信"},
		{"100.3.255.255", "中国|0|浙江省|杭州市|电信"},
	}
	for _, c := range cases {
		got, err := db.Lookup(mustAddr(t, c.ip))
		if err != nil {
			t.Errorf("%s 查询出错：%v", c.ip, err)
			continue
		}
		if got != c.want {
			t.Errorf("%s → %q，期望 %q", c.ip, got, c.want)
		}
	}
}

func TestXDB_LookupNotFound(t *testing.T) {
	db := buildTestDB(t)
	// 段之间的空隙、完全没覆盖的网段、IPv6 都应是 ErrNotFound
	for _, ip := range []string{"1.0.4.0", "5.5.5.5", "99.99.99.99", "2001:db8::1"} {
		if _, err := db.Lookup(mustAddr(t, ip)); !errors.Is(err, ErrNotFound) {
			t.Errorf("%s 应返回 ErrNotFound，得到 %v", ip, err)
		}
	}
}

func TestNewXDB_RejectsBadFiles(t *testing.T) {
	good, err := BuildXDB(sampleEntries(t), 0)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("文件太小", func(t *testing.T) {
		if _, err := NewXDB([]byte{1, 2, 3}); err == nil {
			t.Error("应拒绝残缺文件")
		}
	})
	t.Run("结构版本不支持", func(t *testing.T) {
		bad := append([]byte(nil), good...)
		binary.LittleEndian.PutUint16(bad[0:2], 3) // v3
		if _, err := NewXDB(bad); err == nil {
			t.Error("应拒绝 v3（IPv6 格式未接入）")
		}
	})
	t.Run("索引策略不支持", func(t *testing.T) {
		bad := append([]byte(nil), good...)
		binary.LittleEndian.PutUint16(bad[2:4], 2) // btree
		if _, err := NewXDB(bad); err == nil {
			t.Error("应拒绝 btree 索引")
		}
	})
	t.Run("索引区越界", func(t *testing.T) {
		bad := append([]byte(nil), good...)
		binary.LittleEndian.PutUint32(bad[12:16], uint32(len(bad)+100))
		if _, err := NewXDB(bad); err == nil {
			t.Error("应拒绝索引区超出文件范围的文件")
		}
	})
	t.Run("索引区长度不对齐", func(t *testing.T) {
		bad := append([]byte(nil), good...)
		start := binary.LittleEndian.Uint32(bad[8:12])
		binary.LittleEndian.PutUint32(bad[12:16], start+5) // 不是 14 的倍数
		if _, err := NewXDB(bad); err == nil {
			t.Error("应拒绝索引区长度不对齐的文件")
		}
	})
	t.Run("索引区位置非法", func(t *testing.T) {
		bad := append([]byte(nil), good...)
		binary.LittleEndian.PutUint32(bad[8:12], 10) // 落在 header 里
		if _, err := NewXDB(bad); err == nil {
			t.Error("应拒绝索引区位置非法的文件")
		}
	})
}

func TestBuildXDB_RejectsBadInput(t *testing.T) {
	a := mustAddr(t, "1.1.1.1")
	b := mustAddr(t, "1.1.1.9")
	v6 := mustAddr(t, "2001:db8::1")

	if _, err := BuildXDB(nil, 0); err == nil {
		t.Error("空输入应报错")
	}
	if _, err := BuildXDB([]Entry{{b, a, "中国|0|0|0|0"}}, 0); err == nil {
		t.Error("起止颠倒应报错")
	}
	if _, err := BuildXDB([]Entry{{v6, v6, "中国|0|0|0|0"}}, 0); err == nil {
		t.Error("IPv6 应报错")
	}
	// 重叠的段
	overlap := []Entry{
		{mustAddr(t, "1.0.0.0"), mustAddr(t, "1.0.0.255"), "中国|0|广东省|0|电信"},
		{mustAddr(t, "1.0.0.100"), mustAddr(t, "1.0.1.0"), "中国|0|北京市|0|联通"},
	}
	if _, err := BuildXDB(overlap, 0); err == nil {
		t.Error("重叠的段应报错")
	}
}

func TestParseRegion(t *testing.T) {
	cases := []struct {
		raw  string
		want Region
	}{
		{"中国|0|广东省|深圳市|电信", Region{Country: "CN", Province: "广东", City: "深圳市", ISP: "电信"}},
		{"中国|0|北京市|北京市|联通", Region{Country: "CN", Province: "北京", City: "北京市", ISP: "联通"}},
		{"中国|0|新疆维吾尔自治区|乌鲁木齐市|移动", Region{Country: "CN", Province: "新疆", City: "乌鲁木齐市", ISP: "移动"}},
		{"中国|0|宁夏回族自治区|银川市|电信", Region{Country: "CN", Province: "宁夏", City: "银川市", ISP: "电信"}},
		{"中国|0|香港特别行政区|0|0", Region{Country: "CN", Province: "香港"}},
		// 运营商名归一
		{"中国|0|广东省|广州市|中国电信", Region{Country: "CN", Province: "广东", City: "广州市", ISP: "电信"}},
		{"中国|0|广东省|广州市|中国移动通信", Region{Country: "CN", Province: "广东", City: "广州市", ISP: "移动"}},
		// 未知级别用 0 表示
		{"美国|0|0|0|谷歌", Region{Country: "US", ISP: "谷歌"}},
		{"日本|0|0|东京|0", Region{Country: "JP", City: "东京"}},
		// 未收录的国家留空，让调用方按未知地区处理（不猜）
		{"马耳他|0|0|0|0", Region{}},
		// 字段不全
		{"中国", Region{Country: "CN"}},
		{"", Region{}},
	}
	for _, c := range cases {
		if got := ParseRegion(c.raw); got != c.want {
			t.Errorf("ParseRegion(%q)=%+v，期望 %+v", c.raw, got, c.want)
		}
	}
}
