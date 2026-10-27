// Package http 组装 Gin 路由与中间件链。
package http

import (
	"fmt"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"

	"xsite/service/internal/errs"
	"xsite/service/internal/http/admin"
	adminanalytics "xsite/service/internal/http/admin/analytics"
	adminbilling "xsite/service/internal/http/admin/billing"
	admincommunity "xsite/service/internal/http/admin/community"
	"xsite/service/internal/http/admin/content"
	admindomains "xsite/service/internal/http/admin/domains"
	adminfeedback "xsite/service/internal/http/admin/feedback"
	adminfinance "xsite/service/internal/http/admin/finance"
	"xsite/service/internal/http/admin/media"
	adminops "xsite/service/internal/http/admin/ops"
	adminplatform "xsite/service/internal/http/admin/platform"
	"xsite/service/internal/http/admin/resource"
	adminsecurity "xsite/service/internal/http/admin/security"
	adminsite "xsite/service/internal/http/admin/site"
	"xsite/service/internal/http/admin/storage"
	adminupload "xsite/service/internal/http/admin/upload"
	"xsite/service/internal/http/middleware"
	openfeedback "xsite/service/internal/http/open/feedback"
	"xsite/service/internal/http/public"
	publicauth "xsite/service/internal/http/public/auth"
	publiccommunity "xsite/service/internal/http/public/community"
	publiccontent "xsite/service/internal/http/public/content"
	publicevents "xsite/service/internal/http/public/events"
	publicfeedback "xsite/service/internal/http/public/feedback"
	publiclines "xsite/service/internal/http/public/lines"
	publicme "xsite/service/internal/http/public/me"
	publicops "xsite/service/internal/http/public/ops"
	publicpayment "xsite/service/internal/http/public/payment"
	publicplay "xsite/service/internal/http/public/play"
	publicsite "xsite/service/internal/http/public/site"
	publicvip "xsite/service/internal/http/public/vip"
	"xsite/service/internal/http/respond"
)

// Options 是构建路由所需的依赖与配置。
type Options struct {
	Dev            bool
	TrustedProxies []string
	EdgeAuth       middleware.EdgeAuthOptions
	APIEnc         middleware.APIEncryptionOptions
	Public         *public.Handlers
	// PublicContent 是前台内容读接口（S1-4），注册在 /api/v1 分组上。
	PublicContent *publiccontent.Handlers
	// Lines 是用户端选线与质量上报接口（S1-6d），注册在 /api/v1 分组上。
	Lines *publiclines.Handlers
	// Events 是用户端埋点采集接口（S4-1），注册在 /api/v1 分组上。
	Events *publicevents.Handlers
	// PublicPlay 是用户端播放接口（选源 S1-7），注册在 /api/v1 分组上。
	PublicPlay *publicplay.Handlers
	// PublicAuth 是用户端身份接口（游客签发、令牌刷新与登出 S1-10；注册 / 登录 S2-1），注册在 /api/v1 分组上。
	PublicAuth *publicauth.Handlers
	// PublicVIP 是会员套餐与会员状态接口（S3-10），注册在 /api/v1 分组上。
	PublicVIP *publicvip.Handlers
	// PublicPayment 是用户端订单接口；development 才会附加模拟完成路由。
	PublicPayment *publicpayment.Handlers
	// Me 是「我的」接口（改密、我的设备列表与下线 S2-1），注册在 /api/v1/me 分组上（经身份中间件要求注册用户）。
	Me    *publicme.Handlers
	Admin *admin.Handlers
	// Content 是后台内容管理接口（S1-3），注册在后台会话分组上。
	Content *content.Handlers
	// Resource 是后台资源机接口（S1-6），注册在后台会话分组上。
	Resource *resource.Handlers
	// Storage 是后台存储源接口（S1-6a），注册在后台会话分组上。
	Storage *storage.Handlers
	// Media 是后台转封装接口（S1-5），注册在后台会话分组上。
	Media *media.Handlers
	// Uploads 是后台图片与视频上传会话接口，注册在后台会话分组上。
	Uploads *adminupload.Handlers
	// Feedback 是反馈社区的后台接口（feedback/docs/07），注册在后台会话分组上。
	Feedback *adminfeedback.Handlers
	// PublicFeedback 是反馈社区的前台接口（feedback/docs/08），注册在 /api/v1 分组上。
	PublicFeedback *publicfeedback.Handlers
	// OpenFeedback 是反馈社区的开放提交接口（feedback/docs/07），注册在 /open-api 分组上：
	// 供站外网页直接调用，不加密、免回源鉴权，身份只认 X-Api-Key。
	OpenFeedback *openfeedback.Handlers
	// Ops 是后台「运营」目录的接口（公告、广告、热搜词、排行干预，docs/specs/A3-4.md），注册在后台会话分组上。
	Ops *adminops.Handlers
	// AdminBilling 是后台会员套餐配置接口（S3-15 / A3-5）；支付渠道凭据另行作为高敏模块接入。
	AdminBilling *adminbilling.Handlers
	// Community 是后台「社区」目录的接口（屏蔽词 S2-4；评论 / 弹幕 / 举报 / 用户随 S2-5 起），注册在后台会话分组上。
	Community *admincommunity.Handlers
	// Platform 是后台「系统」目录的平台接口（缓存管理 S4-4），注册在后台会话分组上。
	Platform *adminplatform.Handlers
	// Site 是后台「框架」目录的接口（静态页、前台菜单、页面区块，docs/specs/A3-1.md），注册在后台会话分组上。
	Site *adminsite.Handlers
	// Domains 是后台「系统」目录的域名与证书接口（A4-4，docs/06 第 38 节），注册在后台会话分组上。
	Domains *admindomains.Handlers
	// Security 是后台「系统」目录的安全中心接口（A4-4，docs/06 第 39 节，只读），注册在后台会话分组上。
	Security *adminsecurity.Handlers
	// Analytics 是后台「数据看板 / 剧集分析」只读接口（S4-3 / A4-1），注册在后台会话分组上。
	Analytics *adminanalytics.Handlers
	// Finance 是后台「运营」目录的收入与成本接口（A4-1 / S4-3，docs/06 第 23 节），注册在后台会话分组上。
	Finance *adminfinance.Handlers
	// PublicSite 是前台站点编排接口（静态页、首页区块），注册在 /api/v1 分组上。
	PublicSite *publicsite.Handlers
	// PublicOps 是前台运营类接口（公告、广告、热搜榜、榜单），注册在 /api/v1 分组上。
	PublicOps *publicops.Handlers
	// PublicCommunity 是前台社区接口（评论；弹幕 / 举报 / 表情随后续，S2-5），注册在 /api/v1 分组上。
	PublicCommunity *publiccommunity.Handlers
	// AdminOrigins 是运营后台页面的来源，/api/admin 写请求的 Origin 必须在其中。
	AdminOrigins []string
	Audit        admin.AuditRecorder
}

const maxBodyBytes = 1 << 20 // 1 MiB；上传接口单独放宽

// NewRouter 返回配置好中间件链的 Gin 引擎（顺序见 docs/03 第 1 节）。
func NewRouter(opt Options) (*gin.Engine, error) {
	if opt.Dev {
		gin.SetMode(gin.DebugMode)
	} else {
		gin.SetMode(gin.ReleaseMode)
	}

	// JSON 请求体出现未知字段时拒绝（docs/07 第 0.1 节：未知字段服务端拒绝）
	binding.EnableDecoderDisallowUnknownFields = true

	r := gin.New()
	// 只采信可信代理（CDN 回源、BFF）转发的客户端 IP；为空时不信任任何代理。
	if err := r.SetTrustedProxies(opt.TrustedProxies); err != nil {
		return nil, fmt.Errorf("可信代理配置无效: %w", err)
	}

	quiet := map[string]bool{"/api/v1/health": true, "/internal/ready": true}
	// 回源鉴权的豁免表按完整路径精确匹配，改路径时要一起改。
	// /open-api/feedback 由站外网页直接请求，既不经过 CDN 回源也拿不到 BFF 服务令牌，
	// 因此只能免回源鉴权，改由 X-Api-Key + Key 上的 Origin 白名单把关（feedback/docs/07 第 0.5 节）。
	opt.EdgeAuth.Exempt = map[string]bool{
		"/api/v1/health":     true,
		"/internal/ready":    true,
		"/open-api/feedback": true,
	}

	r.Use(
		middleware.Recover(),
		middleware.RequestID(),
		middleware.EdgeAuth(opt.EdgeAuth),
		middleware.AccessLog(quiet),
		middleware.SecureHeaders(!opt.Dev),
		// 必须在读请求体之前放宽长请求的连接期限：大文件上传与 SSE 都会超过 http.Server 的默认读写超时
		middleware.LongRequest(),
		middleware.BodyLimit(maxBodyBytes),
	)
	r.NoRoute(func(c *gin.Context) { respond.Error(c, errs.ErrNotFound) })
	r.NoMethod(func(c *gin.Context) { respond.Error(c, errs.ErrNotFound) })
	r.HandleMethodNotAllowed = true

	r.GET("/internal/ready", opt.Public.ReadyCheck)

	// 路径规范见 docs/07 第 0.5 节：前台接口在 /api/v1，后台接口在 /api/admin，/api 下没有其他命名空间。

	// 前台（用户端）接口报文加密（docs/10 第 5.3 节）。豁免表按完整路径精确匹配，改前缀时要一起改。
	opt.APIEnc.Exempt = map[string]bool{"/api/v1/health": true, "/api/v1/crypto/keys": true}
	v1 := r.Group("/api/v1", middleware.APIEncryption(opt.APIEnc))
	v1.GET("/health", opt.Public.Health)
	v1.GET("/crypto/keys", opt.Public.GetCryptoKeys)
	v1.GET("/site-config", opt.Public.GetSiteConfig)
	opt.PublicContent.Routes(v1)
	opt.Lines.Routes(v1)
	opt.Events.Routes(v1)
	opt.PublicPlay.Routes(v1)
	opt.PublicAuth.Routes(v1)
	opt.PublicVIP.Routes(v1)
	opt.PublicPayment.Routes(v1)
	opt.Me.Routes(v1)
	// 反馈社区前台（adr/0008）：H5 前台经自身服务端带服务令牌转发，命中 isInternalSSR 豁免，无需在前台实现 HPKE
	opt.PublicFeedback.Routes(v1)
	opt.PublicOps.Routes(v1)
	opt.PublicCommunity.Routes(v1)
	opt.PublicSite.Routes(v1)

	// 运营后台接口（docs/07 第 2 节）：不加密；Origin 白名单 + HttpOnly 会话 + CSRF + 审计，业务模块再按分组挂 RBAC。
	// 必须挂在根路由上（不能挂在 v1 下），否则会继承报文加密。业务模块注册在 Routes 返回的分组上。
	authed := opt.Admin.Routes(r, opt.AdminOrigins, opt.Audit)
	opt.Content.Routes(authed)
	opt.Resource.Routes(authed)
	opt.Storage.Routes(authed)
	opt.Media.Routes(authed)
	opt.Uploads.Routes(authed)
	opt.Feedback.Routes(authed)
	opt.Ops.Routes(authed)
	opt.AdminBilling.Routes(authed)
	opt.Community.Routes(authed)
	opt.Platform.Routes(authed)
	opt.Site.Routes(authed)
	opt.Domains.Routes(authed)
	opt.Security.Routes(authed)
	opt.Analytics.Routes(authed)
	opt.Finance.Routes(authed)

	// 开放接口（/open-api）：给站外系统直连的接口，不加密、不走后台会话，各自在 handler 内声明鉴权方式。
	// 其余 /open-api 能力在 S4-6 接入。
	open := r.Group("/open-api")
	opt.OpenFeedback.Routes(open)

	return r, nil
}
