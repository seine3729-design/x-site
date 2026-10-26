// Package app 负责依赖装配：创建连接、仓储、服务与路由。用手写构造函数注入，不用反射式 DI。
package app

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"
	"uuid"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/valkey-io/valkey-go"

	"xsite/service/internal/config"
	"xsite/service/internal/crypto/apienc"
	"xsite/service/internal/crypto/password"
	"xsite/service/internal/domain/admin"
	"xsite/service/internal/domain/analytics"
	"xsite/service/internal/domain/content"
	"xsite/service/internal/domain/contentimport"
	"xsite/service/internal/domain/domains"
	"xsite/service/internal/domain/feedback"
	"xsite/service/internal/domain/identity"
	"xsite/service/internal/domain/media"
	"xsite/service/internal/domain/moderation"
	"xsite/service/internal/domain/ops"
	"xsite/service/internal/domain/platform"
	"xsite/service/internal/domain/play"
	"xsite/service/internal/domain/resource"
	"xsite/service/internal/domain/resourcecheck"
	"xsite/service/internal/domain/resourcewarm"
	domainsecurity "xsite/service/internal/domain/security"
	"xsite/service/internal/domain/storage"
	"xsite/service/internal/domain/upload"
	"xsite/service/internal/domain/user"
	"xsite/service/internal/geoip"
	apphttp "xsite/service/internal/http"
	adminhttp "xsite/service/internal/http/admin"
	adminbillinghttp "xsite/service/internal/http/admin/billing"
	admincommunityhttp "xsite/service/internal/http/admin/community"
	contenthttp "xsite/service/internal/http/admin/content"
	admindomainshttp "xsite/service/internal/http/admin/domains"
	adminfeedbackhttp "xsite/service/internal/http/admin/feedback"
	mediahttp "xsite/service/internal/http/admin/media"
	adminopshttp "xsite/service/internal/http/admin/ops"
	adminplatformhttp "xsite/service/internal/http/admin/platform"
	resourcehttp "xsite/service/internal/http/admin/resource"
	adminsecurityhttp "xsite/service/internal/http/admin/security"
	adminsitehttp "xsite/service/internal/http/admin/site"
	storagehttp "xsite/service/internal/http/admin/storage"
	adminuploadhttp "xsite/service/internal/http/admin/upload"
	"xsite/service/internal/http/middleware"
	openfeedbackhttp "xsite/service/internal/http/open/feedback"
	"xsite/service/internal/http/public"
	publicauthhttp "xsite/service/internal/http/public/auth"
	publiccommunityhttp "xsite/service/internal/http/public/community"
	publiccontenthttp "xsite/service/internal/http/public/content"
	publiceventshttp "xsite/service/internal/http/public/events"
	publicfeedbackhttp "xsite/service/internal/http/public/feedback"
	publiclineshttp "xsite/service/internal/http/public/lines"
	publicmehttp "xsite/service/internal/http/public/me"
	publicopshttp "xsite/service/internal/http/public/ops"
	publicpaymenthttp "xsite/service/internal/http/public/payment"
	publicplayhttp "xsite/service/internal/http/public/play"
	publicsitehttp "xsite/service/internal/http/public/site"
	publicviphttp "xsite/service/internal/http/public/vip"
	"xsite/service/internal/jobs"
	"xsite/service/internal/netguard"
	"xsite/service/internal/store/blob"
	"xsite/service/internal/store/blob/s3probe"
	"xsite/service/internal/store/cache"
	"xsite/service/internal/store/pg"
	"xsite/service/internal/store/uploadstore"
)

// 同时进行的 Argon2id 计算上限：每次占 64 MiB，4 路最多约 256 MiB。
const maxConcurrentPasswordHashes = 4

// 审计异步队列长度；队列满时改为同步写入，不丢记录。
const auditQueueSize = 1024

// API 是 HTTP 进程的全部依赖。
type API struct {
	Handler http.Handler
	pool    *pgxpool.Pool
	valkey  valkey.Client
	audit   *platform.AuditRecorder
	// stopGeo 停掉 GeoIP 数据库的热更新循环（没配数据文件时为 nil）。
	stopGeo func()
	// stopInvalidate 停掉缓存失效的 Pub/Sub 订阅循环（S1-9）。
	stopInvalidate func()
}

// NewAPI 建立依赖并组装路由。
func NewAPI(ctx context.Context, cfg config.Config) (*API, error) {
	pool, err := pg.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, err
	}
	if cfg.AutoMigrate {
		if err := pg.Migrate(ctx, pool); err != nil {
			pool.Close()
			return nil, err
		}
	}

	vk, err := cache.NewClient(cfg.ValkeyURL)
	if err != nil {
		pool.Close()
		return nil, err
	}

	store := cache.New(vk, "xsite:")
	// 缓存失效总线（S1-9）：配置变更后经 Valkey Pub/Sub 广播到所有实例，让各实例清掉进程内的本地缓存
	invalidator := cache.NewInvalidator(vk, "xsite:invalidate")
	siteConfigRepo := pg.NewSiteConfigRepo(pool)
	siteSvc := platform.NewSiteConfigService(siteConfigRepo, store, invalidator)
	// 缓存管理（S4-4）：只清白名单内的键前缀（domain/platform/cachekeys.go），
	// 清「站点设置」走 siteSvc.BumpVersion 以便一次完成 version+1、清二级缓存与跨实例广播。
	cacheSvc := platform.NewCacheService(cache.NewPurger(vk), siteSvc, siteConfigRepo)
	// 配置导出导入（S4-4）：备份文件签名用 CONFIG_BACKUP_KEYS，留空时不签名也不验签（本地开发）。
	backupSigner, err := cfg.Platform.BackupSigner()
	if err != nil {
		vk.Close()
		pool.Close()
		return nil, fmt.Errorf("CONFIG_BACKUP_KEYS: %w", err)
	}
	configSvc := platform.NewConfigService(pg.NewConfigBackupRepo(pool), backupSigner, siteSvc)
	// 审计日志查询与哈希链校验（S4-7）：写入侧在 pg.AuditRepo，这里是只读的查询与校验
	auditQueryRepo := pg.NewAuditQueryRepo(pool)
	auditSvc := platform.NewAuditService(auditQueryRepo, auditQueryRepo)
	// 回收站（S4-7）：内容类数据软删后在这里还原或彻底删除，过期由 worker 的清理任务物理删除
	trashSvc := platform.NewTrashService(pg.NewTrashRepo(pool), cfg.Platform.TrashRetention)
	// 功能开关与年龄门（S3-3）：存在 site_config 的 features / age_gate 段，保存后让版本 +1 并广播失效
	featureSvc := platform.NewFeatureService(pg.NewFeatureRepo(pool), siteSvc)
	// 白标的品牌、配色与布局（docs/06 第 16 节，docs/specs/A3-3.md）：三段各自保存，保存后经 siteSvc 刷新版本
	brandingSvc := platform.NewBrandingService(pg.NewBrandingRepo(pool), siteSvc)
	// SEO 与统计代码（docs/06 第 17 节）：统计代码原样注入页面，接口另要 seo.analytics_code
	seoSvc := platform.NewSEOService(pg.NewSEORepo(pool), siteSvc)
	// 开放平台 API Key（A4-3）：只存哈希与开头几位，完整密钥仅生成时返回一次。
	// 调用方鉴权（Authenticate）随 S4-6 的采集接口接入，这里先把密钥管理做完
	apiKeySvc := platform.NewAPIKeyService(pg.NewAPIKeyRepo(pool))

	// 配置校验已确认密钥可解析，这里的错误只是兜底
	keyring, err := apienc.ParseKeyring(cfg.APIEncKeys)
	if err != nil {
		vk.Close()
		pool.Close()
		return nil, fmt.Errorf("API_ENC_KEYS: %w", err)
	}

	adminRepo := pg.NewAdminRepo(pool)
	hasher := password.NewHasher(password.DefaultParams, maxConcurrentPasswordHashes)
	// 后台二次验证（S2-6）：TOTP 种子信封加密入库，KEK 来自 ADMIN_TOTP_KEKS（未配置时 keyring 为 nil，
	// TOTP 接口返回 503、登录也不强制二次验证）。MFAStore 同时承担待确认种子、登录票据与高敏重新验证标记。
	adminTOTPKeyring, err := cfg.AdminTOTPKeyring()
	if err != nil {
		vk.Close()
		pool.Close()
		return nil, fmt.Errorf("ADMIN_TOTP_KEKS: %w", err)
	}
	adminMFAStore := cache.NewAdminMFAStore(vk, "xsite:admin:mfa:")
	adminMFA := admin.NewMFAService(adminRepo, adminMFAStore, adminTOTPKeyring)
	// Passkey / WebAuthn（S2-6）：用公钥密码，不需要 KEK；RPID 取后台首个来源的主机名，RPOrigins 为 ADMIN_ORIGINS。
	adminPasskey, err := admin.NewPasskeyService(admin.PasskeyConfig{
		RPID:        originHost(cfg.AdminOrigins[0]),
		DisplayName: "x-site 运营后台",
		Origins:     cfg.AdminOrigins,
	}, adminRepo, adminMFAStore)
	if err != nil {
		vk.Close()
		pool.Close()
		return nil, fmt.Errorf("初始化后台 Passkey: %w", err)
	}
	adminSvc := admin.NewService(
		adminRepo,
		cache.NewAdminSessionStore(vk, "xsite:admin:sess:"),
		cache.NewCounter(vk, "xsite:admin:login_fail:"),
		hasher,
		admin.WithMFA(adminMFA, adminMFAStore),
		admin.WithPasskey(adminPasskey),
	)
	adminMgr := admin.NewManager(adminRepo, hasher)
	// 高敏操作重新验证（S2-6）：启动时载入高敏权限点集合，并注入重新验证窗口的查询实现（同一个 MFAStore）。
	// 只对已绑定二次验证的管理员生效（docs/03 第 2.3 节）。
	sensitivePerms, err := adminMgr.SensitivePermissions(ctx)
	if err != nil {
		vk.Close()
		pool.Close()
		return nil, fmt.Errorf("载入高敏权限点: %w", err)
	}
	adminhttp.SetSensitivePermissions(sensitivePerms)
	adminhttp.SetReauthChecker(adminMFAStore)
	auditRec := platform.NewAuditRecorder(pg.NewAuditRepo(pool), auditQueueSize)

	// API 进程的 River 客户端只入队、不消费（没有 Queues、不调用 Start），任务由 worker 进程执行。
	riverClient, err := river.NewClient(riverpgxv5.New(pool), &river.Config{})
	if err != nil {
		_ = auditRec.Close(ctx)
		vk.Close()
		pool.Close()
		return nil, fmt.Errorf("创建 River 客户端: %w", err)
	}
	// 资源机测连与列目录经 River 交给 worker 执行（docs/03 第 9.7 节：连接由 worker 出口发起）
	resourceSvc := resource.NewService(pg.NewResourceRepo(pool), &jobs.FTPProbeClient{River: riverClient}, resource.Policy{
		ExplicitTLSOnly: !cfg.IsDev(),
		Egress:          netguard.Policy{AllowPrivate: cfg.EgressPrivateAllowed()},
	})
	// 存储源（S1-6a）：凭据信封加密入库，连通性测试在 API 进程内执行（对象存储是主动 HTTPS 出站，自定义端点经 netguard 校验）
	storageKeyring, err := cfg.Storage.Keyring()
	if err != nil {
		_ = auditRec.Close(ctx)
		vk.Close()
		pool.Close()
		return nil, fmt.Errorf("STORAGE_KEKS: %w", err)
	}
	egress := netguard.Policy{AllowPrivate: cfg.EgressPrivateAllowed()}
	storageRepo := pg.NewStorageRepo(pool)
	storagePolicy := storage.Policy{HTTPSOnly: !cfg.IsDev(), Egress: egress}
	storageHealth := cache.NewLineHealthStore(vk, "xsite:line:health:")
	storageSvc := storage.NewService(storageRepo, storageKeyring, &s3probe.Prober{Egress: egress}, storagePolicy)
	// 后台上传按 storage_settings 选中的图片/视频主存储写入；读取线路/CDN 不参与写入。
	uploadStores := uploadstore.New(storageRepo, storageSvc, egress)
	// 线路签名（S1-6e）：hmac_token / cdn_vendor 的密钥来自 LINE_SIGN_KEYS（不入库），
	// s3_presign 用存储源的只读凭据（由 storageSvc 解密）。
	signKeyring, err := storage.ParseSignKeyring(cfg.Storage.SignKeys)
	if err != nil {
		_ = auditRec.Close(ctx)
		vk.Close()
		pool.Close()
		return nil, fmt.Errorf("LINE_SIGN_KEYS: %w", err)
	}
	signSvc := storage.NewSignService(storageRepo, storageSvc, signKeyring, &s3probe.Presigner{Egress: egress}, nil)
	lineSvc := storage.NewLineService(storageRepo, &s3probe.LineProber{Egress: egress}, storagePolicy, signSvc)
	settingsSvc := storage.NewSettingsService(storageRepo)
	// 健康分（S1-6d）：探测分由 worker 周期写入 storageHealth，客户端上报聚合在 storageQuality，
	// 读取时按 40%/60% 融合，因此选线预览、用户端选线与健康看板三处分数同源。
	storageQuality := cache.NewLineQualityStore(vk, "xsite:line:q:")
	// GeoIP（S1-6c）：没配 GEOIP_XDB_PATH 时地区一律未知，只有全局兜底线路会命中
	geoResolver, geoReloader, err := geoip.NewStorageResolver(cfg.Storage.GeoIPXDBPath)
	if err != nil {
		_ = auditRec.Close(ctx)
		vk.Close()
		pool.Close()
		return nil, fmt.Errorf("GEOIP_XDB_PATH: %w", err)
	}
	previewSvc := storage.NewPreviewService(storageRepo, geoResolver, storageHealth, storageQuality)
	healthSvc := storage.NewHealthService(storageRepo, storageHealth, storageQuality, cfg.Storage.HealthProbeEnabled)
	// 用户端选线（S1-6d）：GET /api/v1/lines 选线；POST /api/v1/lines/report 聚合质量到 Valkey
	linesSvc := storage.NewLinesService(storageRepo, geoResolver, storageHealth, storageQuality)
	reportSvc := storage.NewReportService(geoResolver, storageQuality)
	reportLimiter := cache.NewCounter(vk, "xsite:line:report:")
	// 批量运维（S1-6f）：手动补副本与迁移只扫描对象并入队，实际搬运由 worker 执行
	mediaEnqueuer := &jobs.MediaEnqueuer{River: riverClient}
	// 资源体检（S4-5）：API 侧只负责列表、清空与入队，真正的探测在 worker（不暴露 API 节点 IP，
	// 也不让批量出站请求占着 API 进程）。
	resourceCheckSvc := resourcecheck.NewService(
		pg.NewResourceCheckRepo(pool), storageRepo, signSvc,
		&s3probe.AssetProber{Egress: egress},
		&jobs.ResourceCheckEnqueuer{River: riverClient},
	)
	// 资源预热（S4-5）：API 侧只负责开始、停止与看进度，真正的拉取在 worker。
	// 进度存在库里而不是 Valkey（docs/03 第 9.5 节写的是 Valkey）：运营要能关掉页面回来继续看，
	// 库表天然持久；更新频率是每集一次而不是每片一次，不是高频写。
	resourceWarmSvc := resourcewarm.NewControlService(
		pg.NewResourceWarmRepo(pool),
		&jobs.ResourceWarmEnqueuer{River: riverClient},
	)

	manageSvc := storage.NewManageService(storageRepo, storage.NewPlacement(storageRepo), mediaEnqueuer, mediaEnqueuer)
	// 接管历史产物（S1-6f）：API 只扫描并入队，存在性校验与登记在 worker 执行
	adoptSvc := storage.NewAdoptService(storageRepo, storageSvc, nil, mediaEnqueuer)
	// 孤立产物清理（S1-6f）：API 侧提供预览与手动触发，周期清理在 worker
	cleanupSvc := storage.NewCleanupService(storageRepo, storageSvc,
		&s3probe.Copier{Egress: egress}, cfg.Storage.OrphanRetention)
	// 会员核心（S3-10）：套餐列表与会员状态走 Valkey 读缓存；后续支付入账在事务提交后失效该状态缓存。
	vipSvc := newVIPService(pool, vk)
	// development 专用模拟订单：不接真实凭据，production 由 OrderService 拒绝创建且不注册模拟完成路由。
	vipOrderSvc := newVIPOrderService(pool, vipSvc, cfg.IsDev())
	// 会员套餐后台配置（S3-15 / A3-5）：套餐价格与上下架保存后清除同一组前台套餐缓存。
	vipPlanAdminSvc := newVIPPlanAdminService(pool, vk)
	// 选源（S1-7）：观看权限判定 + 与 /api/v1/lines 同一套选线 + 签发播放令牌（PLAY_TOKEN_KEYS，只有 API 进程使用）。
	// 已登录注册用户的 VIP 权益由 S3-10 的 billing.Service 实时判定；游客仍一律非会员。
	playKeys, err := cfg.Play.Keyring()
	if err != nil {
		_ = auditRec.Close(ctx)
		vk.Close()
		pool.Close()
		return nil, fmt.Errorf("PLAY_TOKEN_KEYS: %w", err)
	}
	playSvc := play.NewService(pg.NewPlayRepo(pool), linesSvc, playKeys, vipSvc)
	// 用户端身份（S1-10）：游客签发与令牌轮换。Access Token 用 JWT_SIGNING_KEYS 签发/校验（无状态，只有 API 进程），
	// Refresh Token 只存 SHA-256，轮换与复用检测落在 PostgreSQL 的 user_session（docs/02 第 5.1.1 节）。
	authKeys, err := cfg.Auth.Keyring()
	if err != nil {
		_ = auditRec.Close(ctx)
		vk.Close()
		pool.Close()
		return nil, fmt.Errorf("JWT_SIGNING_KEYS: %w", err)
	}
	identitySvc := identity.NewService(pg.NewSessionRepo(pool), authKeys)
	// 注册用户（S2-1）：注册 / 登录 / 改密。令牌与会话族复用 identitySvc，密码哈希复用全局 hasher。
	// 防撞库（S2-2）：登录失败计数 + 失败过多要求 PoW 人机验证（nonce 一次性存 Valkey）。
	userSvc := user.NewService(pg.NewUserRepo(pool), identitySvc, hasher,
		cache.NewCounter(vk, "xsite:auth:login_fail:"),
		user.NewGuard(cache.NewChallengeStore(vk, "xsite:auth:challenge:")))
	// 后台用户管理（S2-7）：列表 / 详情 / 改账号状态。封禁要即时生效，所以把 identitySvc 交进去吊销会话。
	userAdminSvc := user.NewAdminService(pg.NewUserAdminRepo(pool), identitySvc)
	// 「我的」（S2-3）：收藏、点赞、观看历史（Valkey 合并写）、站内信。与 worker 共用同一装配。
	meSvc := newMeService(pool, vk)
	// 屏蔽词（S2-4）：进程内常驻 Aho-Corasick 自动机，词表变更后重建并经 invalidator 广播、各实例重建。
	blockwordSvc := moderation.NewService(pg.NewBlockwordRepo(pool), invalidator)
	// 评论先审后显策略（S2-5，§24）：后台读写 + 供评论服务判断（替代原固定关闭的桩）。
	commentPolicySvc := newPolicyService(pool)
	// 评论 / 弹幕审核（S2-5，§24）：审核列表 + 通过 / 隐藏 / 删除；hit_words 扫描复用屏蔽词 Service。
	moderationSvc := newModerationService(pool, blockwordSvc)
	// 社区评论（S2-5）：发表过屏蔽词判定 + 先审后显 + 禁言 + 防刷；支持注册用户与游客；
	// 回复 / 点赞经 River 异步投递站内信给注册用户作者。
	commentSvc := newCommentService(pool, vk, blockwordSvc, siteFeatures{siteSvc}, commentPolicySvc, &jobs.CommunityNotifyEnqueuer{River: riverClient})
	// 社区弹幕（S2-5）：过屏蔽词 + 字数 / 颜色白名单 + 先审后显 + 禁言；支持注册用户与游客，不做去重。
	danmakuSvc := newDanmakuService(pool, blockwordSvc, siteFeatures{siteSvc})
	// 社区举报（S2-5）：对评论 / 弹幕 / 剧 / 评论图片举报；同一举报人对同一对象 24 小时去重。
	communityReportSvc := newReportService(pool, vk)
	// 社区表情（S2-5）：前台下发表情面板，后台维护表情包 / 表情。
	emojiSvc := newEmojiService(pool)
	// CSV 批量导入（S4-6）：复用同一个常驻屏蔽词自动机，不另起一个 ——
	// 另起一个就得自己 Reload 并订阅失效广播，词表一改两边立刻不一致。
	// 采集场景只认 block 类命中（docs/02 第 5.7 节）。
	contentImportSvc := contentimport.NewService(
		pg.NewContentImportRepo(pool), blockwordMatcher{svc: blockwordSvc},
	)
	// 清单与密钥（S1-8，app/play.go）：校验同一套播放令牌，切片地址用线路签名器，内容密钥用 MEDIA_KEKS 解开
	hlsSvc, err := newHLSService(cfg, pool, vk, playKeys, storageRepo, storageSvc, signSvc, egress)
	if err != nil {
		_ = auditRec.Close(ctx)
		vk.Close()
		pool.Close()
		return nil, err
	}
	// 后台试播（docs/06 第 37 节）：另一套令牌密钥 + 不要求上架的仓储，其余与前台同一条链路
	episodePreviewSvc, episodePreviewHLS, err := newPreviewServices(cfg, pool, vk, linesSvc, storageRepo, storageSvc, signSvc, egress)
	if err != nil {
		_ = auditRec.Close(ctx)
		vk.Close()
		pool.Close()
		return nil, err
	}
	// 转封装任务（S1-5）与任务行在同一事务内入队，由带 FFmpeg 的 worker 执行
	mediaSvc := media.NewService(pg.NewMediaRepo(pool, func(ctx context.Context, tx pgx.Tx, id uuid.UUID) error {
		_, err := riverClient.InsertTx(ctx, tx, jobs.TranscodeArgs{JobID: id.String()}, nil)
		return err
	}))
	// 后台媒体上传：浏览器只写配置的 storage_provider；此处仅为视频转码准备私有暂存桶客户端。
	stageStore, err := blob.New(blob.Config{
		Endpoint: cfg.Media.S3Endpoint, Region: cfg.Media.S3Region, AccessKey: cfg.Media.S3AccessKey,
		SecretKey: cfg.Media.S3SecretKey, PathStyle: cfg.Media.S3PathStyle,
	})
	if err != nil {
		_ = auditRec.Close(ctx)
		vk.Close()
		pool.Close()
		return nil, fmt.Errorf("初始化后台上传对象存储: %w", err)
	}
	adminUploadSvc := upload.NewService(pg.NewAdminMediaUploadRepo(pool, func(ctx context.Context, tx pgx.Tx, id uuid.UUID) error {
		_, err := riverClient.InsertTx(ctx, tx, jobs.AdminVideoUploadArgs{UploadID: id.String()}, nil)
		return err
	}))

	// 反馈社区（feedback/docs/adr/0008）：与短剧业务同库不同表（fb_ 前缀），复用同一套会话、审计与 RBAC 基础设施。
	// 社区用户会话走 Valkey 独立键前缀；令牌由 H5 前台服务端以 Authorization: Bearer 转发，不用 Cookie。
	feedbackRepo := pg.NewFeedbackRepo(pool)
	fbIdentity := feedback.NewIdentityService(
		feedbackRepo,
		cache.NewFeedbackSessionStore(vk, "xsite:fb:sess:"),
		cache.NewCounter(vk, "xsite:fb:login_fail:"),
		hasher,
	)
	fbCommunity := feedback.NewCommunityService(feedbackRepo)
	fbManage := feedback.NewManageService(feedbackRepo)
	fbTopics := feedback.NewTopicService(pg.NewFeedbackTopicRepo(pool))
	// 开放提交接口（/open-api/feedback）：Key 维度的突发与每日配额记在 Valkey，键前缀独立于登录失败计数
	fbIntake := feedback.NewIntakeService(pg.NewFeedbackAPIKeyRepo(pool), cache.NewCounter(vk, "xsite:fb:intake:"))
	opsRepo := pg.NewOpsRepo(pool)
	announcementSvc := ops.NewAnnouncementService(opsRepo, store)
	// 广告的去重、计数与频控、热搜词的搜索计数在 Valkey，键前缀与 worker 的落库任务共用（app/ops.go）
	adSvc := newAdService(pool, vk)
	hotSvc := newHotsearchService(pool, vk)
	// 榜单：worker 每 5 分钟重新排名；后台「马上重新排名」经 River 交给 worker，前台读时检查 features.ranks
	rankSvc := newRankService(pool, vk, siteFeatures{siteSvc}, &jobs.RankRecomputeClient{River: riverClient})
	// 前台内容读接口（S1-4）：读穿缓存 + 只读查询，TTL 见 docs/03 第 3.2 节。区块的自动选剧复用它的规则引擎
	publicContentRepo := pg.NewPublicContentRepo(pool)
	publicContentSvc := content.NewPublicService(publicContentRepo, store)
	// 前台搜索（S3-4，docs/specs/S3-4.md）：每次搜索计入热搜（给后台选品），联想受 features.search_suggest 控制
	searchSvc := content.NewSearchService(publicContentRepo, store, siteFeatures{siteSvc}, hotSvc)
	// 站点编排（docs/specs/A3-1.md）：静态页、前台菜单与防丢地址、页面区块。
	// 菜单与页脚页随站点配置下发，防丢地址直接存在配置里，改动都经 siteSvc.BumpVersion 让各实例秒级生效；
	// 区块自己有一份读穿缓存，选剧走内容域的规则引擎，排行榜区块走运营域的榜单。
	siteSvcs := newSiteServices(pool, store, siteSvc, publicContentSvc, rankBlocks{rankSvc})
	// 域名与证书（A4-4，docs/06 第 38 节）：域名池、手动生成 / 上传 SSL 证书、签名清单发布与回滚、健康看板。
	domainSvc := domains.NewService(pg.NewDomainRepo(pool))
	// 安全中心（A4-4，docs/06 第 39 节，只读）：登录记录读 admin_login_log；
	// 密钥轮换记录从各密钥环境变量收集版本号（只取 kid、不含密钥本身）；风控概览的实时计数源待接入。
	securitySvc := domainsecurity.NewService(pg.NewSecurityRepo(pool), collectKeyVersions(cfg))
	// 埋点采集（S4-1，docs/03 第 8.1 节）：批量 COPY 进 event_raw 分区表，汇总与看板接口随 S4-2/S4-3。
	eventSvc := analytics.NewService(pg.NewAnalyticsRepo(pool))

	router, err := apphttp.NewRouter(apphttp.Options{
		Dev:            cfg.IsDev(),
		TrustedProxies: cfg.TrustedProxies,
		EdgeAuth: middleware.EdgeAuthOptions{
			Enforced:     cfg.EdgeAuthEnforced(),
			EdgeSecret:   cfg.EdgeSharedSecret,
			ServiceToken: cfg.BFFServiceToken,
		},
		APIEnc: middleware.APIEncryptionOptions{
			Keyring:  keyring,
			Required: cfg.APIEncEnforced(),
		},
		Public: &public.Handlers{
			Env:        cfg.AppEnv,
			SiteConfig: siteSvc,
			// 前台菜单与页脚页随站点配置一起下发（docs/07 第 1 节、docs/specs/A3-1.md）
			Site:    siteSvcs.sections(),
			APIKeys: keyring,
			Ready: map[string]public.Pinger{
				"postgres": pool,
				"valkey":   valkeyPinger{vk},
			},
		},
		Admin: &adminhttp.Handlers{
			Auth:    adminSvc,
			Manage:  adminMgr,
			MFA:     adminMFA,
			Passkey: adminPasskey,
			// 本地开发走 http://localhost，不能用 Secure Cookie
			Cookie: adminhttp.NewSessionCookie(!cfg.IsDev()),
		},
		PublicContent: &publiccontenthttp.Handlers{
			Svc: publicContentSvc, Search: searchSvc,
			SuggestLimiter: cache.NewCounter(vk, "xsite:search:suggest:"),
		},
		// 用户端选线与质量上报（S1-6d），注册在 /api/v1 分组上（走报文加密）
		Lines: &publiclineshttp.Handlers{Svc: linesSvc, Report: reportSvc, Limiter: reportLimiter, Dev: cfg.IsDev()},
		// 埋点采集（S4-1）：按设备每分钟固定窗口限流，键前缀独立
		Events: &publiceventshttp.Handlers{Svc: eventSvc, Limiter: cache.NewCounter(vk, "xsite:events:")},
		// 选源（S1-7）：选源桶每设备每分钟 60 次（S0-7 接入通用限流前用固定窗口计数）；
		// 清单与密钥（S1-8）：只收加密请求，密钥桶每设备每集每分钟 20 次
		PublicPlay: &publicplayhttp.Handlers{
			Svc: playSvc, HLS: hlsSvc, Dev: cfg.IsDev(),
			Limiter:    cache.NewCounter(vk, "xsite:play:sources:"),
			KeyLimiter: cache.NewCounter(vk, "xsite:play:key:"),
		},
		// 用户端身份：游客签发 / 刷新 / 登出（S1-10）与注册 / 登录（S2-1）。游客与注册各自按 IP 前缀限流（前缀不同）
		PublicAuth: &publicauthhttp.Handlers{
			Svc:             identitySvc,
			Users:           userSvc,
			Limiter:         cache.NewCounter(vk, "xsite:auth:guest:"),
			RegisterLimiter: cache.NewCounter(vk, "xsite:auth:register:"),
		},
		// 会员核心（S3-10）：公开套餐列表与注册用户自己的会员状态。
		PublicVIP: &publicviphttp.Handlers{Svc: vipSvc, Resolver: identitySvc, MockEnabled: cfg.IsDev()},
		// development 模拟订单（S3-11）：production 服务拒绝创建且不注册模拟完成端点。
		PublicPayment: &publicpaymenthttp.Handlers{Orders: vipOrderSvc, Resolver: identitySvc},
		// 「我的」（S2-1 改密 / 设备；S2-3 资料、收藏、点赞、历史、消息）；整组经身份中间件要求注册用户（role=user）
		Me: &publicmehttp.Handlers{
			Accounts: userSvc, Sessions: identitySvc, Me: meSvc, Profiles: userSvc, Resolver: identitySvc,
		},
		Content: &contenthttp.Handlers{
			Svc:            content.NewService(pg.NewContentRepo(pool)),
			ResourceChecks: resourceCheckSvc,
			ResourceWarm:   resourceWarmSvc,
			ContentImports: contentImportSvc,
		},
		Resource: &resourcehttp.Handlers{Svc: resourceSvc},
		Storage: &storagehttp.Handlers{
			Svc: storageSvc, LineSvc: lineSvc, SettingsSvc: settingsSvc,
			PreviewSvc: previewSvc, HealthSvc: healthSvc, ManageSvc: manageSvc,
			AdoptSvc: adoptSvc, CleanupSvc: cleanupSvc,
		},
		Media: &mediahttp.Handlers{Svc: mediaSvc, PreviewSvc: episodePreviewSvc, PreviewHLS: episodePreviewHLS},
		Uploads: &adminuploadhttp.Handlers{
			Svc: adminUploadSvc, Media: mediaSvc, Stores: uploadStores,
			Source: stageStore.Bucket(cfg.Media.SourceBucket),
		},
		Feedback: &adminfeedbackhttp.Handlers{Svc: fbManage, Topics: fbTopics, Keys: fbIntake},
		PublicFeedback: &publicfeedbackhttp.Handlers{
			Identity:  fbIdentity,
			Community: fbCommunity,
			Topics:    fbTopics,
		},
		// 开放提交接口：按 IP 的粗粒度限流只为挡住无效 Key 的扫库，Key 维度的配额在领域层
		OpenFeedback: &openfeedbackhttp.Handlers{
			Svc:     fbIntake,
			Limiter: cache.NewCounter(vk, "xsite:fb:intake_ip:"),
		},
		// 运营（docs/specs/A3-4.md）：后台管理与前台下发共用同一组服务，后台修改后清掉前台读穿缓存
		Ops: &adminopshttp.Handlers{Announcements: announcementSvc, Ads: adSvc, Hotsearch: hotSvc, Ranks: rankSvc},
		// 会员套餐（S3-15 / A3-5）：固定五卡配置，保存后清除前台套餐缓存。
		AdminBilling: &adminbillinghttp.Handlers{Plans: vipPlanAdminSvc},
		// 社区（S2-4 / S2-5 / S2-7）：屏蔽词、评论与弹幕审核、举报队列、表情、用户管理
		Community: &admincommunityhttp.Handlers{
			Blockwords: blockwordSvc, Reports: communityReportSvc, Moderation: moderationSvc,
			Policy: commentPolicySvc, Emojis: emojiSvc, Users: userAdminSvc,
		},
		// 前台社区（S2-5）：评论、弹幕、举报、表情（注册用户与游客）。
		PublicCommunity: &publiccommunityhttp.Handlers{Comments: commentSvc, Danmaku: danmakuSvc, Reports: communityReportSvc, Emojis: emojiSvc, Resolver: identitySvc},
		Platform: &adminplatformhttp.Handlers{
			Cache: cacheSvc, Config: configSvc, Audit: auditSvc, Trash: trashSvc,
			Features: featureSvc, APIKeys: apiKeySvc, Branding: brandingSvc, SEO: seoSvc,
		},
		PublicOps: &publicopshttp.Handlers{Announcements: announcementSvc, Ads: adSvc, Hotsearch: hotSvc, Ranks: rankSvc},
		// 框架（docs/specs/A3-1.md）：后台管理与前台下发共用同一组服务
		Site: &adminsitehttp.Handlers{
			Pages: siteSvcs.pages, Menus: siteSvcs.menus, Addresses: siteSvcs.addresses, Blocks: siteSvcs.blocks,
		},
		Domains:      &admindomainshttp.Handlers{Svc: domainSvc},
		Security:     &adminsecurityhttp.Handlers{Svc: securitySvc},
		PublicSite:   &publicsitehttp.Handlers{Pages: siteSvcs.pages, Home: siteSvcs.blocks},
		AdminOrigins: cfg.AdminOrigins,
		Audit:        auditRec,
	})
	if err == nil {
		err = checkDeclaredPermissions(ctx, adminMgr)
	}
	if err == nil {
		// 启动即载入屏蔽词词表并构建自动机（DB 已连通、迁移已执行，失败视为启动错误）
		if rerr := blockwordSvc.Reload(ctx); rerr != nil {
			err = fmt.Errorf("加载屏蔽词词表: %w", rerr)
		}
	}
	if err != nil {
		_ = auditRec.Close(ctx)
		vk.Close()
		pool.Close()
		return nil, err
	}

	return &API{
		Handler: router, pool: pool, valkey: vk, audit: auditRec,
		stopGeo: watchGeoIP(geoReloader, cfg.Storage.GeoIPReloadInterval),
		// 订阅缓存失效广播：收到站点配置变更就清本实例的本地缓存（S1-9，各实例各自订阅、不经 River）
		stopInvalidate: invalidator.Subscribe(func(topic string) {
			switch topic {
			case platform.SiteConfigTopic:
				siteSvc.OnInvalidate(context.Background())
			case moderation.BlockwordTopic:
				blockwordSvc.OnInvalidate(context.Background())
			}
		}),
	}, nil
}

// checkDeclaredPermissions 确认路由声明的权限点都在数据库的权限点目录中。
// 编码拼错或忘了写迁移时启动失败，而不是上线后接口永远 403。
func checkDeclaredPermissions(ctx context.Context, mgr *admin.Manager) error {
	codes, err := mgr.PermissionCodes(ctx)
	if err != nil {
		return fmt.Errorf("读取权限点目录（迁移是否已执行？）: %w", err)
	}
	var missing []string
	for _, c := range adminhttp.DeclaredPermissions() {
		if !slices.Contains(codes, c) {
			missing = append(missing, c)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("路由声明的权限点不在 admin_permission 中，请补迁移：%s", strings.Join(missing, ", "))
	}
	return nil
}

// Close 在 HTTP 服务停止后调用：先写完审计队列，再释放连接。
func (a *API) Close() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if a.stopGeo != nil {
		a.stopGeo()
	}
	// 先停订阅循环（它用着 vk 的专用连接），再关 Valkey
	if a.stopInvalidate != nil {
		a.stopInvalidate()
	}
	if err := a.audit.Close(ctx); err != nil {
		slog.Error("关闭审计队列", "err", err)
	}
	a.valkey.Close()
	a.pool.Close()
}

// watchGeoIP 周期检查 GeoIP 数据文件是否更新并原子替换。
// API 进程才是按地区选线的那个，所以热更新循环放在这里（不经 River：这是纯进程内状态，
// 每个实例都要各自重载，走队列只会让某一个实例收到任务）。
func watchGeoIP(r *geoip.Resolver, interval time.Duration) func() {
	if r == nil {
		return nil
	}
	if interval <= 0 {
		interval = 10 * time.Minute
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if _, err := r.Reload(); err != nil {
					// 文件损坏或暂时读不到时继续用旧数据，不影响解析能力
					slog.Warn("GeoIP 数据库热更新失败，继续使用当前数据", "err", err)
				}
			}
		}
	}()
	return cancel
}

type valkeyPinger struct{ c valkey.Client }

func (p valkeyPinger) Ping(ctx context.Context) error {
	if err := p.c.Do(ctx, p.c.B().Ping().Build()).Error(); err != nil {
		return fmt.Errorf("valkey ping: %w", err)
	}
	return nil
}

// collectKeyVersions 从各密钥环境变量收集当前启用的版本号（只取 kid、不含密钥本身），供安全中心的密钥轮换记录展示。
func collectKeyVersions(cfg config.Config) []domainsecurity.KeyVersion {
	var out []domainsecurity.KeyVersion
	add := func(purpose, title, raw string) {
		out = append(out, domainsecurity.ParseKeyVersions(purpose, title, raw)...)
	}
	add("jwt_sign", "登录令牌签名（JWT）", cfg.Auth.SigningKeys)
	add("play_token", "播放令牌签名", cfg.Play.TokenKeys)
	add("line_sign", "媒体线路签名", cfg.Storage.SignKeys)
	add("api_enc", "接口报文加密", cfg.APIEncKeys)
	add("media_kek", "媒体内容密钥包装", cfg.Media.KEKs)
	add("storage_kek", "存储凭据包装", cfg.Storage.KEKs)
	add("config_backup", "配置备份签名", cfg.Platform.ConfigBackupKeys)
	add("admin_totp_kek", "后台二次验证种子包装", cfg.AdminTOTPKEKs)
	return out
}

// originHost 从来源（scheme://host[:port]）取主机名，作为 WebAuthn 的 RPID（如 http://localhost:5173 → localhost）。
func originHost(origin string) string {
	u, err := url.Parse(origin)
	if err != nil {
		return origin
	}
	return u.Hostname()
}
