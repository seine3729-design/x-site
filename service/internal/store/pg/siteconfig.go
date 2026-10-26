package pg

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"xsite/service/internal/domain/platform"
	"xsite/service/internal/errs"
	"xsite/service/internal/store/pg/sqlcgen"
)

// SiteConfigRepo 实现 platform.SiteConfigRepo。
type SiteConfigRepo struct {
	q *sqlcgen.Queries
}

func NewSiteConfigRepo(pool *pgxpool.Pool) *SiteConfigRepo {
	return &SiteConfigRepo{q: sqlcgen.New(pool)}
}

func (r *SiteConfigRepo) GetPublicSiteConfig(ctx context.Context) (platform.SiteConfig, error) {
	row, err := r.q.GetPublicSiteConfig(ctx)
	if errors.Is(err, pgx.ErrNoRows) {
		return platform.SiteConfig{}, errs.ErrNotFound
	}
	if err != nil {
		return platform.SiteConfig{}, fmt.Errorf("读取站点配置: %w", err)
	}
	return platform.SiteConfig{
		Version:   row.Version,
		Site:      row.Site,
		Theme:     row.Theme,
		Layout:    row.Layout,
		SEO:       row.Seo,
		Analytics: row.Analytics,
		AgeGate:   row.AgeGate,
		Features:  row.Features,
		Addresses: row.Addresses,
		UpdatedAt: row.UpdatedAt.Time,
	}, nil
}

// BumpVersion 令站点版本 +1 并返回新版本（S1-9）。
func (r *SiteConfigRepo) BumpVersion(ctx context.Context) (int64, error) {
	version, err := r.q.BumpSiteConfigVersion(ctx)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, errs.ErrNotFound
	}
	if err != nil {
		return 0, fmt.Errorf("更新站点版本: %w", err)
	}
	return version, nil
}

// DramaObjectPrefixes 返回一部剧各分集当前产物的目录（S4-4 清播放缓存用）。
func (r *SiteConfigRepo) DramaObjectPrefixes(ctx context.Context, dramaID int64) ([]string, error) {
	keys, err := r.q.DramaObjectPrefixes(ctx, dramaID)
	if err != nil {
		return nil, fmt.Errorf("读取剧集产物目录: %w", err)
	}
	return keys, nil
}
