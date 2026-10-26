package platform

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"
)

// SEO 与统计代码（docs/06 第 17 节，S3-3 / A3-3，docs/specs/A3-3.md）。
// 两段都在 site_config 单行里：seo 随站点配置下发给前台渲染标题与 meta，analytics 是要原样注入页面的第三方代码。
// 统计代码另有权限点 seo.analytics_code（默认只有超管），接口也单独一组——没有权限的运营连内容都不该读到。

// SEOPage 是可配置标题 / 描述模板的页面。
type SEOPage string

const (
	PageHome    SEOPage = "home"
	PageList    SEOPage = "list"
	PagePlay    SEOPage = "play"
	PageRank    SEOPage = "rank"
	PageDrama   SEOPage = "drama"
	PageTopic   SEOPage = "topic"
	PageSearch  SEOPage = "search"
	PageChannel SEOPage = "channel"
	PageTag     SEOPage = "tag"
)

// seoPages 是页面清单（docs/06 第 17 节），顺序即后台展示顺序。
var seoPages = []Option{
	{Value: string(PageHome), Label: "首页"},
	{Value: string(PageList), Label: "列表 / 探索"},
	{Value: string(PagePlay), Label: "播放页", Hint: "可用第几集、总集数"},
	{Value: string(PageRank), Label: "排行", Hint: "可用榜单名"},
	{Value: string(PageDrama), Label: "剧集详情", Hint: "可用剧名、简介"},
	{Value: string(PageTopic), Label: "专题", Hint: "可用专题名"},
	{Value: string(PageSearch), Label: "搜索", Hint: "可用搜索词、数量"},
	{Value: string(PageChannel), Label: "频道", Hint: "可用频道名"},
	{Value: string(PageTag), Label: "标签", Hint: "可用标签名"},
}

// seoVariables 是模板里可用的变量（docs/06 第 17 节的「点击插入」清单）。
// 清单放后端：前台替换变量的逻辑也在后端规划内，两边各写一份必然漂移。
var seoVariables = []Option{
	{Value: "site_name", Label: "站名"},
	{Value: "slogan", Label: "口号"},
	{Value: "drama_title", Label: "剧名"},
	{Value: "summary", Label: "简介"},
	{Value: "total_episodes", Label: "总集数"},
	{Value: "episode_no", Label: "第几集"},
	{Value: "channel_name", Label: "频道名"},
	{Value: "topic_name", Label: "专题名"},
	{Value: "tag_name", Label: "标签名"},
	{Value: "count", Label: "数量"},
	{Value: "keyword", Label: "搜索词"},
	{Value: "board_name", Label: "榜单名"},
}

// Verification 是一组站长验证标签（输出成 <meta name=… content=…>）。
type Verification struct {
	Name    string `json:"name"`
	Content string `json:"content"`
}

// SEO 是 seo 段。
type SEO struct {
	// TitleTemplates / DescTemplates 的键是页面编码，值是含 {变量} 的模板；缺哪页就按前台默认。
	TitleTemplates map[string]string `json:"title_templates"`
	DescTemplates  map[string]string `json:"desc_templates"`
	// OGImage 是默认分享图（相对 key 或完整地址）。
	OGImage string `json:"og_image"`
	// RobotsAllow 为假时整站输出 noindex，并让 robots.txt 拒绝抓取。
	RobotsAllow bool `json:"robots_allow"`
	// RobotsExtra 追加到 robots.txt 末尾。
	RobotsExtra string `json:"robots_extra"`
	// Sitemap 控制是否提供 sitemap.xml，StructuredData 控制是否输出结构化数据。
	Sitemap        bool           `json:"sitemap"`
	StructuredData bool           `json:"structured_data"`
	Verifications  []Verification `json:"verifications"`
}

// Analytics 是统计代码（analytics 段）。原样注入页面，不做 HTML 清洗：
// 清洗掉 <script> 这段配置就没有意义了。控制手段是「仅 seo.analytics_code（超管）可改 + 记审计 + 长度上限 + 前台 CSP」。
type Analytics struct {
	HeadSnippet string `json:"head_snippet"`
	// BodySnippet 放在 </body> 前。
	BodySnippet string `json:"body_snippet"`
}

// SEOConfig 是 /seo 页面需要的内容：seo 段 + 页面与变量清单（统计代码另走一个接口）。
type SEOConfig struct {
	SEO SEO `json:"seo"`
	// Pages 是可配置模板的页面，Variables 是可插入的变量。
	Pages     []Option `json:"pages"`
	Variables []Option `json:"variables"`
	// MaxTemplate 是单条模板的字数上限，前端据此限制输入。
	MaxTemplate int `json:"max_template"`
}

// 长度与条数上限。统计代码给得宽一点：一段 GA + 一段自建埋点很容易上千字符。
const (
	maxSEOTemplate     = 120
	maxOGImage         = 300
	maxRobotsExtra     = 2000
	maxVerifications   = 10
	maxVerifyName      = 60
	maxVerifyContent   = 200
	maxAnalyticsLength = 20_000
)

// 模板里的 {变量} 与站长验证的 name 格式。
var (
	templateVarRe = regexp.MustCompile(`\{([^{}]*)\}`)
	verifyNameRe  = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,59}$`)
)

// SEORepo 读写这两段配置。
type SEORepo interface {
	GetSEOConfig(ctx context.Context) (seo json.RawMessage, analytics json.RawMessage, err error)
	SetSEO(ctx context.Context, seo json.RawMessage) error
	SetAnalytics(ctx context.Context, analytics json.RawMessage) error
}

// SEOService 是后台的 SEO 与统计代码。
type SEOService struct {
	repo SEORepo
	site SiteConfigManager
}

func NewSEOService(repo SEORepo, site SiteConfigManager) *SEOService {
	return &SEOService{repo: repo, site: site}
}

// Get 返回 seo 段与页面、变量清单。没保存过时按「允许收录 + 提供 sitemap + 输出结构化数据」返回：
// 这是正常站点想要的默认，默认不收录会让新站白白失去搜索流量。
func (s *SEOService) Get(ctx context.Context) (SEOConfig, error) {
	raw, _, err := s.repo.GetSEOConfig(ctx)
	if err != nil {
		return SEOConfig{}, err
	}
	out := SEOConfig{
		SEO:         SEO{RobotsAllow: true, Sitemap: true, StructuredData: true},
		Pages:       seoPages,
		Variables:   seoVariables,
		MaxTemplate: maxSEOTemplate,
	}
	if err := decodeSegment(raw, &out.SEO, "SEO"); err != nil {
		return SEOConfig{}, err
	}
	if out.SEO.TitleTemplates == nil {
		out.SEO.TitleTemplates = map[string]string{}
	}
	if out.SEO.DescTemplates == nil {
		out.SEO.DescTemplates = map[string]string{}
	}
	if out.SEO.Verifications == nil {
		out.SEO.Verifications = []Verification{}
	}
	return out, nil
}

// Set 整体保存 seo 段。
func (s *SEOService) Set(ctx context.Context, in SEO) (SEOConfig, error) {
	clean, err := cleanSEO(in)
	if err != nil {
		return SEOConfig{}, err
	}
	raw, err := json.Marshal(clean)
	if err != nil {
		return SEOConfig{}, fmt.Errorf("编码 SEO 配置: %w", err)
	}
	if err := s.repo.SetSEO(ctx, raw); err != nil {
		return SEOConfig{}, err
	}
	if _, err := s.site.BumpVersion(ctx); err != nil {
		return SEOConfig{}, fmt.Errorf("保存成功，但刷新站点版本失败（前台可能要等一会儿才生效）: %w", err)
	}
	return s.Get(ctx)
}

// Analytics 返回统计代码（调用方须已校验 seo.analytics_code 权限）。
func (s *SEOService) Analytics(ctx context.Context) (Analytics, error) {
	_, raw, err := s.repo.GetSEOConfig(ctx)
	if err != nil {
		return Analytics{}, err
	}
	var out Analytics
	if err := decodeSegment(raw, &out, "统计代码"); err != nil {
		return Analytics{}, err
	}
	return out, nil
}

// SetAnalytics 整体保存统计代码。只做长度与明显误填的校验：
// 这段代码本来就是要原样注入的，做 HTML 清洗只会让它失效，还给人一种「已经安全了」的错觉。
func (s *SEOService) SetAnalytics(ctx context.Context, in Analytics) (Analytics, error) {
	in.HeadSnippet = strings.TrimSpace(in.HeadSnippet)
	in.BodySnippet = strings.TrimSpace(in.BodySnippet)
	for _, c := range []struct{ value, name string }{
		{in.HeadSnippet, "head 统计代码"},
		{in.BodySnippet, "body 统计代码"},
	} {
		if utf8.RuneCountInString(c.value) > maxAnalyticsLength {
			return Analytics{}, badRequest("%s不能超过 %d 个字符", c.name, maxAnalyticsLength)
		}
	}
	raw, err := json.Marshal(in)
	if err != nil {
		return Analytics{}, fmt.Errorf("编码统计代码: %w", err)
	}
	if err := s.repo.SetAnalytics(ctx, raw); err != nil {
		return Analytics{}, err
	}
	if _, err := s.site.BumpVersion(ctx); err != nil {
		return Analytics{}, fmt.Errorf("保存成功，但刷新站点版本失败（前台可能要等一会儿才生效）: %w", err)
	}
	return s.Analytics(ctx)
}

// cleanSEO 校验并规范化 seo 段。
func cleanSEO(in SEO) (SEO, error) {
	titles, err := cleanTemplates(in.TitleTemplates, "标题模板")
	if err != nil {
		return SEO{}, err
	}
	descs, err := cleanTemplates(in.DescTemplates, "描述模板")
	if err != nil {
		return SEO{}, err
	}
	in.TitleTemplates, in.DescTemplates = titles, descs

	in.OGImage = strings.TrimSpace(in.OGImage)
	if err := checkImageKey(in.OGImage, "分享图"); err != nil {
		return SEO{}, err
	}
	in.RobotsExtra = strings.TrimSpace(in.RobotsExtra)
	if utf8.RuneCountInString(in.RobotsExtra) > maxRobotsExtra {
		return SEO{}, badRequest("robots.txt 追加内容不能超过 %d 个字符", maxRobotsExtra)
	}

	if len(in.Verifications) > maxVerifications {
		return SEO{}, badRequest("站长验证最多 %d 组", maxVerifications)
	}
	out := make([]Verification, 0, len(in.Verifications))
	seen := map[string]bool{}
	for _, v := range in.Verifications {
		v.Name = strings.TrimSpace(v.Name)
		v.Content = strings.TrimSpace(v.Content)
		if v.Name == "" && v.Content == "" {
			continue // 整行留空的忽略（前端的空白行）
		}
		if v.Name == "" || v.Content == "" {
			return SEO{}, badRequest("站长验证的名称与内容都要填（名称是 meta 标签的 name）")
		}
		if len(v.Name) > maxVerifyName || !verifyNameRe.MatchString(v.Name) {
			return SEO{}, badRequest("站长验证名称 %q 只能用字母、数字、点、下划线与连字符", v.Name)
		}
		if utf8.RuneCountInString(v.Content) > maxVerifyContent {
			return SEO{}, badRequest("站长验证「%s」的内容不能超过 %d 个字符", v.Name, maxVerifyContent)
		}
		if seen[strings.ToLower(v.Name)] {
			return SEO{}, badRequest("站长验证名称 %q 重复了", v.Name)
		}
		seen[strings.ToLower(v.Name)] = true
		out = append(out, v)
	}
	in.Verifications = out
	return in, nil
}

// cleanTemplates 校验一组模板：页面键与 {变量} 都要在登记范围内。
//
// 变量名写错（如 {dramaTitle}）不报错的话，前台会把 "{dramaTitle}" 原样显示在标题里给用户看，
// 所以这里直接拒绝并列出可用变量。
func cleanTemplates(in map[string]string, name string) (map[string]string, error) {
	out := make(map[string]string, len(in))
	for _, page := range slices.Sorted(maps.Keys(in)) {
		if !slices.ContainsFunc(seoPages, func(o Option) bool { return o.Value == page }) {
			return nil, badRequest("%s里的页面 %q 不存在（可选：%s）", name, page, optionValues(seoPages))
		}
		tpl := strings.TrimSpace(in[page])
		if tpl == "" {
			continue // 留空表示该页用前台默认模板
		}
		if utf8.RuneCountInString(tpl) > maxSEOTemplate {
			return nil, badRequest("「%s」的%s不能超过 %d 个字", labelOf(seoPages, page), name, maxSEOTemplate)
		}
		for _, m := range templateVarRe.FindAllStringSubmatch(tpl, -1) {
			v := strings.TrimSpace(m[1])
			if !slices.ContainsFunc(seoVariables, func(o Option) bool { return o.Value == v }) {
				return nil, badRequest("「%s」的%s里有未知变量 {%s}（可用：%s）",
					labelOf(seoPages, page), name, m[1], optionValues(seoVariables))
			}
		}
		out[page] = tpl
	}
	return out, nil
}
