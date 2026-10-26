package moderation

import (
	"context"
	"errors"
	"testing"

	"xsite/service/internal/errs"
)

type fakeRepo struct {
	words   []Blockword
	seq     int64
	groups  []string
	listErr error
	createE error
}

func (r *fakeRepo) List(_ context.Context, q BlockwordQuery) ([]Blockword, int64, error) {
	if r.listErr != nil {
		return nil, 0, r.listErr
	}
	return r.words, int64(len(r.words)), nil
}
func (r *fakeRepo) Create(_ context.Context, word, group, action string) (Blockword, error) {
	if r.createE != nil {
		return Blockword{}, r.createE
	}
	for _, w := range r.words {
		if w.Word == word {
			return Blockword{}, errs.ErrConflict
		}
	}
	r.seq++
	bw := Blockword{ID: r.seq, Word: word, Group: group, Action: Action(action)}
	r.words = append(r.words, bw)
	return bw, nil
}
func (r *fakeRepo) Delete(_ context.Context, id int64) (Blockword, error) {
	for i, w := range r.words {
		if w.ID == id {
			r.words = append(r.words[:i], r.words[i+1:]...)
			return w, nil
		}
	}
	return Blockword{}, errs.ErrNotFound
}
func (r *fakeRepo) ListAll(context.Context) ([]WordSpec, error) {
	out := make([]WordSpec, len(r.words))
	for i, w := range r.words {
		out[i] = WordSpec{Word: w.Word, Group: w.Group, Action: w.Action}
	}
	return out, nil
}
func (r *fakeRepo) Groups(context.Context) ([]string, error) { return r.groups, nil }

type fakePublisher struct{ topics []string }

func (p *fakePublisher) Publish(_ context.Context, topic string) error {
	p.topics = append(p.topics, topic)
	return nil
}

func hit(s *Service, text string) bool { return len(s.Scan(text)) > 0 }

func TestService_CreateNormalizesAndRebuilds(t *testing.T) {
	repo := &fakeRepo{}
	pub := &fakePublisher{}
	s := NewService(repo, pub)
	ctx := context.Background()

	if hit(s, "用 VPN 翻墙") {
		t.Fatal("空词表不应命中")
	}
	bw, err := s.Create(ctx, "  ＶＰＮ  ", "引流", "block")
	if err != nil {
		t.Fatal(err)
	}
	if bw.Word != "vpn" {
		t.Fatalf("应规范化为 vpn，得到 %q", bw.Word)
	}
	// 创建后自动机已重建：规范化文本能命中
	hits := s.Scan("买 VPN")
	if len(hits) != 1 || hits[0].Action != ActionBlock || hits[0].Word != "vpn" {
		t.Fatalf("创建后应能命中：%+v", hits)
	}
	if len(pub.topics) != 1 || pub.topics[0] != BlockwordTopic {
		t.Fatalf("应广播一次 %q，得到 %v", BlockwordTopic, pub.topics)
	}
	if _, err := s.Create(ctx, "VPN", "引流", "block"); !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("重复词应 409，得到 %v", err)
	}
}

func TestService_CreateValidation(t *testing.T) {
	s := NewService(&fakeRepo{}, &fakePublisher{})
	ctx := context.Background()
	for _, tt := range []struct{ word, group, action string }{
		{"   ", "g", "block"}, // 空词
		{"词", "g", "bad"},     // 非法 action
		{"词", "g", ""},        // 缺 action
	} {
		if _, err := s.Create(ctx, tt.word, tt.group, tt.action); !errors.Is(err, errs.ErrBadRequest) {
			t.Errorf("Create(%q,%q,%q) 应 400，得到 %v", tt.word, tt.group, tt.action, err)
		}
	}
}

func TestService_DeleteRebuilds(t *testing.T) {
	repo := &fakeRepo{}
	pub := &fakePublisher{}
	s := NewService(repo, pub)
	ctx := context.Background()
	bw, _ := s.Create(ctx, "敏感", "", "mask")
	if !hit(s, "很敏感的话") {
		t.Fatal("创建后应命中")
	}
	if _, err := s.Delete(ctx, bw.ID); err != nil {
		t.Fatal(err)
	}
	if hit(s, "很敏感的话") {
		t.Fatal("删除后重建，不应再命中")
	}
	if len(pub.topics) != 2 {
		t.Fatalf("创建 + 删除应各广播一次，得到 %v", pub.topics)
	}
}

func TestService_OnInvalidateReloads(t *testing.T) {
	repo := &fakeRepo{}
	s := NewService(repo, &fakePublisher{})
	ctx := context.Background()
	// 绕过 service 直接改库（模拟另一实例写入），本实例自动机还是旧的
	repo.words = append(repo.words, Blockword{ID: 1, Word: "翻墙", Action: ActionReview})
	if hit(s, "教你翻墙") {
		t.Fatal("未重建前不应命中")
	}
	s.OnInvalidate(ctx) // 模拟收到广播
	if !hit(s, "教你翻墙") {
		t.Fatal("收到广播重建后应命中")
	}
}

func TestService_ListValidation(t *testing.T) {
	s := NewService(&fakeRepo{}, &fakePublisher{})
	ctx := context.Background()
	if _, err := s.List(ctx, BlockwordFilter{Action: "bad"}); !errors.Is(err, errs.ErrBadRequest) {
		t.Fatal("非法 action 筛选应 400")
	}
	if _, err := s.List(ctx, BlockwordFilter{PageRequest: PageRequest{PageSize: 101}}); !errors.Is(err, errs.ErrBadRequest) {
		t.Fatal("每页过大应 400")
	}
}
