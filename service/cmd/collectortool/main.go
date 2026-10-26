// collectortool 是仅限本地开发的授权测试素材采集工具。
// 它只接受人工核验的 catalog manifest，不发现站点、不解析任意网页、不绕过 robots 或访问控制。
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"xsite/service/internal/collector"
	"xsite/service/internal/config"
	"xsite/service/internal/domain/storage"
	"xsite/service/internal/netguard"
	"xsite/service/internal/store/pg"
	"xsite/service/internal/store/uploadstore"
)

const (
	maxManifestBytes = 1 << 20
	maxReportBytes   = 32 << 20
	usage            = `用法：
  collectortool validate -manifest <授权 catalog manifest.json>
      只校验授权引用、精确下载域名、robots 地址、媒体格式与元数据；不联网、不写对象。

  collectortool stage -manifest <授权 catalog manifest.json> -report <本地报告.json>
      仅 development 环境：按 manifest 读取 robots、抓取已授权封面/视频并分别写入图片/视频主存储。
      不创建剧集、不创建分集、不发布内容；报告权限固定为 0600，可供人工预览和 cleanup。

  collectortool cleanup -report <stage 生成的本地报告.json>
      仅 development 环境：只删除报告中本次 collector 前缀的对象，不会删除其他媒体。

安全边界：只支持 HTTPS、精确登记的来源域名、robots 允许的资源和 WebP/AVIF/MP4/MOV/WebM/MKV。
不支持任意网页解析、自动发现、登录墙、付费墙、验证码、重定向或受限资源绕过。`
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "错误：", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return errors.New(usage)
	}
	switch args[0] {
	case "validate":
		return runValidate(args[1:])
	case "stage":
		return runStage(args[1:])
	case "cleanup":
		return runCleanup(args[1:])
	default:
		return errors.New(usage)
	}
}

func runValidate(args []string) error {
	fs := flag.NewFlagSet("validate", flag.ContinueOnError)
	manifestPath := fs.String("manifest", "", "授权 catalog manifest 路径")
	if err := fs.Parse(args); err != nil {
		return err
	}
	manifest, err := readManifest(*manifestPath)
	if err != nil {
		return err
	}
	normalized, err := collector.ValidateManifest(manifest)
	if err != nil {
		return err
	}
	episodes := 0
	for _, drama := range normalized.Dramas {
		episodes += len(drama.Episodes)
	}
	fmt.Printf("授权 manifest 校验通过：来源=%s，短剧=%d 部，分集=%d 集；尚未发起网络请求或写入对象。\n", normalized.Source.ID, len(normalized.Dramas), episodes)
	return nil
}

func runStage(args []string) error {
	fs := flag.NewFlagSet("stage", flag.ContinueOnError)
	manifestPath := fs.String("manifest", "", "授权 catalog manifest 路径")
	reportPath := fs.String("report", "", "写入清理报告的本地路径")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *reportPath == "" {
		return errors.New("stage 必须提供 -report，以便可控回收测试对象")
	}
	manifest, err := readManifest(*manifestPath)
	if err != nil {
		return err
	}
	service, closeService, err := newCollectorService()
	if err != nil {
		return err
	}
	defer closeService()
	result, err := service.Collect(context.Background(), manifest)
	if err != nil {
		if result.CleanupPending {
			if reportErr := writeReport(*reportPath, result); reportErr != nil {
				return errors.Join(err, fmt.Errorf("写入待清理报告失败: %w", reportErr))
			}
			return fmt.Errorf("采集失败，部分对象待清理；已写入报告 %s: %w", *reportPath, err)
		}
		return err
	}
	if err := writeReport(*reportPath, result); err != nil {
		cleanupErr := service.Cleanup(context.Background(), result)
		return errors.Join(fmt.Errorf("写入采集报告失败: %w", err), cleanupErr)
	}
	fmt.Printf("测试素材暂存完成：run_id=%s，短剧=%d 部，对象=%d 个。\n", result.RunID, len(result.Dramas), len(result.Objects))
	fmt.Printf("报告已写入 %s；确认测试结束后执行 collectortool cleanup -report %s。\n", *reportPath, *reportPath)
	return nil
}

func runCleanup(args []string) error {
	fs := flag.NewFlagSet("cleanup", flag.ContinueOnError)
	reportPath := fs.String("report", "", "stage 生成的本地报告路径")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *reportPath == "" {
		return errors.New("cleanup 必须提供 -report")
	}
	var result collector.Result
	if err := readReport(*reportPath, &result); err != nil {
		return err
	}
	service, closeService, err := newCollectorService()
	if err != nil {
		return err
	}
	defer closeService()
	if err := service.Cleanup(context.Background(), result); err != nil {
		return err
	}
	fmt.Printf("测试素材已清理：run_id=%s，对象=%d 个。报告保留在 %s 供审计。\n", result.RunID, len(result.Objects), *reportPath)
	return nil
}

func newCollectorService() (*collector.Service, func(), error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, nil, err
	}
	if !cfg.IsDev() {
		return nil, nil, errors.New("collectortool 只允许在 development 环境运行，避免测试采集误写入生产存储")
	}
	pool, err := pg.NewPool(context.Background(), cfg.DatabaseURL)
	if err != nil {
		return nil, nil, err
	}
	keyring, err := cfg.Storage.Keyring()
	if err != nil {
		pool.Close()
		return nil, nil, fmt.Errorf("STORAGE_KEKS: %w", err)
	}
	egress := netguard.Policy{AllowPrivate: cfg.EgressPrivateAllowed()}
	repo := pg.NewStorageRepo(pool)
	storageService := storage.NewService(repo, keyring, nil, storage.Policy{HTTPSOnly: !cfg.IsDev(), Egress: egress})
	resolver := uploadstore.New(repo, storageService, egress)
	fetcher := collector.NewHTTPFetcher(collector.HTTPFetcherOptions{Egress: egress, AllowHTTP: false})
	return collector.NewService(resolver, fetcher, collector.DefaultOptions()), pool.Close, nil
}

func readManifest(file string) (collector.Manifest, error) {
	var manifest collector.Manifest
	if err := readJSON(file, &manifest, maxManifestBytes); err != nil {
		return collector.Manifest{}, err
	}
	return manifest, nil
}

func readReport(file string, result *collector.Result) error {
	return readJSON(file, result, maxReportBytes)
}

func readJSON(file string, dst any, maxBytes int) error {
	if file == "" {
		return errors.New("必须提供文件路径")
	}
	f, err := os.Open(file)
	if err != nil {
		return err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, int64(maxBytes)+1))
	if err != nil {
		return err
	}
	if len(data) > maxBytes {
		return fmt.Errorf("文件不能超过 %d 字节", maxBytes)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("JSON 文件只能包含一个对象")
	}
	return nil
}

func writeReport(file string, result collector.Result) error {
	if file == "" {
		return errors.New("报告路径不能为空")
	}
	dir := filepath.Dir(file)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	temp, err := os.CreateTemp(dir, ".collectortool-*.json")
	if err != nil {
		return err
	}
	tempName := temp.Name()
	defer os.Remove(tempName)
	if err := temp.Chmod(0o600); err != nil {
		_ = temp.Close()
		return err
	}
	encoder := json.NewEncoder(temp)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(result); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(tempName, file)
}
