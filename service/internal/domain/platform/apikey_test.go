package platform

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type fakeAPIKeyRepo struct {
	keys     []APIKey
	created  []createdKey
	statuses map[int64]KeyStatus
	authHash string
	authID   int64
	err      error
	notFound bool
}

type createdKey struct {
	in        APIKeyInput
	hash      string
	hint      string
	createdBy int64
}

func (r *fakeAPIKeyRepo) ListAPIKeys(_ context.Context, q APIKeyQuery) ([]APIKey, int64, error) {
	if r.err != nil {
		return nil, 0, r.err
	}
	start := min(q.Offset, len(r.keys))
	end := min(start+q.Limit, len(r.keys))
	return r.keys[start:end], int64(len(r.keys)), nil
}

func (r *fakeAPIKeyRepo) CreateAPIKey(
	_ context.Context, in APIKeyInput, keyHash, hint string, createdBy int64,
) (APIKey, error) {
	if r.err != nil {
		return APIKey{}, r.err
	}
	r.created = append(r.created, createdKey{in: in, hash: keyHash, hint: hint, createdBy: createdBy})
	return APIKey{
		ID: int64(len(r.created)), Name: in.Name, Owner: in.Owner, Hint: hint,
		Scopes: in.Scopes, Status: KeyActive, CreatedBy: createdBy, CreatedAt: time.Now(),
	}, nil
}

func (r *fakeAPIKeyRepo) GetAPIKey(_ context.Context, id int64) (APIKey, error) {
	if r.notFound {
		return APIKey{}, errors.New("内容不存在或已下架")
	}
	return APIKey{ID: id, Name: "旧用途", Owner: "旧使用方", Hint: "xsk_abc123", Status: KeyActive}, nil
}

func (r *fakeAPIKeyRepo) UpdateAPIKey(_ context.Context, id int64, name, owner string) (APIKey, error) {
	if r.notFound {
		return APIKey{}, errors.New("内容不存在或已下架")
	}
	return APIKey{ID: id, Name: name, Owner: owner, Hint: "xsk_abc123", Status: KeyActive}, nil
}

func (r *fakeAPIKeyRepo) SetAPIKeyStatus(_ context.Context, id int64, status KeyStatus) (APIKey, error) {
	if r.notFound {
		return APIKey{}, errors.New("内容不存在或已下架")
	}
	if r.statuses == nil {
		r.statuses = map[int64]KeyStatus{}
	}
	r.statuses[id] = status
	return APIKey{ID: id, Name: "用途", Hint: "xsk_abc123", Status: status}, nil
}

func (r *fakeAPIKeyRepo) AuthenticateAPIKey(_ context.Context, keyHash string) (int64, string, []APIScope, error) {
	if r.authHash == "" || keyHash != r.authHash {
		return 0, "", nil, errors.New("内容不存在或已下架")
	}
	return r.authID, "采集程序", []APIScope{ScopeDramaPush}, nil
}

func (r *fakeAPIKeyRepo) TouchAPIKeyUsage(context.Context, int64) error { return r.err }

func newKeySvc(repo *fakeAPIKeyRepo) *APIKeyService { return NewAPIKeyService(repo) }

func validInput() APIKeyInput {
	return APIKeyInput{Name: "采集程序对接", Owner: "数据组", Scopes: []APIScope{ScopeDramaPush}}
}

// ---------- 生成 ----------

func TestAPIKey_Create(t *testing.T) {
	repo := &fakeAPIKeyRepo{}
	out, err := newKeySvc(repo).Create(context.Background(), validInput(), 7)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out.Secret, APIKeyPrefix) {
		t.Errorf("密钥应当带固定前缀 %q：%s", APIKeyPrefix, out.Secret)
	}
	// 32 字节 base64url 无填充 = 43 字符，加 4 字符前缀
	if len(out.Secret) != len(APIKeyPrefix)+43 {
		t.Errorf("密钥长度 = %d，期望 %d", len(out.Secret), len(APIKeyPrefix)+43)
	}
	if len(repo.created) != 1 {
		t.Fatalf("应当写库一次，实际 %d 次", len(repo.created))
	}
	rec := repo.created[0]
	if rec.createdBy != 7 {
		t.Errorf("没有记下签发人：%d", rec.createdBy)
	}
	// 库里存的必须是哈希，不是原文
	if rec.hash == out.Secret {
		t.Fatal("库里存了密钥原文")
	}
	if rec.hash != HashAPIKey(out.Secret) {
		t.Error("存的哈希与密钥不对应")
	}
	if !strings.HasPrefix(out.Secret, rec.hint) || len(rec.hint) != keyHintLen {
		t.Errorf("开头几位不对：hint=%q secret=%q", rec.hint, out.Secret)
	}
	// 完整密钥不能出现在返回的元数据里（除了 Secret 字段本身）
	if strings.Contains(out.Hint, strings.TrimPrefix(out.Secret, rec.hint)) {
		t.Error("hint 里泄露了密钥的随机部分")
	}
}

// 每次生成的密钥必须不同（随机源正常工作）。
func TestAPIKey_CreateIsRandom(t *testing.T) {
	repo := &fakeAPIKeyRepo{}
	svc := newKeySvc(repo)
	seen := map[string]bool{}
	for range 50 {
		out, err := svc.Create(context.Background(), validInput(), 1)
		if err != nil {
			t.Fatal(err)
		}
		if seen[out.Secret] {
			t.Fatal("生成了重复的密钥")
		}
		seen[out.Secret] = true
	}
	if len(seen) != 50 {
		t.Errorf("50 次生成只得到 %d 个不同的密钥", len(seen))
	}
}

func TestAPIKey_CreateValidation(t *testing.T) {
	cases := []struct {
		name string
		in   APIKeyInput
		want string
	}{
		{"没填用途", APIKeyInput{Scopes: []APIScope{ScopeDramaPush}}, "请填写用途"},
		{"用途只有空格", APIKeyInput{Name: "   ", Scopes: []APIScope{ScopeDramaPush}}, "请填写用途"},
		{"用途过长", APIKeyInput{Name: strings.Repeat("字", 61), Scopes: []APIScope{ScopeDramaPush}}, "用途不能超过"},
		{"使用方过长", APIKeyInput{Name: "x", Owner: strings.Repeat("字", 61), Scopes: []APIScope{ScopeDramaPush}}, "使用方不能超过"},
		{"没选权限范围", APIKeyInput{Name: "x"}, "至少要选一个权限范围"},
		{"权限范围不存在", APIKeyInput{Name: "x", Scopes: []APIScope{"admin.all"}}, "不存在"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			repo := &fakeAPIKeyRepo{}
			_, err := newKeySvc(repo).Create(context.Background(), c.in, 1)
			if err == nil {
				t.Fatalf("%s 应当被拒绝", c.name)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("报错不对（期望含 %q）：%v", c.want, err)
			}
			if len(repo.created) != 0 {
				t.Error("校验失败时不该写库")
			}
		})
	}
}

// 重复的权限范围去重后保存。
func TestAPIKey_CreateDedupesScopes(t *testing.T) {
	repo := &fakeAPIKeyRepo{}
	in := APIKeyInput{Name: "x", Scopes: []APIScope{ScopeDramaPush, ScopeDramaPush, ScopeAnalyticsRead}}
	if _, err := newKeySvc(repo).Create(context.Background(), in, 1); err != nil {
		t.Fatal(err)
	}
	got := repo.created[0].in.Scopes
	if len(got) != 2 {
		t.Errorf("权限范围没有去重：%v", got)
	}
}

// 报错要列出可选的权限范围，调用方才知道该填什么。
func TestAPIKey_UnknownScopeListsOptions(t *testing.T) {
	_, err := newKeySvc(&fakeAPIKeyRepo{}).Create(context.Background(),
		APIKeyInput{Name: "x", Scopes: []APIScope{"nope"}}, 1)
	if err == nil || !strings.Contains(err.Error(), "drama.push") {
		t.Errorf("报错应当列出可选范围：%v", err)
	}
}

// ---------- 哈希 ----------

func TestHashAPIKey(t *testing.T) {
	a := HashAPIKey("xsk_abc")
	if a == "xsk_abc" {
		t.Fatal("哈希等于原文")
	}
	if len(a) != 64 {
		t.Errorf("SHA-256 的十六进制应当是 64 个字符，得到 %d", len(a))
	}
	if a != HashAPIKey("xsk_abc") {
		t.Error("同样的输入应当算出同样的哈希")
	}
	if a == HashAPIKey("xsk_abd") {
		t.Error("不同的输入算出了同样的哈希")
	}
}

func TestEqualHash(t *testing.T) {
	h := HashAPIKey("xsk_x")
	if !EqualHash(h, h) {
		t.Error("相同的哈希应当相等")
	}
	if EqualHash(h, HashAPIKey("xsk_y")) {
		t.Error("不同的哈希不应相等")
	}
	if EqualHash(h, "") {
		t.Error("空串不应与任何哈希相等")
	}
}

// ---------- 显示状态 ----------

// docs/06 第 32 节：可用 / 停用 / 未用过。
func TestAPIKey_DisplayStatus(t *testing.T) {
	used := time.Now()
	cases := []struct {
		name   string
		status KeyStatus
		last   *time.Time
		want   KeyDisplayStatus
	}{
		{"启用且用过 → 可用", KeyActive, &used, KeyDisplayActive},
		{"启用但没用过 → 未用过", KeyActive, nil, KeyDisplayUnused},
		{"停用（用过）→ 停用", KeyInactive, &used, KeyDisplayOff},
		{"停用（没用过）→ 停用", KeyInactive, nil, KeyDisplayOff},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			repo := &fakeAPIKeyRepo{keys: []APIKey{{ID: 1, Status: c.status, LastUsedAt: c.last}}}
			list, _, _, _, err := newKeySvc(repo).List(context.Background(), APIKeyFilter{})
			if err != nil {
				t.Fatal(err)
			}
			if list[0].DisplayStatus != c.want {
				t.Errorf("显示状态 = %q，期望 %q", list[0].DisplayStatus, c.want)
			}
		})
	}
}

// ---------- 停用与启用 ----------

func TestAPIKey_SetStatus(t *testing.T) {
	repo := &fakeAPIKeyRepo{}
	svc := newKeySvc(repo)
	if _, err := svc.SetStatus(context.Background(), 5, KeyInactive); err != nil {
		t.Fatal(err)
	}
	if repo.statuses[5] != KeyInactive {
		t.Errorf("停用没有生效：%v", repo.statuses)
	}
	if _, err := svc.SetStatus(context.Background(), 5, KeyActive); err != nil {
		t.Fatal(err)
	}
	if repo.statuses[5] != KeyActive {
		t.Errorf("重新启用没有生效：%v", repo.statuses)
	}
	if _, err := svc.SetStatus(context.Background(), 5, "deleted"); err == nil {
		t.Error("非法状态应当被拒绝")
	}
}

func TestAPIKey_SetStatusMissing(t *testing.T) {
	svc := newKeySvc(&fakeAPIKeyRepo{notFound: true})
	if _, err := svc.SetStatus(context.Background(), 1, KeyInactive); err == nil {
		t.Error("不存在的密钥应当报错")
	}
}

// ---------- 修改 ----------

// 改用途与使用方可以，权限范围不能改（改范围等于改这把已在外面的密钥能做什么）。
func TestAPIKey_UpdateOnlyText(t *testing.T) {
	repo := &fakeAPIKeyRepo{}
	out, err := newKeySvc(repo).Update(context.Background(), 3, APIKeyInput{
		Name: "  新用途  ", Owner: "  新使用方  ", Scopes: []APIScope{ScopeAnalyticsRead},
	})
	if err != nil {
		t.Fatal(err)
	}
	if out.Name != "新用途" || out.Owner != "新使用方" {
		t.Errorf("没有去掉首尾空白: %+v", out)
	}
}

func TestAPIKey_UpdateValidation(t *testing.T) {
	svc := newKeySvc(&fakeAPIKeyRepo{})
	if _, err := svc.Update(context.Background(), 1, APIKeyInput{Name: ""}); err == nil {
		t.Error("用途为空应当被拒绝")
	}
}

// ---------- 鉴权 ----------

func TestAPIKey_Authenticate(t *testing.T) {
	secret := APIKeyPrefix + "abcdefghijklmnopqrstuvwxyz0123456789ABCDEFG"
	repo := &fakeAPIKeyRepo{authHash: HashAPIKey(secret), authID: 42}
	svc := newKeySvc(repo)

	id, scopes, err := svc.Authenticate(context.Background(), secret)
	if err != nil {
		t.Fatalf("合法密钥应当通过: %v", err)
	}
	if id != 42 || len(scopes) != 1 || scopes[0] != ScopeDramaPush {
		t.Errorf("鉴权结果不对: id=%d scopes=%v", id, scopes)
	}
}

// 失败一律同一个错误：区分「格式不对」「不存在」「已停用」等于告诉试探者哪把密钥真实存在。
func TestAPIKey_AuthenticateFailuresAreIndistinguishable(t *testing.T) {
	secret := APIKeyPrefix + "abcdefghijklmnopqrstuvwxyz0123456789ABCDEFG"
	repo := &fakeAPIKeyRepo{authHash: HashAPIKey(secret), authID: 1}
	svc := newKeySvc(repo)

	var msgs []string
	for _, bad := range []string{
		"",                      // 空
		"nope",                  // 没有前缀
		APIKeyPrefix + "wrong",  // 前缀对但不存在
		secret + "x",            // 多一个字符
		strings.ToUpper(secret), // 大小写不同
	} {
		_, _, err := svc.Authenticate(context.Background(), bad)
		if err == nil {
			t.Fatalf("非法密钥 %q 竟然通过了", bad)
		}
		msgs = append(msgs, err.Error())
	}
	for i := 1; i < len(msgs); i++ {
		if msgs[i] != msgs[0] {
			t.Errorf("不同的失败原因给出了不同的提示：%q vs %q", msgs[0], msgs[i])
		}
	}
}

// ---------- 列表 ----------

func TestAPIKey_ListFilter(t *testing.T) {
	keys := make([]APIKey, 40)
	for i := range keys {
		keys[i] = APIKey{ID: int64(i + 1), Status: KeyActive}
	}
	svc := newKeySvc(&fakeAPIKeyRepo{keys: keys})
	list, total, page, size, err := svc.List(context.Background(), APIKeyFilter{Page: 2, PageSize: 10})
	if err != nil {
		t.Fatal(err)
	}
	if total != 40 || page != 2 || size != 10 || len(list) != 10 || list[0].ID != 11 {
		t.Errorf("分页不对: total=%d page=%d size=%d first=%d", total, page, size, list[0].ID)
	}

	if _, _, _, _, err := svc.List(context.Background(), APIKeyFilter{PageSize: 101}); err == nil {
		t.Error("超过上限的每页条数应当报错")
	}
	if _, _, _, _, err := svc.List(context.Background(), APIKeyFilter{Status: "deleted"}); err == nil {
		t.Error("非法的状态筛选应当报错")
	}
	if _, _, _, _, err := svc.List(context.Background(), APIKeyFilter{Q: strings.Repeat("字", 61)}); err == nil {
		t.Error("超长搜索词应当报错")
	}
}

func TestAPIKey_SearchEscaping(t *testing.T) {
	q, _, _, err := cleanAPIKeyFilter(APIKeyFilter{Q: "100%_a"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(q.Pattern, `\%`) || !strings.Contains(q.Pattern, `\_`) {
		t.Errorf("LIKE 通配符没有转义: %q", q.Pattern)
	}
}

// 每个权限范围都要有名称与说明：页面直接展示。
func TestAPIScopes_AllDescribed(t *testing.T) {
	scopes := APIScopes()
	if len(scopes) != 2 {
		t.Errorf("权限范围数 = %d，期望 2（docs/06 第 32 节列了 2 项）", len(scopes))
	}
	for _, s := range scopes {
		if s.Title == "" || s.Desc == "" {
			t.Errorf("权限范围 %s 缺标题或说明", s.Scope)
		}
	}
}

// APIScopes 返回副本，调用方改不到内部状态。
func TestAPIScopes_ReturnsCopy(t *testing.T) {
	got := APIScopes()
	got[0].Title = "被改了"
	if APIScopes()[0].Title == "被改了" {
		t.Error("APIScopes 返回了内部切片，被外部改动了")
	}
}

func TestAPIKey_RepoErrors(t *testing.T) {
	svc := newKeySvc(&fakeAPIKeyRepo{err: errors.New("库挂了")})
	ctx := context.Background()
	if _, _, _, _, err := svc.List(ctx, APIKeyFilter{}); err == nil {
		t.Error("列表：底层出错应当报错")
	}
	if _, err := svc.Create(ctx, validInput(), 1); err == nil {
		t.Error("生成：底层出错应当报错")
	}
}
