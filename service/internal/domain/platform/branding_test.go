package platform

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"xsite/service/internal/errs"
)

// 白标三段（品牌、配色、布局）的领域层测试：默认值、枚举白名单、颜色与地址校验、保存后刷新站点版本。

type fakeBrandingRepo struct {
	site, theme, layout json.RawMessage
	writes              []string // 记录写过哪几段，确认「只保存这一段」
	err                 error
}

func (r *fakeBrandingRepo) GetBranding(context.Context) (json.RawMessage, json.RawMessage, json.RawMessage, error) {
	return r.site, r.theme, r.layout, r.err
}

func (r *fakeBrandingRepo) SetBrand(_ context.Context, v json.RawMessage) error {
	r.site, r.writes = v, append(r.writes, "site")
	return r.err
}

func (r *fakeBrandingRepo) SetTheme(_ context.Context, v json.RawMessage) error {
	r.theme, r.writes = v, append(r.writes, "theme")
	return r.err
}

func (r *fakeBrandingRepo) SetLayout(_ context.Context, v json.RawMessage) error {
	r.layout, r.writes = v, append(r.writes, "layout")
	return r.err
}

// fakeSiteManager 记录 BumpVersion 的调用次数。
type fakeSiteManager struct {
	bumps int
	err   error
}

func (m *fakeSiteManager) Get(context.Context) (SiteConfig, error) { return SiteConfig{}, nil }

func (m *fakeSiteManager) BumpVersion(context.Context) (int64, error) {
	m.bumps++
	return int64(m.bumps), m.err
}

func newBrandingSvc() (*BrandingService, *fakeBrandingRepo, *fakeSiteManager) {
	repo, site := &fakeBrandingRepo{}, &fakeSiteManager{}
	return NewBrandingService(repo, site), repo, site
}

func okBrand() Brand { return Brand{SiteName: "短剧站"} }

func TestBrandingGetDefaults(t *testing.T) {
	svc, _, _ := newBrandingSvc()
	cfg, err := svc.Get(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// 没保存过的段按默认值返回，页面上不会出现空下拉
	if cfg.Theme.Preset != defaultPreset || cfg.Theme.ColorScheme != "dark" || cfg.Theme.Radius != defaultRadius {
		t.Fatalf("配色默认值：%+v", cfg.Theme)
	}
	if cfg.Theme.CoverRatio != "2:3" || cfg.Theme.Font != "system" {
		t.Fatalf("封面比例与字体默认值：%+v", cfg.Theme)
	}
	// 没保存过时「允许切换深浅色」按开算：前台外壳本来就带这个开关，与前台 normalizeTheme 的默认值一致
	if !cfg.Theme.AllowSchemeToggle {
		t.Fatalf("允许切换深浅色默认应为开：%+v", cfg.Theme)
	}
	if cfg.Layout.HeaderStyle != "classic" || cfg.Layout.CardStyle != "below" || cfg.Layout.PlayMode != "mobile_immersive" || !cfg.Layout.MobileTabbar {
		t.Fatalf("布局默认值：%+v", cfg.Layout)
	}
	if cfg.Theme.Colors == nil {
		t.Fatal("颜色应是空对象而不是 null")
	}
	// 下拉可选值随配置一起给前端，避免前端抄一份枚举
	if len(cfg.Options.Presets) == 0 || len(cfg.Options.PlayModes) == 0 || cfg.Options.MaxRadius != maxRadiusPx {
		t.Fatalf("可选值清单：%+v", cfg.Options)
	}
}

func TestBrandingGetRejectsBrokenJSON(t *testing.T) {
	svc, repo, _ := newBrandingSvc()
	repo.theme = json.RawMessage(`{"preset":`)
	// 坏数据不能静默当成空配置：那样运营会以为自己的配置被清空了
	if _, err := svc.Get(context.Background()); err == nil {
		t.Fatal("坏 JSON 应报错")
	}
}

func TestSetBrand(t *testing.T) {
	svc, repo, site := newBrandingSvc()
	ctx := context.Background()

	cfg, err := svc.SetBrand(ctx, Brand{
		SiteName: "  短剧站  ", Slogan: " 每天追好剧 ", Copyright: "© 2026",
		SupportURL: "https://t.me/support", LogoLight: "img/logo.webp", Favicon: "img/favicon.png",
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Brand.SiteName != "短剧站" || cfg.Brand.Slogan != "每天追好剧" {
		t.Fatalf("去首尾空白：%+v", cfg.Brand)
	}
	if len(repo.writes) != 1 || repo.writes[0] != "site" {
		t.Fatalf("只写品牌段：%v", repo.writes)
	}
	if site.bumps != 1 {
		t.Fatalf("保存后要刷新站点版本：%d", site.bumps)
	}

	bad := []struct {
		in  Brand
		msg string
	}{
		{Brand{}, "请填写站名"},
		{Brand{SiteName: "   "}, "请填写站名"},
		{Brand{SiteName: strings.Repeat("字", 31)}, "站名不能超过"},
		{Brand{SiteName: "站", Slogan: strings.Repeat("字", 61)}, "口号不能超过"},
		{Brand{SiteName: "站", Copyright: strings.Repeat("字", 101)}, "版权文字不能超过"},
		{Brand{SiteName: "站", SupportURL: "t.me/support"}, "客服链接要是完整的 http(s) 网址"},
		{Brand{SiteName: "站", SupportURL: "javascript:alert(1)"}, "客服链接要是完整的 http(s) 网址"},
		{Brand{SiteName: "站", LogoLight: "../../etc/passwd"}, "头部 Logo 要填上传后的图片路径"},
		{Brand{SiteName: "站", Favicon: "javascript:alert(1)"}, "favicon 要填上传后的图片路径"},
	}
	for _, c := range bad {
		_, err := svc.SetBrand(ctx, c.in)
		wantBadRequest(t, err, c.msg)
	}
	// 外链形式的 Logo 也允许（图片管线接入前先填外链）
	if _, err := svc.SetBrand(ctx, Brand{SiteName: "站", LogoLight: "https://cdn.example.com/logo.png"}); err != nil {
		t.Fatalf("完整地址的 Logo 应允许：%v", err)
	}
}

func TestSetTheme(t *testing.T) {
	svc, repo, site := newBrandingSvc()
	ctx := context.Background()

	cfg, err := svc.SetTheme(ctx, Theme{
		Preset: "midnight", ColorScheme: "light", AllowSchemeToggle: true,
		Colors: map[string]string{"primary": " #FF0033 ", "accent": "#abc", "bg": "  "},
		Radius: 8, CoverRatio: "16:9", Font: "serif",
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Theme.Preset != "midnight" || cfg.Theme.ColorScheme != "light" || !cfg.Theme.AllowSchemeToggle {
		t.Fatalf("配色保存：%+v", cfg.Theme)
	}
	// 色值去空白并转小写；留空的键不保存（表示用预设色）
	if cfg.Theme.Colors["primary"] != "#ff0033" || cfg.Theme.Colors["accent"] != "#abc" {
		t.Fatalf("色值规范化：%+v", cfg.Theme.Colors)
	}
	if _, ok := cfg.Theme.Colors["bg"]; ok {
		t.Fatalf("留空的颜色不应保存：%+v", cfg.Theme.Colors)
	}
	if len(repo.writes) != 1 || repo.writes[0] != "theme" || site.bumps != 1 {
		t.Fatalf("只写配色段并刷新版本：%v %d", repo.writes, site.bumps)
	}

	// 空值取默认
	if cfg, err = svc.SetTheme(ctx, Theme{}); err != nil {
		t.Fatal(err)
	}
	if cfg.Theme.Preset != defaultPreset || cfg.Theme.Radius != defaultRadius || cfg.Theme.CoverRatio != defaultCoverRatio {
		t.Fatalf("空值取默认：%+v", cfg.Theme)
	}

	bad := []struct {
		in  Theme
		msg string
	}{
		{Theme{Preset: "neon"}, "皮肤预设"},
		{Theme{ColorScheme: "auto"}, "默认深浅色"},
		{Theme{CoverRatio: "4:3"}, "封面比例"},
		{Theme{Font: "comic"}, "字体"},
		{Theme{Radius: 33}, "卡片圆角为 0–32 像素"},
		{Theme{Radius: -1}, "卡片圆角为 0–32 像素"},
		{Theme{Colors: map[string]string{"shadow": "#fff"}}, "颜色 \"shadow\" 不在可配置范围内"},
		{Theme{Colors: map[string]string{"primary": "red"}}, "要填 #RGB 或 #RRGGBB"},
		{Theme{Colors: map[string]string{"primary": "#12345"}}, "要填 #RGB 或 #RRGGBB"},
	}
	for _, c := range bad {
		_, err := svc.SetTheme(ctx, c.in)
		wantBadRequest(t, err, c.msg)
	}
}

func TestSetLayout(t *testing.T) {
	svc, repo, site := newBrandingSvc()
	ctx := context.Background()

	cfg, err := svc.SetLayout(ctx, Layout{HeaderStyle: "pill", CardStyle: "overlay", PlayMode: "all_immersive", MobileTabbar: false})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Layout.HeaderStyle != "pill" || cfg.Layout.CardStyle != "overlay" || cfg.Layout.PlayMode != "all_immersive" || cfg.Layout.MobileTabbar {
		t.Fatalf("布局保存：%+v", cfg.Layout)
	}
	if len(repo.writes) != 1 || repo.writes[0] != "layout" || site.bumps != 1 {
		t.Fatalf("只写布局段并刷新版本：%v %d", repo.writes, site.bumps)
	}
	for _, c := range []struct {
		in  Layout
		msg string
	}{
		{Layout{HeaderStyle: "sticky"}, "页头样式"},
		{Layout{CardStyle: "side"}, "卡片样式"},
		{Layout{PlayMode: "theater"}, "播放形态"},
	} {
		_, err := svc.SetLayout(ctx, c.in)
		wantBadRequest(t, err, c.msg)
	}
}

func TestBrandingBumpFailureIsReported(t *testing.T) {
	svc, _, site := newBrandingSvc()
	site.err = errors.New("valkey 不可用")
	// 已经写进库了，但前台可能要等 TTL 才生效：要让运营知道，而不是报「保存成功」
	_, err := svc.SetBrand(context.Background(), okBrand())
	if err == nil || !strings.Contains(err.Error(), "刷新站点版本失败") {
		t.Fatalf("刷新版本失败要报出来：%v", err)
	}
}

func wantBadRequest(t *testing.T, err error, contains string) {
	t.Helper()
	if errs.From(err).Code != errs.ErrBadRequest.Code {
		t.Errorf("期望 400，得到 %v", err)
		return
	}
	if !strings.Contains(errs.From(err).Message, contains) {
		t.Errorf("提示 %q 不含 %q", errs.From(err).Message, contains)
	}
}
