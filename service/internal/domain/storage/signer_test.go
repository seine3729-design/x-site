package storage

import (
	"context"
	"errors"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

const testSecret = "0123456789abcdef0123456789abcdef" // 32 字符，满足最小长度

func TestParseSignKeyring(t *testing.T) {
	kr, err := ParseSignKeyring("cdn-main:" + testSecret + ",cdn-next:" + testSecret + "X")
	if err != nil {
		t.Fatalf("解析出错：%v", err)
	}
	if kr.Len() != 2 || !kr.Has("cdn-main") || !kr.Has("cdn-next") {
		t.Errorf("应解析出两个引用，得到 %d", kr.Len())
	}
	if _, err := kr.Key("missing"); !errors.Is(err, ErrSignKeyMissing) {
		t.Errorf("未配置的引用应返回 ErrSignKeyMissing，得到 %v", err)
	}

	// 空串是合法的（本地开发常用 presign 直连，不需要签名密钥）
	empty, err := ParseSignKeyring("")
	if err != nil || empty.Len() != 0 {
		t.Errorf("空串应得到空密钥环，得到 %d %v", empty.Len(), err)
	}
	// nil 密钥环不 panic
	var nilKr *SignKeyring
	if nilKr.Has("x") || nilKr.Len() != 0 {
		t.Error("nil 密钥环应表现为空")
	}
	if _, err := nilKr.Key("x"); !errors.Is(err, ErrSignKeyMissing) {
		t.Error("nil 密钥环取密钥应返回 ErrSignKeyMissing")
	}

	for _, bad := range []string{
		"noseparator",
		":" + testSecret,
		"ref:",
		"ref:tooshort",
		"bad ref:" + testSecret,
		"dup:" + testSecret + ",dup:" + testSecret + "Y",
	} {
		if _, err := ParseSignKeyring(bad); err == nil {
			t.Errorf("%q 应解析失败", bad)
		}
	}
}

func TestHMACTokenAndVerify(t *testing.T) {
	secret := []byte(testSecret)
	now := time.Unix(1_700_000_000, 0)
	exp := now.Add(10 * time.Minute).Unix()
	const prefix = "hls/42/job1/s/"

	sig := HMACToken(secret, prefix, exp)
	if sig == "" || strings.ContainsAny(sig, "+/=") {
		t.Fatalf("签名应为非空的 base64url（无 + / =），得到 %q", sig)
	}
	// 同输入稳定
	if HMACToken(secret, prefix, exp) != sig {
		t.Error("同输入的签名应稳定")
	}

	// 正常校验通过
	if err := VerifyHMACToken(secret, "/"+prefix+"seg001.m4s", prefix, sig, exp, now); err != nil {
		t.Errorf("正常校验应通过：%v", err)
	}
	// 过期
	if err := VerifyHMACToken(secret, prefix+"seg001.m4s", prefix, sig, exp, time.Unix(exp+1, 0)); err == nil {
		t.Error("过期链接应校验失败")
	}
	// 签名不匹配（改了 exp）
	if err := VerifyHMACToken(secret, prefix+"seg001.m4s", prefix, sig, exp+1, now); err == nil {
		t.Error("篡改 exp 应校验失败")
	}
	// 换密钥
	if err := VerifyHMACToken([]byte(testSecret+"Z"), prefix+"seg001.m4s", prefix, sig, exp, now); err == nil {
		t.Error("错误密钥应校验失败")
	}
	// 令牌被挪到其他剧集的前缀下（docs/12 第 9 节：签名不能复用到其他剧集）
	if err := VerifyHMACToken(secret, "hls/99/job9/s/seg001.m4s", prefix, sig, exp, now); err == nil {
		t.Error("路径不在签名前缀下应校验失败")
	}
	// 前缀为空
	if err := VerifyHMACToken(secret, prefix+"a", "", sig, exp, now); err == nil {
		t.Error("空前缀应校验失败")
	}
}

func TestJoinPrefix(t *testing.T) {
	cases := [][3]string{
		{"media", "hls/1/a.m4s", "media/hls/1/a.m4s"},
		{"media/", "/hls/1/a.m4s", "media/hls/1/a.m4s"},
		{"", "hls/1/a.m4s", "hls/1/a.m4s"},
		{"/media/", "hls/1/a.m4s", "media/hls/1/a.m4s"},
	}
	for _, c := range cases {
		if got := JoinPrefix(c[0], c[1]); got != c[2] {
			t.Errorf("JoinPrefix(%q,%q)=%q，期望 %q", c[0], c[1], got, c[2])
		}
	}
}

// ---------- SignService ----------

type stubPresigner struct {
	gotKey string
	gotTTL time.Duration
	gotCfg Conn
}

func (s *stubPresigner) PresignGet(_ context.Context, p PresignParams) (string, error) {
	s.gotKey, s.gotTTL, s.gotCfg = p.Key, p.TTL, p.Conn
	return "https://s3.example.com/" + p.Key + "?X-Amz-Signature=deadbeef&X-Amz-Expires=" +
		strconv.Itoa(int(p.TTL.Seconds())), nil
}

type stubCreds struct {
	cred Credential
	err  error
}

func (s stubCreds) ReadCredential(Row) (Credential, error) { return s.cred, s.err }

// signRepo 是只实现签名需要的两个查询的仓储存根。
type signRepo struct {
	Repo
	provider Row
	line     Line
}

func (r *signRepo) Get(context.Context, int64) (Row, error)      { return r.provider, nil }
func (r *signRepo) GetLine(context.Context, int64) (Line, error) { return r.line, nil }

func newSignService(t *testing.T, repo *signRepo) *SignService {
	t.Helper()
	kr, err := ParseSignKeyring("cdn-main:" + testSecret)
	if err != nil {
		t.Fatalf("密钥环解析失败：%v", err)
	}
	return NewSignService(repo, stubCreds{cred: Credential{AccessKeyID: "ak", SecretAccessKey: "sk"}}, kr, &stubPresigner{}, nil)
}

func TestSignService_None(t *testing.T) {
	repo := &signRepo{}
	svc := newSignService(t, repo)
	line := Line{SignType: SignNone, BaseURL: "https://img.example.com/assets", TokenTTL: 600}

	got, err := svc.SignFor(context.Background(), line, SignTarget{Key: "covers/1.webp"}, 0, time.Now())
	if err != nil {
		t.Fatalf("签名出错：%v", err)
	}
	if got.URL != "https://img.example.com/assets/covers/1.webp" {
		t.Errorf("公开读地址=%q", got.URL)
	}
	if !got.ExpiresAt.IsZero() {
		t.Error("不签名的地址不应有过期时间")
	}
	// 没有访问域名时报错（公开读必须有域名）
	if _, err := svc.SignFor(context.Background(), Line{SignType: SignNone}, SignTarget{Key: "a"}, 0, time.Now()); err == nil {
		t.Error("缺少 Base URL 应报错")
	}
}

func TestSignService_HMACToken(t *testing.T) {
	svc := newSignService(t, &signRepo{})
	now := time.Unix(1_700_000_000, 0)
	line := Line{
		SignType: SignHMACToken, SignKeyRef: "cdn-main",
		BaseURL: "https://cdn.example.com", TokenTTL: 600,
	}
	const prefix = "hls/42/job1/s/"

	got, err := svc.SignFor(context.Background(), line, SignTarget{Key: prefix + "seg001.m4s", Prefix: prefix}, 0, now)
	if err != nil {
		t.Fatalf("签名出错：%v", err)
	}
	u, err := url.Parse(got.URL)
	if err != nil {
		t.Fatalf("地址无法解析：%v", err)
	}
	if u.Path != "/"+prefix+"seg001.m4s" {
		t.Errorf("路径=%q", u.Path)
	}
	q := u.Query()
	if q.Get(SignParamPrefix) != prefix {
		t.Errorf("前缀参数=%q", q.Get(SignParamPrefix))
	}
	// 有效期取线路的 token_ttl
	wantExp := now.Add(600 * time.Second).Unix()
	if q.Get(SignParamExpire) != strconv.FormatInt(wantExp, 10) {
		t.Errorf("exp=%q，期望 %d", q.Get(SignParamExpire), wantExp)
	}
	if !got.ExpiresAt.Equal(time.Unix(wantExp, 0).UTC()) {
		t.Errorf("ExpiresAt=%v", got.ExpiresAt)
	}
	// 生成的签名能被校验逻辑接受（生成与校验对称）
	if err := VerifyHMACToken([]byte(testSecret), u.Path, prefix, q.Get(SignParamSig), wantExp, now); err != nil {
		t.Errorf("生成的签名应能通过校验：%v", err)
	}
	// 同一个令牌覆盖该前缀下的其他切片（目录前缀签名的要点）
	if err := VerifyHMACToken([]byte(testSecret), "/"+prefix+"seg999.m4s", prefix, q.Get(SignParamSig), wantExp, now); err != nil {
		t.Errorf("同前缀下的其他切片应复用同一令牌：%v", err)
	}

	// 密钥未注入时报错
	noKey := Line{SignType: SignHMACToken, SignKeyRef: "absent", BaseURL: "https://cdn.example.com", TokenTTL: 600}
	if _, err := svc.SignFor(context.Background(), noKey, SignTarget{Key: "a"}, 0, now); !errors.Is(err, ErrSignKeyMissing) {
		t.Errorf("缺密钥应返回 ErrSignKeyMissing，得到 %v", err)
	}
}

func TestSignService_S3Presign(t *testing.T) {
	repo := &signRepo{provider: Row{
		ID: 7, Name: "R2", Type: TypeR2, Bucket: "duanju666", Region: R2Region,
		PathPrefix: "media", MaxPresignTTL: 900,
	}}
	ps := &stubPresigner{}
	kr, _ := ParseSignKeyring("")
	svc := NewSignService(repo, stubCreds{cred: Credential{AccessKeyID: "ak", SecretAccessKey: "sk"}}, kr, ps, nil)
	now := time.Now()
	line := Line{SignType: SignS3Presign, ProviderID: 7, TokenTTL: 600}

	got, err := svc.SignFor(context.Background(), line, SignTarget{Key: "hls/42/master.m3u8"}, 0, now)
	if err != nil {
		t.Fatalf("签名出错：%v", err)
	}
	// 对象键要拼上存储源的 path_prefix
	if ps.gotKey != "media/hls/42/master.m3u8" {
		t.Errorf("预签名对象键=%q", ps.gotKey)
	}
	if ps.gotTTL != 600*time.Second {
		t.Errorf("TTL=%v，期望 600s", ps.gotTTL)
	}
	if ps.gotCfg.Bucket != "duanju666" {
		t.Errorf("连接参数未透传：%+v", ps.gotCfg)
	}
	if !strings.Contains(got.URL, "X-Amz-Signature") {
		t.Errorf("应返回预签名地址，得到 %q", got.URL)
	}

	// 超过存储源允许的上限时收敛到上限（max_presign_ttl=900）
	if _, err := svc.SignFor(context.Background(), Line{SignType: SignS3Presign, ProviderID: 7, TokenTTL: 3600},
		SignTarget{Key: "a"}, 0, now); err != nil {
		t.Fatalf("签名出错：%v", err)
	}
	if ps.gotTTL != 900*time.Second {
		t.Errorf("超上限的 TTL 应收敛到 900s，得到 %v", ps.gotTTL)
	}

	// HTTP 源不支持预签名
	repo.provider = Row{Type: TypeCustom, Subtype: SubtypeHTTPOrigin}
	if _, err := svc.SignFor(context.Background(), line, SignTarget{Key: "a"}, 0, now); err == nil {
		t.Error("HTTP 源应拒绝预签名")
	}
}

func TestSignService_CDNVendor(t *testing.T) {
	svc := newSignService(t, &signRepo{})
	now := time.Unix(1_700_000_000, 0)
	line := Line{
		SignType: SignCDNVendor, SignKeyRef: "timestamp_md5/cdn-main",
		BaseURL: "https://cdn.example.com", TokenTTL: 600,
	}
	const prefix = "hls/42/job1/s/"

	got, err := svc.SignFor(context.Background(), line, SignTarget{Key: prefix + "seg001.m4s", Prefix: prefix}, 0, now)
	if err != nil {
		t.Fatalf("签名出错：%v", err)
	}
	u, _ := url.Parse(got.URL)
	auth := u.Query().Get("auth_key")
	// 格式：过期时间-随机串-uid-md5
	parts := strings.Split(auth, "-")
	if len(parts) != 4 {
		t.Fatalf("auth_key 应为四段，得到 %q", auth)
	}
	if parts[0] != strconv.FormatInt(now.Add(600*time.Second).Unix(), 10) {
		t.Errorf("auth_key 的过期时间=%q", parts[0])
	}
	if len(parts[3]) != 32 {
		t.Errorf("md5 段长度应为 32，得到 %d", len(parts[3]))
	}
	// 随机串每次不同
	got2, _ := svc.SignFor(context.Background(), line, SignTarget{Key: prefix + "seg001.m4s", Prefix: prefix}, 0, now)
	if got2.URL == got.URL {
		t.Error("每次签名的随机串应不同")
	}

	// 未指定厂商 / 未知厂商
	for _, ref := range []string{"cdn-main", "unknown_vendor/cdn-main"} {
		bad := Line{SignType: SignCDNVendor, SignKeyRef: ref, BaseURL: "https://cdn.example.com", TokenTTL: 600}
		if _, err := svc.SignFor(context.Background(), bad, SignTarget{Key: "a"}, 0, now); err == nil {
			t.Errorf("密钥引用 %q 应报错", ref)
		}
	}
}

func TestSignService_RejectsBadTarget(t *testing.T) {
	svc := newSignService(t, &signRepo{})
	line := Line{SignType: SignHMACToken, SignKeyRef: "cdn-main", BaseURL: "https://cdn.example.com", TokenTTL: 600}

	if _, err := svc.SignFor(context.Background(), line, SignTarget{}, 0, time.Now()); err == nil {
		t.Error("缺少对象路径应报错")
	}
	// 对象不在声明的前缀之下：防止调用方拿 A 集的前缀去签 B 集的对象
	if _, err := svc.SignFor(context.Background(), line,
		SignTarget{Key: "hls/99/s/a.m4s", Prefix: "hls/42/s/"}, 0, time.Now()); err == nil {
		t.Error("对象不在签名前缀下应报错")
	}
	// 未知签名方式
	if _, err := svc.SignFor(context.Background(), Line{SignType: "weird", BaseURL: "https://x.example.com"},
		SignTarget{Key: "a"}, 0, time.Now()); err == nil {
		t.Error("未知签名方式应报错")
	}
}

func TestLineSignKeyName(t *testing.T) {
	cases := []struct {
		line       Line
		wantVendor string
		wantKey    string
	}{
		{Line{SignType: SignHMACToken, SignKeyRef: "cdn-main"}, "", "cdn-main"},
		{Line{SignType: SignCDNVendor, SignKeyRef: "timestamp_md5/cdn-main"}, "timestamp_md5", "cdn-main"},
		{Line{SignType: SignCDNVendor, SignKeyRef: "cdn-main"}, "", ""},
	}
	for _, c := range cases {
		if got := c.line.VendorRef(); got != c.wantVendor {
			t.Errorf("VendorRef(%q)=%q，期望 %q", c.line.SignKeyRef, got, c.wantVendor)
		}
		if got := c.line.SignKeyName(); got != c.wantKey {
			t.Errorf("SignKeyName(%q)=%q，期望 %q", c.line.SignKeyRef, got, c.wantKey)
		}
	}
}

func TestCleanSignKeyRef(t *testing.T) {
	// s3_presign 与 none 不需要密钥，填了也清掉
	for _, st := range []SignType{SignS3Presign, SignNone} {
		if got, err := cleanSignKeyRef(st, "whatever"); err != nil || got != "" {
			t.Errorf("%s 应清空密钥引用，得到 %q %v", st, got, err)
		}
	}
	// hmac_token 必填且限制字符
	if _, err := cleanSignKeyRef(SignHMACToken, ""); err == nil {
		t.Error("hmac_token 缺密钥引用应报错")
	}
	if got, err := cleanSignKeyRef(SignHMACToken, "  cdn-main  "); err != nil || got != "cdn-main" {
		t.Errorf("应去空白，得到 %q %v", got, err)
	}
	if _, err := cleanSignKeyRef(SignHMACToken, "bad ref"); err == nil {
		t.Error("非法字符应报错")
	}
	// cdn_vendor 需要 厂商名/密钥引用，且厂商要已注册
	if got, err := cleanSignKeyRef(SignCDNVendor, "timestamp_md5/cdn-main"); err != nil || got != "timestamp_md5/cdn-main" {
		t.Errorf("合法的厂商引用应保留，得到 %q %v", got, err)
	}
	for _, bad := range []string{"", "cdn-main", "unknown/cdn-main", "timestamp_md5/", "/cdn-main"} {
		if _, err := cleanSignKeyRef(SignCDNVendor, bad); err == nil {
			t.Errorf("cdn_vendor 引用 %q 应报错", bad)
		}
	}
}

func TestRedactSignature(t *testing.T) {
	raw := "https://cdn.example.com/a.m4s?" + SignParamPrefix + "=hls/&" + SignParamExpire + "=123&" + SignParamSig + "=secretsig"
	got := redactSignature(raw)
	if strings.Contains(got, "secretsig") {
		t.Errorf("签名值应被遮蔽：%s", got)
	}
	if !strings.Contains(got, SignParamExpire+"=123") {
		t.Errorf("非签名参数应保留：%s", got)
	}
	// S3 预签名的签名与凭据也要遮蔽
	s3 := "https://s3.example.com/a?X-Amz-Signature=abc&X-Amz-Credential=ak%2F20260101&X-Amz-Expires=600"
	gotS3 := redactSignature(s3)
	if strings.Contains(gotS3, "abc") || strings.Contains(gotS3, "ak%2F20260101") || strings.Contains(gotS3, "ak/20260101") {
		t.Errorf("预签名的签名与凭据应被遮蔽：%s", gotS3)
	}
	if !strings.Contains(gotS3, "X-Amz-Expires=600") {
		t.Errorf("有效期应保留：%s", gotS3)
	}
}
