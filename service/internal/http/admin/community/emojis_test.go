package community

import (
	"context"
	"slices"
	"strings"
	"testing"

	dom "xsite/service/internal/domain/moderation"
	"xsite/service/internal/errs"
	"xsite/service/internal/http/admin"
)

// fakeEmojis 记录调用并返回可控结果。
type fakeEmojis struct {
	createdPack  string // "name|sort"
	createdEmoji string // "packID|code|image|sort"
	deletedPack  int64
	deletedEmoji int64
	createErr    error
}

func (f *fakeEmojis) List(context.Context) ([]dom.PackWithEmojis, error) {
	return []dom.PackWithEmojis{
		{Pack: dom.EmojiPack{ID: 1, Name: "默认", Sort: 0}, Emojis: []dom.Emoji{{ID: 10, PackID: 1, Code: "[微笑]", Image: "/img/a.png"}}},
		{Pack: dom.EmojiPack{ID: 2, Name: "空包"}, Emojis: []dom.Emoji{}},
	}, nil
}
func (f *fakeEmojis) CreatePack(_ context.Context, name string, sort int) (dom.EmojiPack, error) {
	if f.createErr != nil {
		return dom.EmojiPack{}, f.createErr
	}
	f.createdPack = name + "|" + itoa(int64(sort))
	return dom.EmojiPack{ID: 5, Name: name, Sort: sort}, nil
}
func (f *fakeEmojis) UpdatePack(_ context.Context, id int64, name string, sort int) (dom.EmojiPack, error) {
	return dom.EmojiPack{ID: id, Name: name, Sort: sort}, nil
}
func (f *fakeEmojis) DeletePack(_ context.Context, id int64) (dom.EmojiPack, error) {
	f.deletedPack = id
	return dom.EmojiPack{ID: id, Name: "旧包"}, nil
}
func (f *fakeEmojis) CreateEmoji(_ context.Context, packID int64, code, image string, sort int) (dom.Emoji, error) {
	if f.createErr != nil {
		return dom.Emoji{}, f.createErr
	}
	f.createdEmoji = itoa(packID) + "|" + code + "|" + image + "|" + itoa(int64(sort))
	return dom.Emoji{ID: 7, PackID: packID, Code: code, Image: image, Sort: sort}, nil
}
func (f *fakeEmojis) UpdateEmoji(_ context.Context, id int64, code, image string, sort int) (dom.Emoji, error) {
	return dom.Emoji{ID: id, Code: code, Image: image, Sort: sort}, nil
}
func (f *fakeEmojis) DeleteEmoji(_ context.Context, id int64) (dom.Emoji, error) {
	f.deletedEmoji = id
	return dom.Emoji{ID: id, Code: "[旧]"}, nil
}

func TestEmojiPermissions(t *testing.T) {
	e := newEnv()
	for _, tt := range []struct {
		as, method, path, body string
		want                   int
	}{
		{"viewer", "GET", "/api/admin/emojis", "", 200},
		{"viewer", "POST", "/api/admin/emojis/packs", `{"name":"新包"}`, 403},
		{"viewer", "POST", "/api/admin/emojis/packs/1/emojis", `{"code":"[笑]","image":"/i.png"}`, 403},
		{"editor", "POST", "/api/admin/emojis/packs", `{"name":"新包","sort":3}`, 200},
		{"editor", "PATCH", "/api/admin/emojis/packs/1", `{"name":"改名","sort":1}`, 200},
		{"editor", "DELETE", "/api/admin/emojis/packs/1", "", 200},
		{"editor", "POST", "/api/admin/emojis/packs/2/emojis", `{"code":"[笑]","image":"/i.png","sort":1}`, 200},
		{"editor", "PATCH", "/api/admin/emojis/items/10", `{"code":"[笑]","image":"/i.png"}`, 200},
		{"editor", "DELETE", "/api/admin/emojis/items/10", "", 200},
		{"nobody", "GET", "/api/admin/emojis", "", 403},
		{"", "GET", "/api/admin/emojis", "", 401},
	} {
		if w := e.do(tt.as, tt.method, tt.path, tt.body); w.Code != tt.want {
			t.Errorf("%s %s %s：期望 %d，得到 %d %s", tt.as, tt.method, tt.path, tt.want, w.Code, w.Body.String())
		}
	}
	got := admin.DeclaredPermissions()
	for _, p := range []string{"emojis.view", "emojis.edit"} {
		if !slices.Contains(got, p) {
			t.Errorf("应声明权限点 %s", p)
		}
	}
}

func TestEmojiList(t *testing.T) {
	e := newEnv()
	b := expect(t, e.do("viewer", "GET", "/api/admin/emojis", ""), 200, "OK")
	if !strings.Contains(string(b.Data), `"packs"`) || !strings.Contains(string(b.Data), `"[微笑]"`) {
		t.Fatalf("列表数据不对：%s", b.Data)
	}
	// 空包的 emojis 为 []（非 null）
	if !strings.Contains(string(b.Data), `"emojis":[]`) {
		t.Fatalf("空包的 emojis 应为 []：%s", b.Data)
	}
}

func TestEmojiWrites(t *testing.T) {
	e := newEnv()
	// 建包
	b := expect(t, e.do("editor", "POST", "/api/admin/emojis/packs", `{"name":"  表情 ","sort":3}`), 200, "OK")
	if e.em.createdPack != "  表情 |3" {
		t.Fatalf("建包请求体原样交给服务：%q", e.em.createdPack)
	}
	if !strings.Contains(string(b.Data), `"id":5`) {
		t.Fatalf("应返回新建的包：%s", b.Data)
	}
	if a := e.audit.last(t); a.Action != "emoji_pack.create" || a.TargetType != "emoji_pack" || a.TargetID != "5" {
		t.Fatalf("建包审计：%+v", a)
	}
	// 加表情到指定包
	expect(t, e.do("editor", "POST", "/api/admin/emojis/packs/2/emojis", `{"code":"[微笑]","image":"/img/a.png","sort":1}`), 200, "OK")
	if e.em.createdEmoji != "2|[微笑]|/img/a.png|1" {
		t.Fatalf("加表情请求体 + 包 ID 交给服务：%q", e.em.createdEmoji)
	}
	if a := e.audit.last(t); a.Action != "emoji.create" || a.TargetType != "emoji" || a.TargetID != "7" {
		t.Fatalf("加表情审计：%+v", a)
	}
	// 未知字段 400
	expect(t, e.do("editor", "POST", "/api/admin/emojis/packs", `{"name":"x","extra":1}`), 400, "BAD_REQUEST")
	// 删包
	expect(t, e.do("editor", "DELETE", "/api/admin/emojis/packs/1", ""), 200, "OK")
	if e.em.deletedPack != 1 {
		t.Fatalf("删包 id 交给服务：%d", e.em.deletedPack)
	}
	if a := e.audit.last(t); a.Action != "emoji_pack.delete" || a.TargetID != "1" {
		t.Fatalf("删包审计：%+v", a)
	}
	// 删表情
	expect(t, e.do("editor", "DELETE", "/api/admin/emojis/items/10", ""), 200, "OK")
	if e.em.deletedEmoji != 10 {
		t.Fatalf("删表情 id 交给服务：%d", e.em.deletedEmoji)
	}
	if a := e.audit.last(t); a.Action != "emoji.delete" || a.TargetID != "10" {
		t.Fatalf("删表情审计：%+v", a)
	}
}

func TestEmojiConflict(t *testing.T) {
	e := newEnv()
	e.em.createErr = errs.ErrConflict.WithMessage("该表情码已存在")
	expect(t, e.do("editor", "POST", "/api/admin/emojis/packs/1/emojis", `{"code":"[微笑]","image":"/i.png"}`), 409, "CONFLICT")
}
