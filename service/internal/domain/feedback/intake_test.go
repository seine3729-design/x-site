package feedback

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"xsite/service/internal/errs"
)

// stubKeyRepo 只实现用到的方法；嵌入接口让未实现的方法在被误用时直接 panic。
type stubKeyRepo struct {
	APIKeyRepo
	keys          map[string]APIKey // keyHash → Key
	projectStatus ProjectStatus
	created       IntakeInput
	createdKey    APIKey
	createCalls   int
}

func newStubKeyRepo() *stubKeyRepo {
	return &stubKeyRepo{keys: map[string]APIKey{}, projectStatus: ProjectActive}
}

func (r *stubKeyRepo) APIKeyByHash(_ context.Context, keyHash string) (APIKey, ProjectStatus, error) {
	k, ok := r.keys[keyHash]
	if !ok {
		return APIKey{}, "", errs.ErrNotFound
	}
	return k, r.projectStatus, nil
}

func (r *stubKeyRepo) CreateFeedbackFromAPI(_ context.Context, key APIKey, in IntakeInput) (Feedback, error) {
	r.created, r.createdKey = in, key
	r.createCalls++
	return Feedback{ID: 1, Number: 1024, Key: "DEMO-1024", Status: StatusSubmitted,
		Source: SourceOpenAPI, APIKeyID: key.ID, PageURL: in.PageURL, Contact: in.Contact}, nil
}

func (r *stubKeyRepo) CreateAPIKey(_ context.Context, keyHash, hint string, in APIKeyInput) (APIKey, error) {
	return APIKey{ID: 5, ProjectID: in.ProjectID, Name: in.Name, KeyHint: hint,
		DefaultVisibility: in.DefaultVisibility, AllowedOrigins: in.AllowedOrigins,
		DailyLimit: in.DailyLimit, Status: in.Status}, nil
}

// quotaCounter 是可编程的计数器：counts 给出每次 Incr 的返回值，err 非空则模拟缓存故障。
type quotaCounter struct {
	counts map[string]int64
	err    error
	calls  []string
}

func newQuotaCounter() *quotaCounter { return &quotaCounter{counts: map[string]int64{}} }

func (c *quotaCounter) Get(context.Context, string) (int64, time.Duration, error) { return 0, 0, nil }

func (c *quotaCounter) Incr(_ context.Context, key string, _ time.Duration) (int64, error) {
	c.calls = append(c.calls, key)
	if c.err != nil {
		return 0, c.err
	}
	c.counts[key]++
	return c.counts[key], nil
}

const testRawKey = "fbk_test-raw-key"

// newTestIntake 返回一个装好可用 Key 的服务。
func newTestIntake(repo *stubKeyRepo, limiter RateCounter, key APIKey) *IntakeService {
	key.ID, key.Status = 3, orDefault(key.Status, "active")
	if key.DailyLimit == 0 {
		key.DailyLimit = 500
	}
	repo.keys[HashAPIKey(testRawKey)] = key
	s := NewIntakeService(repo, limiter)
	s.now = func() time.Time { return fixedNow }
	return s
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

func goodIntake() IntakeInput {
	return IntakeInput{Kind: KindCompetitor, Title: "首页加载很慢", Body: "打开首页要等五六秒"}
}

// 无效、未知与已停用的 Key 返回同一个错误，避免外部据此判断 Key 是否存在。
func TestIntakeSubmit_BadKey(t *testing.T) {
	ctx := context.Background()
	revoked := fixedNow.Add(-time.Hour)

	tests := []struct {
		name string
		raw  string
		key  APIKey
	}{
		{"空 Key", "", APIKey{}},
		{"只有空白", "   ", APIKey{}},
		{"未知 Key", "fbk_someone-elses-key", APIKey{}},
		{"已停用", testRawKey, APIKey{Status: "inactive"}},
		{"已作废", testRawKey, APIKey{RevokedAt: &revoked}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := newStubKeyRepo()
			s := newTestIntake(repo, newQuotaCounter(), tt.key)
			_, err := s.Submit(ctx, tt.raw, goodIntake())
			if !errors.Is(err, errs.ErrSignatureInvalid) {
				t.Fatalf("err = %v，期望 ErrSignatureInvalid", err)
			}
			if got := errs.From(err).Message; got != errs.From(errBadAPIKey).Message {
				t.Fatalf("提示 = %q，期望与通用提示一致（不区分不存在与停用）", got)
			}
			if repo.createCalls != 0 {
				t.Fatal("Key 不可用却写了库")
			}
		})
	}
}

// 项目不在可接收状态时不收新反馈，否则会堆进没人看的项目。
func TestIntakeSubmit_ProjectStatus(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		status ProjectStatus
		ok     bool
	}{
		{ProjectRecruiting, true},
		{ProjectActive, true},
		{ProjectDraft, false},
		{ProjectPaused, false},
		{ProjectArchived, false},
	}
	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			repo := newStubKeyRepo()
			repo.projectStatus = tt.status
			s := newTestIntake(repo, newQuotaCounter(), APIKey{})
			_, err := s.Submit(ctx, testRawKey, goodIntake())
			if tt.ok {
				if err != nil {
					t.Fatalf("期望收下，实际 %v", err)
				}
				return
			}
			if !errors.Is(err, errs.ErrConflict) {
				t.Fatalf("err = %v，期望 ErrConflict", err)
			}
		})
	}
}

func TestIntakeSubmit_OriginWhitelist(t *testing.T) {
	ctx := context.Background()
	whitelist := []string{"https://partner.example.com", "https://shop.example.com"}

	tests := []struct {
		name    string
		origins []string
		origin  string
		ok      bool
	}{
		{"没配白名单时任何来源都收", nil, "https://anyone.example.com", true},
		{"没配白名单且没有 Origin 也收", nil, "", true},
		{"白名单内", whitelist, "https://partner.example.com", true},
		{"白名单内（大小写与空白不敏感）", whitelist, "  HTTPS://Partner.Example.com ", true},
		{"白名单外", whitelist, "https://evil.example.com", false},
		{"同域不同端口不算同一来源", whitelist, "https://partner.example.com:8443", false},
		// 服务端直连不带 Origin：配了白名单就必须拒绝，否则白名单可以靠去掉 Origin 绕过
		{"配了白名单但没有 Origin", whitelist, "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := newStubKeyRepo()
			s := newTestIntake(repo, newQuotaCounter(), APIKey{AllowedOrigins: tt.origins})
			in := goodIntake()
			in.Origin = tt.origin
			_, err := s.Submit(ctx, testRawKey, in)
			if tt.ok {
				if err != nil {
					t.Fatalf("期望收下，实际 %v", err)
				}
				return
			}
			if !errors.Is(err, errs.ErrForbidden) {
				t.Fatalf("err = %v，期望 ErrForbidden", err)
			}
			if repo.createCalls != 0 {
				t.Fatal("来源不合法却写了库")
			}
		})
	}
}

func TestIntakeSubmit_Quota(t *testing.T) {
	ctx := context.Background()

	t.Run("超过每分钟突发上限", func(t *testing.T) {
		repo := newStubKeyRepo()
		limiter := newQuotaCounter()
		limiter.counts["m:3"] = intakeBurstPerMinute // 下一次 Incr 就会越界
		s := newTestIntake(repo, limiter, APIKey{})
		_, err := s.Submit(ctx, testRawKey, goodIntake())
		if !errors.Is(err, errs.ErrRateLimited) {
			t.Fatalf("err = %v，期望 ErrRateLimited", err)
		}
		if got := errs.RetryAfter(err); got != time.Minute {
			t.Fatalf("Retry-After = %v，期望 1 分钟", got)
		}
		if repo.createCalls != 0 {
			t.Fatal("超限却写了库")
		}
	})

	t.Run("超过每日上限", func(t *testing.T) {
		repo := newStubKeyRepo()
		limiter := newQuotaCounter()
		limiter.counts["d:3:"+fixedNow.UTC().Format("20060102")] = 10
		s := newTestIntake(repo, limiter, APIKey{DailyLimit: 10})
		_, err := s.Submit(ctx, testRawKey, goodIntake())
		if !errors.Is(err, errs.ErrRateLimited) {
			t.Fatalf("err = %v，期望 ErrRateLimited", err)
		}
		if got := errs.RetryAfter(err); got != time.Hour {
			t.Fatalf("Retry-After = %v，期望 1 小时", got)
		}
	})

	t.Run("日计数按 UTC 日期分键", func(t *testing.T) {
		repo := newStubKeyRepo()
		limiter := newQuotaCounter()
		s := newTestIntake(repo, limiter, APIKey{})
		if _, err := s.Submit(ctx, testRawKey, goodIntake()); err != nil {
			t.Fatal(err)
		}
		want := "d:3:" + fixedNow.UTC().Format("20060102")
		if len(limiter.calls) != 2 || limiter.calls[0] != "m:3" || limiter.calls[1] != want {
			t.Fatalf("计数键 = %v，期望 [m:3 %s]", limiter.calls, want)
		}
	})

	// 配额计数依赖 Valkey：缓存故障时放行，不因此丢用户反馈
	t.Run("计数器故障时放行", func(t *testing.T) {
		repo := newStubKeyRepo()
		limiter := newQuotaCounter()
		limiter.err = fmt.Errorf("valkey 不可用")
		s := newTestIntake(repo, limiter, APIKey{})
		if _, err := s.Submit(ctx, testRawKey, goodIntake()); err != nil {
			t.Fatalf("期望放行，实际 %v", err)
		}
		if repo.createCalls != 1 {
			t.Fatalf("写库次数 = %d，期望 1", repo.createCalls)
		}
	})
}

// 回执只给编号与状态，不回显正文，也不泄露内部 ID。
func TestIntakeSubmit_Result(t *testing.T) {
	repo := newStubKeyRepo()
	s := newTestIntake(repo, newQuotaCounter(), APIKey{ProjectSlug: "demo"})
	in := goodIntake()
	in.PageURL = "https://shop.example.com/order?token=secret#top"
	in.Contact = "  微信 abc  "

	res, err := s.Submit(context.Background(), testRawKey, in)
	if err != nil {
		t.Fatal(err)
	}
	if res.Key != "DEMO-1024" || res.Number != 1024 || res.ProjectSlug != "demo" || res.Status != StatusSubmitted {
		t.Fatalf("回执 = %+v", res)
	}
	// 页面地址的查询串与片段必须在落库前丢掉（可能带令牌）
	if repo.created.PageURL != "https://shop.example.com/order" {
		t.Fatalf("落库的页面地址 = %q，期望已去掉查询串与片段", repo.created.PageURL)
	}
	if repo.created.Contact != "微信 abc" {
		t.Fatalf("落库的联系方式 = %q，期望已去掉首尾空白", repo.created.Contact)
	}
	if repo.createdKey.ID != 3 {
		t.Fatalf("写库用的 Key = %+v，期望 ID 3", repo.createdKey)
	}
}

func TestValidateIntake(t *testing.T) {
	tests := []struct {
		name string
		in   IntakeInput
		ok   bool
	}{
		{"正常", goodIntake(), true},
		{"不填类型按其他处理", IntakeInput{Title: "首页加载很慢", Body: "正文"}, true},
		{"非法类型", IntakeInput{Kind: "whatever", Title: "首页加载很慢", Body: "正文"}, false},
		{"标题太短", IntakeInput{Kind: KindBug, Title: "慢", Body: "正文"}, false},
		{"标题太长", IntakeInput{Kind: KindBug, Title: strings.Repeat("字", 161), Body: "正文"}, false},
		{"正文为空", IntakeInput{Kind: KindBug, Title: "首页加载很慢", Body: "  "}, false},
		{"页面地址不是链接", IntakeInput{Kind: KindBug, Title: "首页加载很慢", Body: "正文", PageURL: "javascript:alert(1)"}, false},
		{"页面地址用站内路径", IntakeInput{Kind: KindBug, Title: "首页加载很慢", Body: "正文", PageURL: "/orders/1?t=x"}, true},
		{"联系方式太长", IntakeInput{Kind: KindBug, Title: "首页加载很慢", Body: "正文", Contact: strings.Repeat("a", 121)}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := validateIntake(tt.in)
			if tt.ok != (err == nil) {
				t.Fatalf("err = %v，期望 ok=%v", err, tt.ok)
			}
			if err == nil && got.Kind == "" {
				t.Fatal("类型应补上默认值")
			}
		})
	}
}

func TestCleanPageURL(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
		ok   bool
	}{
		{"空", "", "", true},
		{"丢掉查询串与片段", "https://a.example.com/p?token=x#frag", "https://a.example.com/p", true},
		{"丢掉 URL 里的账号密码", "https://u:p@a.example.com/p", "https://a.example.com/p", true},
		{"站内路径只留路径", "/orders/1?token=x", "/orders/1", true},
		{"http 也允许", "http://a.example.com/p", "http://a.example.com/p", true},
		{"没有主机名", "https:///p", "", false},
		{"其他协议", "ftp://a.example.com/p", "", false},
		{"脚本协议", "javascript:alert(1)", "", false},
		{"过长", "https://a.example.com/" + strings.Repeat("x", 500), "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := cleanPageURL(tt.in)
			if tt.ok != (err == nil) {
				t.Fatalf("err = %v，期望 ok=%v", err, tt.ok)
			}
			if err == nil && got != tt.want {
				t.Fatalf("结果 = %q，期望 %q", got, tt.want)
			}
		})
	}
}

func TestValidateAPIKeyInput(t *testing.T) {
	base := APIKeyInput{ProjectID: 1, Name: "合作站点 A"}

	t.Run("补默认值", func(t *testing.T) {
		got, err := validateAPIKeyInput(base)
		if err != nil {
			t.Fatal(err)
		}
		// 外部来源默认只对成员可见：先进后台分诊，避免站外内容直接公开
		if got.DefaultVisibility != VisibilityMembers {
			t.Errorf("DefaultVisibility = %q，期望 members", got.DefaultVisibility)
		}
		if got.Status != "active" {
			t.Errorf("Status = %q，期望 active", got.Status)
		}
		if got.DailyLimit != 500 {
			t.Errorf("DailyLimit = %d，期望 500", got.DailyLimit)
		}
	})

	t.Run("来源只保留 scheme 与 host", func(t *testing.T) {
		in := base
		in.AllowedOrigins = []string{"HTTPS://Partner.Example.com/submit?a=1", "  ", "http://localhost:3000"}
		got, err := validateAPIKeyInput(in)
		if err != nil {
			t.Fatal(err)
		}
		want := []string{"https://partner.example.com", "http://localhost:3000"}
		if len(got.AllowedOrigins) != len(want) {
			t.Fatalf("来源 = %v，期望 %v", got.AllowedOrigins, want)
		}
		for i := range want {
			if got.AllowedOrigins[i] != want[i] {
				t.Fatalf("来源 = %v，期望 %v", got.AllowedOrigins, want)
			}
		}
	})

	t.Run("非法来源被拒绝", func(t *testing.T) {
		in := base
		in.AllowedOrigins = []string{"partner.example.com"}
		if _, err := validateAPIKeyInput(in); !errors.Is(err, errs.ErrBadRequest) {
			t.Fatalf("err = %v，期望 ErrBadRequest", err)
		}
	})

	t.Run("越界的每日上限被拒绝", func(t *testing.T) {
		for _, limit := range []int32{-1, 100001} {
			in := base
			in.DailyLimit = limit
			if _, err := validateAPIKeyInput(in); !errors.Is(err, errs.ErrBadRequest) {
				t.Errorf("DailyLimit=%d：err = %v，期望 ErrBadRequest", limit, err)
			}
		}
	})

	t.Run("必须选项目", func(t *testing.T) {
		in := base
		in.ProjectID = 0
		if _, err := validateAPIKeyInput(in); !errors.Is(err, errs.ErrBadRequest) {
			t.Fatalf("err = %v，期望 ErrBadRequest", err)
		}
	})
}

// 创建的 Key 只在响应里出现一次：服务返回明文，库里只拿到哈希，提示串取末 6 位。
func TestIntakeCreateAPIKey(t *testing.T) {
	repo := newStubKeyRepo()
	s := NewIntakeService(repo, newQuotaCounter())
	key, raw, err := s.CreateAPIKey(context.Background(), APIKeyInput{ProjectID: 1, Name: "合作站点 A"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(raw, apiKeyPrefix) {
		t.Fatalf("明文 = %q，期望以 %q 开头", raw, apiKeyPrefix)
	}
	if key.KeyHint != raw[len(raw)-6:] {
		t.Fatalf("提示串 = %q，期望明文末 6 位", key.KeyHint)
	}
	if strings.Contains(fmt.Sprintf("%+v", key), raw) {
		t.Fatal("Key 实体里不应包含明文")
	}
}

func TestNewAPIKeyAndHash(t *testing.T) {
	a, err := NewAPIKey()
	if err != nil {
		t.Fatal(err)
	}
	b, err := NewAPIKey()
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Fatal("两次生成的 Key 不应相同")
	}
	if !strings.HasPrefix(a, apiKeyPrefix) {
		t.Fatalf("Key = %q，缺少前缀 %q", a, apiKeyPrefix)
	}
	// 24 字节随机数的 base64url（无填充）是 32 个字符
	if got := len(a) - len(apiKeyPrefix); got != 32 {
		t.Fatalf("随机部分长度 = %d，期望 32", got)
	}

	// 哈希要确定，且忽略首尾空白（请求头常被带上空格）
	if HashAPIKey(a) != HashAPIKey("  "+a+"\t") {
		t.Fatal("哈希应忽略首尾空白")
	}
	if HashAPIKey(a) == HashAPIKey(b) {
		t.Fatal("不同 Key 的哈希不应相同")
	}
	if strings.Contains(HashAPIKey(a), a) {
		t.Fatal("哈希里不应包含明文")
	}
}

func TestAPIKeyUsable(t *testing.T) {
	revoked := fixedNow
	tests := []struct {
		name   string
		key    APIKey
		expect bool
	}{
		{"启用中", APIKey{Status: "active"}, true},
		{"停用", APIKey{Status: "inactive"}, false},
		{"已作废", APIKey{Status: "active", RevokedAt: &revoked}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.key.Usable(); got != tt.expect {
				t.Fatalf("Usable = %v，期望 %v", got, tt.expect)
			}
		})
	}
}
