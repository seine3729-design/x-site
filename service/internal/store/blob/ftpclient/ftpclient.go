// Package ftpclient 在 worker 上连接资源机，执行测连与列目录（S1-6，docs/specs/S1-6.md）。
// 只有 worker 使用：连接由 worker 出口发起，不暴露 API 节点 IP（docs/03 第 9.7 节）。
package ftpclient

import (
	"cmp"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"net/textproto"
	"slices"
	"strconv"
	"strings"
	"time"

	jftp "github.com/jlaffaye/ftp"

	"xsite/service/internal/domain/resource"
	"xsite/service/internal/netguard"
)

// Credentials 按凭据标识查找资源机账号密码（config.FTPCredentials）。
type Credentials interface {
	Lookup(ref string) (user, password string, ok bool)
}

const (
	defaultTimeout     = 15 * time.Second
	defaultDialTimeout = 10 * time.Second
)

// Prober 执行测连与列目录。Creds 为 nil 时视为没有配置任何账号密码。
type Prober struct {
	Creds Credentials
	// Egress 是出站地址策略（SSRF）：先解析并校验全部 IP，之后每条连接（含数据连接）都按 IP 直连并再校验。
	Egress netguard.Policy
	// ExplicitTLSOnly 为 true 时只允许显式 TLS（非开发环境，docs/10 第 2 节）。
	ExplicitTLSOnly bool
	// Resolver 为 nil 时用 net.DefaultResolver。
	Resolver netguard.Resolver
	// RootCAs 为 nil 时按系统根证书校验资源机证书（不提供跳过校验的开关）；测试中替换为测试 CA。
	RootCAs *x509.CertPool
	// Timeout 是一次操作的总时长上限，默认 15 秒；DialTimeout 是单次建连上限，默认 10 秒。
	Timeout     time.Duration
	DialTimeout time.Duration
}

// Probe 依次执行解析、连接、登录、读取目录，某一步失败后停止。探测失败写在结果的步骤里，不返回 error。
func (p *Prober) Probe(ctx context.Context, req resource.ProbeRequest) resource.ProbeResult {
	start := time.Now()
	ctx, cancel := context.WithTimeout(ctx, cmp.Or(p.Timeout, defaultTimeout))
	defer cancel()

	r := &run{p: p, req: req, ctx: ctx}
	res := resource.ProbeResult{Entries: []resource.Entry{}}
	res.OK = r.probe(&res)
	res.Steps = r.steps
	res.ElapsedMs = time.Since(start).Milliseconds()
	slog.InfoContext(ctx, "资源机探测完成", "server_id", req.Server.ID, "op", req.Op, "ok", res.OK, "elapsed_ms", res.ElapsedMs)
	return res
}

// run 是一次探测的状态。jlaffaye/ftp 的连接不支持并发，回调都在同一个 goroutine 中执行。
type run struct {
	p        *Prober
	req      resource.ProbeRequest
	ctx      context.Context
	steps    []resource.Step
	dials    int                  // 已建立的 TCP 连接数：第一条是控制连接，之后是数据连接
	remote   string               // 控制连接的对端地址
	tlsState *tls.ConnectionState // 控制连接的 TLS 握手结果
}

func (r *run) probe(res *resource.ProbeResult) bool {
	srv := r.req.Server
	var ips []netip.Addr
	if !r.step(resource.StepResolve, func() (string, error) {
		if err := r.p.checkPolicy(srv); err != nil {
			return "", err
		}
		var err error
		if ips, err = r.p.Egress.Resolve(r.ctx, r.p.Resolver, srv.Host); err != nil {
			return "", err
		}
		return joinIPs(ips), nil
	}) {
		return false
	}

	var conn *jftp.ServerConn
	if !r.step(resource.StepConnect, func() (string, error) {
		var err error
		if conn, err = r.dial(ips); err != nil {
			return "", err
		}
		return r.connectedMessage(), nil
	}) {
		return false
	}
	defer func() { _ = conn.Quit() }()

	if !r.step(resource.StepLogin, func() (string, error) {
		user, password, ok := r.lookupCreds()
		if !ok {
			return "", errNoCredentials
		}
		if err := conn.Login(user, password); err != nil {
			return "", err
		}
		return "登录成功", nil
	}) {
		return false
	}

	return r.step(resource.StepList, func() (string, error) {
		if r.req.Op == resource.ProbeList && r.req.Path != "" {
			if err := conn.ChangeDir(r.req.Path); err != nil {
				return "", &dirError{err: err}
			}
		}
		cwd, err := conn.CurrentDir()
		if err != nil {
			return "", err
		}
		list, err := conn.List("")
		if err != nil {
			return "", err
		}
		entries := entriesOf(list)
		res.Path = cwd
		if r.req.Op == resource.ProbeList {
			res.Entries, res.Truncated = entries, false
			if len(entries) > resource.MaxEntries {
				res.Entries, res.Truncated = entries[:resource.MaxEntries], true
			}
		}
		return fmt.Sprintf("目录 %s 共 %d 项", cwd, len(entries)), nil
	})
}

// step 执行一步并记录耗时；失败时把错误转成中文说明，原始错误只进日志。
func (r *run) step(name resource.StepName, fn func() (string, error)) bool {
	start := time.Now()
	msg, err := fn()
	s := resource.Step{Name: name, OK: err == nil, Ms: time.Since(start).Milliseconds(), Message: msg}
	if err != nil {
		s.Message = r.describe(name, err)
		slog.WarnContext(r.ctx, "资源机探测失败",
			"server_id", r.req.Server.ID, "op", r.req.Op, "step", name, "err", err)
	}
	r.steps = append(r.steps, s)
	return err == nil
}

func (r *run) lookupCreds() (string, string, bool) {
	if r.p.Creds == nil {
		return "", "", false
	}
	user, password, ok := r.p.Creds.Lookup(r.req.Server.CredRef)
	return user, password, ok && user != "" && password != ""
}

func (p *Prober) checkPolicy(srv resource.FTPServer) error {
	switch srv.TLS {
	case resource.TLSExplicit:
	case resource.TLSNone, resource.TLSImplicit:
		if p.ExplicitTLSOnly {
			return errTLSPolicy
		}
	default:
		return errTLSMode
	}
	if !srv.Passive {
		return errActiveMode
	}
	if srv.Port < 1 || srv.Port > 65535 {
		return errPort
	}
	return nil
}

// dial 建立控制连接。控制连接依次尝试解析出的 IP；数据连接只连控制连接的对端 IP
// （jlaffaye/ftp 默认忽略 PASV 返回的地址，EPSV 本身不带地址），并再按出站策略校验一次。
func (r *run) dial(ips []netip.Addr) (*jftp.ServerConn, error) {
	srv := r.req.Server
	var tlsCfg *tls.Config
	if srv.TLS != resource.TLSNone {
		tlsCfg = r.tlsConfig()
	}
	dialFunc := func(_, address string) (net.Conn, error) {
		r.dials++
		control := r.dials == 1
		var targets []netip.AddrPort
		if control {
			for _, ip := range ips {
				targets = append(targets, netip.AddrPortFrom(ip, uint16(srv.Port)))
			}
		} else {
			ap, err := netip.ParseAddrPort(address)
			if err != nil {
				return nil, fmt.Errorf("数据连接地址无效 %q: %w", address, err)
			}
			targets = []netip.AddrPort{ap}
		}
		raw, err := r.dialTCP(targets)
		if err != nil {
			return nil, err
		}
		if control {
			r.remote = raw.RemoteAddr().String()
		}
		// 显式 TLS 的控制连接先明文收发，AUTH TLS 之后由 jlaffaye/ftp 升级；其余连接（含 PROT P 的数据连接）直接是 TLS
		if tlsCfg == nil || (control && srv.TLS == resource.TLSExplicit) {
			return raw, nil
		}
		return tls.Client(raw, tlsCfg), nil
	}

	opts := []jftp.DialOption{jftp.DialWithDialFunc(dialFunc)}
	switch srv.TLS {
	case resource.TLSExplicit:
		opts = append(opts, jftp.DialWithExplicitTLS(tlsCfg))
	case resource.TLSImplicit:
		opts = append(opts, jftp.DialWithTLS(tlsCfg))
	}
	conn, err := jftp.Dial(net.JoinHostPort(ips[0].String(), strconv.Itoa(int(srv.Port))), opts...)
	if err != nil {
		return nil, err
	}
	if srv.TLS == resource.TLSExplicit {
		// AUTH TLS 之后 jlaffaye/ftp 要到下一次读写才握手。先发 NOOP 完成握手，证书问题算在「连接」这一步；
		// 有的服务器登录前不接受 NOOP（回 530），收到应答说明握手已经完成，不算失败。
		var reply *textproto.Error
		if err := conn.NoOp(); err != nil && !errors.As(err, &reply) {
			_ = conn.Quit()
			return nil, err
		}
	}
	return conn, nil
}

// dialTCP 按顺序尝试目标地址，返回第一条建立成功的连接。连接的读写截止时间是整个操作的截止时间，
// 操作被取消（River 撤销任务）时立即中断阻塞的读写。
func (r *run) dialTCP(targets []netip.AddrPort) (net.Conn, error) {
	d := net.Dialer{Timeout: cmp.Or(r.p.DialTimeout, defaultDialTimeout)}
	deadline, _ := r.ctx.Deadline()
	var lastErr error
	for _, t := range targets {
		if err := r.p.Egress.Check(t.Addr()); err != nil {
			return nil, err
		}
		conn, err := d.DialContext(r.ctx, "tcp", t.String())
		if err != nil {
			lastErr = err
			continue
		}
		if err := conn.SetDeadline(deadline); err != nil {
			_ = conn.Close()
			return nil, err
		}
		context.AfterFunc(r.ctx, func() { _ = conn.SetDeadline(time.Now()) })
		return conn, nil
	}
	return nil, lastErr
}

// tlsConfig 按地址（域名或 IP）校验资源机证书。数据连接复用控制连接的 TLS 会话（vsftpd 默认要求复用）。
func (r *run) tlsConfig() *tls.Config {
	return &tls.Config{
		ServerName:         r.req.Server.Host,
		RootCAs:            r.p.RootCAs,
		MinVersion:         tls.VersionTLS12,
		ClientSessionCache: tls.NewLRUClientSessionCache(4),
		VerifyConnection: func(cs tls.ConnectionState) error {
			if r.tlsState == nil {
				r.tlsState = &cs
			}
			return nil
		},
	}
}

func (r *run) connectedMessage() string {
	if r.req.Server.TLS == resource.TLSNone {
		return "已连接 " + r.remote + "（未加密，只允许在开发环境使用）"
	}
	version := "TLS"
	if r.tlsState != nil {
		version = tls.VersionName(r.tlsState.Version)
	}
	return "已连接 " + r.remote + "，" + version
}

func joinIPs(ips []netip.Addr) string {
	s := make([]string, len(ips))
	for i, ip := range ips {
		s[i] = ip.String()
	}
	return "解析到 " + strings.Join(s, "、")
}

// entriesOf 转换目录条目：去掉 . 与 ..（以及 MLSD 中带路径的当前目录项），目录在前、按名称排序。
func entriesOf(list []*jftp.Entry) []resource.Entry {
	out := make([]resource.Entry, 0, len(list))
	for _, e := range list {
		if e == nil || e.Name == "" || e.Name == "." || e.Name == ".." || strings.Contains(e.Name, "/") {
			continue
		}
		item := resource.Entry{Name: e.Name, Type: resource.EntryFile, Size: e.Size, Target: e.Target}
		switch e.Type {
		case jftp.EntryTypeFolder:
			item.Type, item.Size = resource.EntryDir, 0
		case jftp.EntryTypeLink:
			item.Type = resource.EntryLink
		}
		if !e.Time.IsZero() {
			t := e.Time.UTC()
			item.ModifiedAt = &t
		}
		out = append(out, item)
	}
	slices.SortFunc(out, func(a, b resource.Entry) int {
		if ad, bd := a.Type == resource.EntryDir, b.Type == resource.EntryDir; ad != bd {
			if ad {
				return -1
			}
			return 1
		}
		return strings.Compare(a.Name, b.Name)
	})
	return out
}
