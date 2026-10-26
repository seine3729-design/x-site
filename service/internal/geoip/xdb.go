// Package geoip 把客户端 IP 解析成地区（docs/12 第 4.2 节，S1-6c）。
//
// 数据源是 ip2region 的 xdb 文件：一个文件就覆盖「国家 | 区域 | 省份 | 城市 | 运营商」，
// 正好是地区规则需要的 country / province / isp 三个字段。mmdb（MaxMind GeoLite2）作为全球国家级的补充，
// 需要时再接（见 Resolver 的扩展点）。
//
// 本包只做纯解析与缓存，不碰数据库与网络：文件由运维放到 GEOIP_XDB_PATH，变更后原子替换内存中的读取器。
package geoip

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"strings"
)

// xdb 文件布局（ip2region v2，小端序）：
//
//	+--------------+----------------------+---------------+----------------+
//	| header 256B  | vector index 512KB   | ... data ...  | segment index  |
//	+--------------+----------------------+---------------+----------------+
//
// header：version(2) + indexPolicy(2) + createdAt(4) + startIndexPtr(4) + endIndexPtr(4) + 保留
// vector index：256×256 格，每格 8 字节（该 /16 前缀对应的 segment index 区间 startPtr(4) + endPtr(4)）
// segment index：每条 14 字节，startIP(4) + endIP(4) + dataLen(2) + dataPtr(4)
// data：region 字符串，形如「中国|0|广东省|深圳市|电信」
const (
	headerLength     = 256
	vectorIndexRows  = 256
	vectorIndexCols  = 256
	vectorIndexSize  = 8
	vectorIndexBytes = vectorIndexRows * vectorIndexCols * vectorIndexSize
	segmentIndexSize = 14

	// structureV2 是本包支持的文件结构版本（v3 加了 IPv6，格式不同，暂不支持）。
	structureV2 = 2
	// policyVector 是向量索引（官方生成器的默认策略）。
	policyVector = 1
)

// ErrNotFound 表示该 IP 不在数据库覆盖范围内。
var ErrNotFound = errors.New("geoip: 该地址不在数据库中")

// XDB 是加载到内存的 ip2region 数据库。整个文件读进内存（约 11 MB），查询无磁盘 IO、可被多个 goroutine 并发使用。
type XDB struct {
	data      []byte
	startPtr  uint32
	endPtr    uint32
	createdAt uint32
}

// OpenXDB 读取并校验一个 xdb 文件。
func OpenXDB(path string) (*XDB, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("geoip: 读取 %s: %w", path, err)
	}
	db, err := NewXDB(raw)
	if err != nil {
		return nil, fmt.Errorf("geoip: %s: %w", path, err)
	}
	return db, nil
}

// NewXDB 用一段 xdb 内容构造读取器，并校验文件头与索引区的完整性。
// 严格校验是故意的：读到不认识的文件要明确报错，而不是解析出看似合理的垃圾地区。
func NewXDB(raw []byte) (*XDB, error) {
	if len(raw) < headerLength+vectorIndexBytes {
		return nil, fmt.Errorf("文件太小（%d 字节），不是完整的 xdb", len(raw))
	}
	version := binary.LittleEndian.Uint16(raw[0:2])
	if version != structureV2 {
		return nil, fmt.Errorf("结构版本 %d 不支持（本包只支持 v2；v3 的 IPv6 格式待接入）", version)
	}
	policy := binary.LittleEndian.Uint16(raw[2:4])
	if policy != policyVector {
		return nil, fmt.Errorf("索引策略 %d 不支持（只支持向量索引）", policy)
	}
	createdAt := binary.LittleEndian.Uint32(raw[4:8])
	startPtr := binary.LittleEndian.Uint32(raw[8:12])
	endPtr := binary.LittleEndian.Uint32(raw[12:16])

	if startPtr < headerLength+vectorIndexBytes || endPtr < startPtr {
		return nil, fmt.Errorf("索引区位置非法（start=%d end=%d）", startPtr, endPtr)
	}
	if uint64(endPtr)+segmentIndexSize > uint64(len(raw)) {
		return nil, fmt.Errorf("索引区超出文件范围（end=%d，文件 %d 字节）", endPtr, len(raw))
	}
	if (endPtr-startPtr)%segmentIndexSize != 0 {
		return nil, fmt.Errorf("索引区长度 %d 不是 %d 的整数倍", endPtr-startPtr, segmentIndexSize)
	}
	return &XDB{data: raw, startPtr: startPtr, endPtr: endPtr, createdAt: createdAt}, nil
}

// CreatedAt 是数据库的生成时间（Unix 秒）；0 表示文件里没写。
func (d *XDB) CreatedAt() uint32 { return d.createdAt }

// Segments 是索引条数（用于日志与健康检查）。
// endPtr 按 ip2region 的约定指向**最后一条索引的起始偏移**（不是索引区末尾），所以条数要加回来一条。
func (d *XDB) Segments() int { return int((d.endPtr-d.startPtr)/segmentIndexSize) + 1 }

// Lookup 查一个 IPv4 地址，返回原始 region 字符串（「国家|区域|省份|城市|运营商」）。
// 不在库中时返回 ErrNotFound。IPv6 暂不支持（v3 格式待接入），同样返回 ErrNotFound。
func (d *XDB) Lookup(addr netip.Addr) (string, error) {
	v4 := addr.Unmap()
	if !v4.Is4() {
		return "", ErrNotFound
	}
	b := v4.As4()
	ip := binary.BigEndian.Uint32(b[:])

	// 用 IP 的前两字节定位 segment index 的搜索区间，把二分范围从全库缩到一个 /16
	slot := headerLength + (int(b[0])*vectorIndexCols+int(b[1]))*vectorIndexSize
	sPtr := binary.LittleEndian.Uint32(d.data[slot : slot+4])
	ePtr := binary.LittleEndian.Uint32(d.data[slot+4 : slot+8])
	if sPtr == 0 || ePtr < sPtr {
		return "", ErrNotFound
	}
	if uint64(ePtr)+segmentIndexSize > uint64(len(d.data)) {
		return "", fmt.Errorf("geoip: 向量索引越界（end=%d）", ePtr)
	}

	// 在 [sPtr, ePtr] 区间二分查找覆盖该 IP 的段
	lo, hi := 0, int((ePtr-sPtr)/segmentIndexSize)
	for lo <= hi {
		mid := (lo + hi) / 2
		pos := int(sPtr) + mid*segmentIndexSize
		startIP := binary.LittleEndian.Uint32(d.data[pos : pos+4])
		endIP := binary.LittleEndian.Uint32(d.data[pos+4 : pos+8])
		switch {
		case ip < startIP:
			hi = mid - 1
		case ip > endIP:
			lo = mid + 1
		default:
			dataLen := int(binary.LittleEndian.Uint16(d.data[pos+8 : pos+10]))
			dataPtr := int(binary.LittleEndian.Uint32(d.data[pos+10 : pos+14]))
			if dataPtr < 0 || dataPtr+dataLen > len(d.data) {
				return "", fmt.Errorf("geoip: 数据区越界（ptr=%d len=%d）", dataPtr, dataLen)
			}
			return string(d.data[dataPtr : dataPtr+dataLen]), nil
		}
	}
	return "", ErrNotFound
}

// Region 是解析出的地区。字段取自 xdb 的 region 字符串，未知的留空。
type Region struct {
	Country  string // ISO alpha-2（由中文国名映射，未知为空）
	Province string
	City     string
	ISP      string
}

// ParseRegion 把 xdb 的 region 字符串（「国家|区域|省份|城市|运营商」）解析成结构。
// ip2region 用 0 表示该级未知；中国的省份带「省」「市」「自治区」等后缀，这里统一去掉，
// 与后台地区规则里填的「广东」「北京」对齐。
func ParseRegion(raw string) Region {
	parts := strings.Split(raw, "|")
	get := func(i int) string {
		if i >= len(parts) {
			return ""
		}
		s := strings.TrimSpace(parts[i])
		if s == "0" || s == "" {
			return ""
		}
		return s
	}
	return Region{
		Country:  countryCode(get(0)),
		Province: trimProvinceSuffix(get(2)),
		City:     get(3),
		ISP:      normalizeISP(get(4)),
	}
}

// 省级行政区的后缀：去掉后与后台规则里填的短名一致。
var provinceSuffixes = []string{
	"维吾尔自治区", "壮族自治区", "回族自治区", "自治区", "特别行政区", "省", "市",
}

func trimProvinceSuffix(s string) string {
	for _, suffix := range provinceSuffixes {
		if trimmed := strings.TrimSuffix(s, suffix); trimmed != s && trimmed != "" {
			return trimmed
		}
	}
	return s
}

// normalizeISP 把运营商名归一成后台规则里用的短名。
func normalizeISP(s string) string {
	switch {
	case s == "":
		return ""
	case strings.Contains(s, "电信"):
		return "电信"
	case strings.Contains(s, "联通"):
		return "联通"
	case strings.Contains(s, "移动"):
		return "移动"
	case strings.Contains(s, "铁通"):
		return "铁通"
	case strings.Contains(s, "教育网"):
		return "教育网"
	case strings.Contains(s, "广电"):
		return "广电"
	default:
		return s
	}
}

// countryCode 把 xdb 里的中文国名映射成 ISO alpha-2。
// 只列常见来源地；未收录的返回空，由调用方按未知地区（XX）处理 —— 宁可当未知走全局兜底线路，
// 也不要猜错国家把用户导到不合适的线路上。
func countryCode(name string) string {
	if code, ok := countryCodes[name]; ok {
		return code
	}
	return ""
}

var countryCodes = map[string]string{
	"中国": "CN", "中國": "CN",
	"香港": "HK", "澳门": "MO", "澳門": "MO", "台湾": "TW", "台灣": "TW",
	"日本": "JP", "韩国": "KR", "韓國": "KR", "朝鲜": "KP",
	"新加坡": "SG", "马来西亚": "MY", "泰国": "TH", "越南": "VN", "印度尼西亚": "ID",
	"菲律宾": "PH", "柬埔寨": "KH", "缅甸": "MM", "老挝": "LA", "文莱": "BN",
	"印度": "IN", "巴基斯坦": "PK", "孟加拉国": "BD", "斯里兰卡": "LK", "尼泊尔": "NP",
	"美国": "US", "加拿大": "CA", "墨西哥": "MX", "巴西": "BR", "阿根廷": "AR", "智利": "CL",
	"英国": "GB", "法国": "FR", "德国": "DE", "意大利": "IT", "西班牙": "ES", "葡萄牙": "PT",
	"荷兰": "NL", "比利时": "BE", "瑞士": "CH", "瑞典": "SE", "挪威": "NO", "丹麦": "DK",
	"芬兰": "FI", "波兰": "PL", "俄罗斯": "RU", "乌克兰": "UA", "土耳其": "TR", "希腊": "GR",
	"奥地利": "AT", "捷克": "CZ", "匈牙利": "HU", "罗马尼亚": "RO", "爱尔兰": "IE",
	"澳大利亚": "AU", "新西兰": "NZ",
	"阿联酋": "AE", "沙特阿拉伯": "SA", "以色列": "IL", "伊朗": "IR", "伊拉克": "IQ", "卡塔尔": "QA",
	"埃及": "EG", "南非": "ZA", "尼日利亚": "NG", "肯尼亚": "KE", "摩洛哥": "MA",
}
