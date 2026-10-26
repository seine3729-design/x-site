package ftpclient

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"slices"
	"strings"
	"testing"
	"time"

	"xsite/service/internal/domain/resource"
	"xsite/service/internal/netguard"
)

type creds map[string][2]string

func (c creds) Lookup(ref string) (string, string, bool) {
	v, ok := c[ref]
	return v[0], v[1], ok
}

// staticResolver 把任意域名解析到固定地址（测试用域名与证书校验）。
type staticResolver []netip.Addr

func (s staticResolver) LookupNetIP(context.Context, string, string) ([]netip.Addr, error) {
	return s, nil
}

var loopback = staticResolver{netip.MustParseAddr("127.0.0.1")}

func server(port int32, tls resource.TLSMode) resource.FTPServer {
	return resource.FTPServer{ID: 7, Name: "测试资源机", Host: "127.0.0.1", Port: port, TLS: tls, Passive: true, CredRef: "TEST"}
}

// devProber 是开发环境的策略：允许回环地址、三种加密方式。
func devProber(t *testing.T) *Prober {
	t.Helper()
	return &Prober{Creds: creds{"TEST": {"xsite", "s3cret"}}, Egress: netguard.Policy{AllowPrivate: true}, Timeout: 5 * time.Second}
}

func stepNames(res resource.ProbeResult) string {
	var b strings.Builder
	for _, s := range res.Steps {
		mark := "✓"
		if !s.OK {
			mark = "✗"
		}
		fmt.Fprintf(&b, "%s%s ", s.Name, mark)
	}
	return strings.TrimSpace(b.String())
}

func lastStep(t *testing.T, res resource.ProbeResult) resource.Step {
	t.Helper()
	if len(res.Steps) == 0 {
		t.Fatal("没有执行任何步骤")
	}
	return res.Steps[len(res.Steps)-1]
}

// expectFail 断言探测在 step 失败，提示包含 want。
func expectFail(t *testing.T, res resource.ProbeResult, step resource.StepName, want string) {
	t.Helper()
	last := lastStep(t, res)
	if res.OK || last.OK || last.Name != step || !strings.Contains(last.Message, want) {
		t.Fatalf("应在 %s 失败并提示「%s」，得到 %s：%+v", step, want, stepNames(res), last)
	}
}

func TestProbe_PlainTestAndList(t *testing.T) {
	f := startFake(t, resource.TLSNone, nil, nil)
	p := devProber(t)
	ctx := context.Background()

	res := p.Probe(ctx, resource.ProbeRequest{Op: resource.ProbeTest, Server: server(f.port(), resource.TLSNone)})
	if !res.OK || stepNames(res) != "resolve✓ connect✓ login✓ list✓" {
		t.Fatalf("测连应四步全部成功：%s %+v", stepNames(res), res.Steps)
	}
	if res.Path != "/" || len(res.Entries) != 0 || !strings.Contains(res.Steps[3].Message, "共 2 项") ||
		!strings.Contains(res.Steps[1].Message, "未加密") || res.Steps[0].Message != "解析到 127.0.0.1" {
		t.Fatalf("测连只报告登录目录与条目数：%+v", res)
	}

	res = p.Probe(ctx, resource.ProbeRequest{Op: resource.ProbeList, Server: server(f.port(), resource.TLSNone), Path: "/剧集"})
	if !res.OK || res.Path != "/剧集" || res.Truncated {
		t.Fatalf("列目录应成功：%s %+v", stepNames(res), res)
	}
	// 去掉 cdir / pdir，目录在前、按名称排序；目录大小为 0，时间为 UTC
	var names []string
	for _, e := range res.Entries {
		names = append(names, string(e.Type)+":"+e.Name)
	}
	if !slices.Equal(names, []string{"dir:a-剧", "dir:霸道总裁", "file:b.txt"}) {
		t.Fatalf("条目不符合预期：%v", names)
	}
	if e := res.Entries[1]; e.ModifiedAt == nil || !e.ModifiedAt.Equal(time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC)) || e.Size != 0 {
		t.Fatalf("修改时间应按 UTC 解析：%+v", e)
	}
	if e := res.Entries[0]; e.ModifiedAt != nil {
		t.Fatalf("服务器没给时间时为 nil：%+v", e)
	}
	if e := res.Entries[2]; e.Size != 5 {
		t.Fatalf("文件大小：%+v", e)
	}

	// 相对路径按当前目录解析，path 为服务器返回的实际路径
	res = p.Probe(ctx, resource.ProbeRequest{Op: resource.ProbeList, Server: server(f.port(), resource.TLSNone), Path: "剧集/霸道总裁"})
	if !res.OK || res.Path != "/剧集/霸道总裁" || len(res.Entries) != 0 {
		t.Fatalf("空目录应成功：%+v", res)
	}

	res = p.Probe(ctx, resource.ProbeRequest{Op: resource.ProbeList, Server: server(f.port(), resource.TLSNone), Path: "/不存在"})
	expectFail(t, res, resource.StepList, "无法进入目录：550 Failed to change directory.")
}

func TestProbe_ExplicitAndImplicitTLS(t *testing.T) {
	cfg, pool := testCA(t)
	for _, mode := range []resource.TLSMode{resource.TLSExplicit, resource.TLSImplicit} {
		t.Run(string(mode), func(t *testing.T) {
			f := startFake(t, mode, cfg, nil)
			p := devProber(t)
			p.RootCAs = pool
			res := p.Probe(context.Background(), resource.ProbeRequest{Op: resource.ProbeList, Server: server(f.port(), mode), Path: "/剧集"})
			if !res.OK || len(res.Entries) != 3 {
				t.Fatalf("应成功：%s %+v", stepNames(res), res.Steps)
			}
			if !strings.Contains(res.Steps[1].Message, "TLS 1.3") {
				t.Fatalf("连接一步应说明 TLS 版本：%q", res.Steps[1].Message)
			}
			cmds := strings.Join(f.commands(), "|")
			if !strings.Contains(cmds, "PBSZ 0|PROT P") || strings.Contains(cmds, "s3cret") {
				t.Fatalf("数据连接也应加密（PROT P），密码不应以明文出现在记录里：%s", cmds)
			}
			if mode == resource.TLSExplicit && !strings.HasPrefix(cmds, "AUTH TLS|NOOP|USER xsite") {
				t.Fatalf("显式 TLS 应先 AUTH TLS 并完成握手再登录：%s", cmds)
			}
		})
	}
}

func TestProbe_TLSFailures(t *testing.T) {
	cfg, pool := testCA(t)
	ctx := context.Background()

	// 不信任自签名证书（系统根证书）：在连接一步失败，不会发送账号密码
	f := startFake(t, resource.TLSExplicit, cfg, nil)
	res := devProber(t).Probe(ctx, resource.ProbeRequest{Op: resource.ProbeTest, Server: server(f.port(), resource.TLSExplicit)})
	expectFail(t, res, resource.StepConnect, "证书不是受信任的机构签发的")
	if slices.ContainsFunc(f.commands(), func(c string) bool { return strings.HasPrefix(c, "USER") }) {
		t.Fatal("证书校验失败后不能再登录")
	}

	// 证书与地址不符：域名 ftp.test 解析到 127.0.0.1，但证书只签了 127.0.0.1 与 localhost
	p := devProber(t)
	p.RootCAs, p.Resolver = pool, loopback
	srv := server(f.port(), resource.TLSExplicit)
	srv.Host = "ftp.test"
	expectFail(t, p.Probe(ctx, resource.ProbeRequest{Op: resource.ProbeTest, Server: srv}), resource.StepConnect, "证书与地址不符")
	// 按证书上的域名连接则成功
	srv.Host = "localhost"
	if res := p.Probe(ctx, resource.ProbeRequest{Op: resource.ProbeTest, Server: srv}); !res.OK {
		t.Fatalf("按证书上的域名应成功：%s %+v", stepNames(res), res.Steps)
	}

	// 服务器不支持 AUTH TLS
	f = startFake(t, resource.TLSExplicit, cfg, func(f *fakeFTP) { f.noAuth = true })
	p = devProber(t)
	p.RootCAs = pool
	expectFail(t, p.Probe(ctx, resource.ProbeRequest{Op: resource.ProbeTest, Server: server(f.port(), resource.TLSExplicit)}),
		resource.StepConnect, "服务器不支持显式 TLS")

	// 登录前拒绝 NOOP 的服务器：收到应答说明握手已完成，照常登录
	f = startFake(t, resource.TLSExplicit, cfg, func(f *fakeFTP) { f.strict = true })
	if res := p.Probe(ctx, resource.ProbeRequest{Op: resource.ProbeTest, Server: server(f.port(), resource.TLSExplicit)}); !res.OK {
		t.Fatalf("NOOP 被拒不影响测连：%s %+v", stepNames(res), res.Steps)
	}

	// 选了隐式 TLS，对方却是明文 FTP
	f = startFake(t, resource.TLSNone, nil, nil)
	expectFail(t, p.Probe(ctx, resource.ProbeRequest{Op: resource.ProbeTest, Server: server(f.port(), resource.TLSImplicit)}),
		resource.StepConnect, "对方没有按 TLS 应答")
}

func TestProbe_Login(t *testing.T) {
	f := startFake(t, resource.TLSNone, nil, nil)
	ctx := context.Background()

	p := devProber(t)
	p.Creds = creds{"TEST": {"xsite", "wrong"}}
	expectFail(t, p.Probe(ctx, resource.ProbeRequest{Op: resource.ProbeTest, Server: server(f.port(), resource.TLSNone)}),
		resource.StepLogin, "登录失败：530 Login incorrect.")

	// 没有配置账号密码：前两步照常执行（确认网络可达），登录一步给出要配置的两个环境变量名
	before := len(f.commands())
	p.Creds = creds{"OTHER": {"u", "p"}}
	res := p.Probe(ctx, resource.ProbeRequest{Op: resource.ProbeTest, Server: server(f.port(), resource.TLSNone)})
	expectFail(t, res, resource.StepLogin, "FTP_TEST_USER 和 FTP_TEST_PASSWORD")
	if stepNames(res) != "resolve✓ connect✓ login✗" {
		t.Fatalf("应执行到登录一步：%s", stepNames(res))
	}
	if slices.ContainsFunc(f.commands()[before:], func(c string) bool { return strings.HasPrefix(c, "USER") }) {
		t.Fatal("没有账号密码时不应发送 USER")
	}
	p.Creds = nil
	expectFail(t, p.Probe(ctx, resource.ProbeRequest{Op: resource.ProbeTest, Server: server(f.port(), resource.TLSNone)}),
		resource.StepLogin, "FTP_TEST_USER")
}

func TestProbe_PolicyBlocksBeforeConnecting(t *testing.T) {
	f := startFake(t, resource.TLSNone, nil, nil)
	ctx := context.Background()

	tests := []struct {
		name string
		p    *Prober
		srv  func() resource.FTPServer
		want string
	}{
		{"生产环境拦截回环地址", &Prober{ExplicitTLSOnly: true}, func() resource.FTPServer { return server(f.port(), resource.TLSExplicit) }, "地址 127.0.0.1 属于内网或保留地址段"},
		{"域名解析到内网也拦截", &Prober{ExplicitTLSOnly: true, Resolver: loopback}, func() resource.FTPServer {
			s := server(f.port(), resource.TLSExplicit)
			s.Host = "ftp.example.com"
			return s
		}, "属于内网或保留地址段"},
		{"生产环境不允许不加密", &Prober{ExplicitTLSOnly: true, Egress: netguard.Policy{AllowPrivate: true}}, func() resource.FTPServer { return server(f.port(), resource.TLSNone) }, "只允许显式 TLS"},
		{"生产环境不允许隐式 TLS", &Prober{ExplicitTLSOnly: true, Egress: netguard.Policy{AllowPrivate: true}}, func() resource.FTPServer { return server(f.port(), resource.TLSImplicit) }, "只允许显式 TLS"},
		{"不支持主动模式", devProber(t), func() resource.FTPServer {
			s := server(f.port(), resource.TLSNone)
			s.Passive = false
			return s
		}, "只支持被动模式"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := tt.p.Probe(ctx, resource.ProbeRequest{Op: resource.ProbeTest, Server: tt.srv()})
			expectFail(t, res, resource.StepResolve, tt.want)
		})
	}
	if n := f.connections(); n != 0 {
		t.Fatalf("被策略拦下时不应建立任何连接，实际 %d 条", n)
	}
}

func TestProbe_NetworkFailures(t *testing.T) {
	ctx := context.Background()

	// 端口没有监听
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := int32(ln.Addr().(*net.TCPAddr).Port)
	_ = ln.Close()
	expectFail(t, devProber(t).Probe(ctx, resource.ProbeRequest{Op: resource.ProbeTest, Server: server(port, resource.TLSNone)}),
		resource.StepConnect, "连接被拒绝")

	// 接受连接但不说话：在操作上限内超时
	f := startFake(t, resource.TLSNone, nil, func(f *fakeFTP) { f.silent = true })
	p := devProber(t)
	p.Timeout = 300 * time.Millisecond
	start := time.Now()
	res := p.Probe(ctx, resource.ProbeRequest{Op: resource.ProbeTest, Server: server(f.port(), resource.TLSNone)})
	expectFail(t, res, resource.StepConnect, "超时")
	if time.Since(start) > 2*time.Second {
		t.Fatalf("应在操作上限附近结束，实际 %v", time.Since(start))
	}

	// 取消（River 撤销任务）立即中断
	f = startFake(t, resource.TLSNone, nil, func(f *fakeFTP) { f.silent = true })
	cctx, cancel := context.WithCancel(ctx)
	time.AfterFunc(200*time.Millisecond, cancel)
	start = time.Now()
	res = devProber(t).Probe(cctx, resource.ProbeRequest{Op: resource.ProbeTest, Server: server(f.port(), resource.TLSNone)})
	if res.OK || time.Since(start) > 2*time.Second {
		t.Fatalf("取消后应很快结束：%v %s", time.Since(start), stepNames(res))
	}

	// 域名不存在
	nx := devProber(t)
	nx.Resolver = netguard.Resolver(failingResolver{})
	srv := server(21, resource.TLSNone)
	srv.Host = "missing.example.com"
	expectFail(t, nx.Probe(ctx, resource.ProbeRequest{Op: resource.ProbeTest, Server: srv}), resource.StepResolve, "域名不存在")
}

type failingResolver struct{}

func (failingResolver) LookupNetIP(context.Context, string, string) ([]netip.Addr, error) {
	return nil, &net.DNSError{Err: "no such host", Name: "missing.example.com", IsNotFound: true}
}

func TestProbe_TruncatesLargeDirectory(t *testing.T) {
	f := startFake(t, resource.TLSNone, nil, func(f *fakeFTP) {
		lines := make([]string, resource.MaxEntries+5)
		for i := range lines {
			lines[i] = fmt.Sprintf("type=dir; 剧%04d", i)
		}
		f.dirs["/大目录"] = lines
	})
	res := devProber(t).Probe(context.Background(), resource.ProbeRequest{Op: resource.ProbeList, Server: server(f.port(), resource.TLSNone), Path: "/大目录"})
	if !res.OK || len(res.Entries) != resource.MaxEntries || !res.Truncated || res.Entries[0].Name != "剧0000" ||
		!strings.Contains(lastStep(t, res).Message, fmt.Sprintf("共 %d 项", resource.MaxEntries+5)) {
		t.Fatalf("超过 %d 条应截断并标记：%d %v %q", resource.MaxEntries, len(res.Entries), res.Truncated, lastStep(t, res).Message)
	}
}

func TestSanitize(t *testing.T) {
	if got := sanitize("530 Login\r\n incorrect.\x00\t "); got != "530 Login incorrect." {
		t.Fatalf("应去掉控制字符并合并空白：%q", got)
	}
	long := strings.Repeat("长", maxReplyRunes+10)
	if got := sanitize(long); len([]rune(got)) != maxReplyRunes+1 || !strings.HasSuffix(got, "…") {
		t.Fatalf("应截断到 %d 字：%d", maxReplyRunes, len([]rune(got)))
	}
}
