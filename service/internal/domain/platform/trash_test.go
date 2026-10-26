package platform

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type fakeTrashRepo struct {
	items     []TrashItem
	counts    int
	oldest    *time.Time
	restored  []int64
	purged    []int64
	expired   []TrashItem
	purgedBef time.Time
	err       error
	notFound  bool
}

func (r *fakeTrashRepo) ListTrashedDramas(_ context.Context, q TrashQuery) ([]TrashItem, int64, error) {
	if r.err != nil {
		return nil, 0, r.err
	}
	// 简单模拟分页
	start := min(q.Offset, len(r.items))
	end := min(start+q.Limit, len(r.items))
	return r.items[start:end], int64(len(r.items)), nil
}

func (r *fakeTrashRepo) RestoreDrama(_ context.Context, id int64) (TrashItem, error) {
	if r.notFound {
		return TrashItem{}, errors.New("内容不存在或已下架")
	}
	r.restored = append(r.restored, id)
	return TrashItem{ID: id, Title: "还原的剧", Status: "draft"}, nil
}

func (r *fakeTrashRepo) PurgeDrama(_ context.Context, id int64) (TrashItem, error) {
	if r.notFound {
		return TrashItem{}, errors.New("内容不存在或已下架")
	}
	r.purged = append(r.purged, id)
	return TrashItem{ID: id, Title: "删掉的剧"}, nil
}

func (r *fakeTrashRepo) TrashedDrama(_ context.Context, id int64) (TrashItem, error) {
	if r.notFound {
		return TrashItem{}, errors.New("内容不存在或已下架")
	}
	return TrashItem{ID: id, Title: "待删的剧", Slug: "to-purge", Extra: 12, DeletedAt: time.Now().Add(-48 * time.Hour)}, nil
}

func (r *fakeTrashRepo) TrashCounts(context.Context) (int, *time.Time, error) {
	if r.err != nil {
		return 0, nil, r.err
	}
	return r.counts, r.oldest, nil
}

func (r *fakeTrashRepo) PurgeExpiredDramas(_ context.Context, before time.Time) ([]TrashItem, error) {
	if r.err != nil {
		return nil, r.err
	}
	r.purgedBef = before
	return r.expired, nil
}

var trashNow = time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)

func newTrashSvc(repo *fakeTrashRepo, retention time.Duration) *TrashService {
	s := NewTrashService(repo, retention)
	s.now = func() time.Time { return trashNow }
	return s
}

func TestTrash_DefaultRetention(t *testing.T) {
	// 传 0 用默认的 30 天（docs/06 第 35 节）
	if got := newTrashSvc(&fakeTrashRepo{}, 0).Retention(); got != DefaultTrashRetention {
		t.Errorf("默认保留期 = %v，期望 %v", got, DefaultTrashRetention)
	}
	if got := newTrashSvc(&fakeTrashRepo{}, 48*time.Hour).Retention(); got != 48*time.Hour {
		t.Errorf("保留期没有按配置生效：%v", got)
	}
}

// 到期时间与剩余天数：页面要显示「今天 / N 天」。
func TestTrash_ExpiryCalculation(t *testing.T) {
	cases := []struct {
		name     string
		deleted  time.Time
		wantDays int
	}{
		{"刚删的还有 30 天", trashNow, 30},
		{"删了 1 天还有 29 天", trashNow.Add(-24 * time.Hour), 29},
		{"删了 29 天还有 1 天", trashNow.Add(-29 * 24 * time.Hour), 1},
		{"还剩半天也算 1 天", trashNow.Add(-29*24*time.Hour - 12*time.Hour), 1},
		{"正好到期算今天", trashNow.Add(-30 * 24 * time.Hour), 0},
		{"已经过期算今天", trashNow.Add(-40 * 24 * time.Hour), 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			repo := &fakeTrashRepo{items: []TrashItem{{ID: 1, DeletedAt: c.deleted}}}
			list, _, _, _, err := newTrashSvc(repo, DefaultTrashRetention).List(context.Background(), TrashFilter{Category: TrashDramas})
			if err != nil {
				t.Fatal(err)
			}
			if list[0].DaysLeft != c.wantDays {
				t.Errorf("剩余天数 = %d，期望 %d", list[0].DaysLeft, c.wantDays)
			}
			want := c.deleted.Add(DefaultTrashRetention)
			if !list[0].ExpiresAt.Equal(want) {
				t.Errorf("到期时间 = %v，期望 %v", list[0].ExpiresAt, want)
			}
		})
	}
}

func TestTrash_Overview(t *testing.T) {
	oldest := trashNow.Add(-10 * 24 * time.Hour)
	repo := &fakeTrashRepo{counts: 7, oldest: &oldest}
	out, err := newTrashSvc(repo, DefaultTrashRetention).Overview(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if out.RetentionDays != 30 {
		t.Errorf("保留期 = %d 天，期望 30", out.RetentionDays)
	}
	if len(out.Categories) != len(trashCategories) {
		t.Fatalf("分类数 = %d，期望 %d", len(out.Categories), len(trashCategories))
	}
	if out.Categories[0].Category != TrashDramas || out.Categories[0].Count != 7 {
		t.Errorf("剧集分类的条数不对: %+v", out.Categories[0])
	}
	// 每个分类都要有标题与说明：页面直接展示
	for _, c := range out.Categories {
		if c.Title == "" || c.Desc == "" {
			t.Errorf("分类 %s 缺标题或说明", c.Category)
		}
	}
	if out.OldestExpiresAt == nil || !out.OldestExpiresAt.Equal(oldest.Add(DefaultTrashRetention)) {
		t.Errorf("最近到期时间不对: %v", out.OldestExpiresAt)
	}
}

// 回收站为空时不报「最近到期」。
func TestTrash_OverviewEmpty(t *testing.T) {
	out, err := newTrashSvc(&fakeTrashRepo{}, DefaultTrashRetention).Overview(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if out.OldestExpiresAt != nil {
		t.Errorf("空回收站不该有到期时间：%v", out.OldestExpiresAt)
	}
}

// 概览会修改分类的 Count，不能污染全局的 trashCategories。
func TestTrash_OverviewDoesNotMutateGlobal(t *testing.T) {
	svc := newTrashSvc(&fakeTrashRepo{counts: 99}, DefaultTrashRetention)
	if _, err := svc.Overview(context.Background()); err != nil {
		t.Fatal(err)
	}
	for _, c := range trashCategories {
		if c.Count != 0 {
			t.Errorf("全局分类表被改了：%s 的 Count = %d", c.Category, c.Count)
		}
	}
}

func TestTrash_UnknownCategory(t *testing.T) {
	svc := newTrashSvc(&fakeTrashRepo{}, DefaultTrashRetention)
	ctx := context.Background()
	for _, call := range []struct {
		name string
		run  func() error
	}{
		{"列表", func() error { _, _, _, _, err := svc.List(ctx, TrashFilter{Category: "users"}); return err }},
		{"还原", func() error { _, err := svc.Restore(ctx, "users", 1); return err }},
		{"彻底删除", func() error { _, err := svc.Purge(ctx, "users", 1); return err }},
	} {
		err := call.run()
		if err == nil {
			t.Errorf("%s：未知分类应当报错", call.name)
			continue
		}
		// 报错要列出可选项
		if !strings.Contains(err.Error(), "dramas") {
			t.Errorf("%s：报错没列出可选分类：%v", call.name, err)
		}
	}
}

func TestTrash_Restore(t *testing.T) {
	repo := &fakeTrashRepo{}
	item, err := newTrashSvc(repo, DefaultTrashRetention).Restore(context.Background(), TrashDramas, 42)
	if err != nil {
		t.Fatal(err)
	}
	if len(repo.restored) != 1 || repo.restored[0] != 42 {
		t.Errorf("还原的编号不对: %v", repo.restored)
	}
	// 还原后要把删除前的状态带回来，页面据此提示「还原为草稿」
	if item.Status != "draft" {
		t.Errorf("还原结果没有带回状态: %+v", item)
	}
}

// 已经不在回收站里（被还原过或已彻底删除）要报错，不能假装成功。
func TestTrash_RestoreMissing(t *testing.T) {
	svc := newTrashSvc(&fakeTrashRepo{notFound: true}, DefaultTrashRetention)
	if _, err := svc.Restore(context.Background(), TrashDramas, 1); err == nil {
		t.Error("不在回收站里的内容还原应当报错")
	}
}

// 超过保留期但清理任务还没跑到时，仍然允许还原：东西还在，能救就救。
func TestTrash_RestoreAfterExpiry(t *testing.T) {
	repo := &fakeTrashRepo{}
	svc := newTrashSvc(repo, DefaultTrashRetention)
	if _, err := svc.Restore(context.Background(), TrashDramas, 7); err != nil {
		t.Errorf("过期但还没被清理的内容应当可以还原: %v", err)
	}
	if len(repo.restored) != 1 {
		t.Error("没有执行还原")
	}
}

// 彻底删除前要先把内容读出来：删完库里就查不到了，审计与提示都要用。
func TestTrash_PurgeReadsBeforeDeleting(t *testing.T) {
	repo := &fakeTrashRepo{}
	item, err := newTrashSvc(repo, DefaultTrashRetention).Purge(context.Background(), TrashDramas, 9)
	if err != nil {
		t.Fatal(err)
	}
	if len(repo.purged) != 1 || repo.purged[0] != 9 {
		t.Errorf("删除的编号不对: %v", repo.purged)
	}
	if item.Title != "待删的剧" || item.Extra != 12 {
		t.Errorf("返回的应当是删除前读到的内容（含附带条数）: %+v", item)
	}
	if item.ExpiresAt.IsZero() {
		t.Error("返回的内容应当带上到期时间")
	}
}

func TestTrash_PurgeMissing(t *testing.T) {
	svc := newTrashSvc(&fakeTrashRepo{notFound: true}, DefaultTrashRetention)
	if _, err := svc.Purge(context.Background(), TrashDramas, 1); err == nil {
		t.Error("不在回收站里的内容彻底删除应当报错")
	}
}

// 清理任务按「当前时间 - 保留期」算截止点。
func TestTrash_PurgeExpiredCutoff(t *testing.T) {
	repo := &fakeTrashRepo{expired: []TrashItem{{ID: 1, Title: "过期剧 A"}, {ID: 2, Title: "过期剧 B"}}}
	svc := newTrashSvc(repo, 48*time.Hour)
	purged, err := svc.PurgeExpired(context.Background(), trashNow)
	if err != nil {
		t.Fatal(err)
	}
	if len(purged) != 2 {
		t.Errorf("清理了 %d 条，期望 2", len(purged))
	}
	want := trashNow.Add(-48 * time.Hour)
	if !repo.purgedBef.Equal(want) {
		t.Errorf("截止点 = %v，期望 %v", repo.purgedBef, want)
	}
}

func TestTrash_ListFilter(t *testing.T) {
	items := make([]TrashItem, 50)
	for i := range items {
		items[i] = TrashItem{ID: int64(i + 1), DeletedAt: trashNow}
	}
	repo := &fakeTrashRepo{items: items}
	svc := newTrashSvc(repo, DefaultTrashRetention)

	list, total, page, size, err := svc.List(context.Background(), TrashFilter{Category: TrashDramas, Page: 2, PageSize: 10})
	if err != nil {
		t.Fatal(err)
	}
	if total != 50 || page != 2 || size != 10 || len(list) != 10 || list[0].ID != 11 {
		t.Errorf("分页不对: total=%d page=%d size=%d first=%d", total, page, size, list[0].ID)
	}

	if _, _, _, _, err := svc.List(context.Background(), TrashFilter{Category: TrashDramas, PageSize: 101}); err == nil {
		t.Error("超过上限的每页条数应当报错")
	}
	if _, _, _, _, err := svc.List(context.Background(), TrashFilter{Category: TrashDramas, Q: strings.Repeat("字", 101)}); err == nil {
		t.Error("超长搜索词应当报错")
	}
}

func TestTrash_SearchEscaping(t *testing.T) {
	q, _, _, err := cleanTrashFilter(TrashFilter{Q: "100%_a"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(q.Pattern, `\%`) || !strings.Contains(q.Pattern, `\_`) {
		t.Errorf("LIKE 通配符没有转义: %q", q.Pattern)
	}
}

func TestTrash_RepoError(t *testing.T) {
	svc := newTrashSvc(&fakeTrashRepo{err: errors.New("库挂了")}, DefaultTrashRetention)
	ctx := context.Background()
	if _, err := svc.Overview(ctx); err == nil {
		t.Error("概览：底层出错应当报错")
	}
	if _, _, _, _, err := svc.List(ctx, TrashFilter{Category: TrashDramas}); err == nil {
		t.Error("列表：底层出错应当报错")
	}
	if _, err := svc.PurgeExpired(ctx, trashNow); err == nil {
		t.Error("清理：底层出错应当报错")
	}
}
