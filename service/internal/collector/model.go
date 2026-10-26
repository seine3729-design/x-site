// Package collector 实现仅限授权来源的测试媒体采集。
// 它不发现站点、不解析任意网页，也不绕过访问控制；调用者必须给出明确的授权 catalog manifest。
package collector

import (
	"fmt"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"xsite/service/internal/domain/upload"
	"xsite/service/internal/errs"
)

const (
	maxManifestDramas   = 100
	maxEpisodesPerDrama = 200
	maxURLBytes         = 2048
	maxRightsRefRunes   = 512
	maxTitleRunes       = 200
	maxSummaryRunes     = 5000
	maxIDBytes          = 64
)

// AuthorizationKind 是采集来源的权利基础。robots 许可不能替代这里的授权声明。
type AuthorizationKind string

const (
	AuthorizationOwned        AuthorizationKind = "owned"
	AuthorizationLicensed     AuthorizationKind = "licensed"
	AuthorizationPublicDomain AuthorizationKind = "public_domain"
	AuthorizationCCBY         AuthorizationKind = "cc_by"
)

// Authorization 是由操作者提供的不可缺失的权利追溯信息。
type Authorization struct {
	Kind      AuthorizationKind `json:"kind"`
	Reference string            `json:"reference"`
	ExpiresAt *time.Time        `json:"expires_at,omitempty"`
}

// Origin 是 manifest 允许下载的单个精确主机及其 robots 规则地址。
type Origin struct {
	Host      string `json:"host"`
	RobotsURL string `json:"robots_url"`
}

// Source 描述一个已经人工核验的授权来源。Origins 只接受精确主机名，禁止通配符。
type Source struct {
	ID            string        `json:"id"`
	Authorization Authorization `json:"authorization"`
	Origins       []Origin      `json:"origins"`
}

// Asset 是需要从来源拉取的一个媒体对象。采集器只接受已经转为支持格式的封面与视频。
type Asset struct {
	URL  string `json:"url"`
	MIME string `json:"mime"`
}

// Episode 是一集待采集的视频。
type Episode struct {
	ID     string `json:"id"`
	Number int32  `json:"number"`
	Title  string `json:"title"`
	Video  Asset  `json:"video"`
}

// Drama 是一部短剧的文案和媒体清单。采集器只暂存素材，不自动发布到前台。
type Drama struct {
	ID       string    `json:"id"`
	Title    string    `json:"title"`
	Summary  string    `json:"summary"`
	Cover    Asset     `json:"cover"`
	Episodes []Episode `json:"episodes"`
}

// Manifest 是合规采集的唯一输入。短剧元数据必须由授权 catalog 提供，而不是任意页面猜测得到。
type Manifest struct {
	Source Source  `json:"source"`
	Dramas []Drama `json:"dramas"`
}

// StoredObject 是一项已暂存的测试素材。物理目标快照仅写入本地报告，供 cleanup 安全回收。
type StoredObject struct {
	Kind               upload.Kind `json:"kind"`
	Key                string      `json:"key"`
	MIME               string      `json:"mime"`
	SizeBytes          int64       `json:"size_bytes"`
	ProviderID         int64       `json:"provider_id"`
	ProviderEndpoint   string      `json:"provider_endpoint"`
	ProviderRegion     string      `json:"provider_region"`
	ProviderBucket     string      `json:"provider_bucket"`
	ProviderPathStyle  bool        `json:"provider_path_style"`
	ProviderPathPrefix string      `json:"provider_path_prefix"`
}

// CollectedEpisode 是已暂存分集的安全结果，不包含远端 URL 或授权引用。
type CollectedEpisode struct {
	ID        string `json:"id"`
	Number    int32  `json:"number"`
	Title     string `json:"title"`
	SourceKey string `json:"source_key"`
}

// CollectedDrama 是已暂存短剧的文案与对象 key。
type CollectedDrama struct {
	ID       string             `json:"id"`
	Title    string             `json:"title"`
	Summary  string             `json:"summary"`
	CoverKey string             `json:"cover_key"`
	Episodes []CollectedEpisode `json:"episodes"`
}

// Result 是一次暂存结果；可由 collectortool cleanup 按记录的物理目标快照清理。
type Result struct {
	RunID          string           `json:"run_id"`
	SourceID       string           `json:"source_id"`
	CleanupPending bool             `json:"cleanup_pending,omitempty"`
	Dramas         []CollectedDrama `json:"dramas"`
	Objects        []StoredObject   `json:"objects"`
}

// ValidateManifest 在不发起任何网络请求的前提下校验授权、域名和素材声明。
func ValidateManifest(in Manifest) (Manifest, error) {
	return validateManifest(in, false, time.Now().UTC())
}

func validateManifest(in Manifest, allowHTTP bool, now time.Time) (Manifest, error) {
	var err error
	if in.Source.ID, err = cleanID("来源编号", in.Source.ID); err != nil {
		return Manifest{}, err
	}
	if err := validateAuthorization(&in.Source.Authorization, now); err != nil {
		return Manifest{}, err
	}
	origins, err := normalizeOrigins(in.Source.Origins, allowHTTP)
	if err != nil {
		return Manifest{}, err
	}
	in.Source.Origins = origins
	if len(in.Dramas) == 0 || len(in.Dramas) > maxManifestDramas {
		return Manifest{}, badRequest("一次采集的短剧数量应为 1–%d 部", maxManifestDramas)
	}

	dramaIDs := make(map[string]struct{}, len(in.Dramas))
	for i := range in.Dramas {
		d := &in.Dramas[i]
		if d.ID, err = cleanID("短剧编号", d.ID); err != nil {
			return Manifest{}, err
		}
		if _, exists := dramaIDs[d.ID]; exists {
			return Manifest{}, badRequest("短剧编号重复：%s", d.ID)
		}
		dramaIDs[d.ID] = struct{}{}
		if d.Title, err = cleanText("剧名", d.Title, maxTitleRunes, true); err != nil {
			return Manifest{}, err
		}
		if d.Summary, err = cleanText("简介", d.Summary, maxSummaryRunes, false); err != nil {
			return Manifest{}, err
		}
		if err := normalizeAsset(&d.Cover, origins, allowHTTP, upload.KindImage); err != nil {
			return Manifest{}, fmt.Errorf("短剧 %s 的封面: %w", d.ID, err)
		}
		if len(d.Episodes) == 0 || len(d.Episodes) > maxEpisodesPerDrama {
			return Manifest{}, badRequest("短剧 %s 的分集数量应为 1–%d 集", d.ID, maxEpisodesPerDrama)
		}
		episodeIDs := make(map[string]struct{}, len(d.Episodes))
		numbers := make(map[int32]struct{}, len(d.Episodes))
		for j := range d.Episodes {
			e := &d.Episodes[j]
			if e.ID, err = cleanID("分集编号", e.ID); err != nil {
				return Manifest{}, err
			}
			if _, exists := episodeIDs[e.ID]; exists {
				return Manifest{}, badRequest("短剧 %s 的分集编号重复：%s", d.ID, e.ID)
			}
			episodeIDs[e.ID] = struct{}{}
			if e.Number <= 0 {
				return Manifest{}, badRequest("短剧 %s 的分集号必须大于 0", d.ID)
			}
			if _, exists := numbers[e.Number]; exists {
				return Manifest{}, badRequest("短剧 %s 的分集号重复：%d", d.ID, e.Number)
			}
			numbers[e.Number] = struct{}{}
			if e.Title, err = cleanText("分集标题", e.Title, maxTitleRunes, false); err != nil {
				return Manifest{}, err
			}
			if err := normalizeAsset(&e.Video, origins, allowHTTP, upload.KindVideo); err != nil {
				return Manifest{}, fmt.Errorf("短剧 %s 的分集 %s: %w", d.ID, e.ID, err)
			}
		}
	}
	return in, nil
}

func validateAuthorization(a *Authorization, now time.Time) error {
	switch a.Kind {
	case AuthorizationOwned, AuthorizationLicensed, AuthorizationPublicDomain, AuthorizationCCBY:
	default:
		return badRequest("授权类型必须是 owned、licensed、public_domain 或 cc_by")
	}
	var err error
	if a.Reference, err = cleanText("授权引用", a.Reference, maxRightsRefRunes, true); err != nil {
		return err
	}
	if a.Kind == AuthorizationLicensed {
		if a.ExpiresAt == nil || !a.ExpiresAt.After(now) {
			return badRequest("licensed 来源必须提供尚未到期的 expires_at")
		}
	}
	return nil
}

func normalizeOrigins(in []Origin, allowHTTP bool) ([]Origin, error) {
	if len(in) == 0 || len(in) > 20 {
		return nil, badRequest("授权来源必须配置 1–20 个精确下载域名")
	}
	out := make([]Origin, len(in))
	seen := make(map[string]struct{}, len(in))
	for i, origin := range in {
		host, err := cleanHost(origin.Host)
		if err != nil {
			return nil, err
		}
		if _, exists := seen[host]; exists {
			return nil, badRequest("下载域名重复：%s", host)
		}
		seen[host] = struct{}{}
		robotsURL, robotsHost, err := cleanRemoteURL(origin.RobotsURL, allowHTTP)
		if err != nil {
			return nil, fmt.Errorf("下载域名 %s 的 robots_url: %w", host, err)
		}
		if robotsHost != host {
			return nil, badRequest("下载域名 %s 的 robots_url 必须使用同一主机", host)
		}
		out[i] = Origin{Host: host, RobotsURL: robotsURL}
	}
	return out, nil
}

func normalizeAsset(asset *Asset, origins []Origin, allowHTTP bool, kind upload.Kind) error {
	urlText, host, err := cleanRemoteURL(asset.URL, allowHTTP)
	if err != nil {
		return err
	}
	if !containsOrigin(origins, host) {
		return badRequest("媒体 URL 的主机未在授权来源中登记")
	}
	mime := normalizeMIME(asset.MIME)
	switch kind {
	case upload.KindImage:
		if mime != "image/webp" && mime != "image/avif" {
			return badRequest("封面只接受 image/webp 或 image/avif")
		}
	case upload.KindVideo:
		switch mime {
		case "video/mp4", "video/quicktime", "video/webm", "video/x-matroska":
		default:
			return badRequest("视频只接受 MP4、MOV、WebM 或 MKV")
		}
	default:
		return badRequest("未知媒体类型")
	}
	asset.URL, asset.MIME = urlText, mime
	return nil
}

func containsOrigin(origins []Origin, host string) bool {
	for _, origin := range origins {
		if origin.Host == host {
			return true
		}
	}
	return false
}

func cleanRemoteURL(raw string, allowHTTP bool) (string, string, error) {
	if len(raw) == 0 || len(raw) > maxURLBytes || !utf8.ValidString(raw) {
		return "", "", badRequest("媒体 URL 长度或编码无效")
	}
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" || u.User != nil || u.Fragment != "" {
		return "", "", badRequest("媒体 URL 格式无效")
	}
	u.Scheme = strings.ToLower(u.Scheme)
	if u.Scheme != "https" && !(allowHTTP && u.Scheme == "http") {
		return "", "", badRequest("媒体 URL 只允许 HTTPS")
	}
	host := strings.ToLower(u.Hostname())
	if host == "" {
		return "", "", badRequest("媒体 URL 缺少主机名")
	}
	return u.String(), host, nil
}

func cleanHost(raw string) (string, error) {
	host := strings.ToLower(strings.TrimSpace(raw))
	if host == "" || strings.ContainsAny(host, "*/:@") {
		return "", badRequest("下载域名格式无效")
	}
	u, err := url.Parse("https://" + host)
	if err != nil || u.Hostname() != host || u.Port() != "" {
		return "", badRequest("下载域名必须是精确主机名，不含端口或通配符")
	}
	return host, nil
}

func cleanID(label, raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" || len(value) > maxIDBytes {
		return "", badRequest("%s长度无效", label)
	}
	for i, r := range value {
		valid := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-'
		if !valid || (i == 0 && (r == '_' || r == '-')) {
			return "", badRequest("%s只能使用字母、数字、下划线和连字符，且不能以下划线或连字符开头", label)
		}
	}
	return value, nil
}

func cleanText(label, raw string, max int, required bool) (string, error) {
	value := strings.TrimSpace(raw)
	if !utf8.ValidString(value) || len([]rune(value)) > max || (required && value == "") {
		return "", badRequest("%s长度或编码无效", label)
	}
	return value, nil
}

func normalizeMIME(value string) string {
	mime, _, _ := strings.Cut(strings.ToLower(strings.TrimSpace(value)), ";")
	return strings.TrimSpace(mime)
}

func badRequest(format string, args ...any) error {
	return errs.ErrBadRequest.WithMessage(fmt.Sprintf(format, args...))
}
