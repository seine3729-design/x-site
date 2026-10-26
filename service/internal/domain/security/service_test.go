package security

import (
	"context"
	"testing"
)

func TestParseKeyVersions(t *testing.T) {
	got := ParseKeyVersions("jwt_sign", "JWT", "k2:secretA, k1:secretB")
	if len(got) != 2 {
		t.Fatalf("应解析出 2 个版本，得 %d", len(got))
	}
	if got[0].Version != "k2" || !got[0].Primary {
		t.Errorf("第一个应为 k2 且 primary，得 %+v", got[0])
	}
	if got[1].Version != "k1" || got[1].Primary {
		t.Errorf("第二个应为 k1 且非 primary，得 %+v", got[1])
	}
	if got[0].Purpose != "jwt_sign" || got[0].PurposeTitle != "JWT" {
		t.Errorf("用途透传错误: %+v", got[0])
	}
	if ParseKeyVersions("x", "X", "") != nil {
		t.Error("空配置应返回 nil")
	}
	if ParseKeyVersions("x", "X", "   ") != nil {
		t.Error("空白配置应返回 nil")
	}
	if g := ParseKeyVersions("x", "X", "onlyid"); len(g) != 1 || g[0].Version != "onlyid" {
		t.Errorf("无冒号时整段作为 kid，得 %+v", g)
	}
}

type fakeLogRepo struct {
	list  []LoginLog
	total int64
	got   LoginLogFilter
}

func (f *fakeLogRepo) ListLoginLogs(_ context.Context, fil LoginLogFilter) ([]LoginLog, int64, error) {
	f.got = fil
	return f.list, f.total, nil
}

func TestLoginLogs(t *testing.T) {
	repo := &fakeLogRepo{list: []LoginLog{{Result: "bad_password"}, {Result: "success"}}, total: 2}
	svc := NewService(repo, nil)

	page, err := svc.LoginLogs(context.Background(), LoginLogFilter{Result: "bad_password", Q: "admin", PageSize: 0})
	if err != nil {
		t.Fatal(err)
	}
	if page.List[0].ResultTitle != "密码错误" || !page.List[0].Suspicious {
		t.Errorf("失败类应填中文名并标记可疑: %+v", page.List[0])
	}
	if page.List[1].Suspicious {
		t.Error("success 不应标记为可疑")
	}
	if repo.got.Result != "bad_password" {
		t.Errorf("合法 result 应透传，得 %q", repo.got.Result)
	}
	if repo.got.PageSize != 20 {
		t.Errorf("pageSize 为 0 应归默认 20，得 %d", repo.got.PageSize)
	}
	if repo.got.Pattern == "" {
		t.Error("有关键词时应填充 LIKE pattern")
	}
	if len(page.Results) != 5 {
		t.Errorf("结果下拉应有 5 项，得 %d", len(page.Results))
	}

	_, _ = svc.LoginLogs(context.Background(), LoginLogFilter{Result: "bogus"})
	if repo.got.Result != "" {
		t.Errorf("非法 result 应被清空，得 %q", repo.got.Result)
	}
}

func TestKeyRotationsAndRisk(t *testing.T) {
	svc := NewService(nil, []KeyVersion{{Purpose: "a", Version: "k1", Primary: true}})
	if got := svc.KeyRotations(context.Background()); len(got) != 1 || got[0].Version != "k1" {
		t.Errorf("密钥版本返回错误: %+v", got)
	}
	r := svc.Risk(context.Background())
	if len(r.Items) != 3 || r.Note == "" {
		t.Errorf("风控概览应有 3 项指标与说明，得 %+v", r)
	}
	for _, it := range r.Items {
		if it.Value != nil {
			t.Errorf("实时计数源未接入，Value 应为空: %+v", it)
		}
	}
}
