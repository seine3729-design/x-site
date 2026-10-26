package pg

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"
)

// 集成测试：连接本地 compose 的 PostgreSQL（./dev.sh go test 会注入 DATABASE_URL）；未配置时跳过。
func TestSiteConfigRepo_Integration(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("未设置 DATABASE_URL，跳过数据库集成测试")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pool, err := NewPool(ctx, url)
	if err != nil {
		t.Skipf("数据库不可用，跳过：%v", err)
	}
	defer pool.Close()

	if err := Migrate(ctx, pool); err != nil {
		t.Fatalf("迁移失败：%v", err)
	}

	cfg, err := NewSiteConfigRepo(pool).GetPublicSiteConfig(ctx)
	if err != nil {
		t.Fatalf("读取失败：%v", err)
	}
	if cfg.Version < 1 {
		t.Fatalf("version 应 >= 1，得到 %d", cfg.Version)
	}
	var features map[string]bool
	if err := json.Unmarshal(cfg.Features, &features); err != nil {
		t.Fatalf("features 不是合法 JSON：%v", err)
	}
	if _, ok := features["register"]; !ok {
		t.Fatalf("默认配置应包含 register 开关：%s", cfg.Features)
	}
	// 迁移 00010 删除演员、00014 删除资讯，对应的功能开关一并去掉
	for _, k := range []string{"actors", "articles"} {
		if _, ok := features[k]; ok {
			t.Fatalf("不应再有 %s 开关：%s", k, cfg.Features)
		}
	}
}
