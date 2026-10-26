package storage

import (
	"context"
	"fmt"
	"time"
)

// Settings 是存储全局设置（docs/12 第 7、8.4 节，S1-6d 设置部分）。单行，始终存在。
// 主存储 ID 为 0 表示未设置。
type Settings struct {
	CandidateCount         int32     `json:"candidate_count"`
	DefaultTTL             int32     `json:"default_ttl"`
	SmartRouting           bool      `json:"smart_routing"`
	PrimaryVideoProviderID int64     `json:"primary_video_provider_id"`
	PrimaryImageProviderID int64     `json:"primary_image_provider_id"`
	ReplicateOnUpload      bool      `json:"replicate_on_upload"`
	UpdatedAt              time.Time `json:"updated_at"`
}

// SettingsInput 是保存存储设置的字段。
type SettingsInput struct {
	CandidateCount         int32
	DefaultTTL             int32
	SmartRouting           bool
	PrimaryVideoProviderID int64
	PrimaryImageProviderID int64
	ReplicateOnUpload      bool
}

// 交付自检的问题编码（docs/12 第 8.4 节）。
const (
	// IssueNoPrimary：该类型内容没有可写入的主存储，上传本身就会失败。
	IssueNoPrimary = "no_primary"
	// IssueNoLineOnPrimary：主存储上没有可下发的线路，但别的存储源上有。
	IssueNoLineOnPrimary = "no_line_on_primary"
	// IssueNoLineAtAll：全站都没有该类型的可用线路。
	IssueNoLineAtAll = "no_line_at_all"
)

// DeliveryIssue 是一条交付配置问题。Message 是给运营看的中文说明，含修复指引。
type DeliveryIssue struct {
	Kind    LineKind `json:"kind"`
	Code    string   `json:"code"`
	Message string   `json:"message"`
}

// DeliveryCheck 是「上传成功的资源能否被前台取到」的配置自检。
//
// 选源只会从「产物副本已就绪的存储源」上挑线路（sources.go），所以主存储没挂线路时，
// 新上传的视频在前台会拿到「暂无可用线路」（503），而后台的上传、转码、副本状态全是成功的，
// 运营无从判断问题在哪 —— 这项自检就是把这个落差显式报出来。
type DeliveryCheck struct {
	OK     bool            `json:"ok"`
	Issues []DeliveryIssue `json:"issues"`
}

// SettingsView 是存储设置加上交付自检，供后台设置页显示风险提示。
type SettingsView struct {
	Settings
	Delivery DeliveryCheck `json:"delivery"`
}

// SettingsService 是存储全局设置的后台规则。
type SettingsService struct {
	repo Repo
}

func NewSettingsService(repo Repo) *SettingsService { return &SettingsService{repo: repo} }

// Get 读取存储设置（单行，始终存在）。
func (s *SettingsService) Get(ctx context.Context) (Settings, error) {
	return s.repo.GetSettings(ctx)
}

// Overview 读取存储设置并附带交付自检结果。
func (s *SettingsService) Overview(ctx context.Context) (SettingsView, error) {
	st, err := s.repo.GetSettings(ctx)
	if err != nil {
		return SettingsView{}, err
	}
	return SettingsView{Settings: st, Delivery: s.CheckDelivery(ctx)}, nil
}

// CheckDelivery 对视频与图片分别检查主存储是否挂了可下发的线路。
// 自身的读取失败不算问题（避免数据库抖动时误报），只在确认缺线路时报出。
func (s *SettingsService) CheckDelivery(ctx context.Context) DeliveryCheck {
	issues := []DeliveryIssue{}
	for _, kind := range []LineKind{LineVideo, LineImage} {
		issues = append(issues, s.checkKind(ctx, kind)...)
	}
	return DeliveryCheck{OK: len(issues) == 0, Issues: issues}
}

func (s *SettingsService) checkKind(ctx context.Context, kind LineKind) []DeliveryIssue {
	label := kindLabel(kind)
	primary, err := NewPlacement(s.repo).Primary(ctx, kind)
	if err != nil {
		return []DeliveryIssue{{
			Kind: kind, Code: IssueNoPrimary,
			Message: fmt.Sprintf("没有可写入的%s主存储，%s资源无法上传：%s", label, label, err),
		}}
	}
	onPrimary, err := s.repo.ListLines(ctx, LineQuery{ProviderID: primary.ID, Kind: kind})
	if err != nil {
		return nil
	}
	if countUsable(onPrimary) > 0 {
		return nil
	}
	all, err := s.repo.ListLines(ctx, LineQuery{Kind: kind})
	if err != nil {
		return nil
	}
	head := fmt.Sprintf("主%s存储源「%s」没有可用的%s线路", label, primary.Name, label)
	if len(onPrimary) > 0 {
		head = fmt.Sprintf("主%s存储源「%s」的 %d 条%s线路都已停用", label, primary.Name, len(onPrimary), label)
	}
	code, extra := IssueNoLineAtAll, "请先到「存储线路」添加一条线路并启用。"
	if countUsable(all) > 0 {
		code = IssueNoLineOnPrimary
		extra = fmt.Sprintf("现有的%s线路都挂在别的存储源上，取不到写入这里的资源。请到「存储线路」为它添加一条线路，或把主存储改成已有线路的那个存储源。", label)
	}
	return []DeliveryIssue{{
		Kind: kind, Code: code,
		Message: fmt.Sprintf("%s：新上传的%s资源前台取不到（%s）。%s", head, label, kindSymptom(kind), extra),
	}}
}

// countUsable 统计会被下发的线路：active 与 watch 都会下发，disabled 不会。
func countUsable(lines []Line) int {
	n := 0
	for _, l := range lines {
		if l.Status != LineDisabled {
			n++
		}
	}
	return n
}

func kindLabel(k LineKind) string {
	if k == LineImage {
		return "图片"
	}
	return "视频"
}

// kindSymptom 是这类线路缺失时用户侧看到的现象，写进提示里便于运营对号入座。
func kindSymptom(k LineKind) string {
	if k == LineImage {
		return "封面、海报等图片不显示"
	}
	return "播放时提示「暂无可用线路，请稍后重试」"
}

// Update 保存存储设置；校验候选数量、有效期范围与主存储的存在性和用途。
func (s *SettingsService) Update(ctx context.Context, in SettingsInput) (Settings, error) {
	if in.CandidateCount < 1 || in.CandidateCount > 5 {
		return Settings{}, badRequest("候选线路数量为 1–5")
	}
	if in.DefaultTTL < MinPresignTTL || in.DefaultTTL > MaxPresignTTL {
		return Settings{}, badRequest("默认链接有效期为 %d–%d 秒", MinPresignTTL, MaxPresignTTL)
	}
	if err := s.checkPrimary(ctx, in.PrimaryVideoProviderID, LineVideo); err != nil {
		return Settings{}, err
	}
	if err := s.checkPrimary(ctx, in.PrimaryImageProviderID, LineImage); err != nil {
		return Settings{}, err
	}
	return s.repo.UpdateSettings(ctx, in)
}

func (s *SettingsService) checkPrimary(ctx context.Context, id int64, kind LineKind) error {
	if id == 0 {
		return nil
	}
	p, err := s.repo.Get(ctx, id)
	if err != nil {
		return badRequest("指定的主存储不存在")
	}
	if !purposeAllowsKind(p.Purpose, kind) {
		return badRequest("主存储「%s」的用途不支持作为 %s 主存储", p.Name, kind)
	}
	return nil
}
