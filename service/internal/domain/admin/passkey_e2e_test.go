package admin

import (
	"bytes"
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/descope/virtualwebauthn"
)

// ---------- 测试替身 ----------

type fakePasskeyRepo struct {
	creds map[int64][]PasskeyCredential
	seq   int64
}

func newFakePasskeyRepo() *fakePasskeyRepo {
	return &fakePasskeyRepo{creds: map[int64][]PasskeyCredential{}}
}

func (f *fakePasskeyRepo) Insert(_ context.Context, adminID int64, c PasskeyCredential) (int64, time.Time, error) {
	f.seq++
	c.ID, c.CreatedAt = f.seq, time.Now()
	f.creds[adminID] = append(f.creds[adminID], c)
	return c.ID, c.CreatedAt, nil
}

func (f *fakePasskeyRepo) List(_ context.Context, adminID int64) ([]PasskeyCredential, error) {
	return f.creds[adminID], nil
}

func (f *fakePasskeyRepo) UpdateOnLogin(_ context.Context, credentialID []byte, signCount uint32) error {
	for id, list := range f.creds {
		for i := range list {
			if bytes.Equal(list[i].CredentialID, credentialID) {
				f.creds[id][i].SignCount = signCount
			}
		}
	}
	return nil
}

func (f *fakePasskeyRepo) Delete(_ context.Context, adminID, id int64) (bool, error) {
	list := f.creds[adminID]
	for i := range list {
		if list[i].ID == id {
			f.creds[adminID] = append(list[:i], list[i+1:]...)
			return true, nil
		}
	}
	return false, nil
}

func (f *fakePasskeyRepo) Count(_ context.Context, adminID int64) (int, error) {
	return len(f.creds[adminID]), nil
}

func (f *fakePasskeyRepo) DeleteByAdmin(_ context.Context, adminID int64) error {
	delete(f.creds, adminID)
	return nil
}

type fakeWAStore struct{ m map[string][]byte }

func newFakeWAStore() *fakeWAStore { return &fakeWAStore{m: map[string][]byte{}} }

func (s *fakeWAStore) SaveWebAuthnSession(_ context.Context, key string, data []byte, _ time.Duration) error {
	s.m[key] = data
	return nil
}

func (s *fakeWAStore) LoadWebAuthnSession(_ context.Context, key string) ([]byte, bool, error) {
	b, ok := s.m[key]
	return b, ok, nil
}

func (s *fakeWAStore) DelWebAuthnSession(_ context.Context, key string) error {
	delete(s.m, key)
	return nil
}

// ---------- 真实加密 E2E（用 descope/virtualwebauthn 模拟认证器）----------

func TestPasskey_RegisterThenAssert(t *testing.T) {
	const (
		rpID   = "localhost"
		origin = "http://localhost:5173"
	)
	repo := newFakePasskeyRepo()
	svc, err := NewPasskeyService(PasskeyConfig{RPID: rpID, DisplayName: "x-site 运营后台", Origins: []string{origin}}, repo, newFakeWAStore())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	rp := virtualwebauthn.RelyingParty{Name: "x-site 运营后台", ID: rpID, Origin: origin}
	authr := virtualwebauthn.NewAuthenticator()
	cred := virtualwebauthn.NewCredential(virtualwebauthn.KeyTypeEC2)

	// ---- 注册 ----
	regOpts, err := svc.BeginRegistration(ctx, 7, "alice")
	if err != nil {
		t.Fatalf("BeginRegistration：%v", err)
	}
	attOpts, err := virtualwebauthn.ParseAttestationOptions(string(regOpts))
	if err != nil {
		t.Fatalf("解析注册选项：%v", err)
	}
	if attOpts.RelyingPartyID != rpID {
		t.Fatalf("RPID 不符：%q", attOpts.RelyingPartyID)
	}
	attResp := virtualwebauthn.CreateAttestationResponse(rp, authr, cred, *attOpts)
	if err := svc.FinishRegistration(ctx, 7, "alice", "我的钥匙", []byte(attResp)); err != nil {
		t.Fatalf("FinishRegistration：%v", err)
	}
	list, _ := svc.ListPasskeys(ctx, 7)
	if len(list) != 1 || list[0].Name != "我的钥匙" {
		t.Fatalf("应注册一把并记下名字：%+v", list)
	}

	// 认证器记住该凭据与用户句柄（断言时返回）
	authr.AddCredential(cred)
	authr.Options.UserHandle = []byte(strconv.FormatInt(7, 10))

	// ---- 断言（登录第二步 / reauth 共用）----
	asOpts, err := svc.BeginAssertion(ctx, 7, PasskeyLoginKey("tkt-1"))
	if err != nil {
		t.Fatalf("BeginAssertion：%v", err)
	}
	parsedAs, err := virtualwebauthn.ParseAssertionOptions(string(asOpts))
	if err != nil {
		t.Fatalf("解析断言选项：%v", err)
	}
	asResp := virtualwebauthn.CreateAssertionResponse(rp, authr, cred, *parsedAs)
	if err := svc.FinishAssertion(ctx, 7, PasskeyLoginKey("tkt-1"), []byte(asResp)); err != nil {
		t.Fatalf("FinishAssertion 应通过：%v", err)
	}

	// ---- 负例：换一把没注册的钥匙断言应失败 ----
	asOpts2, err := svc.BeginAssertion(ctx, 7, PasskeyLoginKey("tkt-2"))
	if err != nil {
		t.Fatal(err)
	}
	parsedAs2, _ := virtualwebauthn.ParseAssertionOptions(string(asOpts2))
	other := virtualwebauthn.NewCredential(virtualwebauthn.KeyTypeEC2)
	otherAuthr := virtualwebauthn.NewAuthenticator()
	otherAuthr.AddCredential(other)
	otherAuthr.Options.UserHandle = []byte("7")
	badResp := virtualwebauthn.CreateAssertionResponse(rp, otherAuthr, other, *parsedAs2)
	if err := svc.FinishAssertion(ctx, 7, PasskeyLoginKey("tkt-2"), []byte(badResp)); err == nil {
		t.Fatal("未注册的凭据断言应失败")
	}
}

func TestPasskey_BeginAssertion_NoCredential(t *testing.T) {
	svc, err := NewPasskeyService(PasskeyConfig{RPID: "localhost", DisplayName: "x", Origins: []string{"http://localhost:5173"}}, newFakePasskeyRepo(), newFakeWAStore())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.BeginAssertion(context.Background(), 7, "login:x"); err == nil {
		t.Fatal("没有任何 Passkey 时不应能发起断言")
	}
}
