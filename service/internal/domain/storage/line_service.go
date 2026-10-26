package storage

import (
	"context"
	"net/netip"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

var (
	countryRe = regexp.MustCompile(`^[A-Z]{2}$`)
	asnRe     = regexp.MustCompile(`^[0-9]{1,10}$`)
	// signKeyRefRe 限制签名密钥引用的字符（它要能安全地出现在环境变量名与日志里）。
	signKeyRefRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
)

// LineSigner 是线路签名服务的子集（由 SignService 实现），线路测试用它自检签名配置。
type LineSigner interface {
	SignFor(ctx context.Context, line Line, target SignTarget, ttl time.Duration, now time.Time) (SignedURL, error)
}

// LineService 是线路与地区规则的后台规则。
type LineService struct {
	repo   Repo
	prober LineProber
	policy Policy
	signer LineSigner
}

func NewLineService(repo Repo, prober LineProber, policy Policy, signer LineSigner) *LineService {
	return &LineService{repo: repo, prober: prober, policy: policy, signer: signer}
}

// ListLines 返回线路列表（含地区规则），按存储源、优先级倒序。
func (s *LineService) ListLines(ctx context.Context, f LineFilter) ([]Line, error) {
	if f.Kind != "" && f.Kind != LineVideo && f.Kind != LineImage {
		return nil, badRequest("类型筛选无效")
	}
	if f.Status != "" && f.Status != LineActive && f.Status != LineWatch && f.Status != LineDisabled {
		return nil, badRequest("状态筛选无效")
	}
	lines, err := s.repo.ListLines(ctx, LineQuery{ProviderID: f.ProviderID, Kind: f.Kind, Status: f.Status})
	if err != nil {
		return nil, err
	}
	byLine := map[int64][]Rule{}
	if f.ProviderID > 0 {
		rules, err := s.repo.ListRulesByProvider(ctx, f.ProviderID)
		if err != nil {
			return nil, err
		}
		for _, r := range rules {
			byLine[r.LineID] = append(byLine[r.LineID], r)
		}
	} else {
		for _, l := range lines {
			rs, err := s.repo.ListRulesByLine(ctx, l.ID)
			if err != nil {
				return nil, err
			}
			byLine[l.ID] = rs
		}
	}
	for i := range lines {
		lines[i].Rules = byLine[lines[i].ID]
	}
	return lines, nil
}

// GetLine 返回单条线路（含规则）。
func (s *LineService) GetLine(ctx context.Context, id int64) (Line, error) {
	line, err := s.repo.GetLine(ctx, id)
	if err != nil {
		return Line{}, err
	}
	if line.Rules, err = s.repo.ListRulesByLine(ctx, id); err != nil {
		return Line{}, err
	}
	return line, nil
}

// CreateLine 新建线路：校验所属存储源存在且用途与线路类型兼容。
func (s *LineService) CreateLine(ctx context.Context, in LineInput) (Line, error) {
	in, err := s.cleanLine(in, true)
	if err != nil {
		return Line{}, err
	}
	provider, err := s.repo.Get(ctx, in.ProviderID)
	if err != nil {
		return Line{}, err
	}
	if !purposeAllowsKind(provider.Purpose, in.Kind) {
		return Line{}, badRequest("该存储源用途为「%s」，不能挂 %s 线路", provider.Purpose, in.Kind)
	}
	return s.repo.CreateLine(ctx, in)
}

// UpdateLine 整体保存线路（不改所属存储源），返回修改前后的记录（均含规则）。
func (s *LineService) UpdateLine(ctx context.Context, id int64, in LineInput) (before, after Line, err error) {
	if in, err = s.cleanLine(in, false); err != nil {
		return
	}
	if before, err = s.GetLine(ctx, id); err != nil {
		return Line{}, Line{}, err
	}
	if !purposeAllowsKind(mustPurpose(ctx, s, before.ProviderID), in.Kind) {
		return Line{}, Line{}, badRequest("该存储源用途不支持 %s 线路", in.Kind)
	}
	if after, err = s.repo.UpdateLine(ctx, id, in); err != nil {
		return Line{}, Line{}, err
	}
	after.Rules = before.Rules
	return before, after, nil
}

// DeleteLine 删除线路（级联删除其地区规则），返回删除前的记录。
func (s *LineService) DeleteLine(ctx context.Context, id int64) (Line, error) {
	before, err := s.GetLine(ctx, id)
	if err != nil {
		return Line{}, err
	}
	if _, err := s.repo.DeleteLine(ctx, id); err != nil {
		return Line{}, err
	}
	return before, nil
}

// SetRules 整体替换线路的地区规则。
func (s *LineService) SetRules(ctx context.Context, lineID int64, in []RuleInput) ([]Rule, error) {
	if _, err := s.repo.GetLine(ctx, lineID); err != nil {
		return nil, err
	}
	if len(in) > 100 {
		return nil, badRequest("一条线路最多 100 条地区规则")
	}
	cleaned := make([]RuleInput, len(in))
	for i, r := range in {
		c, err := cleanRule(r)
		if err != nil {
			return nil, err
		}
		cleaned[i] = c
	}
	return s.repo.ReplaceLineRules(ctx, lineID, cleaned)
}

// TestLine 测试线路：对 base_url + probe_path 发起 HTTP 探测，并自检签名配置是否可用（docs/12 第 3.3、8.2 节）。
// 直连存储线路（无 base_url）没有独立访问域名，连通性在存储源测试里看，但签名自检仍然执行。
func (s *LineService) TestLine(ctx context.Context, id int64) (LineProbeResult, error) {
	line, err := s.repo.GetLine(ctx, id)
	if err != nil {
		return LineProbeResult{}, err
	}
	var res LineProbeResult
	if line.BaseURL == "" {
		res = LineProbeResult{Message: "该线路直连存储、没有独立访问域名，连通性请在对应存储源的测试中查看"}
	} else {
		res = s.prober.ProbeLine(ctx, LineProbeRequest{BaseURL: line.BaseURL, ProbePath: line.ProbePath})
	}
	res.Sign = s.checkSign(ctx, line)
	return res, nil
}

// checkSign 用线路当前的签名配置试生成一个示例地址：能生成说明密钥已注入、算法与参数可用。
// 没有配置签名服务（未注入签名密钥环）时跳过，不影响连通性测试。
func (s *LineService) checkSign(ctx context.Context, line Line) SignCheck {
	chk := SignCheck{Type: line.SignType}
	if s.signer == nil {
		chk.Message = "未配置签名服务，跳过签名自检"
		return chk
	}
	// 用一个固定的示例前缀自检，不触碰真实对象
	const samplePrefix = "_probe/sign-check/"
	signed, err := s.signer.SignFor(ctx, line, SignTarget{
		Key: samplePrefix + "sample.m4s", Prefix: samplePrefix,
	}, 0, time.Now())
	if err != nil {
		chk.Message = "签名失败：" + err.Error()
		return chk
	}
	chk.OK = true
	chk.SampleURL = redactSignature(signed.URL)
	switch line.SignType {
	case SignNone:
		chk.Message = "该线路不签名（公开读）"
	default:
		chk.Message = "签名生成成功，有效期至 " + signed.ExpiresAt.Format(time.RFC3339)
	}
	return chk
}

// redactSignature 去掉示例地址里的签名值，只保留形态（签名值是用真实密钥算出来的，不该回显给运营）。
func redactSignature(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	q := u.Query()
	for _, name := range []string{SignParamSig, "auth_key", "X-Amz-Signature", "X-Amz-Credential", "X-Amz-Security-Token"} {
		if q.Has(name) {
			q.Set(name, "***")
		}
	}
	u.RawQuery = q.Encode()
	return u.String()
}

func mustPurpose(ctx context.Context, s *LineService, providerID int64) Purpose {
	p, err := s.repo.Get(ctx, providerID)
	if err != nil {
		return PurposeBoth // 取不到时不因用途拦截（真正的存在性由其它查询保证）
	}
	return p.Purpose
}

func purposeAllowsKind(p Purpose, k LineKind) bool {
	switch k {
	case LineVideo:
		return p == PurposeVideo || p == PurposeBoth
	case LineImage:
		return p == PurposeImage || p == PurposeBoth
	}
	return false
}

// cleanLine 校验并补齐默认值。requireProvider 为 true（新建）时校验 ProviderID。
func (s *LineService) cleanLine(in LineInput, requireProvider bool) (LineInput, error) {
	var err error
	if requireProvider && in.ProviderID <= 0 {
		return in, badRequest("请指定所属存储源")
	}
	switch in.Kind {
	case LineVideo, LineImage:
	default:
		return in, badRequest("线路类型只能是 video 或 image")
	}
	switch in.AccessMode {
	case AccessPresign, AccessCDN, AccessPublic:
	default:
		return in, badRequest("访问方式只能是 presign、cdn 或 public")
	}
	switch in.SignType {
	case SignS3Presign, SignHMACToken, SignCDNVendor, SignNone:
	default:
		return in, badRequest("签名方式只能是 s3_presign、hmac_token、cdn_vendor 或 none")
	}
	switch in.RegionScope {
	case "":
		in.RegionScope = ScopeGlobal
	case ScopeRules, ScopeGlobal:
	default:
		return in, badRequest("地区范围只能是 rules 或 global")
	}
	switch in.VideoProcess {
	case "":
		in.VideoProcess = ProcessStandard
	case ProcessStandard, ProcessTextToImage:
	default:
		return in, badRequest("视频处理方式只能是 standard 或 text_to_image")
	}
	switch in.Status {
	case "":
		in.Status = LineActive
	case LineActive, LineWatch, LineDisabled:
	default:
		return in, badRequest("状态只能是 active、watch 或 disabled")
	}

	in.Name = strings.TrimSpace(in.Name)
	if utf8.RuneCountInString(in.Name) > maxLineNameRunes {
		return in, badRequest("线路名称不能超过 %d 个字", maxLineNameRunes)
	}
	in.Note = strings.TrimSpace(in.Note)
	if utf8.RuneCountInString(in.Note) > maxNoteRunes {
		return in, badRequest("备注不能超过 %d 个字", maxNoteRunes)
	}
	if in.SignKeyRef, err = cleanSignKeyRef(in.SignType, in.SignKeyRef); err != nil {
		return in, err
	}

	// base_url：cdn / public 必填；presign 直连存储时可留空（由存储源生成）
	if in.BaseURL, err = s.cleanBaseURL(in.BaseURL); err != nil {
		return in, err
	}
	if in.AccessMode != AccessPresign && in.BaseURL == "" {
		return in, badRequest("访问方式为 %s 时必须填写访问域名（Base URL）", in.AccessMode)
	}

	if in.TokenTTL == 0 {
		in.TokenTTL = LineDefaultTokenTTL
	}
	if in.TokenTTL < MinPresignTTL || in.TokenTTL > MaxPresignTTL {
		return in, badRequest("链接有效期为 %d–%d 秒", MinPresignTTL, MaxPresignTTL)
	}
	if in.Priority < 0 || in.Priority > maxPriority {
		return in, badRequest("优先级为 0–%d", maxPriority)
	}
	if in.Weight == 0 {
		in.Weight = LineDefaultWeight
	}
	if in.Weight < 1 || in.Weight > 100 {
		return in, badRequest("权重为 1–100")
	}
	in.ProbePath = strings.TrimSpace(in.ProbePath)
	if in.ProbePath == "" {
		in.ProbePath = LineDefaultProbePath
	}
	if !strings.HasPrefix(in.ProbePath, "/") || strings.ContainsAny(in.ProbePath, " \t\r\n") {
		return in, badRequest("探测路径要以 / 开头且不含空白")
	}
	return in, nil
}

func (s *LineService) cleanBaseURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "", badRequest("访问域名格式错误，应为 https://host[/路径前缀]")
	}
	if u.Scheme != "https" && (u.Scheme != "http" || s.policy.HTTPSOnly) {
		return "", badRequest("访问域名当前环境只允许 https")
	}
	if u.RawQuery != "" || u.Fragment != "" || u.User != nil {
		return "", badRequest("访问域名不要带查询串、锚点或用户信息")
	}
	if ip, err := netip.ParseAddr(u.Hostname()); err == nil {
		if err := s.policy.Egress.Check(ip); err != nil {
			return "", badRequest("访问域名 %s 属于内网或保留地址段，当前环境不允许", ip)
		}
	}
	return strings.TrimRight(raw, "/"), nil
}

// cleanSignKeyRef 校验签名密钥引用（S1-6e）：
// hmac_token 需要一个密钥引用；cdn_vendor 需要 `厂商名/密钥引用`；s3_presign 与 none 不用密钥（填了也清掉，避免误解）。
// 这里只校验格式，不校验密钥是否已注入——后台保存可能早于密钥下发，缺密钥由线路测试与签名时报错。
func cleanSignKeyRef(signType SignType, ref string) (string, error) {
	ref = strings.TrimSpace(ref)
	switch signType {
	case SignS3Presign, SignNone:
		return "", nil
	case SignHMACToken:
		if ref == "" {
			return "", badRequest("HMAC 令牌签名需要填写密钥引用（对应 LINE_SIGN_KEYS 中的一项）")
		}
		if !signKeyRefRe.MatchString(ref) {
			return "", badRequest("密钥引用只能是字母、数字、连字符或下划线，最长 64 个字符")
		}
		return ref, nil
	case SignCDNVendor:
		vendor, key, ok := strings.Cut(ref, "/")
		if !ok || vendor == "" || key == "" {
			return "", badRequest("CDN 厂商鉴权的密钥引用格式为 厂商名/密钥引用，如 timestamp_md5/cdn-main")
		}
		if _, err := DefaultVendors().Lookup(vendor); err != nil {
			return "", err
		}
		if !signKeyRefRe.MatchString(key) {
			return "", badRequest("密钥引用只能是字母、数字、连字符或下划线，最长 64 个字符")
		}
		return vendor + "/" + key, nil
	}
	return ref, nil
}

func cleanRule(r RuleInput) (RuleInput, error) {
	r.Country = strings.ToUpper(strings.TrimSpace(r.Country))
	if r.Country == "" {
		r.Country = "*"
	}
	if r.Country != "*" && !countryRe.MatchString(r.Country) {
		return r, badRequest("国家代码为 ISO 两位字母（如 CN、HK）或 *")
	}
	r.Province = strings.TrimSpace(r.Province)
	if r.Province == "" {
		r.Province = "*"
	}
	r.ISP = strings.TrimSpace(r.ISP)
	if r.ISP == "" {
		r.ISP = "*"
	}
	r.ASN = strings.TrimSpace(r.ASN)
	if r.ASN != "" && !asnRe.MatchString(r.ASN) {
		return r, badRequest("ASN 为数字或留空")
	}
	switch r.Effect {
	case "":
		r.Effect = EffectInclude
	case EffectInclude, EffectExclude:
	default:
		return r, badRequest("规则效果只能是 include 或 exclude")
	}
	if r.Boost < 0 || r.Boost > maxBoost {
		return r, badRequest("加分为 0–%d", maxBoost)
	}
	return r, nil
}
