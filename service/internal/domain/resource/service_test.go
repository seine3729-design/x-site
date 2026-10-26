package resource

import (
	"context"
	"errors"
	"strings"
	"testing"

	"xsite/service/internal/errs"
	"xsite/service/internal/netguard"
)

// memRepo 是内存仓储，只做增删改查，不重复校验。
type memRepo struct {
	rows   map[int64]FTPServer
	nextID int64
	lastQ  Query
}

func newMemRepo() *memRepo { return &memRepo{rows: map[int64]FTPServer{}} }

func (m *memRepo) ListFTPServers(_ context.Context, q Query) ([]FTPServer, int64, error) {
	m.lastQ = q
	var out []FTPServer
	for _, s := range m.rows {
		out = append(out, s)
	}
	return out, int64(len(out)), nil
}

func (m *memRepo) GetFTPServer(_ context.Context, id int64) (FTPServer, error) {
	s, ok := m.rows[id]
	if !ok {
		return FTPServer{}, errs.ErrNotFound
	}
	return s, nil
}

func (m *memRepo) CreateFTPServer(_ context.Context, in FTPServerInput) (FTPServer, error) {
	m.nextID++
	s := FTPServer{ID: m.nextID, Name: in.Name, Host: in.Host, Port: in.Port, TLS: in.TLS, Passive: in.Passive, CredRef: in.CredRef, Status: in.Status}
	m.rows[s.ID] = s
	return s, nil
}

func (m *memRepo) UpdateFTPServer(_ context.Context, id int64, in FTPServerInput) (FTPServer, error) {
	if _, ok := m.rows[id]; !ok {
		return FTPServer{}, errs.ErrNotFound
	}
	s := FTPServer{ID: id, Name: in.Name, Host: in.Host, Port: in.Port, TLS: in.TLS, Passive: in.Passive, CredRef: in.CredRef, Status: in.Status}
	m.rows[id] = s
	return s, nil
}

func (m *memRepo) DeleteFTPServer(_ context.Context, id int64) (FTPServer, error) {
	s, ok := m.rows[id]
	if !ok {
		return FTPServer{}, errs.ErrNotFound
	}
	delete(m.rows, id)
	return s, nil
}

type fakeProber struct {
	got []ProbeRequest
	res ProbeResult
	err error
}

func (f *fakeProber) Probe(_ context.Context, req ProbeRequest) (ProbeResult, error) {
	f.got = append(f.got, req)
	return f.res, f.err
}

var (
	prodPolicy = Policy{ExplicitTLSOnly: true}
	devPolicy  = Policy{Egress: netguard.Policy{AllowPrivate: true}}
)

func valid() FTPServerInput {
	return FTPServerInput{Name: " 香港资源机 ", Host: " FTP.Example.COM. ", Passive: true}
}

func isBadRequest(err error) bool { return errors.Is(err, errs.ErrBadRequest) }

func TestCreate_DefaultsAndNormalization(t *testing.T) {
	svc := NewService(newMemRepo(), &fakeProber{}, prodPolicy)
	s, err := svc.CreateFTPServer(context.Background(), valid())
	if err != nil {
		t.Fatal(err)
	}
	if s.Name != "香港资源机" || s.Host != "ftp.example.com" || s.Port != DefaultPort || s.TLS != TLSExplicit ||
		!s.Passive || s.Status != StatusActive {
		t.Fatalf("应去掉空白、地址转小写并补齐默认值（端口 21、显式 TLS、启用）：%+v", s)
	}
	if !credRefRe.MatchString(s.CredRef) || !strings.HasPrefix(s.CredRef, "R") || len(s.CredRef) != 7 {
		t.Fatalf("凭据标识留空时应生成 R + 6 位：%q", s.CredRef)
	}
	if u, p := CredEnv(s.CredRef); u != "FTP_"+s.CredRef+"_USER" || p != "FTP_"+s.CredRef+"_PASSWORD" {
		t.Fatalf("环境变量名不符合约定：%s %s", u, p)
	}

	in := valid()
	in.CredRef, in.Port, in.Status = " hk_1 ", 2121, StatusInactive
	in.Host = "[2606:4700:0:0::1]"
	if s, err = svc.CreateFTPServer(context.Background(), in); err != nil {
		t.Fatal(err)
	}
	if s.CredRef != "HK_1" || s.Port != 2121 || s.Status != StatusInactive || s.Host != "2606:4700::1" {
		t.Fatalf("凭据标识转大写、IPv6 去掉方括号并用标准写法：%+v", s)
	}
	in.Host = "::FFFF:203.0.114.9"
	if s, err = svc.CreateFTPServer(context.Background(), in); err != nil || s.Host != "203.0.114.9" {
		t.Fatalf("IPv4 映射地址按 IPv4 保存：%+v %v", s, err)
	}
}

func TestCreate_Validation(t *testing.T) {
	tests := []struct {
		name   string
		policy Policy
		edit   func(*FTPServerInput)
		want   string // 错误提示包含的片段；空表示应成功
	}{
		{"名称为空", prodPolicy, func(in *FTPServerInput) { in.Name = "  " }, "名称不能为空"},
		{"名称过长", prodPolicy, func(in *FTPServerInput) { in.Name = strings.Repeat("机", 65) }, "名称不能超过 64"},
		{"名称 64 字", prodPolicy, func(in *FTPServerInput) { in.Name = strings.Repeat("机", 64) }, ""},
		{"地址为空", prodPolicy, func(in *FTPServerInput) { in.Host = "" }, "地址不能为空"},
		{"地址带协议", prodPolicy, func(in *FTPServerInput) { in.Host = "ftp://ftp.example.com" }, "地址只能是域名或 IP"},
		{"地址带端口", prodPolicy, func(in *FTPServerInput) { in.Host = "ftp.example.com:21" }, "地址只能是域名或 IP"},
		{"地址带路径", prodPolicy, func(in *FTPServerInput) { in.Host = "ftp.example.com/a" }, "地址只能是域名或 IP"},
		{"地址带账号", prodPolicy, func(in *FTPServerInput) { in.Host = "u@ftp.example.com" }, "地址只能是域名或 IP"},
		{"地址有空格", prodPolicy, func(in *FTPServerInput) { in.Host = "ftp example.com" }, "地址只能是域名或 IP"},
		{"连字符开头", prodPolicy, func(in *FTPServerInput) { in.Host = "-ftp.example.com" }, "地址只能是域名或 IP"},
		{"中文域名", prodPolicy, func(in *FTPServerInput) { in.Host = "资源.中国" }, "xn--"},
		{"不完整的 IP", prodPolicy, func(in *FTPServerInput) { in.Host = "10.1" }, "地址只能是域名或 IP"},
		{"带区域的 IPv6", devPolicy, func(in *FTPServerInput) { in.Host = "fe80::1%eth0" }, "地址只能是域名或 IP"},
		{"地址过长", prodPolicy, func(in *FTPServerInput) { in.Host = strings.Repeat("a.", 127) + "com" }, "不能超过 253"},
		{"生产禁止内网 IP", prodPolicy, func(in *FTPServerInput) { in.Host = "10.0.0.5" }, "内网或保留地址段"},
		{"生产禁止元数据地址", prodPolicy, func(in *FTPServerInput) { in.Host = "169.254.169.254" }, "内网或保留地址段"},
		{"开发环境允许内网 IP", devPolicy, func(in *FTPServerInput) { in.Host = "192.168.1.20" }, ""},
		{"开发环境允许 compose 服务名", devPolicy, func(in *FTPServerInput) { in.Host = "ftp" }, ""},
		{"端口越界", prodPolicy, func(in *FTPServerInput) { in.Port = 70000 }, "端口为 1–65535"},
		{"端口为负", prodPolicy, func(in *FTPServerInput) { in.Port = -1 }, "端口为 1–65535"},
		{"未知加密方式", devPolicy, func(in *FTPServerInput) { in.TLS = "ssl" }, "加密方式只能是"},
		{"生产不允许不加密", prodPolicy, func(in *FTPServerInput) { in.TLS = TLSNone }, "只允许显式 TLS"},
		{"生产不允许隐式 TLS", prodPolicy, func(in *FTPServerInput) { in.TLS = TLSImplicit }, "只允许显式 TLS"},
		{"开发环境允许不加密", devPolicy, func(in *FTPServerInput) { in.TLS = TLSNone }, ""},
		{"开发环境允许隐式 TLS", devPolicy, func(in *FTPServerInput) { in.TLS = TLSImplicit; in.Port = 990 }, ""},
		{"不支持主动模式", devPolicy, func(in *FTPServerInput) { in.Passive = false }, "只支持被动模式"},
		{"凭据标识数字开头", prodPolicy, func(in *FTPServerInput) { in.CredRef = "1HK" }, "凭据标识"},
		{"凭据标识含连字符", prodPolicy, func(in *FTPServerInput) { in.CredRef = "HK-1" }, "凭据标识"},
		{"凭据标识过长", prodPolicy, func(in *FTPServerInput) { in.CredRef = "A" + strings.Repeat("B", 32) }, "凭据标识"},
		{"未知状态", prodPolicy, func(in *FTPServerInput) { in.Status = "deleted" }, "状态只能是"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := valid()
			tt.edit(&in)
			_, err := NewService(newMemRepo(), &fakeProber{}, tt.policy).CreateFTPServer(context.Background(), in)
			if tt.want == "" {
				if err != nil {
					t.Fatalf("应成功，得到 %v", err)
				}
				return
			}
			if !isBadRequest(err) || !strings.Contains(errs.From(err).Message, tt.want) {
				t.Fatalf("应 400 并提示「%s」，得到 %v", tt.want, err)
			}
		})
	}
}

func TestUpdate_KeepsCredRefWhenEmpty(t *testing.T) {
	repo := newMemRepo()
	svc := NewService(repo, &fakeProber{}, prodPolicy)
	ctx := context.Background()
	in := valid()
	in.CredRef = "HK1"
	created, err := svc.CreateFTPServer(ctx, in)
	if err != nil {
		t.Fatal(err)
	}

	// 保存时留空：保持原标识（否则已配置的账号密码会失效）
	in = valid()
	in.Name = "改名"
	before, after, err := svc.UpdateFTPServer(ctx, created.ID, in)
	if err != nil {
		t.Fatal(err)
	}
	if before.Name != "香港资源机" || after.Name != "改名" || after.CredRef != "HK1" {
		t.Fatalf("凭据标识留空应保持原值：before=%+v after=%+v", before, after)
	}
	// 填写了就换成新标识
	in.CredRef = "hk2"
	if _, after, err = svc.UpdateFTPServer(ctx, created.ID, in); err != nil || after.CredRef != "HK2" {
		t.Fatalf("应换成新标识：%+v %v", after, err)
	}
	// 整体保存：未传的端口、加密方式、状态回到默认值
	in = valid()
	in.Port, in.Status = 2121, StatusInactive
	if _, after, err = svc.UpdateFTPServer(ctx, created.ID, in); err != nil {
		t.Fatal(err)
	}
	in = valid()
	if _, after, err = svc.UpdateFTPServer(ctx, created.ID, in); err != nil || after.Port != DefaultPort || after.Status != StatusActive {
		t.Fatalf("未传的可选字段应按默认值保存：%+v %v", after, err)
	}

	if _, _, err := svc.UpdateFTPServer(ctx, 999, valid()); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("不存在应 404，得到 %v", err)
	}
	bad := valid()
	bad.Host = ""
	if _, _, err := svc.UpdateFTPServer(ctx, 999, bad); !isBadRequest(err) {
		t.Fatalf("先校验参数，得到 %v", err)
	}
}

func TestList_FilterAndPaging(t *testing.T) {
	repo := newMemRepo()
	svc := NewService(repo, &fakeProber{}, prodPolicy)
	ctx := context.Background()

	p, err := svc.ListFTPServers(ctx, Filter{Q: " 50%_off\x00 ", Status: StatusInactive})
	if err != nil {
		t.Fatal(err)
	}
	if repo.lastQ.Pattern != `%50\%\_off%` || repo.lastQ.Status != StatusInactive || repo.lastQ.Limit != 20 || repo.lastQ.Offset != 0 ||
		p.Page != 1 || p.PageSize != 20 {
		t.Fatalf("搜索词应去掉控制字符并转义，分页取默认值：%+v %+v", repo.lastQ, p)
	}
	if _, err := svc.ListFTPServers(ctx, Filter{Page: 3, PageSize: 50}); err != nil || repo.lastQ.Offset != 100 || repo.lastQ.Pattern != "" {
		t.Fatalf("第 3 页偏移 100：%+v %v", repo.lastQ, err)
	}
	for _, f := range []Filter{{Status: "deleted"}, {PageSize: 101}, {Page: -1}, {Q: strings.Repeat("字", 51)}} {
		if _, err := svc.ListFTPServers(ctx, f); !isBadRequest(err) {
			t.Errorf("%+v 应 400，得到 %v", f, err)
		}
	}
}

func TestProbe_PassesSnapshotToWorker(t *testing.T) {
	repo := newMemRepo()
	prober := &fakeProber{res: ProbeResult{OK: true, Path: "/"}}
	svc := NewService(repo, prober, devPolicy)
	ctx := context.Background()
	created, err := svc.CreateFTPServer(ctx, valid())
	if err != nil {
		t.Fatal(err)
	}

	res, err := svc.TestFTPServer(ctx, created.ID)
	if err != nil || !res.OK {
		t.Fatalf("应返回 worker 的结果：%+v %v", res, err)
	}
	if _, err := svc.ListFTPDir(ctx, created.ID, "  /剧集/2026  "); err != nil {
		t.Fatal(err)
	}
	if len(prober.got) != 2 || prober.got[0].Op != ProbeTest || prober.got[0].Server != created ||
		prober.got[1].Op != ProbeList || prober.got[1].Path != "/剧集/2026" {
		t.Fatalf("应把资源机配置与路径交给 worker：%+v", prober.got)
	}

	for _, p := range []string{"/a\r\nDELE x", "/a\x00", strings.Repeat("a", 1025), "\xff"} {
		if _, err := svc.ListFTPDir(ctx, created.ID, p); !isBadRequest(err) {
			t.Errorf("路径 %q 应 400，得到 %v", p, err)
		}
	}
	if len(prober.got) != 2 {
		t.Fatal("参数错误时不应发起连接")
	}
	if _, err := svc.TestFTPServer(ctx, 999); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("不存在应 404，得到 %v", err)
	}
	if _, err := svc.ListFTPDir(ctx, 999, ""); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("不存在应 404，得到 %v", err)
	}

	prober.err = errs.ErrUnavailable
	if _, err := svc.TestFTPServer(ctx, created.ID); !errors.Is(err, errs.ErrUnavailable) {
		t.Fatalf("拿不到结果时原样返回，得到 %v", err)
	}
}

func TestDelete(t *testing.T) {
	repo := newMemRepo()
	svc := NewService(repo, &fakeProber{}, prodPolicy)
	ctx := context.Background()
	created, _ := svc.CreateFTPServer(ctx, valid())
	if before, err := svc.DeleteFTPServer(ctx, created.ID); err != nil || before.ID != created.ID {
		t.Fatalf("应返回删除前的记录：%+v %v", before, err)
	}
	if _, err := svc.DeleteFTPServer(ctx, created.ID); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("再删应 404，得到 %v", err)
	}
}
