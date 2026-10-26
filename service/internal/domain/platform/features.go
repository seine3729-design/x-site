package platform

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"unicode/utf8"
)

// 功能开关与年龄门（docs/06 第 36 节，S3-3）。两段都存在 site_config 里，
// 保存后经 SiteConfigService.BumpVersion 让版本 +1 并广播失效，各端秒级生效（docs/06「保存后几秒生效」）。

// FeatureFlag 是一个功能开关的编码。
type FeatureFlag string

const (
	FlagComments         FeatureFlag = "comments"
	FlagDanmaku          FeatureFlag = "danmaku"
	FlagRegister         FeatureFlag = "register"
	FlagGuestInteraction FeatureFlag = "guest_interaction"
	FlagRanks            FeatureFlag = "ranks"
	FlagSharePoster      FeatureFlag = "share_poster"
	FlagPWA              FeatureFlag = "pwa"
	FlagSearchSuggest    FeatureFlag = "search_suggest"
)

// FlagInfo 描述一个开关：名称与「关掉会怎样」。
//
// docs/06 第 36 节要求「每项附关闭影响说明」—— 关开关是会影响线上体验的操作，
// 运营得先知道代价。说明写在后端而不是前端：开关的实际行为由后端决定，两边分开写必然漂移。
type FlagInfo struct {
	Flag   FeatureFlag `json:"flag"`
	Title  string      `json:"title"`
	Impact string      `json:"impact"`
	On     bool        `json:"on"`
}

// featureFlags 是全部开关，按页面展示顺序排列。
//
// 这里是开关的唯一清单：`site_config.features` 里出现的其他键（如建表时写入的 articles、actors，
// 对应的模块已经在迁移 00010 / 00014 删掉了）保存时一并剔除，避免页面上长出已经不存在的功能。
var featureFlags = []FlagInfo{
	{Flag: FlagComments, Title: "评论", Impact: "关闭后剧集详情与播放页不再显示评论区，已有评论保留但不可见，也无法新增。"},
	{Flag: FlagDanmaku, Title: "弹幕", Impact: "关闭后播放器不加载弹幕、不接受发送，已有弹幕保留。"},
	{Flag: FlagRegister, Title: "开放注册", Impact: "关闭后新用户无法注册，已注册用户仍可登录。活动与福利类功能会因为拉不到新用户而失去效果。"},
	{Flag: FlagGuestInteraction, Title: "游客互动", Impact: "关闭后未登录用户不能点赞、收藏、发评论与弹幕，会先要求登录。可能明显降低互动量。"},
	{Flag: FlagRanks, Title: "排行", Impact: "关闭后前台排行榜接口返回 404、入口隐藏。后台的排行干预仍可配置，开启后立即生效。"},
	{Flag: FlagSharePoster, Title: "分享海报", Impact: "关闭后分享时只给链接，不再生成带封面与二维码的图片。"},
	{Flag: FlagPWA, Title: "添加到主屏幕（PWA）", Impact: "关闭后不再下发安装清单，浏览器不提示「添加到主屏幕」，已安装的不受影响。"},
	{Flag: FlagSearchSuggest, Title: "搜索联想", Impact: "关闭后搜索框不再实时联想，只能提交完整关键词搜索。"},
}

// AgeGate 是年龄确认门（docs/06 第 36 节）。
type AgeGate struct {
	Enabled bool `json:"enabled"`
	// Title 是弹层标题，Desc 是说明文字（docs/06 要求的「说明文字」，docs/02 原本漏了这个字段）。
	Title       string `json:"title"`
	Desc        string `json:"desc"`
	ConfirmText string `json:"confirm_text"`
	DenyText    string `json:"deny_text"`
	// DenyURL 是点「拒绝」后跳转的地址，留空则留在当前页并保持遮挡。
	DenyURL string `json:"deny_url"`
	// RememberDays 是确认后多少天内不再弹（0 表示每次打开都弹）。
	RememberDays int `json:"remember_days"`
}

// FeatureConfig 是功能开关与年龄门的完整配置。
type FeatureConfig struct {
	Flags   []FlagInfo `json:"flags"`
	AgeGate AgeGate    `json:"age_gate"`
}

// FeatureRepo 读写这两段配置。
type FeatureRepo interface {
	GetFeatureConfig(ctx context.Context) (features json.RawMessage, ageGate json.RawMessage, err error)
	SetFeatureFlags(ctx context.Context, features json.RawMessage) error
	SetAgeGate(ctx context.Context, ageGate json.RawMessage) error
}

// FeatureService 是后台的功能开关与年龄门。
type FeatureService struct {
	repo FeatureRepo
	site SiteConfigManager
}

func NewFeatureService(repo FeatureRepo, site SiteConfigManager) *FeatureService {
	return &FeatureService{repo: repo, site: site}
}

// 年龄门文案的长度上限。
const (
	maxAgeGateTitle   = 60
	maxAgeGateDesc    = 500
	maxAgeGateButton  = 20
	maxAgeGateURL     = 500
	maxRememberDays   = 365
	ageGateDefaultDay = 30
)

// Get 返回两段配置。未设置的开关按打开处理（与 ops.FeatureFlags 的语义一致：
// 只有明确设为 false 才算关闭，这样新增开关不会因为老配置里没有而默认关掉功能）。
func (s *FeatureService) Get(ctx context.Context) (FeatureConfig, error) {
	rawFlags, rawGate, err := s.repo.GetFeatureConfig(ctx)
	if err != nil {
		return FeatureConfig{}, err
	}
	stored := map[string]any{}
	if len(rawFlags) > 0 {
		if err := json.Unmarshal(rawFlags, &stored); err != nil {
			return FeatureConfig{}, fmt.Errorf("解析功能开关: %w", err)
		}
	}
	out := FeatureConfig{Flags: make([]FlagInfo, len(featureFlags))}
	copy(out.Flags, featureFlags)
	for i := range out.Flags {
		// 只有明确 false 才是关闭
		out.Flags[i].On = stored[string(out.Flags[i].Flag)] != false
	}

	gate := AgeGate{RememberDays: ageGateDefaultDay}
	if len(rawGate) > 0 {
		if err := json.Unmarshal(rawGate, &gate); err != nil {
			return FeatureConfig{}, fmt.Errorf("解析年龄确认门: %w", err)
		}
	}
	out.AgeGate = gate
	return out, nil
}

// SetFlags 整体保存功能开关。入参只接受已登记的开关编码，未传的按打开保存。
//
// 保存时只写登记过的键：`site_config.features` 里可能残留已删除模块的开关
// （建表时写入的 articles、actors），留着会让前台与后台都读到不存在的功能。
func (s *FeatureService) SetFlags(ctx context.Context, in map[string]bool) (FeatureConfig, error) {
	known := make(map[string]bool, len(featureFlags))
	for _, f := range featureFlags {
		known[string(f.Flag)] = true
	}
	for k := range in {
		if !known[k] {
			names := make([]string, 0, len(featureFlags))
			for _, f := range featureFlags {
				names = append(names, string(f.Flag))
			}
			return FeatureConfig{}, badRequest("功能开关 %q 不存在（可选：%s）", k, strings.Join(names, "、"))
		}
	}
	out := make(map[string]bool, len(featureFlags))
	for _, f := range featureFlags {
		on, ok := in[string(f.Flag)]
		out[string(f.Flag)] = !ok || on
	}
	raw, err := json.Marshal(out)
	if err != nil {
		return FeatureConfig{}, fmt.Errorf("编码功能开关: %w", err)
	}
	if err := s.repo.SetFeatureFlags(ctx, raw); err != nil {
		return FeatureConfig{}, err
	}
	if _, err := s.site.BumpVersion(ctx); err != nil {
		return FeatureConfig{}, fmt.Errorf("保存成功，但刷新站点版本失败（前台可能要等一会儿才生效）: %w", err)
	}
	return s.Get(ctx)
}

// SetAgeGate 整体保存年龄确认门。
func (s *FeatureService) SetAgeGate(ctx context.Context, in AgeGate) (FeatureConfig, error) {
	clean, err := cleanAgeGate(in)
	if err != nil {
		return FeatureConfig{}, err
	}
	raw, err := json.Marshal(clean)
	if err != nil {
		return FeatureConfig{}, fmt.Errorf("编码年龄确认门: %w", err)
	}
	if err := s.repo.SetAgeGate(ctx, raw); err != nil {
		return FeatureConfig{}, err
	}
	if _, err := s.site.BumpVersion(ctx); err != nil {
		return FeatureConfig{}, fmt.Errorf("保存成功，但刷新站点版本失败（前台可能要等一会儿才生效）: %w", err)
	}
	return s.Get(ctx)
}

// cleanAgeGate 校验并补默认值。
//
// 开着的年龄门会挡在所有访客前面，文案缺失会让用户看到空白弹层无法继续，
// 所以启用时强制要求标题与两个按钮文字；关闭时只做长度校验，允许留着草稿。
func cleanAgeGate(in AgeGate) (AgeGate, error) {
	in.Title = strings.TrimSpace(in.Title)
	in.Desc = strings.TrimSpace(in.Desc)
	in.ConfirmText = strings.TrimSpace(in.ConfirmText)
	in.DenyText = strings.TrimSpace(in.DenyText)
	in.DenyURL = strings.TrimSpace(in.DenyURL)

	for _, c := range []struct {
		value, name string
		max         int
	}{
		{in.Title, "标题", maxAgeGateTitle},
		{in.Desc, "说明文字", maxAgeGateDesc},
		{in.ConfirmText, "确认按钮文字", maxAgeGateButton},
		{in.DenyText, "拒绝按钮文字", maxAgeGateButton},
	} {
		if utf8.RuneCountInString(c.value) > c.max {
			return AgeGate{}, badRequest("%s不能超过 %d 个字", c.name, c.max)
		}
	}
	if len(in.DenyURL) > maxAgeGateURL {
		return AgeGate{}, badRequest("拒绝跳转地址不能超过 %d 个字符", maxAgeGateURL)
	}
	if in.DenyURL != "" {
		u, err := url.Parse(in.DenyURL)
		if err != nil || u.Scheme == "" || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
			return AgeGate{}, badRequest("拒绝跳转地址要是完整的 http(s) 网址")
		}
	}
	if in.RememberDays < 0 || in.RememberDays > maxRememberDays {
		return AgeGate{}, badRequest("重复间隔为 0–%d 天（0 表示每次打开都确认）", maxRememberDays)
	}
	if in.Enabled {
		for _, c := range []struct{ value, name string }{
			{in.Title, "标题"},
			{in.ConfirmText, "确认按钮文字"},
			{in.DenyText, "拒绝按钮文字"},
		} {
			if c.value == "" {
				return AgeGate{}, badRequest("启用年龄确认门时必须填写%s（否则访客会看到无法操作的空白弹层）", c.name)
			}
		}
	}
	return in, nil
}
