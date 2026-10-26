package pg

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"xsite/service/internal/domain/content"
	"xsite/service/internal/domain/ops"
	"xsite/service/internal/errs"
	"xsite/service/internal/store/pg/sqlcgen"
)

// OpsRepo 读写运营域（公告、广告、热搜词、榜单，docs/specs/A3-4.md）。
type OpsRepo struct {
	pool *pgxpool.Pool
	q    *sqlcgen.Queries
}

func NewOpsRepo(pool *pgxpool.Pool) *OpsRepo {
	return &OpsRepo{pool: pool, q: sqlcgen.New(pool)}
}

// opsErr 把查询错误映射为领域错误：查不到为 ErrNotFound；其他错误加上下文。
func opsErr(err error, what string) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return errs.ErrNotFound
	}
	return fmt.Errorf("%s: %w", what, err)
}

// ---------- 公告 ----------

func announcementOf(r sqlcgen.Announcement) ops.Announcement {
	return ops.Announcement{
		ID: r.ID, Kind: ops.AnnouncementKind(r.Kind), Content: r.Content, ClickURL: r.ClickUrl, Frequency: ops.Frequency(r.Frequency),
		Pages: strs(r.Pages), Locales: strs(r.Locales), Image: r.Image, StartAt: utcPtr(r.StartAt), EndAt: utcPtr(r.EndAt),
		Status: ops.Status(r.Status), CreatedAt: r.CreatedAt.Time.UTC(), UpdatedAt: r.UpdatedAt.Time.UTC(),
	}
}

func announcementsOf(rows []sqlcgen.Announcement) []ops.Announcement {
	out := make([]ops.Announcement, len(rows))
	for i, r := range rows {
		out[i] = announcementOf(r)
	}
	return out
}

func (r *OpsRepo) ListAnnouncements(ctx context.Context, q ops.AnnouncementQuery) ([]ops.Announcement, int64, error) {
	pattern, kind, status := nullText(q.Pattern), nullText(string(q.Kind)), nullText(string(q.Status))
	rows, err := r.q.ListAnnouncements(ctx, sqlcgen.ListAnnouncementsParams{
		Pattern: pattern, Kind: kind, Status: status, PageLimit: int32(q.Limit), PageOffset: int32(q.Offset),
	})
	if err != nil {
		return nil, 0, fmt.Errorf("读取公告列表: %w", err)
	}
	total, err := r.q.CountAnnouncements(ctx, sqlcgen.CountAnnouncementsParams{Pattern: pattern, Kind: kind, Status: status})
	if err != nil {
		return nil, 0, fmt.Errorf("统计公告: %w", err)
	}
	return announcementsOf(rows), total, nil
}

func (r *OpsRepo) GetAnnouncement(ctx context.Context, id int64) (ops.Announcement, error) {
	row, err := r.q.GetAnnouncement(ctx, id)
	if err != nil {
		return ops.Announcement{}, opsErr(err, "读取公告")
	}
	return announcementOf(row), nil
}

func (r *OpsRepo) CreateAnnouncement(ctx context.Context, in ops.AnnouncementInput) (ops.Announcement, error) {
	row, err := r.q.CreateAnnouncement(ctx, sqlcgen.CreateAnnouncementParams{
		Kind: string(in.Kind), Content: in.Content, ClickUrl: in.ClickURL, Frequency: string(in.Frequency), Pages: strs(in.Pages),
		Locales: strs(in.Locales), Image: in.Image, StartAt: tsOf(in.StartAt), EndAt: tsOf(in.EndAt), Status: string(in.Status),
	})
	if err != nil {
		return ops.Announcement{}, opsErr(err, "创建公告")
	}
	return announcementOf(row), nil
}

func (r *OpsRepo) UpdateAnnouncement(ctx context.Context, id int64, in ops.AnnouncementInput) (ops.Announcement, error) {
	row, err := r.q.UpdateAnnouncement(ctx, sqlcgen.UpdateAnnouncementParams{
		ID: id, Kind: string(in.Kind), Content: in.Content, ClickUrl: in.ClickURL, Frequency: string(in.Frequency), Pages: strs(in.Pages),
		Locales: strs(in.Locales), Image: in.Image, StartAt: tsOf(in.StartAt), EndAt: tsOf(in.EndAt), Status: string(in.Status),
	})
	if err != nil {
		return ops.Announcement{}, opsErr(err, "保存公告")
	}
	return announcementOf(row), nil
}

func (r *OpsRepo) DeleteAnnouncement(ctx context.Context, id int64) (ops.Announcement, error) {
	row, err := r.q.DeleteAnnouncement(ctx, id)
	if err != nil {
		return ops.Announcement{}, opsErr(err, "删除公告")
	}
	return announcementOf(row), nil
}

func (r *OpsRepo) ListAnnouncementCandidates(ctx context.Context) ([]ops.Announcement, error) {
	rows, err := r.q.ListAnnouncementCandidates(ctx)
	if err != nil {
		return nil, fmt.Errorf("读取前台公告: %w", err)
	}
	return announcementsOf(rows), nil
}

// ---------- 广告 ----------

func adOf(r sqlcgen.Ad) ops.Ad {
	return ops.Ad{
		ID: r.ID, Name: r.Name, Slot: ops.AdSlot(r.Slot), CreativeType: ops.CreativeType(r.CreativeType), Image: r.Image,
		VideoURL: r.VideoUrl, Text: r.Text, HTML: r.Html, Alt: r.Alt, ClickURL: r.ClickUrl, Device: ops.AdDevice(r.Device),
		Locales: strs(r.Locales), Weight: int(r.Weight), EndAt: utcPtr(r.EndAt), FreqCap: int(r.FreqCap), Status: ops.Status(r.Status),
		ImpCount: r.ImpCount, ClickCount: r.ClickCount, CreatedAt: r.CreatedAt.Time.UTC(), UpdatedAt: r.UpdatedAt.Time.UTC(),
	}
}

func adsOf(rows []sqlcgen.Ad) []ops.Ad {
	out := make([]ops.Ad, len(rows))
	for i, r := range rows {
		out[i] = adOf(r)
	}
	return out
}

func (r *OpsRepo) ListAds(ctx context.Context, q ops.AdQuery) ([]ops.Ad, int64, error) {
	pattern, slot, status := nullText(q.Pattern), nullText(string(q.Slot)), nullText(string(q.Status))
	rows, err := r.q.ListAds(ctx, sqlcgen.ListAdsParams{
		Pattern: pattern, Slot: slot, Status: status, PageLimit: int32(q.Limit), PageOffset: int32(q.Offset),
	})
	if err != nil {
		return nil, 0, fmt.Errorf("读取广告列表: %w", err)
	}
	total, err := r.q.CountAds(ctx, sqlcgen.CountAdsParams{Pattern: pattern, Slot: slot, Status: status})
	if err != nil {
		return nil, 0, fmt.Errorf("统计广告: %w", err)
	}
	return adsOf(rows), total, nil
}

func (r *OpsRepo) GetAd(ctx context.Context, id int64) (ops.Ad, error) {
	row, err := r.q.GetAd(ctx, id)
	if err != nil {
		return ops.Ad{}, opsErr(err, "读取广告")
	}
	return adOf(row), nil
}

func (r *OpsRepo) CreateAd(ctx context.Context, in ops.AdInput) (ops.Ad, error) {
	row, err := r.q.CreateAd(ctx, sqlcgen.CreateAdParams{
		Name: in.Name, Slot: string(in.Slot), CreativeType: string(in.CreativeType), Image: in.Image, VideoUrl: in.VideoURL,
		Text: in.Text, Html: in.HTML, Alt: in.Alt, ClickUrl: in.ClickURL, Device: string(in.Device), Locales: strs(in.Locales),
		Weight: int32(in.Weight), EndAt: tsOf(in.EndAt), FreqCap: int32(in.FreqCap), Status: string(in.Status),
	})
	if err != nil {
		return ops.Ad{}, opsErr(err, "创建广告")
	}
	return adOf(row), nil
}

func (r *OpsRepo) UpdateAd(ctx context.Context, id int64, in ops.AdInput) (ops.Ad, error) {
	row, err := r.q.UpdateAd(ctx, sqlcgen.UpdateAdParams{
		ID: id, Name: in.Name, Slot: string(in.Slot), CreativeType: string(in.CreativeType), Image: in.Image, VideoUrl: in.VideoURL,
		Text: in.Text, Html: in.HTML, Alt: in.Alt, ClickUrl: in.ClickURL, Device: string(in.Device), Locales: strs(in.Locales),
		Weight: int32(in.Weight), EndAt: tsOf(in.EndAt), FreqCap: int32(in.FreqCap), Status: string(in.Status),
	})
	if err != nil {
		return ops.Ad{}, opsErr(err, "保存广告")
	}
	return adOf(row), nil
}

func (r *OpsRepo) DeleteAd(ctx context.Context, id int64) (ops.Ad, error) {
	row, err := r.q.DeleteAd(ctx, id)
	if err != nil {
		return ops.Ad{}, opsErr(err, "删除广告")
	}
	return adOf(row), nil
}

func (r *OpsRepo) ListAdCandidates(ctx context.Context) ([]ops.Ad, error) {
	rows, err := r.q.ListAdCandidates(ctx)
	if err != nil {
		return nil, fmt.Errorf("读取前台广告: %w", err)
	}
	return adsOf(rows), nil
}

// AddAdCounts 用一条语句把各广告的增量加到累计上。
func (r *OpsRepo) AddAdCounts(ctx context.Context, counts map[int64]ops.AdCounts) error {
	if len(counts) == 0 {
		return nil
	}
	p := sqlcgen.AddAdCountsParams{
		Ids: make([]int64, 0, len(counts)), Imps: make([]int64, 0, len(counts)), Clicks: make([]int64, 0, len(counts)),
	}
	for id, c := range counts {
		p.Ids, p.Imps, p.Clicks = append(p.Ids, id), append(p.Imps, c.Impressions), append(p.Clicks, c.Clicks)
	}
	if _, err := r.q.AddAdCounts(ctx, p); err != nil {
		return fmt.Errorf("广告计数落库: %w", err)
	}
	return nil
}

// ---------- 热搜词 ----------

func hotWordOf(r sqlcgen.Hotsearch) ops.HotWord {
	return ops.HotWord{
		ID: r.ID, Word: r.Word, SearchCount: r.SearchCount, MissCount: r.MissCount, Pinned: r.Pinned, PinSort: int(r.PinSort),
		Visible: r.Visible, Source: ops.HotSource(r.Source), LastSearchedAt: utcPtr(r.LastSearchedAt),
		CreatedAt: r.CreatedAt.Time.UTC(), UpdatedAt: r.UpdatedAt.Time.UTC(),
	}
}

func hotWordsOf(rows []sqlcgen.Hotsearch) []ops.HotWord {
	out := make([]ops.HotWord, len(rows))
	for i, r := range rows {
		out[i] = hotWordOf(r)
	}
	return out
}

func nullBool(b *bool) pgtype.Bool {
	if b == nil {
		return pgtype.Bool{}
	}
	return pgtype.Bool{Bool: *b, Valid: true}
}

func (r *OpsRepo) ListHotWords(ctx context.Context, q ops.HotQuery) ([]ops.HotWord, int64, error) {
	pattern, visible, pinned := nullText(q.Pattern), nullBool(q.Visible), nullBool(q.Pinned)
	rows, err := r.q.ListHotWords(ctx, sqlcgen.ListHotWordsParams{
		Pattern: pattern, Visible: visible, Pinned: pinned, Missing: q.Missing, PageLimit: int32(q.Limit), PageOffset: int32(q.Offset),
	})
	if err != nil {
		return nil, 0, fmt.Errorf("读取热搜词列表: %w", err)
	}
	total, err := r.q.CountHotWords(ctx, sqlcgen.CountHotWordsParams{Pattern: pattern, Visible: visible, Pinned: pinned, Missing: q.Missing})
	if err != nil {
		return nil, 0, fmt.Errorf("统计热搜词: %w", err)
	}
	return hotWordsOf(rows), total, nil
}

func (r *OpsRepo) GetHotWord(ctx context.Context, id int64) (ops.HotWord, error) {
	row, err := r.q.GetHotWord(ctx, id)
	if err != nil {
		return ops.HotWord{}, opsErr(err, "读取热搜词")
	}
	return hotWordOf(row), nil
}

func (r *OpsRepo) CreateHotWord(ctx context.Context, word string) (ops.HotWord, error) {
	row, err := r.q.CreateHotWord(ctx, word)
	if isUniqueViolation(err) {
		return ops.HotWord{}, errs.ErrConflict
	}
	if err != nil {
		return ops.HotWord{}, opsErr(err, "添加热搜词")
	}
	return hotWordOf(row), nil
}

// SetHotWordFlags 在事务里加咨询锁后检查置顶上限再保存，两人同时置顶也不会超过上限。
func (r *OpsRepo) SetHotWordFlags(ctx context.Context, id int64, visible, pinned bool, maxPinned int) (ops.HotWord, error) {
	var out ops.HotWord
	err := InTx(ctx, r.pool, func(tx pgx.Tx) error {
		q := r.q.WithTx(tx)
		if err := q.LockHotPins(ctx); err != nil {
			return fmt.Errorf("加锁: %w", err)
		}
		if pinned {
			cur, err := q.GetHotWord(ctx, id)
			if err != nil {
				return err
			}
			if !cur.Pinned {
				n, err := q.CountOtherPinnedHotWords(ctx, id)
				if err != nil {
					return err
				}
				if n >= int64(maxPinned) {
					return ops.ErrTooManyPinned
				}
			}
		}
		row, err := q.SetHotWordFlags(ctx, sqlcgen.SetHotWordFlagsParams{ID: id, Visible: visible, Pinned: pinned})
		if err != nil {
			return err
		}
		out = hotWordOf(row)
		return nil
	})
	if err != nil {
		return ops.HotWord{}, opsErr(err, "保存热搜词")
	}
	return out, nil
}

// ReorderPinned 在同一把锁下比对当前置顶词再重排。
func (r *OpsRepo) ReorderPinned(ctx context.Context, ids []int64) error {
	err := InTx(ctx, r.pool, func(tx pgx.Tx) error {
		q := r.q.WithTx(tx)
		if err := q.LockHotPins(ctx); err != nil {
			return fmt.Errorf("加锁: %w", err)
		}
		cur, err := q.ListPinnedHotWordIDs(ctx)
		if err != nil {
			return err
		}
		if !slices.Equal(slices.Sorted(slices.Values(cur)), slices.Sorted(slices.Values(ids))) {
			return ops.ErrPinsChanged
		}
		sorts := make([]int32, len(ids))
		for i := range ids {
			sorts[i] = int32(i + 1)
		}
		return q.SetHotWordPinSort(ctx, sqlcgen.SetHotWordPinSortParams{Ids: ids, Sorts: sorts})
	})
	return opsErr(err, "调整置顶顺序")
}

func (r *OpsRepo) DeleteHotWord(ctx context.Context, id int64) (ops.HotWord, error) {
	row, err := r.q.DeleteHotWord(ctx, id)
	if err != nil {
		return ops.HotWord{}, opsErr(err, "删除热搜词")
	}
	return hotWordOf(row), nil
}

func (r *OpsRepo) ListHotBoard(ctx context.Context, n int) ([]ops.HotWord, error) {
	rows, err := r.q.ListHotBoard(ctx, int32(n))
	if err != nil {
		return nil, fmt.Errorf("读取热搜榜: %w", err)
	}
	return hotWordsOf(rows), nil
}

// AddHotCounts 用一条语句累加搜索次数并收录新词。
func (r *OpsRepo) AddHotCounts(ctx context.Context, counts map[string]ops.HotCounts) error {
	if len(counts) == 0 {
		return nil
	}
	p := sqlcgen.AddHotCountsParams{
		Words: make([]string, 0, len(counts)), Searches: make([]int64, 0, len(counts)), Misses: make([]int64, 0, len(counts)),
	}
	for w, c := range counts {
		p.Words, p.Searches, p.Misses = append(p.Words, w), append(p.Searches, c.Searches), append(p.Misses, c.Misses)
	}
	if err := r.q.AddHotCounts(ctx, p); err != nil {
		return fmt.Errorf("热搜计数落库: %w", err)
	}
	return nil
}

// ---------- 榜单 ----------

// RankSignals 实现 ops.RankSource：埋点就绪前两张榜用同一份数据（播放 = 初始播放量 + 真实播放量），board 暂不区分。
func (r *OpsRepo) RankSignals(ctx context.Context, _ ops.RankBoard) ([]ops.RankSignals, error) {
	rows, err := r.q.ListRankSignals(ctx)
	if err != nil {
		return nil, fmt.Errorf("读取榜单计分数据: %w", err)
	}
	out := make([]ops.RankSignals, len(rows))
	for i, row := range rows {
		out[i] = ops.RankSignals{DramaID: row.ID, Plays: row.Plays}
	}
	return out, nil
}

func (r *OpsRepo) RankOverrideRules(ctx context.Context, b ops.RankBoard) ([]ops.RankOverride, error) {
	rows, err := r.q.ListRankOverrideRules(ctx, string(b))
	if err != nil {
		return nil, fmt.Errorf("读取人工调整: %w", err)
	}
	out := make([]ops.RankOverride, len(rows))
	for i, row := range rows {
		out[i] = ops.RankOverride{DramaID: row.DramaID, Pinned: row.Pinned, Blocked: row.Blocked, Boost: row.Boost}
	}
	return out, nil
}

func (r *OpsRepo) ListRankOverrides(ctx context.Context, b ops.RankBoard) ([]ops.RankOverrideView, error) {
	rows, err := r.q.ListRankOverrides(ctx, string(b))
	if err != nil {
		return nil, fmt.Errorf("读取人工调整: %w", err)
	}
	out := make([]ops.RankOverrideView, len(rows))
	for i, row := range rows {
		out[i] = ops.RankOverrideView{
			RankOverride: ops.RankOverride{DramaID: row.DramaID, Pinned: row.Pinned, Blocked: row.Blocked, Boost: row.Boost},
			Title:        row.Title, DramaStatus: row.Status, InTrash: row.InTrash, UpdatedAt: row.UpdatedAt.Time.UTC(),
		}
	}
	return out, nil
}

// ReplaceRankOverrides 在同一事务里删掉旧的调整、写入新的（同一张榜的保存用咨询锁串行）。
func (r *OpsRepo) ReplaceRankOverrides(ctx context.Context, b ops.RankBoard, list []ops.RankOverride) error {
	p := sqlcgen.InsertRankOverridesParams{
		Board: string(b), DramaIds: make([]int64, len(list)), Pinned: make([]bool, len(list)),
		Blocked: make([]bool, len(list)), Boosts: make([]int64, len(list)),
	}
	for i, o := range list {
		p.DramaIds[i], p.Pinned[i], p.Blocked[i], p.Boosts[i] = o.DramaID, o.Pinned, o.Blocked, o.Boost
	}
	err := InTx(ctx, r.pool, func(tx pgx.Tx) error {
		q := r.q.WithTx(tx)
		if err := q.LockRankOverrides(ctx, string(b)); err != nil {
			return fmt.Errorf("加锁: %w", err)
		}
		if err := q.DeleteRankOverrides(ctx, string(b)); err != nil {
			return err
		}
		if len(list) == 0 {
			return nil
		}
		return q.InsertRankOverrides(ctx, p)
	})
	return opsErr(err, "保存人工调整")
}

// MissingDramas 按输入顺序返回不存在或在回收站的剧。
func (r *OpsRepo) MissingDramas(ctx context.Context, ids []int64) ([]int64, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	live, err := r.q.ListLiveDramaIDsAmong(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("校验剧集: %w", err)
	}
	var out []int64
	for _, id := range ids {
		if !slices.Contains(live, id) {
			out = append(out, id)
		}
	}
	return out, nil
}

func (r *OpsRepo) DramaTitles(ctx context.Context, ids []int64) (map[int64]string, error) {
	out := make(map[int64]string, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := r.q.ListDramaTitles(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("读取剧名: %w", err)
	}
	for _, row := range rows {
		out[row.ID] = row.Title
	}
	return out, nil
}

// RankCards 按 ids 的顺序返回前台可见的剧卡片（与前台列表同一套卡片字段）。
func (r *OpsRepo) RankCards(ctx context.Context, ids []int64) ([]content.DramaCard, error) {
	out := []content.DramaCard{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := r.q.ListRankCards(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("读取榜单剧集: %w", err)
	}
	for _, d := range rows {
		out = append(out, cardOf(d.ID, d.Title, d.Slug, d.CoverPortrait, d.CoverLandscape, d.Badge, d.Score, d.Year, d.IsFinished,
			d.LatestEpisode, d.PlayCount, d.PublishAt, d.CreatedAt, d.HotScore, d.SortWeight))
	}
	return out, nil
}

func (r *OpsRepo) RankingWeights(ctx context.Context) (ops.RankWeights, error) {
	raw, err := r.q.GetRankingWeights(ctx)
	if err != nil {
		return ops.RankWeights{}, opsErr(err, "读取计分系数")
	}
	return weightsOf(raw)
}

func (r *OpsRepo) SetRankingWeights(ctx context.Context, w ops.RankWeights) (ops.RankWeights, error) {
	b, err := json.Marshal(w)
	if err != nil {
		return ops.RankWeights{}, fmt.Errorf("编码计分系数: %w", err)
	}
	raw, err := r.q.SetRankingWeights(ctx, b)
	if err != nil {
		return ops.RankWeights{}, opsErr(err, "保存计分系数")
	}
	return weightsOf(raw)
}

func weightsOf(raw []byte) (ops.RankWeights, error) {
	var w ops.RankWeights
	if err := json.Unmarshal(raw, &w); err != nil {
		return ops.RankWeights{}, fmt.Errorf("解析计分系数: %w", err)
	}
	return w, nil
}
