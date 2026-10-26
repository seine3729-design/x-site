package pg

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"xsite/service/internal/domain/storage"
	"xsite/service/internal/errs"
)

// 线路与地区规则（S1-6b）走真实 PostgreSQL：线路 CRUD、规则整体替换、按线路/按存储源查规则、级联删除与表约束。
func TestStorageRepo_LineCRUD(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	mark := fmt.Sprintf("it%d", time.Now().UnixNano())
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM storage_line WHERE provider_id IN (SELECT id FROM storage_provider WHERE name LIKE $1)`, "%"+mark)
		_, _ = pool.Exec(context.Background(), `DELETE FROM storage_provider WHERE name LIKE $1`, "%"+mark)
	})
	repo := NewStorageRepo(pool)

	prov, err := repo.Create(ctx, storage.Row{
		Name: "线路测试存储源" + mark, Type: storage.TypeS3, Purpose: storage.PurposeBoth, Role: storage.RolePrimary,
		Region: "us-east-1", Bucket: "xsite-media", MaxPresignTTL: 600, Status: storage.StatusActive,
	})
	if err != nil {
		t.Fatal(err)
	}

	line, err := repo.CreateLine(ctx, storage.LineInput{
		ProviderID: prov.ID, Kind: storage.LineVideo, Name: "线路 1 · 电信优化", BaseURL: "https://cdn.example.com",
		AccessMode: storage.AccessCDN, SignType: storage.SignHMACToken, TokenTTL: 600, Priority: 10, Weight: 100,
		RegionScope: storage.ScopeRules, VideoProcess: storage.ProcessStandard, ProbePath: "/_probe/256k.bin", Status: storage.LineActive,
	})
	if err != nil {
		t.Fatal(err)
	}
	if line.ID == 0 || line.ProviderID != prov.ID || line.CreatedAt.Location() != time.UTC {
		t.Fatalf("新建线路结果不符合预期：%+v", line)
	}

	// 列表（按存储源）
	lines, err := repo.ListLines(ctx, storage.LineQuery{ProviderID: prov.ID})
	if err != nil || len(lines) != 1 || lines[0].ID != line.ID {
		t.Fatalf("按存储源列线路：%v %v", lines, err)
	}

	// 整体替换地区规则
	rules, err := repo.ReplaceLineRules(ctx, line.ID, []storage.RuleInput{
		{Country: "CN", Province: "广东", ISP: "电信", Effect: storage.EffectInclude, Boost: 50},
		{Country: "CN", Province: "*", ISP: "移动", Effect: storage.EffectExclude, Boost: 0},
	})
	if err != nil || len(rules) != 2 {
		t.Fatalf("替换规则应返回 2 条：%v %v", rules, err)
	}
	byLine, err := repo.ListRulesByLine(ctx, line.ID)
	if err != nil || len(byLine) != 2 {
		t.Fatalf("按线路查规则应 2 条：%v %v", byLine, err)
	}
	byProvider, err := repo.ListRulesByProvider(ctx, prov.ID)
	if err != nil || len(byProvider) != 2 {
		t.Fatalf("按存储源查规则应 2 条：%v %v", byProvider, err)
	}
	// 再次替换为 1 条（验证整体替换：旧的被清掉）
	if rules, err = repo.ReplaceLineRules(ctx, line.ID, []storage.RuleInput{{Country: "HK", Province: "*", ISP: "*", Effect: storage.EffectInclude, Boost: 0}}); err != nil || len(rules) != 1 {
		t.Fatalf("再次替换应为 1 条：%v %v", rules, err)
	}
	if byLine, _ = repo.ListRulesByLine(ctx, line.ID); len(byLine) != 1 || byLine[0].Country != "HK" {
		t.Fatalf("整体替换后应只剩 HK：%v", byLine)
	}

	// 整体保存线路
	after, err := repo.UpdateLine(ctx, line.ID, storage.LineInput{
		Kind: storage.LineVideo, Name: "线路 1（改）", BaseURL: "https://cdn2.example.com", AccessMode: storage.AccessCDN,
		SignType: storage.SignHMACToken, TokenTTL: 1200, Priority: 20, Weight: 50, RegionScope: storage.ScopeGlobal,
		VideoProcess: storage.ProcessStandard, ProbePath: "/_probe/256k.bin", Status: storage.LineWatch,
	})
	if err != nil || after.Name != "线路 1（改）" || after.Priority != 20 || after.Status != storage.LineWatch {
		t.Fatalf("保存线路结果不符合预期：%+v %v", after, err)
	}

	// 有线路时不能删除存储源（外键 RESTRICT）
	if _, err := repo.Delete(ctx, prov.ID); err == nil {
		t.Fatal("有线路引用时删除存储源应失败")
	}

	// 删除线路级联删除规则
	if _, err := repo.DeleteLine(ctx, line.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.GetLine(ctx, line.ID); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("删除后应 404，得到 %v", err)
	}
	if byLine, _ = repo.ListRulesByLine(ctx, line.ID); len(byLine) != 0 {
		t.Fatalf("删除线路应级联删除规则，仍有 %d 条", len(byLine))
	}

	// 表约束兜底
	for _, sql := range []string{
		`INSERT INTO storage_line (provider_id, kind, access_mode, sign_type) VALUES ($1, 'audio', 'cdn', 'none')`,
		`INSERT INTO storage_line (provider_id, kind, access_mode, sign_type) VALUES ($1, 'video', 'ftp', 'none')`,
		`INSERT INTO storage_line (provider_id, kind, access_mode, sign_type) VALUES ($1, 'video', 'cdn', 'bad')`,
		`INSERT INTO storage_line (provider_id, kind, access_mode, sign_type, weight) VALUES ($1, 'video', 'cdn', 'none', 0)`,
	} {
		if _, err := pool.Exec(ctx, sql, prov.ID); pgCode(err) != "23514" {
			t.Errorf("应违反 CHECK 约束（23514），得到 %v\n%s", err, sql)
		}
	}
}
