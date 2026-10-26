package domains

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"xsite/service/internal/errs"
)

const (
	maxReleasesPageSize     = 100
	defaultReleasesPageSize = 20
	healthWindowDays        = 30
	releaseTTLDays          = 30 // 清单有效期：客户端超过这个时长没更新到新清单会认为过期
	maxSignatureLen         = 2048
)

// Repo 读写域名、清单与健康数据。
type Repo interface {
	List(ctx context.Context, f Filter) ([]Entry, error)
	Get(ctx context.Context, id int64) (Entry, error)
	Create(ctx context.Context, in EntryInput) (Entry, error)
	Update(ctx context.Context, id int64, in EntryInput) (Entry, error)
	Delete(ctx context.Context, id int64) (Entry, error)
	UpdateCertExpiry(ctx context.Context, id int64, expires *time.Time) (Entry, error)

	ListReleases(ctx context.Context, limit, offset int) ([]Release, int64, error)
	GetRelease(ctx context.Context, version int64) (Release, error)
	// LatestRelease 返回最新一条；没有任何发布时返回 errs.ErrNotFound。
	LatestRelease(ctx context.Context) (Release, error)
	NextReleaseVersion(ctx context.Context) (int64, error)
	InsertRelease(ctx context.Context, row ReleaseRow) (Release, error)

	ListHealth(ctx context.Context, since time.Time) ([]HealthDaily, error)
}

// Service 是域名与证书的业务逻辑。
type Service struct {
	repo Repo
	now  func() time.Time
}

// NewService 构造 Service。
func NewService(repo Repo) *Service {
	return &Service{repo: repo, now: time.Now}
}

// ---------- 域名池 ----------

// List 返回域名池（已填派生字段），按角色、权重排序。
func (s *Service) List(ctx context.Context, f Filter) ([]Entry, error) {
	list, err := s.repo.List(ctx, cleanFilter(f))
	if err != nil {
		return nil, err
	}
	now := s.now()
	for i := range list {
		list[i].enrich(now)
	}
	return list, nil
}

// Create 新建域名；host 已存在时由仓储映射为 CONFLICT。
func (s *Service) Create(ctx context.Context, in EntryInput) (Entry, error) {
	clean, err := in.normalize()
	if err != nil {
		return Entry{}, err
	}
	e, err := s.repo.Create(ctx, clean)
	if err != nil {
		return Entry{}, err
	}
	e.enrich(s.now())
	return e, nil
}

// Update 整体保存域名。返回保存前后，供审计。
func (s *Service) Update(ctx context.Context, id int64, in EntryInput) (before, after Entry, err error) {
	clean, err := in.normalize()
	if err != nil {
		return Entry{}, Entry{}, err
	}
	before, err = s.repo.Get(ctx, id)
	if err != nil {
		return Entry{}, Entry{}, err
	}
	after, err = s.repo.Update(ctx, id, clean)
	if err != nil {
		return Entry{}, Entry{}, err
	}
	now := s.now()
	before.enrich(now)
	after.enrich(now)
	return before, after, nil
}

// Delete 删除域名（不进回收站，设置类数据）。
func (s *Service) Delete(ctx context.Context, id int64) (Entry, error) {
	e, err := s.repo.Delete(ctx, id)
	if err != nil {
		return Entry{}, err
	}
	e.enrich(s.now())
	return e, nil
}

// ---------- 证书（B+D：手动生成自签 / 上传回填）----------

// GenerateCert 现场生成一张自签证书（私钥只在返回里出现一次，不落库）。
func (s *Service) GenerateCert(_ context.Context, in GenCertInput) (GeneratedCert, error) {
	return GenerateSelfSigned(in, s.now())
}

// InspectCert 只解析一张证书、不改数据（上传前预览）。
func (s *Service) InspectCert(_ context.Context, pemText string) (CertInfo, error) {
	return ParseCertificate(pemText)
}

// UploadCertForDomain 解析上传的证书，把到期日回填到指定域名。证书是公开信息，可记审计；私钥不在此流程。
func (s *Service) UploadCertForDomain(ctx context.Context, id int64, pemText string) (Entry, CertInfo, error) {
	info, err := ParseCertificate(pemText)
	if err != nil {
		return Entry{}, CertInfo{}, err
	}
	exp := info.NotAfter
	e, err := s.repo.UpdateCertExpiry(ctx, id, &exp)
	if err != nil {
		return Entry{}, CertInfo{}, err
	}
	e.enrich(s.now())
	return e, info, nil
}

// ---------- 签名清单 ----------

// LatestRelease 返回当前线上清单；还没有发布过时 ok 为 false。
func (s *Service) LatestRelease(ctx context.Context) (rel Release, ok bool, err error) {
	rel, err = s.repo.LatestRelease(ctx)
	if errors.Is(err, errs.ErrNotFound) {
		return Release{}, false, nil
	}
	if err != nil {
		return Release{}, false, err
	}
	return rel, true, nil
}

// ListReleases 返回发布历史（页码分页，倒序）。
func (s *Service) ListReleases(ctx context.Context, page, pageSize int) ([]Release, int64, error) {
	limit, offset := pageClamp(page, pageSize)
	return s.repo.ListReleases(ctx, limit, offset)
}

// PreviewRelease 生成待离线签名的清单预览。
// fromVersion 为 nil 用当前域名池生成；非 nil 用该历史版本的内容作为基础（回滚到旧清单的内容）。
func (s *Service) PreviewRelease(ctx context.Context, fromVersion *int64) (ReleasePreview, error) {
	next, err := s.repo.NextReleaseVersion(ctx)
	if err != nil {
		return ReleasePreview{}, err
	}
	var payload ReleasePayload
	if fromVersion != nil {
		rel, err := s.repo.GetRelease(ctx, *fromVersion)
		if err != nil {
			return ReleasePreview{}, err
		}
		if err := json.Unmarshal(rel.Payload, &payload); err != nil {
			return ReleasePreview{}, fmt.Errorf("历史清单内容损坏（版本 %d）: %w", *fromVersion, err)
		}
	} else {
		entries, err := s.repo.List(ctx, Filter{})
		if err != nil {
			return ReleasePreview{}, err
		}
		payload = buildPayload(entries)
	}
	now := s.now().UTC().Truncate(time.Second)
	payload.V = next
	payload.IssuedAt = now
	payload.ExpiresAt = now.AddDate(0, 0, releaseTTLDays)
	raw, err := json.Marshal(payload)
	if err != nil {
		return ReleasePreview{}, err
	}
	return ReleasePreview{
		Version:     next,
		FromVersion: fromVersion,
		Hosts:       len(payload.API) + len(payload.Web) + len(payload.Media),
		Payload:     raw,
	}, nil
}

// PublishRelease 上传已离线签名的清单。校验 payload 合法、版本号是当前应发的下一个、签名非空。
func (s *Service) PublishRelease(ctx context.Context, in PublishInput) (Release, error) {
	var p ReleasePayload
	if err := json.Unmarshal(in.Payload, &p); err != nil {
		return Release{}, badRequest("清单内容不是合法的 JSON，请直接粘贴预览生成的内容")
	}
	if p.V <= 0 {
		return Release{}, badRequest("清单缺少版本号 v")
	}
	sig := strings.TrimSpace(in.Signature)
	if sig == "" {
		return Release{}, badRequest("请填写离线签名工具生成的签名")
	}
	if len(sig) > maxSignatureLen || !isBase64(sig) {
		return Release{}, badRequest("签名应为 base64 字符串")
	}
	note := strings.TrimSpace(in.Note)
	if utf8.RuneCountInString(note) > 200 {
		return Release{}, badRequest("备注最多 200 个字符")
	}
	next, err := s.repo.NextReleaseVersion(ctx)
	if err != nil {
		return Release{}, err
	}
	if p.V != next {
		return Release{}, errs.ErrConflict.WithMessage(
			fmt.Sprintf("当前应发布第 %d 版，但这份清单是第 %d 版；域名池或清单已变化，请重新生成预览后再签名上传", next, p.V))
	}
	// 规范化后存储：字段顺序由 struct 固定，读回来与签名材料一致。
	canon, err := json.Marshal(p)
	if err != nil {
		return Release{}, err
	}
	return s.repo.InsertRelease(ctx, ReleaseRow{
		Version: next, Payload: canon, Signature: sig, Note: note, PublishedBy: in.PublishedBy,
	})
}

// ---------- 健康看板 ----------

// Health 返回最近窗口内按域名 + 地区聚合的健康看板。
func (s *Service) Health(ctx context.Context) (HealthOverview, error) {
	since := s.now().AddDate(0, 0, -healthWindowDays)
	rows, err := s.repo.ListHealth(ctx, since)
	if err != nil {
		return HealthOverview{}, err
	}
	ov := aggregateHealth(rows)
	ov.SinceDays = healthWindowDays
	if len(ov.List) == 0 {
		ov.Note = "暂无健康数据。服务端拨测与客户端（APP / Web）切换上报接入后，这里会按域名与地区展示探测成功率和切换情况。"
	}
	return ov, nil
}

// ---------- 内部辅助 ----------

// buildPayload 把域名池摊成签名清单正文（排除停用的域名）。
func buildPayload(entries []Entry) ReleasePayload {
	var p ReleasePayload
	for _, e := range entries {
		if e.Status == StatusDisabled {
			continue
		}
		switch e.Role {
		case RoleAPI:
			p.API = append(p.API, HostEntry{Host: e.Host, Weight: e.Weight, Region: e.Region})
		case RoleMedia:
			p.Media = append(p.Media, HostEntry{Host: e.Host, Weight: e.Weight, Region: e.Region})
		case RoleWebPrimary:
			p.Web = append(p.Web, WebEntry{Host: e.Host, Role: "primary"})
		case RoleWebBackup:
			p.Web = append(p.Web, WebEntry{Host: e.Host, Role: "backup"})
		case RoleNav:
			if p.Nav == "" {
				p.Nav = "https://" + e.Host
			}
		}
	}
	return p
}

// aggregateHealth 按 (host, region) 聚合明细，保持首次出现的顺序。
func aggregateHealth(rows []HealthDaily) HealthOverview {
	type key struct{ host, region string }
	acc := map[key]*HealthRow{}
	order := make([]key, 0, len(rows))
	for _, r := range rows {
		k := key{r.Host, r.Region}
		row, ok := acc[k]
		if !ok {
			row = &HealthRow{Host: r.Host, Region: r.Region, FailReasons: map[string]int64{}}
			acc[k] = row
			order = append(order, k)
		}
		row.ProbeOK += r.ProbeOK
		row.ProbeFail += r.ProbeFail
		row.FailoverCount += r.ClientFailoverCount
		for reason, n := range r.FailReasons {
			row.FailReasons[reason] += n
		}
	}
	list := make([]HealthRow, 0, len(order))
	for _, k := range order {
		row := acc[k]
		if tot := row.ProbeOK + row.ProbeFail; tot > 0 {
			rate := float64(row.ProbeOK) / float64(tot)
			row.SuccessRate = &rate
		}
		list = append(list, *row)
	}
	return HealthOverview{List: list}
}

func cleanFilter(f Filter) Filter {
	out := Filter{Q: strings.TrimSpace(f.Q)}
	if f.Status.valid() {
		out.Status = f.Status
	}
	if f.Role.valid() {
		out.Role = f.Role
	}
	if out.Q != "" {
		out.Pattern = "%" + likeEscaper.Replace(out.Q) + "%"
	}
	return out
}

func pageClamp(page, size int) (limit, offset int) {
	if size < 1 || size > maxReleasesPageSize {
		size = defaultReleasesPageSize
	}
	if page < 1 {
		page = 1
	}
	return size, (page - 1) * size
}

// isBase64 粗校验 base64 字符集（含标准与 URL 变体），不解码。
func isBase64(s string) bool {
	for _, r := range s {
		switch {
		case r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z', r >= '0' && r <= '9':
		case r == '+', r == '/', r == '=', r == '-', r == '_':
		default:
			return false
		}
	}
	return true
}

// likeEscaper 转义 LIKE 通配符。
var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
