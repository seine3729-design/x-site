package pg

import (
	"context"
	"testing"

	"xsite/service/internal/domain/moderation"
)

// 表情包 / 表情仓储（S2-5）集成测试：建包 → 加表情 → 列表嵌套 → 改 → 删包级联。
func TestEmojiRepo(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewEmojiRepo(pool)

	// 建两个包（sort 决定顺序）
	p2, err := repo.CreatePack(ctx, "后建但靠前", 1)
	if err != nil {
		t.Fatal(err)
	}
	p1, err := repo.CreatePack(ctx, "先建但靠后", 2)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = repo.DeletePack(context.Background(), p1.ID)
		_, _ = repo.DeletePack(context.Background(), p2.ID)
	})

	// 往 p2 加两个表情（code 全局唯一，用 nonce 避免并发冲突）
	code1 := "[集成笑]"
	code2 := "[集成哭]"
	_, _ = pool.Exec(ctx, `DELETE FROM emoji WHERE code IN ($1,$2)`, code1, code2)
	e1, err := repo.CreateEmoji(ctx, p2.ID, code1, "/img/a.png", 2)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.CreateEmoji(ctx, p2.ID, code2, "/img/b.png", 1); err != nil {
		t.Fatal(err)
	}

	// 列表：包按 sort（p2 在前），表情按 sort（code2 在前）
	packs, emojis, err := repo.ListPacks(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var gotP2 *moderation.EmojiPack
	for i := range packs {
		if packs[i].ID == p2.ID {
			gotP2 = &packs[i]
		}
	}
	if gotP2 == nil || gotP2.Name != "后建但靠前" {
		t.Fatalf("应能读到 p2：%+v", packs)
	}
	var p2Emojis []moderation.Emoji
	for _, e := range emojis {
		if e.PackID == p2.ID {
			p2Emojis = append(p2Emojis, e)
		}
	}
	if len(p2Emojis) != 2 || p2Emojis[0].Code != code2 {
		t.Fatalf("p2 的表情应按 sort：%+v", p2Emojis)
	}

	// 改表情
	if up, err := repo.UpdateEmoji(ctx, e1.ID, code1, "/img/a2.png", 9); err != nil || up.Image != "/img/a2.png" || up.Sort != 9 {
		t.Fatalf("改表情：%+v %v", up, err)
	}

	// 删包级联删表情
	if _, err := repo.DeletePack(ctx, p2.ID); err != nil {
		t.Fatal(err)
	}
	var cnt int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM emoji WHERE code IN ($1,$2)`, code1, code2).Scan(&cnt); err != nil {
		t.Fatal(err)
	}
	if cnt != 0 {
		t.Fatalf("删包应级联删表情，残留 %d", cnt)
	}
}
