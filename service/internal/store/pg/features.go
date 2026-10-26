package pg

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"xsite/service/internal/errs"
	"xsite/service/internal/store/pg/sqlcgen"
)

// FeatureRepo 实现 platform.FeatureRepo（功能开关与年龄门，docs/06 第 36 节）。
type FeatureRepo struct {
	q *sqlcgen.Queries
}

func NewFeatureRepo(pool *pgxpool.Pool) *FeatureRepo {
	return &FeatureRepo{q: sqlcgen.New(pool)}
}

func (r *FeatureRepo) GetFeatureConfig(ctx context.Context) (json.RawMessage, json.RawMessage, error) {
	row, err := r.q.GetFeatureConfig(ctx)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, errs.ErrNotFound
	}
	if err != nil {
		return nil, nil, fmt.Errorf("读取功能开关: %w", err)
	}
	return row.Features, row.AgeGate, nil
}

func (r *FeatureRepo) SetFeatureFlags(ctx context.Context, features json.RawMessage) error {
	if err := r.q.SetFeatureFlags(ctx, features); err != nil {
		return fmt.Errorf("保存功能开关: %w", err)
	}
	return nil
}

func (r *FeatureRepo) SetAgeGate(ctx context.Context, ageGate json.RawMessage) error {
	if err := r.q.SetAgeGate(ctx, ageGate); err != nil {
		return fmt.Errorf("保存年龄确认门: %w", err)
	}
	return nil
}

// BrandingRepo 实现 platform.BrandingRepo（品牌、配色与布局，docs/06 第 16 节）。
// 与 FeatureRepo 分开：三段配置各自单独保存，调用方也是不同的页面。
type BrandingRepo struct {
	q *sqlcgen.Queries
}

func NewBrandingRepo(pool *pgxpool.Pool) *BrandingRepo {
	return &BrandingRepo{q: sqlcgen.New(pool)}
}

func (r *BrandingRepo) GetBranding(ctx context.Context) (json.RawMessage, json.RawMessage, json.RawMessage, error) {
	row, err := r.q.GetBranding(ctx)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, nil, errs.ErrNotFound
	}
	if err != nil {
		return nil, nil, nil, fmt.Errorf("读取白标配置: %w", err)
	}
	return row.Site, row.Theme, row.Layout, nil
}

func (r *BrandingRepo) SetBrand(ctx context.Context, site json.RawMessage) error {
	if err := r.q.SetBrand(ctx, site); err != nil {
		return fmt.Errorf("保存品牌: %w", err)
	}
	return nil
}

func (r *BrandingRepo) SetTheme(ctx context.Context, theme json.RawMessage) error {
	if err := r.q.SetTheme(ctx, theme); err != nil {
		return fmt.Errorf("保存配色: %w", err)
	}
	return nil
}

func (r *BrandingRepo) SetLayout(ctx context.Context, layout json.RawMessage) error {
	if err := r.q.SetLayout(ctx, layout); err != nil {
		return fmt.Errorf("保存布局: %w", err)
	}
	return nil
}

// SEORepo 实现 platform.SEORepo（SEO 与统计代码，docs/06 第 17 节）。
type SEORepo struct {
	q *sqlcgen.Queries
}

func NewSEORepo(pool *pgxpool.Pool) *SEORepo {
	return &SEORepo{q: sqlcgen.New(pool)}
}

func (r *SEORepo) GetSEOConfig(ctx context.Context) (json.RawMessage, json.RawMessage, error) {
	row, err := r.q.GetSEOConfig(ctx)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil, errs.ErrNotFound
	}
	if err != nil {
		return nil, nil, fmt.Errorf("读取 SEO 配置: %w", err)
	}
	return row.Seo, row.Analytics, nil
}

func (r *SEORepo) SetSEO(ctx context.Context, seo json.RawMessage) error {
	if err := r.q.SetSEO(ctx, seo); err != nil {
		return fmt.Errorf("保存 SEO 配置: %w", err)
	}
	return nil
}

func (r *SEORepo) SetAnalytics(ctx context.Context, analytics json.RawMessage) error {
	if err := r.q.SetAnalytics(ctx, analytics); err != nil {
		return fmt.Errorf("保存统计代码: %w", err)
	}
	return nil
}
