package feedback

import (
	"errors"
	"strings"
	"testing"
	"time"

	"xsite/service/internal/errs"
)

var testNow = time.Date(2026, 10, 23, 8, 0, 0, 0, time.UTC)

// 状态机是分诊的核心规则：非法流转必须被拒，否则运营能把「已收到」直接改成「已上线」。
func TestCanTransition(t *testing.T) {
	tests := []struct {
		name string
		from Status
		to   Status
		want bool
	}{
		{"同状态允许（只改优先级）", StatusTriaged, StatusTriaged, true},
		{"收到后确认", StatusSubmitted, StatusTriaged, true},
		{"收到后直接上线不允许", StatusSubmitted, StatusShipped, false},
		{"收到后直接开发不允许", StatusSubmitted, StatusInProgress, false},
		{"确认后进入计划", StatusTriaged, StatusPlanned, true},
		{"计划后开发", StatusPlanned, StatusInProgress, true},
		{"开发后等待验证", StatusInProgress, StatusVerifying, true},
		{"验证后上线", StatusVerifying, StatusShipped, true},
		{"上线后关闭", StatusShipped, StatusClosed, true},
		{"上线后可因新证据重开", StatusShipped, StatusTriaged, true},
		{"已拒绝可重开", StatusDeclined, StatusTriaged, true},
		{"重复可重开", StatusDuplicate, StatusTriaged, true},
		{"关闭后不能直接上线", StatusClosed, StatusShipped, false},
		{"验证后不能回到收到", StatusVerifying, StatusSubmitted, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CanTransition(tt.from, tt.to); got != tt.want {
				t.Fatalf("CanTransition(%s, %s) = %v，期望 %v", tt.from, tt.to, got, tt.want)
			}
		})
	}
}

func TestValidateSubmit(t *testing.T) {
	base := SubmitInput{Kind: KindFeature, Title: "希望支持夜间模式", Body: "晚上看着太亮了"}

	t.Run("默认可见性为公开", func(t *testing.T) {
		got, err := validateSubmit(base)
		if err != nil {
			t.Fatalf("不该出错：%v", err)
		}
		if got.Visibility != VisibilityPublic {
			t.Fatalf("可见性 = %q，期望 public", got.Visibility)
		}
	})

	t.Run("标题过短被拒", func(t *testing.T) {
		in := base
		in.Title = "卡"
		if _, err := validateSubmit(in); !errors.Is(err, errs.ErrBadRequest) {
			t.Fatalf("期望 400，得到 %v", err)
		}
	})

	t.Run("类型不合法被拒", func(t *testing.T) {
		in := base
		in.Kind = "whatever"
		if _, err := validateSubmit(in); !errors.Is(err, errs.ErrBadRequest) {
			t.Fatalf("期望 400，得到 %v", err)
		}
	})

	t.Run("Bug 正文过短被拒", func(t *testing.T) {
		in := base
		in.Kind = KindBug
		in.Body = "打不开"
		if _, err := validateSubmit(in); !errors.Is(err, errs.ErrBadRequest) {
			t.Fatalf("期望 400，得到 %v", err)
		}
	})

	t.Run("标题里的控制字符被清掉", func(t *testing.T) {
		in := base
		in.Title = "希望支持\u0000夜间\t模式  啊"
		got, err := validateSubmit(in)
		if err != nil {
			t.Fatalf("不该出错：%v", err)
		}
		if strings.ContainsRune(got.Title, '\u0000') || strings.Contains(got.Title, "  ") {
			t.Fatalf("标题未规范化：%q", got.Title)
		}
	})

	t.Run("正文保留换行", func(t *testing.T) {
		in := base
		in.Kind = KindBug
		in.Body = "步骤：\r\n1 打开首页\r\n2 点提交\n实际：没反应"
		got, err := validateSubmit(in)
		if err != nil {
			t.Fatalf("不该出错：%v", err)
		}
		if strings.Contains(got.Body, "\r") || !strings.Contains(got.Body, "\n1 打开首页") {
			t.Fatalf("正文换行未规范化：%q", got.Body)
		}
	})
}

func TestValidateProjectInput(t *testing.T) {
	base := ProjectInput{Slug: "maomi-app", KeyPrefix: "MM", Name: "猫咪短剧"}

	t.Run("补齐默认值", func(t *testing.T) {
		got, err := validateProjectInput(base)
		if err != nil {
			t.Fatalf("不该出错：%v", err)
		}
		if got.Status != ProjectDraft || got.Visibility != VisibilityPublic || got.JoinMode != JoinInviteOnly {
			t.Fatalf("默认值不对：%+v", got)
		}
	})

	t.Run("标识大写会被转小写", func(t *testing.T) {
		in := base
		in.Slug = "  MaoMi-App  "
		got, err := validateProjectInput(in)
		if err != nil {
			t.Fatalf("不该出错：%v", err)
		}
		if got.Slug != "maomi-app" {
			t.Fatalf("slug = %q", got.Slug)
		}
	})

	t.Run("编号前缀必须大写字母开头", func(t *testing.T) {
		in := base
		in.KeyPrefix = "1a"
		if _, err := validateProjectInput(in); !errors.Is(err, errs.ErrBadRequest) {
			t.Fatalf("期望 400，得到 %v", err)
		}
	})

	t.Run("去试用地址只允许 https", func(t *testing.T) {
		in := base
		in.TryURL = "http://example.com"
		if _, err := validateProjectInput(in); !errors.Is(err, errs.ErrBadRequest) {
			t.Fatalf("期望 400，得到 %v", err)
		}
		in.TryURL = "https://example.com/app"
		if _, err := validateProjectInput(in); err != nil {
			t.Fatalf("https 不该被拒：%v", err)
		}
	})
}

func TestValidateTriage(t *testing.T) {
	cur := Feedback{ID: 7, ProjectID: 1, Status: StatusSubmitted, Priority: PriorityUnset, Visibility: VisibilityPublic, Version: 3}

	t.Run("version 不匹配返回冲突", func(t *testing.T) {
		_, err := validateTriage(TriageInput{Status: StatusTriaged, ExpectedVersion: 2}, cur)
		if !errors.Is(err, errs.ErrConflict) {
			t.Fatalf("期望 409，得到 %v", err)
		}
	})

	t.Run("非法流转被拒", func(t *testing.T) {
		_, err := validateTriage(TriageInput{Status: StatusShipped, ExpectedVersion: 3}, cur)
		if !errors.Is(err, errs.ErrBadRequest) {
			t.Fatalf("期望 400，得到 %v", err)
		}
	})

	t.Run("留空沿用原值", func(t *testing.T) {
		got, err := validateTriage(TriageInput{ExpectedVersion: 3}, cur)
		if err != nil {
			t.Fatalf("不该出错：%v", err)
		}
		if got.Status != cur.Status || got.Priority != cur.Priority || got.Visibility != cur.Visibility {
			t.Fatalf("未沿用原值：%+v", got)
		}
	})

	t.Run("标记重复必须给主反馈", func(t *testing.T) {
		_, err := validateTriage(TriageInput{Status: StatusDuplicate, ExpectedVersion: 3}, cur)
		if !errors.Is(err, errs.ErrBadRequest) {
			t.Fatalf("期望 400，得到 %v", err)
		}
	})

	t.Run("不能把反馈标记为自己的重复项", func(t *testing.T) {
		_, err := validateTriage(TriageInput{Status: StatusDuplicate, DuplicateOf: cur.ID, ExpectedVersion: 3}, cur)
		if !errors.Is(err, errs.ErrBadRequest) {
			t.Fatalf("期望 400，得到 %v", err)
		}
	})

	t.Run("非重复状态会清掉主反馈", func(t *testing.T) {
		got, err := validateTriage(TriageInput{Status: StatusTriaged, DuplicateOf: 99, ExpectedVersion: 3}, cur)
		if err != nil {
			t.Fatalf("不该出错：%v", err)
		}
		if got.DuplicateOf != 0 {
			t.Fatalf("duplicate_of 应被清零，实际 %d", got.DuplicateOf)
		}
	})

	t.Run("暂不采纳必须写公开原因", func(t *testing.T) {
		_, err := validateTriage(TriageInput{Status: StatusDeclined, ExpectedVersion: 3}, cur)
		if !errors.Is(err, errs.ErrBadRequest) {
			t.Fatalf("期望 400，得到 %v", err)
		}
		if _, err := validateTriage(TriageInput{Status: StatusDeclined, Note: "与产品方向不一致", ExpectedVersion: 3}, cur); err != nil {
			t.Fatalf("写了原因不该被拒：%v", err)
		}
	})
}

// 搜索词要转成转义后的 ILIKE 模式，否则 % 和 _ 会被当通配符，且不加 % 时变成精确匹配。
func TestCleanQuery(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"", ""},
		{"   ", ""},
		{"猫咪", "%猫咪%"},
		{"50%", `%50\%%`},
		{"a_b", `%a\_b%`},
		{`c\d`, `%c\\d%`},
	}
	for _, tt := range tests {
		if got := cleanQuery(tt.in); got != tt.want {
			t.Errorf("cleanQuery(%q) = %q，期望 %q", tt.in, got, tt.want)
		}
	}
	if got := cleanQuery(strings.Repeat("长", 100)); len([]rune(got)) != 62 {
		t.Errorf("超长搜索词未截断：%d 个字符", len([]rune(got)))
	}
}

func TestLimitOffset(t *testing.T) {
	t.Run("默认第一页", func(t *testing.T) {
		p, size, limit, offset, err := limitOffset(0, 0)
		if err != nil || p != 1 || size != defaultPageSize || limit != defaultPageSize || offset != 0 {
			t.Fatalf("得到 p=%d size=%d limit=%d offset=%d err=%v", p, size, limit, offset, err)
		}
	})
	t.Run("第三页偏移", func(t *testing.T) {
		_, _, _, offset, err := limitOffset(3, 10)
		if err != nil || offset != 20 {
			t.Fatalf("offset = %d, err = %v", offset, err)
		}
	})
	t.Run("越界被拒", func(t *testing.T) {
		if _, _, _, _, err := limitOffset(1, maxPageSize+1); err == nil {
			t.Fatal("每页条数超限应报错")
		}
		if _, _, _, _, err := limitOffset(-1, 10); err == nil {
			t.Fatal("负页码应报错")
		}
	})
}

func TestInviteUsable(t *testing.T) {
	expired := testNow.Add(-time.Hour)
	future := testNow.Add(time.Hour)
	revoked := testNow.Add(-time.Minute)

	tests := []struct {
		name   string
		inv    Invite
		wantOK bool
	}{
		{"正常可用", Invite{MaxUses: 3, UsedCount: 1, ExpiresAt: &future}, true},
		{"没有过期时间也可用", Invite{MaxUses: 1, UsedCount: 0}, true},
		{"已作废", Invite{MaxUses: 3, UsedCount: 0, RevokedAt: &revoked}, false},
		{"已过期", Invite{MaxUses: 3, UsedCount: 0, ExpiresAt: &expired}, false},
		{"次数用完", Invite{MaxUses: 2, UsedCount: 2}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ok, reason := tt.inv.Usable(testNow)
			if ok != tt.wantOK {
				t.Fatalf("Usable = %v（原因 %q），期望 %v", ok, reason, tt.wantOK)
			}
			if !ok && reason == "" {
				t.Fatal("不可用时必须给出原因")
			}
		})
	}
}

// 邀请码允许用户随意加连字符、空格与大小写。
func TestCodeNormalization(t *testing.T) {
	if a, b := HashInviteCode("abcd-efgh-ijkl"), HashInviteCode(" ABCDEFGHIJKL "); a != b {
		t.Fatalf("邀请码哈希应忽略大小写与分隔符：%s vs %s", a[:8], b[:8])
	}
}

func TestNewInviteCodeFormat(t *testing.T) {
	seen := map[string]bool{}
	for range 20 {
		code, err := NewInviteCode()
		if err != nil {
			t.Fatalf("生成邀请码失败：%v", err)
		}
		if len(code) != 14 || code[4] != '-' || code[9] != '-' {
			t.Fatalf("邀请码格式不对：%q", code)
		}
		if seen[code] {
			t.Fatalf("邀请码重复：%q", code)
		}
		seen[code] = true
	}
}

// 贡献分只在「确认」与「上线」时各记一次，且用 source_key 保证幂等。
func TestAwardFor(t *testing.T) {
	cur := Feedback{ID: 42, ProjectID: 3, AuthorID: 9, Status: StatusSubmitted}

	t.Run("确认记 1 分", func(t *testing.T) {
		a := awardFor(cur, TriageInput{Status: StatusTriaged})
		if a == nil || a.Points != 1 || a.SourceKey != "triaged:42" || a.UserID != 9 {
			t.Fatalf("得到 %+v", a)
		}
	})

	t.Run("重复确认不再记分", func(t *testing.T) {
		already := cur
		already.Status = StatusTriaged
		if a := awardFor(already, TriageInput{Status: StatusTriaged}); a != nil {
			t.Fatalf("同状态不该记分，得到 %+v", a)
		}
	})

	t.Run("上线记 4 分", func(t *testing.T) {
		verifying := cur
		verifying.Status = StatusVerifying
		a := awardFor(verifying, TriageInput{Status: StatusShipped})
		if a == nil || a.Points != 4 || a.SourceKey != "shipped:42" {
			t.Fatalf("得到 %+v", a)
		}
	})

	t.Run("作者已注销不记分", func(t *testing.T) {
		anon := cur
		anon.AuthorID = 0
		if a := awardFor(anon, TriageInput{Status: StatusTriaged}); a != nil {
			t.Fatalf("无作者不该记分，得到 %+v", a)
		}
	})
}

func TestValidateHandle(t *testing.T) {
	tests := []struct {
		in   string
		want bool
	}{
		{"maomi_user", true},
		{"ab", false},
		{"12345", false},
		{"admin", false},
		{"Official", false},
		{"用户名", false},
		{strings.Repeat("a", 33), false},
		{"a-b_c9", true},
	}
	for _, tt := range tests {
		_, err := validateHandle(tt.in)
		if (err == nil) != tt.want {
			t.Errorf("validateHandle(%q) err = %v，期望通过 = %v", tt.in, err, tt.want)
		}
	}
}

func TestSortKey(t *testing.T) {
	for _, s := range []string{"", "latest", "newest", "votes"} {
		if _, err := sortKey(s, false); err != nil {
			t.Errorf("sortKey(%q) 不该出错：%v", s, err)
		}
	}
	if _, err := sortKey("oldest", false); err == nil {
		t.Error("前台不支持 oldest，应报错")
	}
	if got, err := sortKey("oldest", true); err != nil || got != "oldest" {
		t.Errorf("后台应支持 oldest：%q %v", got, err)
	}
	if _, err := sortKey("'; DROP TABLE", true); err == nil {
		t.Error("排序字段必须走白名单")
	}
}

func TestMemberRoleIsTeam(t *testing.T) {
	if RoleMember.IsTeam() {
		t.Error("普通成员不属于项目团队")
	}
	for _, r := range []MemberRole{RoleOperator, RoleMaintainer, RoleOwner} {
		if !r.IsTeam() {
			t.Errorf("%s 应属于项目团队", r)
		}
	}
}

func TestProjectAcceptsFeedback(t *testing.T) {
	tests := map[ProjectStatus]bool{
		ProjectDraft: false, ProjectRecruiting: true, ProjectActive: true,
		ProjectPaused: false, ProjectArchived: false,
	}
	for st, want := range tests {
		if got := (Project{Status: st}).AcceptsFeedback(); got != want {
			t.Errorf("状态 %s 可提交 = %v，期望 %v", st, got, want)
		}
	}
}

func TestUserDisplayName(t *testing.T) {
	if got := (User{Handle: "maomi", Nickname: "小猫"}).DisplayName(); got != "小猫" {
		t.Errorf("有昵称时应显示昵称，得到 %q", got)
	}
	if got := (User{Handle: "maomi"}).DisplayName(); got != "maomi" {
		t.Errorf("无昵称时应回退用户名，得到 %q", got)
	}
}
