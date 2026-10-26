package moderation

import (
	"context"
	"errors"
	"strings"
	"testing"

	"xsite/service/internal/errs"
)

type fakeEmojiRepo struct {
	packs      []EmojiPack
	emojis     []Emoji
	createdP   EmojiPack
	createdE   Emoji
	updatedE   Emoji
	deletedPID int64
	deletedEID int64
}

func (r *fakeEmojiRepo) ListPacks(context.Context) ([]EmojiPack, []Emoji, error) {
	return r.packs, r.emojis, nil
}
func (r *fakeEmojiRepo) CreatePack(_ context.Context, name string, sort int) (EmojiPack, error) {
	r.createdP = EmojiPack{ID: 1, Name: name, Sort: sort}
	return r.createdP, nil
}
func (r *fakeEmojiRepo) UpdatePack(_ context.Context, id int64, name string, sort int) (EmojiPack, error) {
	return EmojiPack{ID: id, Name: name, Sort: sort}, nil
}
func (r *fakeEmojiRepo) DeletePack(_ context.Context, id int64) (EmojiPack, error) {
	r.deletedPID = id
	return EmojiPack{ID: id, Name: "旧包"}, nil
}
func (r *fakeEmojiRepo) CreateEmoji(_ context.Context, packID int64, code, image string, sort int) (Emoji, error) {
	r.createdE = Emoji{ID: 1, PackID: packID, Code: code, Image: image, Sort: sort}
	return r.createdE, nil
}
func (r *fakeEmojiRepo) UpdateEmoji(_ context.Context, id int64, code, image string, sort int) (Emoji, error) {
	r.updatedE = Emoji{ID: id, Code: code, Image: image, Sort: sort}
	return r.updatedE, nil
}
func (r *fakeEmojiRepo) DeleteEmoji(_ context.Context, id int64) (Emoji, error) {
	r.deletedEID = id
	return Emoji{ID: id, Code: "[旧]"}, nil
}

func TestEmoji_FrontNesting(t *testing.T) {
	repo := &fakeEmojiRepo{
		packs:  []EmojiPack{{ID: 1, Name: "默认"}, {ID: 2, Name: "空包"}},
		emojis: []Emoji{{ID: 10, PackID: 1, Code: "[微笑]", Image: "/img/a.png"}, {ID: 11, PackID: 1, Code: "[哭]", Image: "/img/b.png"}},
	}
	svc := NewEmojiService(repo)
	packs, err := svc.Front(context.Background())
	if err != nil || len(packs) != 2 {
		t.Fatalf("前台应 2 个包：%+v %v", packs, err)
	}
	if len(packs[0].Emojis) != 2 || packs[0].Emojis[0].Code != "[微笑]" {
		t.Fatalf("包 1 应含 2 个表情：%+v", packs[0])
	}
	if packs[1].Emojis == nil || len(packs[1].Emojis) != 0 {
		t.Fatalf("空包的 emojis 应为非 nil 空切片：%+v", packs[1])
	}
}

func TestEmoji_PackValidation(t *testing.T) {
	svc := NewEmojiService(&fakeEmojiRepo{})
	if _, err := svc.CreatePack(context.Background(), "   ", 0); !errors.Is(err, errs.ErrBadRequest) {
		t.Fatal("空名应 400")
	}
	if _, err := svc.CreatePack(context.Background(), strings.Repeat("字", maxEmojiPackNameRunes+1), 0); !errors.Is(err, errs.ErrBadRequest) {
		t.Fatal("超长名应 400")
	}
	// 正常创建（去首尾空白）
	repo := &fakeEmojiRepo{}
	svc2 := NewEmojiService(repo)
	if _, err := svc2.CreatePack(context.Background(), "  默认  ", 5); err != nil || repo.createdP.Name != "默认" || repo.createdP.Sort != 5 {
		t.Fatalf("创建包：%+v %v", repo.createdP, err)
	}
}

func TestEmoji_EmojiValidation(t *testing.T) {
	svc := NewEmojiService(&fakeEmojiRepo{})
	// 码未加中括号
	if _, err := svc.CreateEmoji(context.Background(), 1, "微笑", "/img/a.png", 0); !errors.Is(err, errs.ErrBadRequest) {
		t.Fatal("码须形如 [微笑]，否则 400")
	}
	// 空码
	if _, err := svc.CreateEmoji(context.Background(), 1, "  ", "/img/a.png", 0); !errors.Is(err, errs.ErrBadRequest) {
		t.Fatal("空码应 400")
	}
	// 空图
	if _, err := svc.CreateEmoji(context.Background(), 1, "[微笑]", "  ", 0); !errors.Is(err, errs.ErrBadRequest) {
		t.Fatal("空图应 400")
	}
	// 正常创建
	repo := &fakeEmojiRepo{}
	svc2 := NewEmojiService(repo)
	if _, err := svc2.CreateEmoji(context.Background(), 2, "  [微笑] ", " /img/a.png ", 3); err != nil ||
		repo.createdE.Code != "[微笑]" || repo.createdE.Image != "/img/a.png" || repo.createdE.PackID != 2 {
		t.Fatalf("创建表情：%+v %v", repo.createdE, err)
	}
}

func TestEmoji_Delete(t *testing.T) {
	repo := &fakeEmojiRepo{}
	svc := NewEmojiService(repo)
	if p, err := svc.DeletePack(context.Background(), 7); err != nil || repo.deletedPID != 7 || p.Name != "旧包" {
		t.Fatalf("删包应返回被删对象：%+v %v", p, err)
	}
	if e, err := svc.DeleteEmoji(context.Background(), 9); err != nil || repo.deletedEID != 9 || e.Code != "[旧]" {
		t.Fatalf("删表情应返回被删对象：%+v %v", e, err)
	}
}
