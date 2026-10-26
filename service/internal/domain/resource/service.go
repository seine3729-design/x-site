package resource

import (
	"context"

	"xsite/service/internal/netguard"
)

// Policy 是随运行环境变化的规则。
type Policy struct {
	// ExplicitTLSOnly 为 true 时只允许显式 TLS（非开发环境，docs/10 第 2 节）；worker 连接前还会再校验一次。
	ExplicitTLSOnly bool
	// Egress 是出站地址策略：地址是被禁止的 IP 字面量时保存即拒绝；域名由 worker 在连接前解析并校验。
	Egress netguard.Policy
}

// Service 是资源机的后台规则。
type Service struct {
	repo   Repo
	prober Prober
	policy Policy
}

func NewService(repo Repo, prober Prober, policy Policy) *Service {
	return &Service{repo: repo, prober: prober, policy: policy}
}

// ListFTPServers 返回资源机列表的一页（按编号倒序）。
func (s *Service) ListFTPServers(ctx context.Context, f Filter) (Page[FTPServer], error) {
	pattern, err := cleanQuery(f.Q)
	if err != nil {
		return Page[FTPServer]{}, err
	}
	if f.Status != "" && f.Status != StatusActive && f.Status != StatusInactive {
		return Page[FTPServer]{}, badRequest("状态筛选无效")
	}
	page, size, limit, offset, err := limitOffset(f.Page, f.PageSize)
	if err != nil {
		return Page[FTPServer]{}, err
	}
	list, total, err := s.repo.ListFTPServers(ctx, Query{Pattern: pattern, Status: f.Status, Limit: limit, Offset: offset})
	if err != nil {
		return Page[FTPServer]{}, err
	}
	return Page[FTPServer]{List: list, Total: total, Page: page, PageSize: size}, nil
}

// CreateFTPServer 新建资源机；凭据标识留空时自动生成。
func (s *Service) CreateFTPServer(ctx context.Context, in FTPServerInput) (FTPServer, error) {
	in, err := s.clean(in)
	if err != nil {
		return FTPServer{}, err
	}
	if in.CredRef == "" {
		in.CredRef = newCredRef()
	}
	return s.repo.CreateFTPServer(ctx, in)
}

// UpdateFTPServer 整体保存资源机，返回修改前后的记录。凭据标识留空时保持原值：
// 换了标识就要重新配置账号密码，不能因为表单没带上就悄悄换掉。
func (s *Service) UpdateFTPServer(ctx context.Context, id int64, in FTPServerInput) (before, after FTPServer, err error) {
	if in, err = s.clean(in); err != nil {
		return
	}
	if before, err = s.repo.GetFTPServer(ctx, id); err != nil {
		return
	}
	if in.CredRef == "" {
		in.CredRef = before.CredRef
	}
	after, err = s.repo.UpdateFTPServer(ctx, id, in)
	return
}

// DeleteFTPServer 删除资源机（不进回收站），返回删除前的记录。
func (s *Service) DeleteFTPServer(ctx context.Context, id int64) (FTPServer, error) {
	return s.repo.DeleteFTPServer(ctx, id)
}

// TestFTPServer 测连：由 worker 解析地址、连接、登录并读取登录目录，返回每一步的结果。停用的资源机也可以测。
func (s *Service) TestFTPServer(ctx context.Context, id int64) (ProbeResult, error) {
	srv, err := s.repo.GetFTPServer(ctx, id)
	if err != nil {
		return ProbeResult{}, err
	}
	return s.prober.Probe(ctx, ProbeRequest{Op: ProbeTest, Server: srv})
}

// ListFTPDir 打开目录：path 为空时是登录目录。
func (s *Service) ListFTPDir(ctx context.Context, id int64, path string) (ProbeResult, error) {
	path, err := cleanPath(path)
	if err != nil {
		return ProbeResult{}, err
	}
	srv, err := s.repo.GetFTPServer(ctx, id)
	if err != nil {
		return ProbeResult{}, err
	}
	return s.prober.Probe(ctx, ProbeRequest{Op: ProbeList, Server: srv, Path: path})
}
