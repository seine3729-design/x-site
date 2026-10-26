package pg

import (
	"context"
	"crypto/rand"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"xsite/service/internal/domain/contentimport"
	"xsite/service/internal/store/pg/sqlcgen"
)

// ContentImportRepo 是 CSV 批量导入的仓储（contentimport.Repo）。
type ContentImportRepo struct {
	q    *sqlcgen.Queries
	pool *pgxpool.Pool
}

var _ contentimport.Repo = (*ContentImportRepo)(nil)

func NewContentImportRepo(pool *pgxpool.Pool) *ContentImportRepo {
	return &ContentImportRepo{q: sqlcgen.New(pool), pool: pool}
}

func (r *ContentImportRepo) FindDramas(ctx context.Context, titles []string) ([]contentimport.ExistingDrama, error) {
	rows, err := r.q.FindDramasForImport(ctx, titles)
	if err != nil {
		return nil, fmt.Errorf("查重剧集: %w", err)
	}
	out := make([]contentimport.ExistingDrama, 0, len(rows))
	for _, row := range rows {
		out = append(out, contentimport.ExistingDrama{
			ID: row.ID, Title: row.Title, Aliases: row.Aliases, InTrash: row.Deleted,
		})
	}
	return out, nil
}

func (r *ContentImportRepo) FindTags(ctx context.Context, names []string) ([]contentimport.ExistingTag, error) {
	rows, err := r.q.FindTagsByNames(ctx, names)
	if err != nil {
		return nil, fmt.Errorf("查标签: %w", err)
	}
	out := make([]contentimport.ExistingTag, 0, len(rows))
	for _, row := range rows {
		out = append(out, contentimport.ExistingTag{
			ID: row.ID, Name: row.Name, MergedInto: row.MergedIntoID.Int64,
		})
	}
	return out, nil
}

// Apply 在单事务内落地一批导入：建标签 → 建剧 → 更新剧 → 挂标签 → 记录批次。
//
// 整批一个事务：导入对运营是一个动作，出错就该什么都没发生 ——
// 一半剧进去了、另一半没进，他既不知道该从哪一行重来，也没法干净地重试。
// 行数上限（contentimport.MaxRows）保证这个事务不会长时间持锁。
func (r *ContentImportRepo) Apply(ctx context.Context, plan contentimport.ApplyPlan) (contentimport.ApplyOutcome, error) {
	var out contentimport.ApplyOutcome
	err := InTx(ctx, r.pool, func(tx pgx.Tx) error {
		q := r.q.WithTx(tx)

		// 1) 本批要新建的标签。name 没有唯一约束，所以先在事务里再查一遍：
		// 编排阶段那次查询到现在，别人可能已经建了同名标签
		tagIDs, created, err := r.resolveTags(ctx, q, plan)
		if err != nil {
			return err
		}
		out.TagsCreated = created

		// 2) 建剧
		out.CreatedIDs = make([]int64, 0, len(plan.Creates))
		for _, c := range plan.Creates {
			id, err := q.ImportCreateDrama(ctx, sqlcgen.ImportCreateDramaParams{
				Title: c.Title, Aliases: emptySlice(c.Aliases), Summary: c.Summary, Source: c.Source,
			})
			if err != nil {
				return fmt.Errorf("新建剧集「%s」: %w", c.Title, err)
			}
			out.CreatedIDs = append(out.CreatedIDs, id)
			if err := addTags(ctx, q, id, c.TagNames, tagIDs); err != nil {
				return err
			}
		}

		// 3) 更新已有的剧
		for _, u := range plan.Updates {
			if err := q.ImportUpdateDrama(ctx, sqlcgen.ImportUpdateDramaParams{
				ID: u.ID, Aliases: emptySlice(u.Aliases), Summary: u.Summary, Source: u.Source,
			}); err != nil {
				return fmt.Errorf("更新剧集 #%d: %w", u.ID, err)
			}
			if err := addTags(ctx, q, u.ID, u.TagNames, tagIDs); err != nil {
				return err
			}
		}

		// 4) 记这一批（预览不会走到这里）
		st := plan.Log.Stats
		log, err := q.CreateImportLog(ctx, sqlcgen.CreateImportLogParams{
			Source: plan.Log.Source, FileName: plan.Log.FileName, RowsTotal: int32(st.RowsTotal),
			DramasCreated: int32(st.DramasCreated), DramasUpdated: int32(st.DramasUpdated),
			EpisodesCreated: int32(st.EpisodesCreated), EpisodesUpdated: int32(st.EpisodesUpdated),
			Skipped: int32(st.Skipped), Rejected: int32(st.Rejected), TagsCreated: int32(created),
			AdminID: plan.Log.AdminID,
		})
		if err != nil {
			return fmt.Errorf("记录导入批次: %w", err)
		}
		out.LogID = log.ID
		return nil
	})
	if err != nil {
		return contentimport.ApplyOutcome{}, err
	}
	return out, nil
}

// resolveTags 把标签名解析成编号：已有的直接用（已合并的改指目标），缺的新建。
// 返回「小写名 → 编号」与实际新建的个数。
func (r *ContentImportRepo) resolveTags(
	ctx context.Context, q *sqlcgen.Queries, plan contentimport.ApplyPlan,
) (map[string]int64, int, error) {
	// 把本批用到的全部标签名收齐
	wanted := map[string]string{} // 小写 → 原文
	for _, c := range plan.Creates {
		for _, t := range c.TagNames {
			wanted[strings.ToLower(t)] = t
		}
	}
	for _, u := range plan.Updates {
		for _, t := range u.TagNames {
			wanted[strings.ToLower(t)] = t
		}
	}
	if len(wanted) == 0 {
		return map[string]int64{}, 0, nil
	}
	keys := make([]string, 0, len(wanted))
	for k := range wanted {
		keys = append(keys, k)
	}
	rows, err := q.FindTagsByNames(ctx, keys)
	if err != nil {
		return nil, 0, fmt.Errorf("查标签: %w", err)
	}
	ids := make(map[string]int64, len(wanted))
	for _, row := range rows {
		k := strings.ToLower(row.Name)
		if _, dup := ids[k]; dup {
			continue // 同名标签有多条时取第一条
		}
		// 已并入别的标签：挂到目标标签上。否则 content 域的校验会在别处拒绝这个标签
		if row.MergedIntoID.Valid {
			ids[k] = row.MergedIntoID.Int64
			continue
		}
		ids[k] = row.ID
	}

	created := 0
	for k, name := range wanted {
		if _, ok := ids[k]; ok {
			continue
		}
		id, err := createTagWithSlug(ctx, q, name)
		if err != nil {
			return nil, 0, err
		}
		ids[k] = id
		created++
	}
	return ids, created, nil
}

// createTagWithSlug 建一个标签，slug 自动生成；碰上冲突换一个再试（与 content 域一致）。
func createTagWithSlug(ctx context.Context, q *sqlcgen.Queries, name string) (int64, error) {
	var lastErr error
	for i := 0; i < 5; i++ {
		id, err := q.ImportCreateTag(ctx, sqlcgen.ImportCreateTagParams{
			Name: name, Slug: contentimport.TagSlugPrefix + randSlug(6),
		})
		if err == nil {
			return id, nil
		}
		if !isUniqueViolation(err) {
			return 0, fmt.Errorf("新建标签「%s」: %w", name, err)
		}
		lastErr = err
	}
	return 0, fmt.Errorf("新建标签「%s」：slug 连续冲突: %w", name, lastErr)
}

// addTags 给一部剧追加标签（不是整体替换：导入不该抹掉剧上 CSV 里没写的标签）。
func addTags(ctx context.Context, q *sqlcgen.Queries, dramaID int64, names []string, ids map[string]int64) error {
	if len(names) == 0 {
		return nil
	}
	tagIDs := make([]int64, 0, len(names))
	seen := map[int64]bool{}
	for _, n := range names {
		id, ok := ids[strings.ToLower(n)]
		if !ok || seen[id] {
			continue
		}
		seen[id] = true
		tagIDs = append(tagIDs, id)
	}
	if len(tagIDs) == 0 {
		return nil
	}
	if err := q.ImportAddDramaTags(ctx, sqlcgen.ImportAddDramaTagsParams{DramaID: dramaID, TagIds: tagIDs}); err != nil {
		return fmt.Errorf("给剧集 #%d 挂标签: %w", dramaID, err)
	}
	return nil
}

func (r *ContentImportRepo) ListLogs(ctx context.Context, f contentimport.LogFilter) ([]contentimport.Log, int64, error) {
	from, to := tsOrNull(f.From), tsOrNull(f.To)
	rows, err := r.q.ListImportLogs(ctx, sqlcgen.ListImportLogsParams{
		FromTs: from, ToTs: to,
		Lim: int32(f.PageSize), Off: int32((f.Page - 1) * f.PageSize),
	})
	if err != nil {
		return nil, 0, fmt.Errorf("读导入记录: %w", err)
	}
	total, err := r.q.CountImportLogs(ctx, sqlcgen.CountImportLogsParams{FromTs: from, ToTs: to})
	if err != nil {
		return nil, 0, fmt.Errorf("统计导入记录: %w", err)
	}
	out := make([]contentimport.Log, 0, len(rows))
	for _, row := range rows {
		out = append(out, contentimport.Log{
			ID: row.ID, Source: row.Source, FileName: row.FileName, RowsTotal: int(row.RowsTotal),
			DramasCreated: int(row.DramasCreated), DramasUpdated: int(row.DramasUpdated),
			EpisodesCreated: int(row.EpisodesCreated), EpisodesUpdated: int(row.EpisodesUpdated),
			Skipped: int(row.Skipped), Rejected: int(row.Rejected), TagsCreated: int(row.TagsCreated),
			AdminID: row.AdminID, AdminName: row.AdminName, CreatedAt: row.CreatedAt.Time,
		})
	}
	return out, total, nil
}

// emptySlice 把 nil 变成空切片：aliases 列是 NOT NULL，传 nil 会写进 NULL。
func emptySlice(v []string) []string {
	if v == nil {
		return []string{}
	}
	return v
}

func tsOrNull(t *time.Time) pgtype.Timestamptz {
	if t == nil {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: *t, Valid: true}
}

// randSlug 生成 n 位随机小写串（与 content 域的 newSlug 同一种形式）。
func randSlug(n int) string {
	return strings.ToLower(rand.Text()[:n])
}
