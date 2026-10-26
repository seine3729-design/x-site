package pg

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"xsite/service/internal/domain/resource"
	"xsite/service/internal/errs"
)

// 资源机（S1-6）走真实 PostgreSQL：领域服务 + 仓储，覆盖搜索、筛选、分页、整体保存与表约束。
func TestResourceRepo_FTPServerCRUD(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	mark := fmt.Sprintf("it%d", time.Now().UnixNano())
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM ftp_server WHERE host LIKE $1`, mark+"%")
	})
	repo := NewResourceRepo(pool)
	svc := resource.NewService(repo, nil, resource.Policy{ExplicitTLSOnly: true})

	hk, err := svc.CreateFTPServer(ctx, resource.FTPServerInput{Name: "香港 50%_资源机" + mark, Host: mark + "-hk.example.com", Passive: true, CredRef: "hk1"})
	if err != nil {
		t.Fatal(err)
	}
	if hk.ID == 0 || hk.Port != 21 || hk.TLS != resource.TLSExplicit || !hk.Passive || hk.CredRef != "HK1" ||
		hk.Status != resource.StatusActive || hk.CreatedAt.IsZero() || hk.CreatedAt.Location() != time.UTC {
		t.Fatalf("新建结果不符合预期：%+v", hk)
	}
	jp, err := svc.CreateFTPServer(ctx, resource.FTPServerInput{
		Name: "日本资源机" + mark, Host: mark + "-JP.example.com", Port: 2121, Passive: true, Status: resource.StatusInactive,
	})
	if err != nil {
		t.Fatal(err)
	}

	ids := func(f resource.Filter) ([]int64, int64) {
		t.Helper()
		p, err := svc.ListFTPServers(ctx, f)
		if err != nil {
			t.Fatal(err)
		}
		out := make([]int64, len(p.List))
		for i, s := range p.List {
			out[i] = s.ID
		}
		return out, p.Total
	}
	if got, total := ids(resource.Filter{Q: mark}); len(got) != 2 || got[0] != jp.ID || got[1] != hk.ID || total != 2 {
		t.Fatalf("按标记搜索应命中两台、按编号倒序：%v %d", got, total)
	}
	// 地址存小写，搜索不区分大小写；% 与 _ 按字面匹配
	if got, _ := ids(resource.Filter{Q: mark + "-jp.EXAMPLE"}); len(got) != 1 || got[0] != jp.ID {
		t.Fatalf("按地址搜索（不区分大小写）：%v", got)
	}
	if got, _ := ids(resource.Filter{Q: "50%_资源机" + mark}); len(got) != 1 || got[0] != hk.ID {
		t.Fatalf("%% 与 _ 应按字面匹配：%v", got)
	}
	if got, _ := ids(resource.Filter{Q: "50%资源机" + mark}); len(got) != 0 {
		t.Fatalf("%% 不是通配符：%v", got)
	}
	if got, total := ids(resource.Filter{Q: mark, Status: resource.StatusInactive}); len(got) != 1 || got[0] != jp.ID || total != 1 {
		t.Fatalf("按状态筛选：%v %d", got, total)
	}
	if got, total := ids(resource.Filter{Q: mark, Page: 2, PageSize: 1}); len(got) != 1 || got[0] != hk.ID || total != 2 {
		t.Fatalf("第 2 页（每页 1 条）应是较早的那台，总数仍为 2：%v %d", got, total)
	}

	// 整体保存：凭据标识留空保持原值，未传的端口回到默认值；updated_at 刷新
	time.Sleep(10 * time.Millisecond)
	before, after, err := svc.UpdateFTPServer(ctx, jp.ID, resource.FTPServerInput{Name: "日本资源机（新）" + mark, Host: jp.Host, Passive: true})
	if err != nil {
		t.Fatal(err)
	}
	if before.Port != 2121 || after.Port != 21 || after.CredRef != jp.CredRef || after.Status != resource.StatusActive ||
		!after.UpdatedAt.After(before.UpdatedAt) || !after.CreatedAt.Equal(before.CreatedAt) {
		t.Fatalf("保存结果不符合预期：before=%+v after=%+v", before, after)
	}
	if got, err := repo.GetFTPServer(ctx, jp.ID); err != nil || got.Name != "日本资源机（新）"+mark {
		t.Fatalf("应已写入：%+v %v", got, err)
	}

	// 删除不进回收站
	if del, err := svc.DeleteFTPServer(ctx, hk.ID); err != nil || del.ID != hk.ID {
		t.Fatalf("删除应返回删除前的记录：%+v %v", del, err)
	}
	if _, err := repo.GetFTPServer(ctx, hk.ID); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("删除后应 404，得到 %v", err)
	}
	if _, _, err := svc.UpdateFTPServer(ctx, hk.ID, resource.FTPServerInput{Name: "x", Host: "x.example.com", Passive: true}); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("修改已删除的资源机应 404，得到 %v", err)
	}
	if _, err := repo.UpdateFTPServer(ctx, hk.ID, resource.FTPServerInput{Name: "x", Host: "x", Port: 21, TLS: "explicit", Passive: true, CredRef: "X", Status: "active"}); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("仓储修改不存在的记录应 404，得到 %v", err)
	}
	if _, err := svc.DeleteFTPServer(ctx, hk.ID); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("再删应 404，得到 %v", err)
	}

	// 表约束兜底：绕过领域层写入非法值会被拒绝
	for _, sql := range []string{
		`INSERT INTO ftp_server (name, host, cred_ref, tls) VALUES ('x', $1, 'HK', 'ssl')`,
		`INSERT INTO ftp_server (name, host, cred_ref) VALUES ('x', $1, 'hk-1')`,
		`INSERT INTO ftp_server (name, host, cred_ref, port) VALUES ('x', $1, 'HK', 0)`,
		`INSERT INTO ftp_server (name, host, cred_ref, status) VALUES ('x', $1, 'HK', 'deleted')`,
		`INSERT INTO ftp_server (name, host, cred_ref) VALUES ('', $1, 'HK')`,
	} {
		if _, err := pool.Exec(ctx, sql, mark+"-bad.example.com"); pgCode(err) != "23514" {
			t.Errorf("应违反 CHECK 约束（23514），得到 %v\n%s", err, sql)
		}
	}
}
