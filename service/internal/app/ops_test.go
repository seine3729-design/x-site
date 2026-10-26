package app

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"xsite/service/internal/domain/platform"
)

type fakeSiteConfig struct {
	features string
	err      error
}

func (f fakeSiteConfig) Get(context.Context) (platform.SiteConfig, error) {
	return platform.SiteConfig{Features: json.RawMessage(f.features)}, f.err
}

func TestSiteFeatures(t *testing.T) {
	ctx := context.Background()
	for features, want := range map[string]bool{
		`{"ranks": true, "comments": false}`: true,
		`{"ranks": false}`:                   false,
		`{"comments": true}`:                 true, // 没设置的开关视为打开
		``:                                   true,
	} {
		got, err := siteFeatures{fakeSiteConfig{features: features}}.Enabled(ctx, "ranks")
		if err != nil || got != want {
			t.Errorf("features=%q：期望 %v，得到 %v %v", features, want, got, err)
		}
	}
	if _, err := (siteFeatures{fakeSiteConfig{features: `[1]`}}).Enabled(ctx, "ranks"); err == nil {
		t.Error("功能开关格式不对时报错")
	}
	if _, err := (siteFeatures{fakeSiteConfig{err: errors.New("db down")}}).Enabled(ctx, "ranks"); err == nil {
		t.Error("读不到站点配置时报错")
	}
}
