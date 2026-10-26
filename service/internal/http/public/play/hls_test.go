package play

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"xsite/service/internal/crypto/apienc"
	dom "xsite/service/internal/domain/play"
	"xsite/service/internal/errs"
	"xsite/service/internal/http/middleware"
)

// 清单与密钥接口（S1-8）的 HTTP 层测试：经真实的报文加密中间件发请求，验证只收加密请求、参数解析、
// 响应类型与缓存头、错误映射与密钥限流。

const hlsServiceToken = "svc-token-for-hls-tests"

type fakeHLS struct {
	playlist  []byte
	key       []byte
	err       error
	playlists []dom.PlaylistInput
	keys      []dom.KeyInput
}

func (f *fakeHLS) Playlist(_ context.Context, in dom.PlaylistInput) ([]byte, error) {
	f.playlists = append(f.playlists, in)
	return f.playlist, f.err
}

func (f *fakeHLS) Key(_ context.Context, in dom.KeyInput) ([]byte, error) {
	f.keys = append(f.keys, in)
	return f.key, f.err
}

type hlsEnv struct {
	r      *gin.Engine
	pk     apienc.PublicKey
	hls    *fakeHLS
	keyLim *fakeLimiter
}

// newHLSEnv 按生产顺序挂中间件：回源鉴权（识别 BFF）→ 报文加密（required 控制是否强制）→ 播放接口。
func newHLSEnv(t *testing.T, required bool) hlsEnv {
	t.Helper()
	gin.SetMode(gin.TestMode)
	kr, err := apienc.ParseKeyring("k1:0123456789abcdef0123456789abcdef-hls")
	if err != nil {
		t.Fatal(err)
	}
	r := gin.New()
	r.Use(
		middleware.Recover(),
		middleware.RequestID(),
		middleware.EdgeAuth(middleware.EdgeAuthOptions{ServiceToken: hlsServiceToken}),
		middleware.APIEncryption(middleware.APIEncryptionOptions{Keyring: kr, Required: required}),
	)
	f := &fakeHLS{playlist: []byte("#EXTM3U\n#EXT-X-ENDLIST\n"), key: bytes.Repeat([]byte{0xAB}, 16)}
	lim := &fakeLimiter{counts: map[string]int64{}}
	h := &Handlers{Svc: &fakeService{}, HLS: f, Limiter: &fakeLimiter{counts: map[string]int64{}}, KeyLimiter: lim}
	h.Routes(r.Group("/api/v1"))
	return hlsEnv{r: r, pk: kr.PublicKeys()[0], hls: f, keyLim: lim}
}

type encResult struct {
	status  int
	header  http.Header
	body    []byte // 解密后的明文
	code    string // JSON 错误包的业务码
	encType string
}

// get 发一个加密的 GET 请求并解开响应。
func (e hlsEnv) get(t *testing.T, uri string, headers map[string]string) encResult {
	t.Helper()
	creq, err := apienc.NewClientRequest(e.pk.Kid, e.pk.PublicKey, time.Now().Unix(), http.MethodGet, uri, nil)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, uri, nil)
	req.RemoteAddr = "203.0.113.9:4321"
	req.Header.Set(middleware.HeaderEnc, creq.Header)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	e.r.ServeHTTP(w, req)
	if ct := w.Header().Get("Content-Type"); ct != apienc.ContentType {
		t.Fatalf("加密请求的响应应为密文，得到 %q：%s", ct, w.Body.String())
	}
	pt, err := creq.Session.OpenResponse(w.Body.Bytes())
	if err != nil {
		t.Fatalf("客户端解不开响应：%v", err)
	}
	res := encResult{status: w.Code, header: w.Header(), body: pt, encType: w.Header().Get(middleware.HeaderEncType)}
	if strings.HasPrefix(res.encType, "application/json") {
		var env struct {
			Code string `json:"code"`
		}
		_ = json.Unmarshal(pt, &env)
		res.code = env.Code
	}
	return res
}

// plain 发一个明文请求。
func (e hlsEnv) plain(t *testing.T, uri string, headers map[string]string) (int, string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, uri, nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	e.r.ServeHTTP(w, req)
	var env struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("明文错误应是 JSON：%s", w.Body.String())
	}
	return w.Code, env.Code
}

const (
	testPlaylistURI = "/api/v1/hls/k1.eyJlIjo0MiwibCI6MTJ9.c2lnbmF0dXJl/index.m3u8"
	testKeyURI      = "/api/v1/hls/key/42/3?t=k1.eyJlIjo0MiwibCI6MTJ9.c2lnbmF0dXJl"
)

// 明文请求一律 400（11 的验收）：不受 API_ENCRYPTION_REQUIRED 影响，SSR 内网直连（带服务令牌、不是代转）也不放行。
func TestHLS_PlaintextRejected(t *testing.T) {
	for _, required := range []bool{false, true} {
		e := newHLSEnv(t, required)
		for _, uri := range []string{testPlaylistURI, "/api/v1/hls/tok/v720.m3u8", testKeyURI} {
			for name, headers := range map[string]map[string]string{
				"浏览器 / APP":    nil,
				"SSR 内网直连":     {middleware.HeaderServiceToken: hlsServiceToken},
				"BFF 代转的浏览器请求": {middleware.HeaderServiceToken: hlsServiceToken, middleware.HeaderBFFProxy: "1"},
			} {
				status, code := e.plain(t, uri, headers)
				if status != http.StatusBadRequest || code != "ENC_REQUIRED" {
					t.Errorf("强制加密=%v %s %s：期望 400 ENC_REQUIRED，得到 %d %s", required, name, uri, status, code)
				}
			}
		}
		if len(e.hls.playlists)+len(e.hls.keys) != 0 {
			t.Fatal("明文请求不应调用清单与密钥服务")
		}
	}
}

func TestHLS_Playlist(t *testing.T) {
	e := newHLSEnv(t, false)
	res := e.get(t, testPlaylistURI, map[string]string{"X-Device-Id": "dev-1"})
	if res.status != http.StatusOK || !bytes.Equal(res.body, e.hls.playlist) {
		t.Fatalf("期望 200 与清单原文，得到 %d %q", res.status, res.body)
	}
	if res.encType != "application/vnd.apple.mpegurl" || res.header.Get("Cache-Control") != "no-store" {
		t.Fatalf("清单类型放在 X-Enc-Type、不可缓存：%q %q", res.encType, res.header.Get("Cache-Control"))
	}
	in := e.hls.playlists[0]
	if in.Token != "k1.eyJlIjo0MiwibCI6MTJ9.c2lnbmF0dXJl" || in.File != "index.m3u8" || in.DeviceID != "dev-1" {
		t.Fatalf("参数未正确传入：%+v", in)
	}
	// 一档清单（相对地址解析出来的路径）
	e.get(t, "/api/v1/hls/tok/v720.m3u8", nil)
	if in := e.hls.playlists[1]; in.Token != "tok" || in.File != "v720.m3u8" || in.DeviceID != "" {
		t.Fatalf("一档清单的参数：%+v", in)
	}
}

func TestHLS_Key(t *testing.T) {
	e := newHLSEnv(t, false)
	res := e.get(t, testKeyURI, map[string]string{"X-Device-Id": "dev-1"})
	if res.status != http.StatusOK || !bytes.Equal(res.body, e.hls.key) {
		t.Fatalf("期望 200 与 16 字节密钥，得到 %d %x", res.status, res.body)
	}
	if res.encType != "application/octet-stream" || res.header.Get("Cache-Control") != "no-store" {
		t.Fatalf("密钥类型放在 X-Enc-Type、不可缓存：%q %q", res.encType, res.header.Get("Cache-Control"))
	}
	in := e.hls.keys[0]
	if in.Token != "k1.eyJlIjo0MiwibCI6MTJ9.c2lnbmF0dXJl" || in.EpisodeID != 42 || in.KeyVer != 3 || in.DeviceID != "dev-1" {
		t.Fatalf("参数未正确传入：%+v", in)
	}
	if len(e.hls.playlists) != 0 {
		t.Fatal("/hls/key/... 不能被当成令牌为 key 的清单请求")
	}

	for _, uri := range []string{
		"/api/v1/hls/key/abc/3?t=x", "/api/v1/hls/key/0/3?t=x", "/api/v1/hls/key/42/0?t=x",
		"/api/v1/hls/key/42/-1?t=x", "/api/v1/hls/key/42/2147483648?t=x",
	} {
		if res := e.get(t, uri, nil); res.status != http.StatusNotFound || res.code != "NOT_FOUND" {
			t.Errorf("%s：期望 404，得到 %d %s", uri, res.status, res.code)
		}
	}
	if len(e.hls.keys) != 1 {
		t.Fatal("路径参数不对时不应调用密钥服务")
	}
}

func TestHLS_ErrorMapping(t *testing.T) {
	for _, tt := range []struct {
		err    error
		status int
		code   string
	}{
		{dom.ErrPlayTokenExpired, 401, "TOKEN_EXPIRED"},
		{dom.ErrPlayTokenInvalid, 403, "FORBIDDEN"},
		{errs.ErrNotFound, 404, "NOT_FOUND"},
		{dom.ErrLineUnavailable, 503, "UNAVAILABLE"},
		{errors.New("db down"), 500, "INTERNAL"},
	} {
		e := newHLSEnv(t, false)
		e.hls.err = tt.err
		for _, uri := range []string{testPlaylistURI, testKeyURI} {
			res := e.get(t, uri, nil)
			if res.status != tt.status || res.code != tt.code || res.header.Get("Cache-Control") != "no-store" {
				t.Errorf("%v %s：期望 %d %s，得到 %d %s", tt.err, uri, tt.status, tt.code, res.status, res.code)
			}
		}
	}

	e := newHLSEnv(t, false)
	if res := e.get(t, testPlaylistURI, map[string]string{"X-Device-Id": "a b"}); res.status != 400 || res.code != "BAD_REQUEST" {
		t.Fatalf("设备标识格式不对应 400，得到 %d %s", res.status, res.code)
	}
}

func TestHLS_KeyRateLimit(t *testing.T) {
	e := newHLSEnv(t, false)
	dev := map[string]string{"X-Device-Id": "dev-1"}
	for i := range keyPerMinute {
		if res := e.get(t, testKeyURI, dev); res.status != http.StatusOK {
			t.Fatalf("第 %d 次应放行，得到 %d", i+1, res.status)
		}
	}
	res := e.get(t, testKeyURI, dev)
	if res.status != http.StatusTooManyRequests || res.code != "RATE_LIMITED" {
		t.Fatalf("每集每分钟第 21 次应 429，得到 %d %s", res.status, res.code)
	}
	if after, err := strconv.Atoi(res.header.Get("Retry-After")); err != nil || after < 1 || after > 60 {
		t.Fatalf("Retry-After 应为到下一分钟的秒数：%q", res.header.Get("Retry-After"))
	}
	if len(e.hls.keys) != keyPerMinute {
		t.Fatalf("被限流的请求不应调用密钥服务：%d", len(e.hls.keys))
	}
	// 其他分集、其他设备不受影响
	if res := e.get(t, "/api/v1/hls/key/43/1?t=x", dev); res.status != http.StatusOK {
		t.Fatalf("其他分集应放行，得到 %d", res.status)
	}
	if res := e.get(t, testKeyURI, map[string]string{"X-Device-Id": "dev-2"}); res.status != http.StatusOK {
		t.Fatalf("其他设备应放行，得到 %d", res.status)
	}
	// 没有设备标识时按 IP 计数
	e.get(t, testKeyURI, nil)
	found := false
	for k := range e.keyLim.counts {
		if strings.HasPrefix(k, "ip:203.0.113.9:e42:") {
			found = true
		}
	}
	if !found {
		t.Fatalf("没有设备标识时应按 IP 计数：%v", e.keyLim.counts)
	}
	// 清单不计入密钥桶
	before := len(e.keyLim.counts)
	e.get(t, testPlaylistURI, dev)
	if len(e.keyLim.counts) != before {
		t.Fatal("清单请求不应计入密钥桶")
	}

	// 计数器故障时放行
	e = newHLSEnv(t, false)
	e.keyLim.err = errors.New("valkey down")
	if res := e.get(t, testKeyURI, dev); res.status != http.StatusOK {
		t.Fatalf("限流器不可用时应放行，得到 %d", res.status)
	}
}
