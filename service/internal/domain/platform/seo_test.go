package platform

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// SEO 与统计代码的领域层测试：默认值、页面与变量白名单、站长验证、统计代码只校长度。

type fakeSEORepo struct {
	seo, analytics json.RawMessage
	writes         []string
	err            error
}

func (r *fakeSEORepo) GetSEOConfig(context.Context) (json.RawMessage, json.RawMessage, error) {
	return r.seo, r.analytics, r.err
}

func (r *fakeSEORepo) SetSEO(_ context.Context, v json.RawMessage) error {
	r.seo, r.writes = v, append(r.writes, "seo")
	return r.err
}

func (r *fakeSEORepo) SetAnalytics(_ context.Context, v json.RawMessage) error {
	r.analytics, r.writes = v, append(r.writes, "analytics")
	return r.err
}

func newSEOSvc() (*SEOService, *fakeSEORepo, *fakeSiteManager) {
	repo, site := &fakeSEORepo{}, &fakeSiteManager{}
	return NewSEOService(repo, site), repo, site
}

func TestSEOGetDefaults(t *testing.T) {
	svc, _, _ := newSEOSvc()
	cfg, err := svc.Get(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// 新站点默认允许收录：默认不收录会让站点白白失去搜索流量
	if !cfg.SEO.RobotsAllow || !cfg.SEO.Sitemap || !cfg.SEO.StructuredData {
		t.Fatalf("默认值：%+v", cfg.SEO)
	}
	if cfg.SEO.TitleTemplates == nil || cfg.SEO.DescTemplates == nil || cfg.SEO.Verifications == nil {
		t.Fatalf("空集合要输出 {} / [] 而不是 null：%#v", cfg.SEO)
	}
	if len(cfg.Pages) != 9 || len(cfg.Variables) != 12 || cfg.MaxTemplate != maxSEOTemplate {
		t.Fatalf("页面与变量清单随配置下发：%d 页 %d 变量", len(cfg.Pages), len(cfg.Variables))
	}
}

func TestSetSEO(t *testing.T) {
	svc, repo, site := newSEOSvc()
	ctx := context.Background()

	cfg, err := svc.Set(ctx, SEO{
		TitleTemplates: map[string]string{
			"home":  " {site_name} - {slogan} ",
			"drama": "{drama_title}（全{total_episodes}集）- {site_name}",
			"play":  "", // 留空表示用前台默认
		},
		DescTemplates: map[string]string{"drama": "{summary}"},
		OGImage:       "img/og.webp",
		RobotsAllow:   true,
		RobotsExtra:   " Disallow: /admin ",
		Verifications: []Verification{
			{Name: "google-site-verification", Content: " abc123 "},
			{Name: "  ", Content: "  "}, // 整行留空的忽略
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.SEO.TitleTemplates["home"] != "{site_name} - {slogan}" {
		t.Fatalf("模板去首尾空白：%q", cfg.SEO.TitleTemplates["home"])
	}
	if _, ok := cfg.SEO.TitleTemplates["play"]; ok {
		t.Fatalf("留空的模板不保存：%+v", cfg.SEO.TitleTemplates)
	}
	if len(cfg.SEO.Verifications) != 1 || cfg.SEO.Verifications[0].Content != "abc123" {
		t.Fatalf("站长验证：%+v", cfg.SEO.Verifications)
	}
	if cfg.SEO.RobotsExtra != "Disallow: /admin" {
		t.Fatalf("robots 追加去空白：%q", cfg.SEO.RobotsExtra)
	}
	if len(repo.writes) != 1 || repo.writes[0] != "seo" || site.bumps != 1 {
		t.Fatalf("只写 seo 段并刷新版本：%v %d", repo.writes, site.bumps)
	}

	bad := []struct {
		in  SEO
		msg string
	}{
		{SEO{TitleTemplates: map[string]string{"about": "x"}}, `页面 "about" 不存在`},
		{SEO{TitleTemplates: map[string]string{"home": "{dramaTitle}"}}, "未知变量 {dramaTitle}"},
		{SEO{DescTemplates: map[string]string{"home": "{ 站名 }"}}, "未知变量"},
		{SEO{TitleTemplates: map[string]string{"home": strings.Repeat("字", 121)}}, "不能超过 120 个字"},
		{SEO{OGImage: "../../etc/passwd"}, "分享图 要填上传后的图片路径"},
		{SEO{RobotsExtra: strings.Repeat("x", 2001)}, "robots.txt 追加内容不能超过"},
		{SEO{Verifications: []Verification{{Name: "google", Content: ""}}}, "名称与内容都要填"},
		{SEO{Verifications: []Verification{{Name: "坏 名称", Content: "x"}}}, "只能用字母、数字"},
		{SEO{Verifications: []Verification{{Name: "a", Content: "x"}, {Name: "A", Content: "y"}}}, "重复了"},
		{SEO{Verifications: make([]Verification, 11)}, "最多 10 组"},
	}
	for _, c := range bad {
		_, err := svc.Set(ctx, c.in)
		wantBadRequest(t, err, c.msg)
	}
}

func TestSEOGetRejectsBrokenJSON(t *testing.T) {
	svc, repo, _ := newSEOSvc()
	repo.seo = json.RawMessage(`{"robots_allow":`)
	if _, err := svc.Get(context.Background()); err == nil {
		t.Fatal("坏 JSON 应报错而不是静默当空配置")
	}
}

func TestAnalytics(t *testing.T) {
	svc, repo, site := newSEOSvc()
	ctx := context.Background()

	// 统计代码原样保存：清洗掉 <script> 这段配置就没有意义了
	snippet := `<script async src="https://example.com/a.js"></script><script>window.x=1</script>`
	out, err := svc.SetAnalytics(ctx, Analytics{HeadSnippet: "  " + snippet + "  ", BodySnippet: `<noscript><img src="https://e.com/p.gif"></noscript>`})
	if err != nil {
		t.Fatal(err)
	}
	if out.HeadSnippet != snippet {
		t.Fatalf("统计代码应原样保存（只去首尾空白）：%q", out.HeadSnippet)
	}
	if !strings.Contains(out.BodySnippet, "noscript") {
		t.Fatalf("body 代码：%q", out.BodySnippet)
	}
	if len(repo.writes) != 1 || repo.writes[0] != "analytics" || site.bumps != 1 {
		t.Fatalf("只写 analytics 段并刷新版本：%v %d", repo.writes, site.bumps)
	}

	// 超长拒绝
	_, err = svc.SetAnalytics(ctx, Analytics{HeadSnippet: strings.Repeat("x", maxAnalyticsLength+1)})
	wantBadRequest(t, err, "head 统计代码不能超过")
	_, err = svc.SetAnalytics(ctx, Analytics{BodySnippet: strings.Repeat("x", maxAnalyticsLength+1)})
	wantBadRequest(t, err, "body 统计代码不能超过")

	// 读回来
	got, err := svc.Analytics(ctx)
	if err != nil || got.HeadSnippet != snippet {
		t.Fatalf("读回统计代码：%+v %v", got, err)
	}
}

// TestSEOAndAnalyticsAreSeparateSegments 守住「保存 SEO 不会动统计代码」：
// 两段在同一行记录里，共用一条 UPDATE 的话改 SEO 会把统计代码清掉。
func TestSEOAndAnalyticsAreSeparateSegments(t *testing.T) {
	svc, repo, _ := newSEOSvc()
	ctx := context.Background()
	if _, err := svc.SetAnalytics(ctx, Analytics{HeadSnippet: "<script>a</script>"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Set(ctx, SEO{RobotsAllow: true}); err != nil {
		t.Fatal(err)
	}
	got, err := svc.Analytics(ctx)
	if err != nil || got.HeadSnippet != "<script>a</script>" {
		t.Fatalf("保存 SEO 不应影响统计代码：%+v %v", got, err)
	}
	if strings.Join(repo.writes, ",") != "analytics,seo" {
		t.Fatalf("两段分别写入：%v", repo.writes)
	}
}
