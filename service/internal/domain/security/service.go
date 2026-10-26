package security

import (
	"context"
	"strings"
)

const (
	defaultLogPageSize = 20
	maxLogPageSize     = 100
)

// LoginLogRepo 读取管理员登录记录（admin_login_log）。
type LoginLogRepo interface {
	ListLoginLogs(ctx context.Context, f LoginLogFilter) ([]LoginLog, int64, error)
}

// Service 是安全中心的只读聚合。keys 是启动时从各密钥环境变量收集的版本快照（只含 kid）。
type Service struct {
	logs LoginLogRepo
	keys []KeyVersion
}

// NewService 构造 Service。keys 由 app 装配时用 ParseKeyVersions 从各密钥配置收集。
func NewService(logs LoginLogRepo, keys []KeyVersion) *Service {
	return &Service{logs: logs, keys: keys}
}

// LoginLogs 返回管理员登录记录（页码分页，倒序），并填充结果中文名与异常标记。
func (s *Service) LoginLogs(ctx context.Context, f LoginLogFilter) (LoginLogPage, error) {
	clean := cleanLoginFilter(f)
	list, total, err := s.logs.ListLoginLogs(ctx, clean)
	if err != nil {
		return LoginLogPage{}, err
	}
	for i := range list {
		list[i].ResultTitle = resultTitle(list[i].Result)
		list[i].Suspicious = list[i].Result != "success"
	}
	return LoginLogPage{
		List: list, Total: total, Page: clean.Page, PageSize: clean.PageSize, Results: resultInfos(),
	}, nil
}

// KeyRotations 返回当前启用的各密钥版本（只有版本号与用途，没有密钥内容）。
func (s *Service) KeyRotations(_ context.Context) []KeyVersion {
	out := make([]KeyVersion, len(s.keys))
	copy(out, s.keys)
	return out
}

// Risk 返回风控概览。实时计数源接入前，Value 为空、以 Note 说明。
func (s *Service) Risk(_ context.Context) RiskOverview {
	return RiskOverview{
		Items: []RiskItem{
			{Kind: "rate_limited", Title: "限流命中", Desc: "触发限流的请求与 Top 来源，需要限流埋点汇总"},
			{Kind: "flagged_devices", Title: "异常设备", Desc: "被行为识别标记为异常的设备数"},
			{Kind: "challenge", Title: "人机验证触发", Desc: "触发人机验证（PoW / 验证码）的次数"},
		},
		Note: "风控实时计数将在限流与人机验证的埋点汇总接入后填充；目前展示指标口径。",
	}
}

func cleanLoginFilter(f LoginLogFilter) LoginLogFilter {
	out := LoginLogFilter{Since: f.Since, Until: f.Until}
	if validResult(f.Result) {
		out.Result = f.Result
	}
	out.Q = strings.TrimSpace(f.Q)
	if out.Q != "" {
		out.Pattern = "%" + likeEscaper.Replace(out.Q) + "%"
	}
	out.PageSize = f.PageSize
	if out.PageSize < 1 || out.PageSize > maxLogPageSize {
		out.PageSize = defaultLogPageSize
	}
	out.Page = f.Page
	if out.Page < 1 {
		out.Page = 1
	}
	return out
}

var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
