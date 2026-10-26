package user

// 后台用户管理（S2-7 / A2-2，docs/06 第 26 节）：列表、详情、改账号状态。
//
// 不提供重设用户密码（adr/0001）：账号只有用户名 + 密码，没有找回渠道，后台也不开后门。
// 删除 / 回收站暂未接入（见 docs/06 第 26 节的说明）。
// 游客（guest_device）只读：该表没有账号状态列，约束游客只能用「游客互动」总开关。

import (
	"context"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"xsite/service/internal/errs"
	"xsite/service/internal/textnorm"
)

const (
	adminDefaultPageSize = 20
	adminMaxPageSize     = 100
	adminMaxPage         = 10000
	adminMaxQueryRunes   = 50 // 后台搜索框的长度上限
)

// PageRequest 是后台列表的页码参数；0 表示取默认值。
type PageRequest struct {
	Page     int
	PageSize int
}

// Page 是后台列表的一页。
type Page[T any] struct {
	List     []T   `json:"list"`
	Total    int64 `json:"total"`
	Page     int   `json:"page"`
	PageSize int   `json:"page_size"`
}

// AdminUser 是后台列表里的注册用户。不含密码哈希：后台没有任何接口读它。
type AdminUser struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
	Nickname string `json:"nickname"`
	Avatar   string `json:"avatar"`
	Bio      string `json:"bio"`
	Status   Status `json:"status"`
	Locale   string `json:"locale"`
	// VerifiedAt 为空表示未做过实名 / 验证（目前还没有写入它的业务）。
	VerifiedAt *time.Time `json:"verified_at"`
	CreatedAt  time.Time  `json:"created_at"`
}

// AdminUserDetail 是用户详情：比列表多出资料审核状态、看剧历史开关与社区足迹。
type AdminUserDetail struct {
	AdminUser
	NicknameStatus string         `json:"nickname_status"`
	BioStatus      string         `json:"bio_status"`
	AvatarStatus   string         `json:"avatar_status"`
	HistoryPaused  bool           `json:"history_paused"`
	Stats          AdminUserStats `json:"stats"`
}

// AdminUserStats 是详情里的社区足迹。docs/06 第 26 节还要求会员状态、等级成长值、金币余额、
// 邀请关系与风控标记，那些表要等 S3-10（会员与支付）与 M6（福利体系），这里先不含。
type AdminUserStats struct {
	CommentCount  int64 `json:"comment_count"` // 未删除的评论数
	DanmakuCount  int64 `json:"danmaku_count"`
	ReportSent    int64 `json:"report_sent"`    // 他发出的举报
	ReportAgainst int64 `json:"report_against"` // 针对他所发评论 / 弹幕的举报
	FollowCount   int64 `json:"follow_count"`
	HistoryCount  int64 `json:"history_count"`
	SessionCount  int64 `json:"session_count"` // 当前有效会话（登录设备）数
}

// Guest 是后台列表里的游客设备。MergedUserID 非空表示该设备的数据已并入某个注册账号。
type Guest struct {
	ID             string    `json:"id"`
	FirstSeen      time.Time `json:"first_seen"`
	SourceChannel  string    `json:"source_channel"`
	Platform       string    `json:"platform"`
	AttestLevel    string    `json:"attest_level"`
	MergedUserID   *int64    `json:"merged_user_id"`
	MergedUsername string    `json:"merged_username"`
}

// AdminFilter 是后台列表的筛选条件。Status 为空表示不筛。
type AdminFilter struct {
	Q      string
	Status string
	PageRequest
}

// AdminQuery 是传给仓储的已归一参数：Pattern 是 LIKE 模式（已转小写、已转义），空表示不搜。
type AdminQuery struct {
	Pattern string
	Status  string
	Limit   int
	Offset  int
}

// AdminRepo 读写后台用户管理所需的数据（实现见 internal/store/pg）。
type AdminRepo interface {
	ListUsers(ctx context.Context, q AdminQuery) ([]AdminUser, int64, error)
	UserDetail(ctx context.Context, id int64) (AdminUserDetail, error)
	// SetUserStatus 改账号状态；用户不存在或在回收站里返回 errs.ErrNotFound。
	SetUserStatus(ctx context.Context, id int64, status Status) (AdminUser, error)
	ListGuests(ctx context.Context, q AdminQuery) ([]Guest, int64, error)
}

// SessionRevoker 吊销某注册用户的全部会话（identity.Service 实现）。
type SessionRevoker interface {
	RevokeAllSessions(ctx context.Context, userID int64, reason string) error
}

// AdminService 是后台用户管理域服务。
type AdminService struct {
	repo    AdminRepo
	revoker SessionRevoker
}

func NewAdminService(repo AdminRepo, revoker SessionRevoker) *AdminService {
	return &AdminService{repo: repo, revoker: revoker}
}

// ListUsers 返回注册用户列表（页码分页，按注册时间倒序），可搜用户名 / 昵称、按账号状态筛。
func (s *AdminService) ListUsers(ctx context.Context, f AdminFilter) (Page[AdminUser], error) {
	q, err := adminQuery(f, true)
	if err != nil {
		return Page[AdminUser]{}, err
	}
	list, total, err := s.repo.ListUsers(ctx, q)
	if err != nil {
		return Page[AdminUser]{}, err
	}
	return adminPage(list, total, f.PageRequest), nil
}

// ListGuests 返回游客设备列表（按首次出现倒序），可搜设备编号或已并入的账号用户名。
// 游客没有账号状态，Status 筛选在这里不适用（传了就报错，避免前端误以为筛生效了）。
func (s *AdminService) ListGuests(ctx context.Context, f AdminFilter) (Page[Guest], error) {
	q, err := adminQuery(f, false)
	if err != nil {
		return Page[Guest]{}, err
	}
	list, total, err := s.repo.ListGuests(ctx, q)
	if err != nil {
		return Page[Guest]{}, err
	}
	return adminPage(list, total, f.PageRequest), nil
}

// GetUser 返回用户详情。
func (s *AdminService) GetUser(ctx context.Context, id int64) (AdminUserDetail, error) {
	if id <= 0 {
		return AdminUserDetail{}, errs.ErrNotFound
	}
	return s.repo.UserDetail(ctx, id)
}

// SetStatus 改账号状态。
//
// 封禁要即时生效（docs/11 S2-7）。Access Token 是无状态 JWT（15 分钟），改状态挡不住它，所以这里补两件事：
//  1. 吊销全部会话，让 Refresh 族立刻作废 —— 否则被封禁的账号还能靠 30 天滑动有效的 Refresh 一直续期；
//  2. 发评论 / 弹幕的前置校验把 banned 一并拦下（见 db/queries/user.sql 的 UserMuted）——
//     否则手上那张最多还活 15 分钟的 Access Token 仍能发言。
//
// 禁言与恢复正常不吊销会话：禁言只限制发言，不该顺带把人踢下线；恢复正常时旧会话早已在封禁时作废。
func (s *AdminService) SetStatus(ctx context.Context, id int64, status string) (AdminUser, error) {
	st, err := ParseStatus(status)
	if err != nil {
		return AdminUser{}, err
	}
	if id <= 0 {
		return AdminUser{}, errs.ErrNotFound
	}
	u, err := s.repo.SetUserStatus(ctx, id, st)
	if err != nil {
		return AdminUser{}, err
	}
	if st != StatusBanned {
		return u, nil
	}
	if err := s.revoker.RevokeAllSessions(ctx, id, "banned_by_admin"); err != nil {
		// 状态已落库，只是会话没吊销干净。报错让运营重试（再点一次封禁会重新吊销），
		// 不能当成功返回：那就回到了「封禁只挡登录」的半失效状态。
		return AdminUser{}, fmt.Errorf("账号已置为封禁，但下线其登录设备失败，请重试: %w", err)
	}
	return u, nil
}

// ParseStatus 校验账号状态取值。
func ParseStatus(s string) (Status, error) {
	switch Status(s) {
	case StatusNormal, StatusMuted, StatusBanned:
		return Status(s), nil
	}
	return "", errs.ErrBadRequest.WithMessage("账号状态只能是 normal、muted 或 banned")
}

// adminQuery 校验并归一筛选条件。allowStatus=false 时不接受状态筛选（游客没有账号状态）。
func adminQuery(f AdminFilter, allowStatus bool) (AdminQuery, error) {
	limit, offset, err := adminLimitOffset(f.PageRequest)
	if err != nil {
		return AdminQuery{}, err
	}
	if f.Status != "" {
		if !allowStatus {
			return AdminQuery{}, errs.ErrBadRequest.WithMessage("游客没有账号状态，不能按状态筛选")
		}
		if _, err := ParseStatus(f.Status); err != nil {
			return AdminQuery{}, err
		}
	}
	q := textnorm.Search(f.Q)
	if utf8.RuneCountInString(q) > adminMaxQueryRunes {
		return AdminQuery{}, errs.ErrBadRequest.WithMessage(fmt.Sprintf("搜索词不能超过 %d 个字", adminMaxQueryRunes))
	}
	return AdminQuery{Pattern: adminLikePattern(q), Status: f.Status, Limit: limit, Offset: offset}, nil
}

// adminPage 包装一页结果，补回归一后的页码。
func adminPage[T any](list []T, total int64, p PageRequest) Page[T] {
	if p.Page == 0 {
		p.Page = 1
	}
	if p.PageSize == 0 {
		p.PageSize = adminDefaultPageSize
	}
	if list == nil {
		list = []T{} // nil 切片会序列化成 JSON null，契约声明的是数组
	}
	return Page[T]{List: list, Total: total, Page: p.Page, PageSize: p.PageSize}
}

// adminLimitOffset 把页码换成 LIMIT / OFFSET。
func adminLimitOffset(p PageRequest) (int, int, error) {
	if p.Page == 0 {
		p.Page = 1
	}
	if p.PageSize == 0 {
		p.PageSize = adminDefaultPageSize
	}
	if p.Page < 1 || p.Page > adminMaxPage || p.PageSize < 1 || p.PageSize > adminMaxPageSize {
		return 0, 0, errs.ErrBadRequest.WithMessage(
			fmt.Sprintf("分页参数错误：page 为 1–%d，page_size 为 1–%d", adminMaxPage, adminMaxPageSize))
	}
	return p.PageSize, (p.Page - 1) * p.PageSize, nil
}

var adminLikeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

// adminLikePattern 把搜索词转为包含匹配的 LIKE 模式（转义 \ % _）；空词返回空。
// textnorm.Search 已转小写，与 00045 建在 lower(username) / lower(nickname) 上的索引对齐。
func adminLikePattern(q string) string {
	if q == "" {
		return ""
	}
	return "%" + adminLikeEscaper.Replace(q) + "%"
}
