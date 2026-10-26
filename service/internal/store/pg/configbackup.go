package pg

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"xsite/service/internal/domain/platform"
	"xsite/service/internal/errs"
	"xsite/service/internal/store/pg/sqlcgen"
)

// ConfigBackupRepo 实现 platform.ConfigBackupRepo（S4-4 配置导出导入）。
type ConfigBackupRepo struct {
	pool *pgxpool.Pool
	q    *sqlcgen.Queries
}

func NewConfigBackupRepo(pool *pgxpool.Pool) *ConfigBackupRepo {
	return &ConfigBackupRepo{pool: pool, q: sqlcgen.New(pool)}
}

// ExportConfig 读出当前的维度（含取值）与广告。
func (r *ConfigBackupRepo) ExportConfig(ctx context.Context) ([]platform.BackupTaxonomy, []platform.BackupAd, error) {
	taxonomies, err := r.q.ListTaxonomies(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("读取维度: %w", err)
	}
	terms, err := r.q.ListAllTerms(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("读取取值: %w", err)
	}
	outTax := make([]platform.BackupTaxonomy, len(taxonomies))
	index := make(map[int64]int, len(taxonomies))
	for i, t := range taxonomies {
		outTax[i] = platform.BackupTaxonomy{
			Name: t.Name, Slug: t.Slug, MultiSelect: t.MultiSelect, ShowInExplore: t.ShowInExplore,
			Status: t.Status, Sort: t.Sort, Terms: []platform.BackupTerm{},
		}
		index[t.ID] = i
	}
	for _, tm := range terms {
		i, ok := index[tm.TaxonomyID]
		if !ok {
			continue
		}
		outTax[i].Terms = append(outTax[i].Terms, platform.BackupTerm{
			Name: tm.Name, Slug: tm.Slug, Status: tm.Status, IsChannel: tm.IsChannel, Code: tm.Code, Sort: tm.Sort,
		})
	}

	ads, err := r.q.ListAllAdsForExport(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("读取广告: %w", err)
	}
	outAds := make([]platform.BackupAd, len(ads))
	for i, a := range ads {
		locales := a.Locales
		if locales == nil {
			locales = []string{}
		}
		outAds[i] = platform.BackupAd{
			Name: a.Name, Slot: a.Slot, CreativeType: a.CreativeType, Image: a.Image, VideoURL: a.VideoUrl,
			Text: a.Text, HTML: a.Html, Alt: a.Alt, ClickURL: a.ClickUrl, Device: a.Device, Locales: locales,
			Weight: int(a.Weight), EndAt: timePtr(a.EndAt), FreqCap: int(a.FreqCap), Status: a.Status,
		}
	}
	return outTax, outAds, nil
}

// ImportConfig 在一个事务内导入配置。
//
// 两类数据的策略不同，原因在外键上：
//   - 维度与取值按 slug upsert，**不删**备份里没有的。taxonomy_term 被 drama_term 以 ON DELETE CASCADE
//     引用，删一个取值会静默清掉所有剧集对它的归属且不报错；多出来的项只统计并报告，由运维确认后手动删。
//   - 广告整表替换。ad 没有任何外键进出，重建是安全的；代价是曝光与点击计数归零（备份本就不含计数）。
func (r *ConfigBackupRepo) ImportConfig(ctx context.Context, data platform.BackupData) (platform.ImportResult, error) {
	var out platform.ImportResult
	err := InTx(ctx, r.pool, func(tx pgx.Tx) error {
		q := r.q.WithTx(tx)
		out = platform.ImportResult{KeptNames: []string{}}

		slugs := make([]string, 0, len(data.Taxonomies))
		for _, t := range data.Taxonomies {
			row, err := q.UpsertTaxonomyBySlug(ctx, sqlcgen.UpsertTaxonomyBySlugParams{
				Name: t.Name, Slug: t.Slug, MultiSelect: t.MultiSelect, ShowInExplore: t.ShowInExplore,
				Status: t.Status, Sort: t.Sort,
			})
			if err != nil {
				return fmt.Errorf("导入维度 %q: %w", t.Slug, err)
			}
			slugs = append(slugs, t.Slug)
			out.Applied.Taxonomies++

			termSlugs := make([]string, 0, len(t.Terms))
			for _, tm := range t.Terms {
				if _, err := q.UpsertTermBySlug(ctx, sqlcgen.UpsertTermBySlugParams{
					TaxonomyID: row.ID, Name: tm.Name, Slug: tm.Slug, Status: tm.Status,
					IsChannel: tm.IsChannel, Code: tm.Code, Sort: tm.Sort,
				}); err != nil {
					return fmt.Errorf("导入维度 %q 的取值 %q: %w", t.Slug, tm.Slug, err)
				}
				termSlugs = append(termSlugs, tm.Slug)
				out.Applied.Terms++
			}
			// 该维度下库里多出的取值：保留不删，只报告
			extra, err := q.ListTermSlugsNotIn(ctx, sqlcgen.ListTermSlugsNotInParams{TaxonomyID: row.ID, Slugs: termSlugs})
			if err != nil {
				return fmt.Errorf("检查维度 %q 下多出的取值: %w", t.Slug, err)
			}
			out.TermsKept += len(extra)
			for _, e := range extra {
				out.KeptNames = append(out.KeptNames, t.Slug+" / "+e.Name+"（"+e.Slug+"）")
			}
		}

		// 库里多出的整个维度：同样保留不删
		extraTax, err := q.ListTaxonomySlugsNotIn(ctx, slugs)
		if err != nil {
			return fmt.Errorf("检查多出的维度: %w", err)
		}
		out.TaxonomiesKept = len(extraTax)
		for _, e := range extraTax {
			out.KeptNames = append(out.KeptNames, e.Name+"（"+e.Slug+"）")
		}

		// 广告整表替换
		if _, err := q.DeleteAllAds(ctx); err != nil {
			return fmt.Errorf("清除原有广告: %w", err)
		}
		for i, a := range data.Ads {
			locales := a.Locales
			if locales == nil {
				locales = []string{}
			}
			if err := q.InsertAdForImport(ctx, sqlcgen.InsertAdForImportParams{
				Name: a.Name, Slot: a.Slot, CreativeType: a.CreativeType, Image: a.Image, VideoUrl: a.VideoURL,
				Text: a.Text, Html: a.HTML, Alt: a.Alt, ClickUrl: a.ClickURL, Device: a.Device, Locales: locales,
				Weight: int32(a.Weight), EndAt: timestamptz(a.EndAt), FreqCap: int32(a.FreqCap), Status: a.Status,
			}); err != nil {
				return fmt.Errorf("导入第 %d 条广告 %q: %w", i+1, a.Name, err)
			}
			out.Applied.Ads++
		}
		return nil
	})
	if err != nil {
		return platform.ImportResult{}, err
	}
	return out, nil
}

// SaveBackup 存一份备份存档。
func (r *ConfigBackupRepo) SaveBackup(ctx context.Context, b platform.BackupRecord) (int64, error) {
	payload, err := json.Marshal(b.Payload)
	if err != nil {
		return 0, fmt.Errorf("序列化备份正文: %w", err)
	}
	id, err := r.q.InsertConfigBackup(ctx, sqlcgen.InsertConfigBackupParams{
		Format: int32(b.Format), Reason: string(b.Reason), AdminID: b.AdminID,
		Note: b.Note, Payload: payload, Signature: b.Signature,
	})
	if err != nil {
		return 0, fmt.Errorf("保存备份存档: %w", err)
	}
	return id, nil
}

// ListBackups 按时间倒序列出备份存档（不含正文）。
func (r *ConfigBackupRepo) ListBackups(ctx context.Context, limit int) ([]platform.BackupSummary, error) {
	rows, err := r.q.ListConfigBackups(ctx, int32(limit))
	if err != nil {
		return nil, fmt.Errorf("读取备份存档: %w", err)
	}
	out := make([]platform.BackupSummary, len(rows))
	for i, x := range rows {
		out[i] = platform.BackupSummary{
			ID: x.ID, Format: int(x.Format), Reason: platform.BackupReason(x.Reason), AdminID: x.AdminID,
			Note: x.Note, Signed: x.Signature != "", CreatedAt: x.CreatedAt.Time,
			Counts: platform.Counts{
				Menus: int(x.Menus), StaticPages: int(x.StaticPages), Blocks: int(x.Blocks),
				Taxonomies: int(x.Taxonomies), Terms: int(x.Terms), Ads: int(x.Ads),
			},
		}
	}
	return out, nil
}

// GetBackup 读出一份备份存档的正文。
func (r *ConfigBackupRepo) GetBackup(ctx context.Context, id int64) (platform.BackupRecord, error) {
	row, err := r.q.GetConfigBackup(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return platform.BackupRecord{}, errs.ErrNotFound
	}
	if err != nil {
		return platform.BackupRecord{}, fmt.Errorf("读取备份存档: %w", err)
	}
	var payload platform.BackupData
	if err := json.Unmarshal(row.Payload, &payload); err != nil {
		return platform.BackupRecord{}, fmt.Errorf("解析备份正文: %w", err)
	}
	return platform.BackupRecord{
		ID: row.ID, Format: int(row.Format), Reason: platform.BackupReason(row.Reason), AdminID: row.AdminID,
		Note: row.Note, Payload: payload, Signature: row.Signature, CreatedAt: row.CreatedAt.Time,
	}, nil
}

// timePtr 把可空时间转成指针。
func timePtr(t pgtype.Timestamptz) *time.Time {
	if !t.Valid {
		return nil
	}
	v := t.Time
	return &v
}

// timestamptz 把可空时间转成 pgtype。
func timestamptz(t *time.Time) pgtype.Timestamptz {
	if t == nil {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: *t, Valid: true}
}
