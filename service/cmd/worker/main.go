// worker 是后台任务进程：River 消费者（体检、预热、转封装、复制、汇总等任务在对应任务中注册）。
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"

	"xsite/service/internal/app"
	"xsite/service/internal/config"
	"xsite/service/internal/domain/analytics"
	"xsite/service/internal/domain/platform"
	"xsite/service/internal/domain/resourcecheck"
	"xsite/service/internal/domain/resourcewarm"
	"xsite/service/internal/domain/storage"
	"xsite/service/internal/geoip"
	"xsite/service/internal/jobs"
	"xsite/service/internal/netguard"
	"xsite/service/internal/observe"
	"xsite/service/internal/store/blob/ftpclient"
	"xsite/service/internal/store/blob/s3probe"
	"xsite/service/internal/store/cache"
	"xsite/service/internal/store/pg"
)

func main() {
	if err := run(); err != nil {
		slog.Error("worker 退出", "err", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	observe.SetupLogger(cfg.LogLevel, "worker", cfg.AppEnv)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pool, err := pg.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	vk, err := cache.NewClient(cfg.ValkeyURL)
	if err != nil {
		return err
	}
	defer vk.Close()

	driver := riverpgxv5.New(pool)
	if cfg.AutoMigrate {
		migrator, err := rivermigrate.New(driver, nil)
		if err != nil {
			return err
		}
		if _, err := migrator.Migrate(ctx, rivermigrate.DirectionUp, nil); err != nil {
			return err
		}
	}

	storageRepo := pg.NewStorageRepo(pool)
	riverCfg := jobs.Config()
	// 资源机测连与列目录（S1-6）：账号密码只在 worker 进程中（FTP_<凭据标识>_USER / _PASSWORD）
	river.AddWorker(riverCfg.Workers, &jobs.FTPProbeWorker{Prober: &ftpclient.Prober{
		Creds:           cfg.FTPCredentials,
		Egress:          netguard.Policy{AllowPrivate: cfg.EgressPrivateAllowed()},
		ExplicitTLSOnly: !cfg.IsDev(),
	}})
	// 媒体转封装（S1-5）：只有 MEDIA_TRANSCODE_CONCURRENCY > 0 且带 FFmpeg 的 worker 消费 media 队列。
	// 产物记录后要在同一事务内入队复制任务（S1-6f），而 River 客户端要等队列配置完才能创建，
	// 这里用闭包延迟绑定：转封装任务只会在 client.Start 之后执行，那时 riverClient 已就绪。
	var riverClient *river.Client[pgx.Tx]
	transcodeWorker, err := app.NewTranscodeWorker(ctx, cfg, pool,
		func(ctx context.Context, tx pgx.Tx, objectID int64) error {
			if riverClient == nil {
				return errors.New("River 客户端尚未就绪")
			}
			_, err := riverClient.InsertTx(ctx, tx, jobs.ReplicateArgs{ObjectID: objectID}, nil)
			return err
		})
	if err != nil {
		return err
	}
	jobs.ConfigureTranscode(riverCfg, transcodeWorker, cfg.Media.TranscodeConcurrency)
	// 后台广告视频：上传源文件写入 video/source/，worker 用同一套 MPEG-TS HLS 工具输出 video/assets/<upload_id>/master.m3u8。
	adminVideoWorker, err := app.NewAdminVideoUploadWorker(ctx, cfg, pool)
	if err != nil {
		return err
	}
	jobs.ConfigureAdminVideoUpload(riverCfg, adminVideoWorker)
	// 后台上传会话过期清理不依赖 FFmpeg，回收失败/未完成对象与广告视频私有源文件。
	adminUploadCleanupWorker, err := app.NewAdminMediaUploadCleanupWorker(cfg, pool)
	if err != nil {
		return err
	}
	jobs.ConfigureAdminMediaUploadCleanup(riverCfg, adminUploadCleanupWorker)
	// 媒体复制（S1-6f）：把主存储上的产物复制到全部 role=mirror 的存储源
	storageKeyring, err := cfg.Storage.Keyring()
	if err != nil {
		return fmt.Errorf("STORAGE_KEKS: %w", err)
	}
	egress := netguard.Policy{AllowPrivate: cfg.EgressPrivateAllowed()}
	storageSvc := storage.NewService(storageRepo, storageKeyring, nil, storage.Policy{HTTPSOnly: !cfg.IsDev(), Egress: egress})
	// Copier 同时提供跨存储源复制与对象存在性校验，三个任务共用一个
	copier := &s3probe.Copier{Egress: egress, WorkDir: cfg.Media.WorkDir}
	replicateSvc := storage.NewReplicateService(storageRepo, storage.NewPlacement(storageRepo), storageSvc, copier)
	jobs.ConfigureReplicate(riverCfg, &jobs.ReplicateWorker{Svc: replicateSvc})
	// 存储源迁移（S1-6f）：复制到目标后撤掉迁出的副本记录
	jobs.ConfigureMigrate(riverCfg, &jobs.MigrateWorker{Svc: replicateSvc})
	// 接管历史产物（S1-6f）：校验对象在存储源上真实存在后登记副本（worker 不入队，故 enqueue 为 nil）
	jobs.ConfigureAdopt(riverCfg, &jobs.AdoptWorker{
		Svc: storage.NewAdoptService(storageRepo, storageSvc, copier, nil),
	})
	// 孤立产物清理（S1-6f）：每天标记并删除已过保留期的产物（分集删除、重新转封装后不再被引用的）
	if err := cfg.Storage.ValidateCleanup(cfg.AppEnv); err != nil {
		return err
	}
	jobs.ConfigureMediaCleanup(riverCfg, &jobs.MediaCleanupWorker{
		Svc: storage.NewCleanupService(storageRepo, storageSvc, copier, cfg.Storage.OrphanRetention),
	}, cfg.Storage.CleanupInterval)
	// 回收站清理（S4-7）：每天物理删除超过保留期的内容。删剧会级联删分集与关联，
	// 失去引用的转码产物由上面的孤立产物清理回收
	if err := cfg.Platform.ValidateTrash(cfg.AppEnv); err != nil {
		return err
	}
	jobs.ConfigureTrashCleanup(riverCfg, &jobs.TrashCleanupWorker{
		Svc: platform.NewTrashService(pg.NewTrashRepo(pool), cfg.Platform.TrashRetention),
	}, cfg.Platform.TrashCleanupInterval)
	// GeoIP 数据库热更新（S1-6c）：worker 侧也要各自重载，让任务日志里的地区是最新的
	_, geoReloader, err := geoip.NewStorageResolver(cfg.Storage.GeoIPXDBPath)
	if err != nil {
		return fmt.Errorf("GEOIP_XDB_PATH: %w", err)
	}
	// 必须在这里判断具体类型：geoReloader 是 *geoip.Resolver，没配数据文件时为 nil。
	// 直接传给接口参数会变成「非 nil 接口里装着 nil 指针」，ConfigureGeoIPReload 的守卫失效，
	// 于是注册一个每轮都白跑的周期任务（改之前是每轮 panic）。
	if geoReloader != nil {
		jobs.ConfigureGeoIPReload(riverCfg, geoReloader, cfg.Storage.GeoIPReloadInterval)
	}
	// 线路健康探测（S1-6d）：周期性拉线路 probe_path，把成功率与耗时换算成健康分写入 Valkey
	if cfg.Storage.HealthProbeEnabled {
		if err := cfg.Storage.ValidateHealthProbe(); err != nil {
			return err
		}
		jobs.ConfigureLineHealth(riverCfg, &jobs.LineHealthWorker{
			Lines:        pg.NewStorageRepo(pool),
			Prober:       &s3probe.LineProber{Egress: netguard.Policy{AllowPrivate: cfg.EgressPrivateAllowed()}},
			Store:        cache.NewLineHealthStore(vk, "xsite:line:health:"),
			ProbeTimeout: cfg.Storage.HealthProbeTimeout,
			TTL:          cfg.Storage.HealthTTL,
		}, cfg.Storage.HealthProbeInterval)
	}
	// 资源体检（S4-5）：按剧探测视频产物与封面的可达性。探测由 worker 发起，不暴露 API 节点 IP。
	// 需要自己的签名服务：生成可访问地址要按线路的签名方式算（hmac_token / s3_presign 等）。
	signKeyring, err := storage.ParseSignKeyring(cfg.Storage.SignKeys)
	if err != nil {
		return fmt.Errorf("LINE_SIGN_KEYS: %w", err)
	}
	jobs.ConfigureResourceCheck(riverCfg, &jobs.ResourceCheckWorker{
		Svc: resourcecheck.NewService(
			pg.NewResourceCheckRepo(pool), storageRepo,
			storage.NewSignService(storageRepo, storageSvc, signKeyring, &s3probe.Presigner{Egress: egress}, nil),
			&s3probe.AssetProber{Egress: egress},
			// worker 不入队体检任务（只有后台会发起），传 nil
			nil,
		),
	})
	// 资源预热（S4-5）：预先拉取清单与前 N 个切片，让边缘缓存住。
	//
	// 关键点：预热拉的必须是**播放器真正会请求的那些地址**，所以这里复用播放链路
	// （play.HLSService）而不是自己拼地址 —— 签名方式或路径一旦有偏差，边缘缓存的就是另一个 key。
	// 用 WarmPlayRepo 而不是 PlayRepo：它放宽了「剧必须已上架」这条，让运营能提前热明天要上的剧。
	playKeyring, err := cfg.Play.Keyring()
	if err != nil {
		return fmt.Errorf("PLAY_TOKEN_KEYS: %w", err)
	}
	warmSignSvc := storage.NewSignService(storageRepo, storageSvc, signKeyring, &s3probe.Presigner{Egress: egress}, nil)
	warmRepo := pg.NewWarmPlayRepo(pool)
	warmHLS, err := app.NewWarmHLSService(cfg, playKeyring, warmRepo, storageRepo, storageSvc, warmSignSvc, egress, vk)
	if err != nil {
		return err
	}
	warmLines, err := app.NewWarmLineLookup(cfg, pool, vk, storageRepo, warmRepo)
	if err != nil {
		return err
	}
	jobs.ConfigureResourceWarm(riverCfg, &jobs.ResourceWarmWorker{
		Svc: resourcewarm.NewService(
			pg.NewResourceWarmRepo(pool),
			app.NewWarmPlaylists(warmHLS, playKeyring, warmLines),
			&s3probe.WarmFetcher{Egress: egress},
			// 拿不到的地址记进资源体检的结果，运营在那边逐条排查
			app.NewWarmChecks(resourcecheck.NewService(pg.NewResourceCheckRepo(pool), storageRepo, warmSignSvc, nil, nil)),
			// worker 不入队预热任务（只有后台会发起），传 nil
			nil,
		),
	})
	// 运营（docs/specs/A3-4.md）：每分钟把 Valkey 里累计的广告曝光、点击落库
	jobs.ConfigureOps(riverCfg, app.NewOpsWorkers(pool, vk))
	// 「我的」域（S2-3）：每分钟把 Valkey 里待落库的观看历史续播断点批量 UPSERT 落库
	jobs.ConfigureMe(riverCfg, app.NewMeFlusher(pool, vk))
	// 社区通知（S2-5）：评论被回复 / 被点赞后异步写站内信给注册用户作者
	jobs.ConfigureCommunity(riverCfg, app.NewCommunityNotifier(pool))
	// 看板汇总与事件分区维护（S4-2）：周期把 event_raw 聚合到 metric_*、滚动建未来分区并删过期分区
	analyticsRollup := analytics.NewRollup(pg.NewAnalyticsRepo(pool))
	jobs.ConfigureMetricsRollup(riverCfg, &jobs.MetricsRollupWorker{Svc: analyticsRollup}, 0)
	jobs.ConfigureEventPartition(riverCfg, &jobs.EventPartitionWorker{Svc: analyticsRollup}, 0)
	client, err := river.NewClient(driver, riverCfg)
	if err != nil {
		return err
	}
	riverClient = client // 让产物记录能在事务内入队复制任务（S1-6f）
	if err := client.Start(ctx); err != nil {
		return err
	}
	slog.Info("worker 启动")

	<-ctx.Done()
	slog.Info("收到退出信号，等待任务结束")
	stopCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	return client.Stop(stopCtx)
}
