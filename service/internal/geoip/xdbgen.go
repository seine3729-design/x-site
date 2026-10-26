package geoip

import (
	"encoding/binary"
	"fmt"
	"net/netip"
	"sort"
)

// xdb 生成器：按 ip2region v2 的格式写出一个数据库。
//
// 用途是造测试数据与本地开发用的小数据集 —— 没有真实 ip2region 文件时也能让整条选线链路跑起来。
// 它不是数据生产工具：真实数据请用 ip2region 官方生成器，本包的读取器直接读官方文件。

// Entry 是一条地区段：[Start, End] 区间内的地址属于 Region。
type Entry struct {
	Start, End netip.Addr
	// Region 是 ip2region 的原始格式「国家|区域|省份|城市|运营商」，未知的级别写 0。
	Region string
}

// BuildXDB 把若干地区段写成一个 xdb 文件内容。段会按起始地址排序，必须互不重叠且都是 IPv4。
func BuildXDB(entries []Entry, createdAt uint32) ([]byte, error) {
	if len(entries) == 0 {
		return nil, fmt.Errorf("geoip: 至少需要一条地区段")
	}
	type seg struct {
		start, end uint32
		region     string
	}
	segs := make([]seg, 0, len(entries))
	for _, e := range entries {
		s, err := toV4(e.Start)
		if err != nil {
			return nil, err
		}
		t, err := toV4(e.End)
		if err != nil {
			return nil, err
		}
		if s > t {
			return nil, fmt.Errorf("geoip: 段的起止顺序颠倒（%s > %s）", e.Start, e.End)
		}
		segs = append(segs, seg{start: s, end: t, region: e.Region})
	}
	sort.Slice(segs, func(i, j int) bool { return segs[i].start < segs[j].start })
	for i := 1; i < len(segs); i++ {
		if segs[i].start <= segs[i-1].end {
			return nil, fmt.Errorf("geoip: 地区段重叠（%d 与 %d）", i-1, i)
		}
	}

	// 数据区：相同 region 只存一份
	var dataBuf []byte
	dataPos := make(map[string][2]int) // region → [ptr, len]
	for _, s := range segs {
		if _, ok := dataPos[s.region]; ok {
			continue
		}
		dataPos[s.region] = [2]int{len(dataBuf), len(s.region)}
		dataBuf = append(dataBuf, s.region...)
	}

	// 布局：header | vector index | data | segment index
	dataStart := headerLength + vectorIndexBytes
	indexStart := dataStart + len(dataBuf)
	total := indexStart + len(segs)*segmentIndexSize
	out := make([]byte, total)

	// 数据区
	copy(out[dataStart:], dataBuf)

	// segment index，并同时记录每个 /16 槽位覆盖的索引区间
	type span struct{ first, last int }
	slots := make(map[int]*span)
	for i, s := range segs {
		pos := indexStart + i*segmentIndexSize
		binary.LittleEndian.PutUint32(out[pos:pos+4], s.start)
		binary.LittleEndian.PutUint32(out[pos+4:pos+8], s.end)
		p := dataPos[s.region]
		binary.LittleEndian.PutUint16(out[pos+8:pos+10], uint16(p[1]))
		binary.LittleEndian.PutUint32(out[pos+10:pos+14], uint32(dataStart+p[0]))

		// 一个段可能横跨多个 /16，每个都要指向它
		for slot := int(s.start >> 16); slot <= int(s.end>>16); slot++ {
			if sp, ok := slots[slot]; ok {
				sp.last = i
			} else {
				slots[slot] = &span{first: i, last: i}
			}
		}
	}

	// vector index：每格 8 字节，指向该 /16 在 segment index 中的起止位置
	for slot, sp := range slots {
		off := headerLength + slot*vectorIndexSize
		binary.LittleEndian.PutUint32(out[off:off+4], uint32(indexStart+sp.first*segmentIndexSize))
		binary.LittleEndian.PutUint32(out[off+4:off+8], uint32(indexStart+sp.last*segmentIndexSize))
	}

	// header
	binary.LittleEndian.PutUint16(out[0:2], structureV2)
	binary.LittleEndian.PutUint16(out[2:4], policyVector)
	binary.LittleEndian.PutUint32(out[4:8], createdAt)
	binary.LittleEndian.PutUint32(out[8:12], uint32(indexStart))
	binary.LittleEndian.PutUint32(out[12:16], uint32(total-segmentIndexSize))
	return out, nil
}

func toV4(addr netip.Addr) (uint32, error) {
	a := addr.Unmap()
	if !a.Is4() {
		return 0, fmt.Errorf("geoip: 只支持 IPv4，得到 %s", addr)
	}
	b := a.As4()
	return binary.BigEndian.Uint32(b[:]), nil
}
