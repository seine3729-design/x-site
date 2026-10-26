package geoip

import (
	"log/slog"

	"xsite/service/internal/domain/storage"
)

// 把 geoip.Resolver 接到选线域（S1-6c → S1-6d）。
// 适配器放在这里而不是 domain/storage：domain 不该知道 xdb 文件格式。

// GeoAdapter 让 *Resolver 满足 storage.GeoResolver。
type GeoAdapter struct{ R *Resolver }

var _ storage.GeoResolver = GeoAdapter{}

// Lookup 把解析结果转成选线用的地区。解析不出国家时返回未知地区（只匹配全局兜底线路），
// 不猜国家 —— 猜错会把用户导到完全不合适的线路上。
func (a GeoAdapter) Lookup(ip string) storage.Geo {
	if a.R == nil {
		return storage.Geo{}
	}
	r := a.R.Lookup(ip)
	return storage.Geo{Country: r.Country, Province: r.Province, ISP: r.ISP}
}

// NewStorageResolver 按文件路径建一个选线用的解析器。path 为空时返回 storage.NopGeoResolver
// （没有配置 GeoIP 数据：地区一律未知，只有全局兜底线路会命中）。
// 文件存在但损坏时返回错误，由调用方决定是启动失败还是降级 —— 这是部署配置问题，不该静默。
func NewStorageResolver(path string) (storage.GeoResolver, *Resolver, error) {
	if path == "" {
		slog.Info("未配置 GEOIP_XDB_PATH，地区解析一律为未知（只有全局兜底线路会命中）")
		return storage.NopGeoResolver, nil, nil
	}
	r, err := NewResolver(path)
	if err != nil {
		return nil, nil, err
	}
	s := r.Stats()
	slog.Info("GeoIP 数据库已加载", "path", path, "segments", s.Segments,
		"created_at", s.CreatedAt.Format("2006-01-02"))
	return GeoAdapter{R: r}, r, nil
}
