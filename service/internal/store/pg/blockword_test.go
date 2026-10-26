package pg

import (
	"context"
	"errors"
	"testing"

	"xsite/service/internal/domain/moderation"
	"xsite/service/internal/errs"
)

// 屏蔽词仓储（S2-4）集成测试：新增、唯一冲突、按分组 / 办法 / 词筛选、ListAll、分组去重、删除。
func TestBlockwordRepo(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewBlockwordRepo(pool)

	// 用独特分组隔离本测试的数据，避免与并发测试互相干扰
	grp := "t_blockword"
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM blockword WHERE "group" = $1`, grp)
	})

	mk := func(word, action string) moderation.Blockword {
		bw, err := repo.Create(ctx, word, grp, action)
		if err != nil {
			t.Fatalf("新增 %q：%v", word, err)
		}
		return bw
	}
	a := mk("vpn", "block")
	mk("敏感", "mask")
	mk("翻墙", "review")

	// 唯一冲突 → CONFLICT
	if _, err := repo.Create(ctx, "vpn", grp, "mask"); !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("重复词应 CONFLICT，得到 %v", err)
	}

	// 按分组筛选：3 条
	list, total, err := repo.List(ctx, moderation.BlockwordQuery{Group: grp, Limit: 10, Offset: 0})
	if err != nil || total != 3 || len(list) != 3 {
		t.Fatalf("按分组应 3 条：total=%d len=%d err=%v", total, len(list), err)
	}
	// 按处理办法筛选：block 1 条
	if _, total, _ := repo.List(ctx, moderation.BlockwordQuery{Group: grp, Action: "block", Limit: 10}); total != 1 {
		t.Fatalf("block 应 1 条，得到 %d", total)
	}
	// 按词模糊搜索（LIKE 模式由领域层加 %，这里直接给）
	if _, total, _ := repo.List(ctx, moderation.BlockwordQuery{Pattern: "%vp%", Group: grp, Limit: 10}); total != 1 {
		t.Fatalf("搜 vp 应 1 条，得到 %d", total)
	}

	// ListAll 含本测试的词（可能还有别的分组的词，至少包含我们这 3 个）
	all, err := repo.ListAll(ctx)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]moderation.Action{}
	for _, w := range all {
		seen[w.Word] = w.Action
	}
	if seen["vpn"] != moderation.ActionBlock || seen["敏感"] != moderation.ActionMask || seen["翻墙"] != moderation.ActionReview {
		t.Fatalf("ListAll 应含本测试 3 个词及其办法：%v", seen)
	}

	// 分组去重包含本测试分组
	groups, _ := repo.Groups(ctx)
	found := false
	for _, g := range groups {
		if g == grp {
			found = true
		}
	}
	if !found {
		t.Fatalf("Groups 应包含 %q：%v", grp, groups)
	}

	// 删除返回被删行；再删 NotFound
	del, err := repo.Delete(ctx, a.ID)
	if err != nil || del.Word != "vpn" {
		t.Fatalf("删除应返回被删行：%+v %v", del, err)
	}
	if _, err := repo.Delete(ctx, a.ID); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("删不存在应 NotFound，得到 %v", err)
	}
}
