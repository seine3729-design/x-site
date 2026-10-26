package platform

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"
)

// 白标的品牌、配色与布局（docs/06 第 16 节，S3-3 / A3-3，docs/specs/A3-3.md）。
// 三段都在 site_config 单行里，各自单独保存；保存后经 SiteConfigService.BumpVersion 让版本 +1
// 并广播失效，各端秒级生效。坏值一律 400 而不是静默兜底：配色或布局写错会让全站歪掉。

// Brand 是品牌信息（site 段）。
type Brand struct {
	SiteName string `json:"site_name"`
	Slogan   string `json:"slogan"`
	// Copyright 显示在页脚。
	Copyright string `json:"copyright"`
	// SupportURL 是在线客服链接（http(s) 完整地址）。
	SupportURL string `json:"support_url"`
	// LogoLight 是头部 Logo 的相对 key（与剧集封面一样由图片线路拼完整地址）；留空表示用站名文字。
	LogoLight string `json:"logo_light"`
	Favicon   string `json:"favicon"`
}

// Theme 是配色与观感（theme 段）。
type Theme struct {
	Preset string `json:"preset"`
	// ColorScheme 是默认深浅色：dark / light / system（跟随系统）。
	ColorScheme string `json:"color_scheme"`
	// AllowSchemeToggle 为真时前台显示深浅色切换。
	AllowSchemeToggle bool `json:"allow_scheme_toggle"`
	// Colors 覆盖预设里的颜色，键见 themeColorKeys，值为 #rgb 或 #rrggbb；不填的用预设色。
	Colors map[string]string `json:"colors"`
	// Radius 是卡片圆角（px）。
	Radius int `json:"radius"`
	// CoverRatio 是封面比例：2:3 / 3:4 / 16:9。
	CoverRatio string `json:"cover_ratio"`
	Font       string `json:"font"`
}

// Layout 是布局形态（layout 段）。
type Layout struct {
	// HeaderStyle 是页头样式：classic 经典 / centered 居中 / pill 胶囊。
	HeaderStyle string `json:"header_style"`
	// CardStyle 是卡片样式：below 标题在封面下 / overlay 标题压在封面上。
	CardStyle string `json:"card_style"`
	// PlayMode 是播放形态：mobile_immersive 手机沉浸 + 桌面详情 / all_immersive 全沉浸 / all_detail 全详情。
	PlayMode string `json:"play_mode"`
	// MobileTabbar 为真时手机显示底部标签栏。
	MobileTabbar bool `json:"mobile_tabbar"`
}

// Option 是后台下拉里的一项：编码 + 中文名。清单由后端给，避免前端抄一份枚举。
type Option struct {
	Value string `json:"value"`
	Label string `json:"label"`
	// Hint 是补充说明，只有需要解释的选项才有。
	Hint string `json:"hint,omitempty"`
}

// BrandingConfig 是 /brand 页面需要的全部内容：三段配置 + 各个下拉的可选值。
type BrandingConfig struct {
	Brand  Brand  `json:"brand"`
	Theme  Theme  `json:"theme"`
	Layout Layout `json:"layout"`
	// Options 给前端渲染下拉与预览用。
	Options BrandingOptions `json:"options"`
}

// BrandingOptions 是各枚举的可选值与颜色键。
type BrandingOptions struct {
	Presets      []Option `json:"presets"`
	ColorSchemes []Option `json:"color_schemes"`
	Colors       []Option `json:"colors"`
	CoverRatios  []Option `json:"cover_ratios"`
	Fonts        []Option `json:"fonts"`
	HeaderStyles []Option `json:"header_styles"`
	CardStyles   []Option `json:"card_styles"`
	PlayModes    []Option `json:"play_modes"`
	// MaxRadius 是圆角上限（px），前端据此限制输入。
	MaxRadius int `json:"max_radius"`
}

// 各段的白名单。前台按这些编码渲染，新增取值要前后端一起加。
var (
	themePresets = []Option{
		{Value: "classic", Label: "经典红"},
		{Value: "midnight", Label: "午夜蓝"},
		{Value: "forest", Label: "墨绿"},
		{Value: "violet", Label: "紫调"},
		{Value: "neutral", Label: "中性灰"},
	}
	colorSchemes = []Option{
		{Value: "dark", Label: "深色"},
		{Value: "light", Label: "浅色"},
		{Value: "system", Label: "跟随系统"},
	}
	themeColors = []Option{
		{Value: "primary", Label: "主色", Hint: "按钮、选中态"},
		{Value: "accent", Label: "强调色", Hint: "角标、进度条"},
		{Value: "bg", Label: "页面背景"},
		{Value: "surface", Label: "卡片背景"},
		{Value: "text", Label: "正文文字"},
		{Value: "muted", Label: "次要文字"},
	}
	coverRatios = []Option{
		{Value: "2:3", Label: "2:3（竖版海报）"},
		{Value: "3:4", Label: "3:4"},
		{Value: "16:9", Label: "16:9（横版）"},
	}
	themeFonts = []Option{
		{Value: "system", Label: "系统字体", Hint: "不额外加载，首屏最快"},
		{Value: "serif", Label: "衬线"},
		{Value: "rounded", Label: "圆体"},
	}
	headerStyles = []Option{
		{Value: "classic", Label: "经典（左 Logo 右导航）"},
		{Value: "centered", Label: "居中"},
		{Value: "pill", Label: "胶囊"},
	}
	cardStyles = []Option{
		{Value: "below", Label: "标题在封面下"},
		{Value: "overlay", Label: "标题压在封面上"},
	}
	playModes = []Option{
		{Value: "mobile_immersive", Label: "手机沉浸 + 电脑详情", Hint: "推荐"},
		{Value: "all_immersive", Label: "全部沉浸式"},
		{Value: "all_detail", Label: "全部详情页"},
	}
)

// 默认值：配置为空（没保存过）时按这些返回，与前台的兜底保持一致。
const (
	defaultPreset      = "classic"
	defaultColorScheme = "dark"
	defaultRadius      = 12
	defaultCoverRatio  = "2:3"
	defaultFont        = "system"
	defaultHeaderStyle = "classic"
	defaultCardStyle   = "below"
	defaultPlayMode    = "mobile_immersive"
)

// 长度与取值上限。
const (
	maxSiteName   = 30
	maxSlogan     = 60
	maxCopyright  = 100
	maxImageKey   = 300
	maxSupportURL = 500
	maxRadiusPx   = 32
)

// 图片相对 key：后台上传产生的 img/...，也允许完整 http(s) 地址（接入图片管线前便于先填外链）。
var imageKeyRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9/._-]{0,299}$`)

// hex 颜色：#rgb 或 #rrggbb。
var hexColorRe = regexp.MustCompile(`^#([0-9a-fA-F]{3}|[0-9a-fA-F]{6})$`)

// BrandingRepo 读写这三段配置。
type BrandingRepo interface {
	GetBranding(ctx context.Context) (site, theme, layout json.RawMessage, err error)
	SetBrand(ctx context.Context, site json.RawMessage) error
	SetTheme(ctx context.Context, theme json.RawMessage) error
	SetLayout(ctx context.Context, layout json.RawMessage) error
}

// BrandingService 是后台的品牌、配色与布局。
type BrandingService struct {
	repo BrandingRepo
	site SiteConfigManager
}

func NewBrandingService(repo BrandingRepo, site SiteConfigManager) *BrandingService {
	return &BrandingService{repo: repo, site: site}
}

// Get 返回三段配置与各下拉的可选值。没保存过的段按默认值返回。
func (s *BrandingService) Get(ctx context.Context) (BrandingConfig, error) {
	rawSite, rawTheme, rawLayout, err := s.repo.GetBranding(ctx)
	if err != nil {
		return BrandingConfig{}, err
	}
	out := BrandingConfig{
		// 没保存过时「允许切换深浅色」按开算（与 MobileTabbar 一致）：前台的全局外壳本来就带深浅色切换，
		// 默认关会让新站点既没有开关、后台里也显示成关（docs/04 B.0、W0-6）
		Theme: Theme{
			Preset: defaultPreset, ColorScheme: defaultColorScheme, AllowSchemeToggle: true,
			Radius: defaultRadius, CoverRatio: defaultCoverRatio, Font: defaultFont,
		},
		Layout:  Layout{HeaderStyle: defaultHeaderStyle, CardStyle: defaultCardStyle, PlayMode: defaultPlayMode, MobileTabbar: true},
		Options: brandingOptions(),
	}
	if err := decodeSegment(rawSite, &out.Brand, "品牌"); err != nil {
		return BrandingConfig{}, err
	}
	if err := decodeSegment(rawTheme, &out.Theme, "配色"); err != nil {
		return BrandingConfig{}, err
	}
	if err := decodeSegment(rawLayout, &out.Layout, "布局"); err != nil {
		return BrandingConfig{}, err
	}
	if out.Theme.Colors == nil {
		out.Theme.Colors = map[string]string{}
	}
	return out, nil
}

// SetBrand 整体保存品牌段。
func (s *BrandingService) SetBrand(ctx context.Context, in Brand) (BrandingConfig, error) {
	clean, err := cleanBrand(in)
	if err != nil {
		return BrandingConfig{}, err
	}
	return s.save(ctx, "品牌", clean, s.repo.SetBrand)
}

// SetTheme 整体保存配色段。
func (s *BrandingService) SetTheme(ctx context.Context, in Theme) (BrandingConfig, error) {
	clean, err := cleanTheme(in)
	if err != nil {
		return BrandingConfig{}, err
	}
	return s.save(ctx, "配色", clean, s.repo.SetTheme)
}

// SetLayout 整体保存布局段。
func (s *BrandingService) SetLayout(ctx context.Context, in Layout) (BrandingConfig, error) {
	clean, err := cleanLayout(in)
	if err != nil {
		return BrandingConfig{}, err
	}
	return s.save(ctx, "布局", clean, s.repo.SetLayout)
}

// save 编码并写入一段配置，然后刷新站点版本，最后回读全部三段（响应即保存结果）。
func (s *BrandingService) save(ctx context.Context, name string, payload any, write func(context.Context, json.RawMessage) error) (BrandingConfig, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return BrandingConfig{}, fmt.Errorf("编码%s: %w", name, err)
	}
	if err := write(ctx, raw); err != nil {
		return BrandingConfig{}, err
	}
	if _, err := s.site.BumpVersion(ctx); err != nil {
		return BrandingConfig{}, fmt.Errorf("保存成功，但刷新站点版本失败（前台可能要等一会儿才生效）: %w", err)
	}
	return s.Get(ctx)
}

func brandingOptions() BrandingOptions {
	return BrandingOptions{
		Presets: themePresets, ColorSchemes: colorSchemes, Colors: themeColors, CoverRatios: coverRatios,
		Fonts: themeFonts, HeaderStyles: headerStyles, CardStyles: cardStyles, PlayModes: playModes,
		MaxRadius: maxRadiusPx,
	}
}

// decodeSegment 解析一段 JSON。坏 JSON 直接报错，不当成空配置：
// 静默清空会让运营以为「配置丢了」，而真正的原因是数据有问题。
func decodeSegment(raw json.RawMessage, dst any, name string) error {
	if len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, dst); err != nil {
		return fmt.Errorf("解析%s配置: %w", name, err)
	}
	return nil
}

func cleanBrand(in Brand) (Brand, error) {
	in.SiteName = strings.TrimSpace(in.SiteName)
	in.Slogan = strings.TrimSpace(in.Slogan)
	in.Copyright = strings.TrimSpace(in.Copyright)
	in.SupportURL = strings.TrimSpace(in.SupportURL)
	in.LogoLight = strings.TrimSpace(in.LogoLight)
	in.Favicon = strings.TrimSpace(in.Favicon)

	// 站名是页头、标题模板与兜底文案都要用的，必须有
	if in.SiteName == "" {
		return Brand{}, badRequest("请填写站名")
	}
	for _, c := range []struct {
		value, name string
		max         int
	}{
		{in.SiteName, "站名", maxSiteName},
		{in.Slogan, "口号", maxSlogan},
		{in.Copyright, "版权文字", maxCopyright},
	} {
		if utf8.RuneCountInString(c.value) > c.max {
			return Brand{}, badRequest("%s不能超过 %d 个字", c.name, c.max)
		}
	}
	if err := checkExternalURL(in.SupportURL, "客服链接", maxSupportURL); err != nil {
		return Brand{}, err
	}
	for _, c := range []struct{ value, name string }{
		{in.LogoLight, "头部 Logo"},
		{in.Favicon, "favicon"},
	} {
		if err := checkImageKey(c.value, c.name); err != nil {
			return Brand{}, err
		}
	}
	return in, nil
}

func cleanTheme(in Theme) (Theme, error) {
	var err error
	if in.Preset, err = pickOption(in.Preset, themePresets, defaultPreset, "皮肤预设"); err != nil {
		return Theme{}, err
	}
	if in.ColorScheme, err = pickOption(in.ColorScheme, colorSchemes, defaultColorScheme, "默认深浅色"); err != nil {
		return Theme{}, err
	}
	if in.CoverRatio, err = pickOption(in.CoverRatio, coverRatios, defaultCoverRatio, "封面比例"); err != nil {
		return Theme{}, err
	}
	if in.Font, err = pickOption(in.Font, themeFonts, defaultFont, "字体"); err != nil {
		return Theme{}, err
	}
	if in.Radius == 0 && len(in.Colors) == 0 && in.Preset == defaultPreset {
		in.Radius = defaultRadius // 整段都是零值（新站点第一次保存）时给个像样的圆角
	}
	if in.Radius < 0 || in.Radius > maxRadiusPx {
		return Theme{}, badRequest("卡片圆角为 0–%d 像素", maxRadiusPx)
	}
	colors := make(map[string]string, len(in.Colors))
	for key, value := range in.Colors {
		if !slices.ContainsFunc(themeColors, func(o Option) bool { return o.Value == key }) {
			return Theme{}, badRequest("颜色 %q 不在可配置范围内（可选：%s）", key, optionValues(themeColors))
		}
		value = strings.TrimSpace(value)
		if value == "" {
			continue // 留空表示用预设色
		}
		if !hexColorRe.MatchString(value) {
			return Theme{}, badRequest("颜色「%s」要填 #RGB 或 #RRGGBB 形式的色值", labelOf(themeColors, key))
		}
		colors[key] = strings.ToLower(value)
	}
	in.Colors = colors
	return in, nil
}

func cleanLayout(in Layout) (Layout, error) {
	var err error
	if in.HeaderStyle, err = pickOption(in.HeaderStyle, headerStyles, defaultHeaderStyle, "页头样式"); err != nil {
		return Layout{}, err
	}
	if in.CardStyle, err = pickOption(in.CardStyle, cardStyles, defaultCardStyle, "卡片样式"); err != nil {
		return Layout{}, err
	}
	if in.PlayMode, err = pickOption(in.PlayMode, playModes, defaultPlayMode, "播放形态"); err != nil {
		return Layout{}, err
	}
	return in, nil
}

// pickOption 校验枚举：空值取默认，未登记的取值报错并列出可选项。
func pickOption(value string, options []Option, def, name string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return def, nil
	}
	if !slices.ContainsFunc(options, func(o Option) bool { return o.Value == value }) {
		return "", badRequest("%s %q 无效（可选：%s）", name, value, optionValues(options))
	}
	return value, nil
}

func optionValues(options []Option) string {
	out := make([]string, len(options))
	for i, o := range options {
		out[i] = o.Value
	}
	return strings.Join(out, "、")
}

func labelOf(options []Option, value string) string {
	for _, o := range options {
		if o.Value == value {
			return o.Label
		}
	}
	return value
}

// checkImageKey 校验图片相对 key 或完整 http(s) 地址；留空表示不设置。
func checkImageKey(value, name string) error {
	if value == "" {
		return nil
	}
	// 名称里有 Logo、favicon 这类拉丁词，模板里留一个空格，中英文混排才不挤在一起
	if len(value) > maxImageKey {
		return badRequest("%s 的地址不能超过 %d 个字符", name, maxImageKey)
	}
	if strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://") {
		u, err := url.Parse(value)
		if err != nil || u.Host == "" {
			return badRequest("%s 的地址不是有效的网址", name)
		}
		return nil
	}
	if !imageKeyRe.MatchString(value) {
		return badRequest("%s 要填上传后的图片路径（如 img/logo.webp）或完整的 http(s) 地址", name)
	}
	return nil
}

// checkExternalURL 校验 http(s) 完整地址；留空表示不设置。
func checkExternalURL(value, name string, max int) error {
	if value == "" {
		return nil
	}
	if len(value) > max {
		return badRequest("%s不能超过 %d 个字符", name, max)
	}
	u, err := url.Parse(value)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return badRequest("%s要是完整的 http(s) 网址", name)
	}
	return nil
}
