package main

import (
	"fmt"
	"net/netip"
	"os"
	"path/filepath"

	"xsite/service/internal/geoip"
)

// geoipSample 生成一个本地开发用的小 xdb 数据集（S1-6c）。
//
// 没有真实 ip2region 数据文件时，用它让整条「IP → 地区 → 选线」链路能跑起来：
// 覆盖几个真实世界里好认的网段（各运营商、港台、海外），足够验证地区规则与按地区选线。
// 线上请用 ip2region 官方生成的数据文件，本包的读取器直接读官方格式。
func geoipSample(outPath string) error {
	addr := func(s string) netip.Addr {
		a, err := netip.ParseAddr(s)
		if err != nil {
			panic("样本网段写错了: " + s)
		}
		return a
	}
	seg := func(start, end, region string) geoip.Entry {
		return geoip.Entry{Start: addr(start), End: addr(end), Region: region}
	}
	// 这些网段按真实归属挑选，方便用熟悉的 IP 验证（如 114.114.114.114 是江苏电信的公共 DNS）
	entries := []geoip.Entry{
		seg("1.2.4.0", "1.2.4.255", "中国|0|北京市|北京市|电信"),
		seg("36.96.0.0", "36.111.255.255", "中国|0|河南省|郑州市|电信"),
		seg("114.114.114.0", "114.114.115.255", "中国|0|江苏省|南京市|电信"),
		seg("119.29.0.0", "119.29.255.255", "中国|0|北京市|北京市|联通"),
		seg("120.76.0.0", "120.79.255.255", "中国|0|广东省|深圳市|电信"),
		seg("183.60.0.0", "183.63.255.255", "中国|0|广东省|广州市|电信"),
		seg("211.136.0.0", "211.139.255.255", "中国|0|广东省|广州市|移动"),
		seg("221.5.0.0", "221.7.255.255", "中国|0|广东省|深圳市|联通"),
		seg("223.5.5.0", "223.5.5.255", "中国|0|浙江省|杭州市|移动"),
		seg("202.96.209.0", "202.96.209.255", "中国|0|上海市|上海市|电信"),
		// 港澳台
		seg("8.210.0.0", "8.210.255.255", "香港|0|0|香港|阿里云"),
		seg("103.252.0.0", "103.252.3.255", "台湾|0|0|台北|中华电信"),
		// 海外
		seg("8.8.8.0", "8.8.8.255", "美国|0|0|0|谷歌"),
		seg("1.1.1.0", "1.1.1.255", "美国|0|0|0|Cloudflare"),
		seg("13.107.0.0", "13.107.255.255", "美国|0|0|西雅图|微软"),
		seg("133.242.0.0", "133.242.255.255", "日本|0|0|东京|SAKURA"),
		seg("175.45.176.0", "175.45.179.255", "新加坡|0|0|新加坡|0"),
	}
	raw, err := geoip.BuildXDB(entries, 0)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(outPath, raw, 0o644); err != nil {
		return err
	}
	db, err := geoip.NewXDB(raw)
	if err != nil {
		return fmt.Errorf("生成的文件读不回来: %w", err)
	}
	fmt.Printf("已生成 %s（%d 段，%d 字节）\n", outPath, db.Segments(), len(raw))
	fmt.Println("把它接入：在 .env 里设 GEOIP_XDB_PATH，重启 service 后 /api/v1/lines 就按真实地区选线")
	return nil
}
