package platform

import (
	"fmt"
	"strings"
)

// Valkey 里的键分两类，清缓存只能动第一类（docs/06 第 28 节，S4-4）：
//
//   - 可丢弃的读穿缓存：清掉只是下次请求回源一次，没有副作用。
//   - 有状态数据：后台会话、限流计数、尚未落库的统计、线上榜单有序集合。
//     清掉会把管理员踢下线、给人重置限流额度、丢掉还没落库的广告曝光与搜索次数、让榜单空一轮。
//
// 两类键都在 `xsite:` 下混放（装配见 internal/app/app.go 与 ops.go），所以这里用白名单而不是黑名单：
// 只有明确登记在 purgeable 里的前缀才可能被清，新增缓存时要同步登记，否则清不到。
// 绝对不要用 FLUSHDB 或 `xsite:*` 这种笼统的模式。
const (
	// 可清的读穿缓存
	keySiteConfig = "xsite:site_config:public" // 站点配置（单键）
	keyContent    = "xsite:content:"           // 前台内容读接口（S1-4）
	keyOpsCache   = "xsite:ops:"               // 运营下发：公告、广告、热搜词、榜单下发
	keyManifest   = "xsite:play:manifest:"     // HLS 清单原文（S1-8，产物不可变）
	keyVIP        = "xsite:vip:"               // 会员状态与按客户端套餐读缓存（S3-10）

	// 有状态数据，不可清。列在这里是为了让 KeyPrefixes() 能在页面上标出来，
	// 让运维知道「这些键存在但故意不清」，而不是以为漏了。
	keyAdminSession = "xsite:admin:sess:"       // 后台会话
	keyAdminFail    = "xsite:admin:login_fail:" // 登录失败计数
	keyFbSession    = "xsite:fb:sess:"          // 反馈社区会话
	keyFbFail       = "xsite:fb:login_fail:"    // 反馈社区登录失败计数
	keyFbIntake     = "xsite:fb:intake:"        // 开放提交配额
	keyFbIntakeIP   = "xsite:fb:intake_ip:"     // 开放提交 IP 限流
	keyLineHealth   = "xsite:line:health:"      // 线路探测健康分
	keyLineQuality  = "xsite:line:q:"           // 线路上报质量分
	keyLineReport   = "xsite:line:report:"      // 上报限流
	keyPlaySources  = "xsite:play:sources:"     // 选源限流
	keyPlayKey      = "xsite:play:key:"         // HLS 密钥限流
	keyAdStats      = "xsite:ad:"               // 广告去重、待落库计数、频控
	keyHotCounter   = "xsite:hot:"              // 热搜词待落库的搜索次数
	keyRankStore    = "xsite:rank:"             // 线上榜单有序集合
)

// PurgeScope 是清缓存的范围（docs/06 第 28 节）。
type PurgeScope string

const (
	ScopeSiteConfig PurgeScope = "site_config" // 站点设置：清配置缓存并令 version+1
	ScopeHome       PurgeScope = "home"        // 首页信息流
	ScopeList       PurgeScope = "list"        // 频道与标签列表
	ScopeDrama      PurgeScope = "drama"       // 剧详情（需要剧集编号）
	ScopePlay       PurgeScope = "play"        // 播放用的清单原文（需要剧集编号）
	ScopeTopic      PurgeScope = "topic"       // 专题
	ScopeRank       PurgeScope = "rank"        // 排行下发
	ScopeOps        PurgeScope = "ops"         // 运营下发：公告、广告、热搜词
	ScopeAll        PurgeScope = "all"         // 全部可清缓存
)

// scopeInfo 描述一个清理范围。
type scopeInfo struct {
	Scope PurgeScope
	Title string
	Desc  string
	// Patterns 是要清的键模式（Valkey 全局键，含 xsite: 前缀）。`*` 结尾表示按前缀扫描。
	Patterns []string
	// NeedsDrama 为真时必须带剧集编号，Patterns 由 patternsFor 按编号生成。
	NeedsDrama bool
}

// scopes 按页面上的展示顺序排列。
var scopes = []scopeInfo{
	{
		Scope: ScopeSiteConfig, Title: "站点设置",
		Desc:     "品牌、主题、菜单、SEO、功能开关。同时令站点版本 +1，作废前台与 CDN 上的旧配置（改了设置但前台没变化时用这个）。",
		Patterns: []string{keySiteConfig},
	},
	{
		Scope: ScopeHome, Title: "首页",
		Desc:     "首页信息流（探索接口）的各种排序与筛选组合。",
		Patterns: []string{keyContent + "explore:*"},
	},
	{
		Scope: ScopeList, Title: "列表",
		Desc:     "频道页与标签页的列表，含各排序与分页。",
		Patterns: []string{keyContent + "channel:*", keyContent + "tag:*"},
	},
	{
		Scope: ScopeDrama, Title: "剧详情",
		Desc:       "单部剧的详情（资料、标签、分集、相关推荐）。",
		NeedsDrama: true,
	},
	{
		Scope: ScopePlay, Title: "播放",
		Desc:       "单部剧各分集的 HLS 清单原文。转码产物本身不可变，一般只在换了存储源或副本异常后才需要清。",
		NeedsDrama: true,
	},
	{
		Scope: ScopeTopic, Title: "专题",
		Desc:     "专题列表与专题详情。",
		Patterns: []string{keyContent + "topic:*", keyContent + "topics:*"},
	},
	{
		Scope: ScopeRank, Title: "排行",
		Desc:     "排行榜的下发缓存。不动榜单本体，下次请求会重新读当前排名。",
		Patterns: []string{keyOpsCache + "rank:*"},
	},
	{
		Scope: ScopeOps, Title: "运营下发",
		Desc:     "公告、广告投放、热搜词。平时改完就自动失效，这里用于兜底。",
		Patterns: []string{keyOpsCache + "announcements", keyOpsCache + "ads", keyOpsCache + "hotsearch"},
	},
	{
		Scope: ScopeAll, Title: "全部",
		Desc:     "上面所有范围（含每一部剧的详情与清单原文、会员状态与套餐缓存），但不动会话、限流计数、尚未落库的统计与榜单本体。",
		Patterns: []string{keySiteConfig, keyContent + "*", keyOpsCache + "*", keyManifest + "*", keyVIP + "*"},
	},
}

// statefulPrefixes 是故意不清的有状态键，按用途归类后展示在页面上。
var statefulPrefixes = []KeyPrefix{
	{Prefix: keyAdminSession, Title: "后台会话", Purgeable: false, Reason: "清掉会让所有管理员被迫重新登录"},
	{Prefix: keyAdminFail, Title: "后台登录失败计数", Purgeable: false, Reason: "清掉等于解除暴力破解的锁定"},
	{Prefix: keyFbSession, Title: "反馈社区会话", Purgeable: false, Reason: "清掉会让社区用户掉线"},
	{Prefix: keyFbFail, Title: "反馈社区登录失败计数", Purgeable: false, Reason: "清掉等于解除锁定"},
	{Prefix: keyFbIntake, Title: "开放提交配额", Purgeable: false, Reason: "清掉等于重置调用方的每日额度"},
	{Prefix: keyFbIntakeIP, Title: "开放提交 IP 限流", Purgeable: false, Reason: "清掉等于重置限流额度"},
	{Prefix: keyLineHealth, Title: "线路探测健康分", Purgeable: false, Reason: "由 worker 周期覆写，清掉会让选线短时间按缺省分处理"},
	{Prefix: keyLineQuality, Title: "线路上报质量分", Purgeable: false, Reason: "客户端上报的滑动窗口数据，清掉会丢统计"},
	{Prefix: keyLineReport, Title: "线路上报限流", Purgeable: false, Reason: "清掉等于重置限流额度"},
	{Prefix: keyPlaySources, Title: "选源限流", Purgeable: false, Reason: "清掉等于重置限流额度"},
	{Prefix: keyPlayKey, Title: "HLS 密钥限流", Purgeable: false, Reason: "清掉等于重置限流额度"},
	{Prefix: keyAdStats, Title: "广告去重与待落库计数", Purgeable: false, Reason: "清掉会丢尚未落库的曝光与点击"},
	{Prefix: keyHotCounter, Title: "热搜词待落库计数", Purgeable: false, Reason: "清掉会丢尚未落库的搜索次数"},
	{Prefix: keyRankStore, Title: "榜单本体", Purgeable: false, Reason: "线上排名数据，清掉要等 worker 下一轮重算才恢复"},
}

// KeyPrefix 是页面上展示的一个键前缀。
type KeyPrefix struct {
	Prefix    string `json:"prefix"`
	Title     string `json:"title"`
	Purgeable bool   `json:"purgeable"`
	Reason    string `json:"reason"` // 不可清的原因；可清时为空
	Keys      int64  `json:"keys"`   // 当前键数量，由服务层填
}

// ScopeInfo 是对外描述一个清理范围。
type ScopeInfo struct {
	Scope      PurgeScope `json:"scope"`
	Title      string     `json:"title"`
	Desc       string     `json:"desc"`
	NeedsDrama bool       `json:"needs_drama"`
}

// Scopes 返回全部清理范围，供后台页面渲染选项。
func Scopes() []ScopeInfo {
	out := make([]ScopeInfo, 0, len(scopes))
	for _, s := range scopes {
		out = append(out, ScopeInfo{Scope: s.Scope, Title: s.Title, Desc: s.Desc, NeedsDrama: s.NeedsDrama})
	}
	return out
}

// lookupScope 按名称取范围定义。
func lookupScope(name PurgeScope) (scopeInfo, error) {
	for _, s := range scopes {
		if s.Scope == name {
			return s, nil
		}
	}
	names := make([]string, 0, len(scopes))
	for _, s := range scopes {
		names = append(names, string(s.Scope))
	}
	return scopeInfo{}, badRequest("清理范围无效（可选：%s）", strings.Join(names, "、"))
}

// patternsFor 返回该范围实际要清的键模式。需要剧集编号的范围按编号生成，其余忽略 dramaID。
func patternsFor(s scopeInfo, dramaID int64, prefixes []string) []string {
	if !s.NeedsDrama {
		return s.Patterns
	}
	switch s.Scope {
	case ScopeDrama:
		return []string{fmt.Sprintf("%sdrama:%d", keyContent, dramaID)}
	case ScopePlay:
		// 清单原文按产物前缀成键，一部剧的分集可能有多个产物，前缀由调用方从库里查出。
		out := make([]string, 0, len(prefixes))
		for _, p := range prefixes {
			out = append(out, keyManifest+p+"*")
		}
		return out
	default:
		return nil
	}
}
