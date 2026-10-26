package middleware

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"xsite/service/internal/crypto/apienc"
)

const encSecret = "0123456789abcdef0123456789abcdef-mw"

var encNow = time.Unix(1_792_575_000, 0)

func encEngine(t *testing.T, required bool) (*gin.Engine, apienc.PublicKey) {
	t.Helper()
	kr, err := apienc.ParseKeyring("k1:" + encSecret)
	if err != nil {
		t.Fatal(err)
	}
	r := gin.New()
	r.Use(
		Recover(),
		RequestID(),
		EdgeAuth(EdgeAuthOptions{ServiceToken: "svc-token"}),
		APIEncryption(APIEncryptionOptions{
			Keyring:  kr,
			Required: required,
			Exempt:   map[string]bool{"/api/v1/crypto/keys": true},
			Now:      func() time.Time { return encNow },
		}),
	)
	r.POST("/api/v1/echo", func(c *gin.Context) {
		body, _ := io.ReadAll(c.Request.Body)
		c.JSON(http.StatusOK, gin.H{
			"body": string(body), "content_type": c.GetHeader("Content-Type"), "encrypted": IsEncrypted(c),
		})
	})
	r.GET("/api/v1/items", func(c *gin.Context) {
		c.Header("Cache-Control", "public, s-maxage=60")
		c.JSON(http.StatusOK, gin.H{"q": c.Query("q")})
	})
	r.GET("/api/v1/missing", func(c *gin.Context) {
		c.AbortWithStatusJSON(http.StatusNotFound, gin.H{"code": "NOT_FOUND"})
	})
	r.GET("/api/v1/cached", func(c *gin.Context) {
		c.Header("ETag", `W/"v1"`)
		c.Status(http.StatusNotModified)
	})
	r.GET("/api/v1/panic", func(c *gin.Context) { panic("boom") })
	r.GET("/api/v1/crypto/keys", func(c *gin.Context) { c.JSON(http.StatusOK, kr.PublicKeys()) })
	return r, kr.PublicKeys()[0]
}

type encCall struct {
	method, uri string
	body        []byte
	ts          int64
	headers     map[string]string
	// tamperURI 让请求实际发往与签入 AAD 不同的地址
	tamperURI string
}

func doEnc(t *testing.T, r http.Handler, pk apienc.PublicKey, call encCall) (*httptest.ResponseRecorder, *apienc.Session) {
	t.Helper()
	if call.ts == 0 {
		call.ts = encNow.Unix()
	}
	creq, err := apienc.NewClientRequest(pk.Kid, pk.PublicKey, call.ts, call.method, call.uri, call.body)
	if err != nil {
		t.Fatal(err)
	}
	target := call.uri
	if call.tamperURI != "" {
		target = call.tamperURI
	}
	req := httptest.NewRequest(call.method, target, bytes.NewReader(creq.Body))
	req.Header.Set(HeaderEnc, creq.Header)
	if creq.Body != nil {
		req.Header.Set("Content-Type", apienc.ContentType)
	}
	for k, v := range call.headers {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w, creq.Session
}

func decodeEnc(t *testing.T, w *httptest.ResponseRecorder, s *apienc.Session) map[string]any {
	t.Helper()
	if ct := w.Header().Get("Content-Type"); ct != apienc.ContentType {
		t.Fatalf("响应应为加密类型，得到 %q：%s", ct, w.Body.String())
	}
	if cc := w.Header().Get("Cache-Control"); cc != "no-store" {
		t.Fatalf("加密响应必须 no-store，得到 %q", cc)
	}
	pt, err := s.OpenResponse(w.Body.Bytes())
	if err != nil {
		t.Fatalf("客户端解不开响应: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(pt, &out); err != nil {
		t.Fatalf("响应明文不是 JSON: %q", pt)
	}
	return out
}

func plainCode(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct{ Code string }
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("期望明文 JSON 错误，得到 %q", w.Body.String())
	}
	return body.Code
}

func TestAPIEncryption_RoundTripPOST(t *testing.T) {
	r, pk := encEngine(t, true)
	w, s := doEnc(t, r, pk, encCall{method: http.MethodPost, uri: "/api/v1/echo?x=1", body: []byte(`{"name":"短剧"}`)})
	if w.Code != http.StatusOK {
		t.Fatalf("期望 200，得到 %d %s", w.Code, w.Body.String())
	}
	if got := w.Header().Get(HeaderEncType); got != "application/json; charset=utf-8" {
		t.Fatalf("X-Enc-Type 应带原始类型，得到 %q", got)
	}
	out := decodeEnc(t, w, s)
	if out["body"] != `{"name":"短剧"}` || out["content_type"] != "application/json" || out["encrypted"] != true {
		t.Fatalf("handler 应拿到解密后的 JSON：%v", out)
	}
	if bytes.Contains(w.Body.Bytes(), []byte("短剧")) {
		t.Fatal("响应体中不应出现明文")
	}
}

func TestAPIEncryption_GETOverridesSharedCache(t *testing.T) {
	r, pk := encEngine(t, true)
	w, s := doEnc(t, r, pk, encCall{method: http.MethodGet, uri: "/api/v1/items?q=%E7%9F%AD"})
	if out := decodeEnc(t, w, s); out["q"] != "短" {
		t.Fatalf("查询串应正常传给 handler：%v", out)
	}
}

func TestAPIEncryption_ErrorStatusKeptAndEncrypted(t *testing.T) {
	r, pk := encEngine(t, true)
	w, s := doEnc(t, r, pk, encCall{method: http.MethodGet, uri: "/api/v1/missing"})
	if w.Code != http.StatusNotFound {
		t.Fatalf("状态码应保留，得到 %d", w.Code)
	}
	if out := decodeEnc(t, w, s); out["code"] != "NOT_FOUND" {
		t.Fatalf("业务错误也应加密返回：%v", out)
	}
}

func TestAPIEncryption_NotModifiedHasNoBody(t *testing.T) {
	r, pk := encEngine(t, true)
	w, _ := doEnc(t, r, pk, encCall{method: http.MethodGet, uri: "/api/v1/cached"})
	if w.Code != http.StatusNotModified || w.Body.Len() != 0 || w.Header().Get("ETag") != `W/"v1"` {
		t.Fatalf("304 应无响应体并保留 ETag，得到 %d %q %q", w.Code, w.Body.String(), w.Header().Get("ETag"))
	}
}

func TestAPIEncryption_PanicStillRecovered(t *testing.T) {
	r, pk := encEngine(t, true)
	w, _ := doEnc(t, r, pk, encCall{method: http.MethodGet, uri: "/api/v1/panic"})
	if w.Code != http.StatusInternalServerError || plainCode(t, w) != "INTERNAL" {
		t.Fatalf("panic 应返回 500 INTERNAL，得到 %d %s", w.Code, w.Body.String())
	}
}

func TestAPIEncryption_Rejections(t *testing.T) {
	r, pk := encEngine(t, true)
	tests := []struct {
		name string
		call encCall
		code string
	}{
		{"查询串被改", encCall{method: http.MethodGet, uri: "/api/v1/items?q=a", tamperURI: "/api/v1/items?q=b"}, "ENC_INVALID"},
		{"时间超前", encCall{method: http.MethodGet, uri: "/api/v1/items", ts: encNow.Add(6 * time.Minute).Unix()}, "ENC_TIME_SKEW"},
		{"时间落后", encCall{method: http.MethodGet, uri: "/api/v1/items", ts: encNow.Add(-6 * time.Minute).Unix()}, "ENC_TIME_SKEW"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w, _ := doEnc(t, r, pk, tt.call)
			if w.Code != http.StatusBadRequest || plainCode(t, w) != tt.code {
				t.Fatalf("期望 400 %s，得到 %d %s", tt.code, w.Code, w.Body.String())
			}
		})
	}

	t.Run("未知 kid", func(t *testing.T) {
		w, _ := doEnc(t, r, apienc.PublicKey{Kid: "k9", PublicKey: pk.PublicKey}, encCall{method: http.MethodGet, uri: "/api/v1/items"})
		if plainCode(t, w) != "ENC_KEY_UNKNOWN" {
			t.Fatalf("得到 %s", w.Body.String())
		}
	})
	t.Run("头格式错误", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/items", nil)
		req.Header.Set(HeaderEnc, "1.k1.123.not-base64")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if plainCode(t, w) != "ENC_INVALID" {
			t.Fatalf("得到 %s", w.Body.String())
		}
	})
}

func TestAPIEncryption_PlaintextPolicy(t *testing.T) {
	strict, _ := encEngine(t, true)
	loose, _ := encEngine(t, false)
	tests := []struct {
		name    string
		r       http.Handler
		path    string
		headers map[string]string
		status  int
	}{
		{"强制时浏览器或 APP 明文被拒", strict, "/api/v1/items", nil, http.StatusBadRequest},
		{"强制时公钥接口豁免", strict, "/api/v1/crypto/keys", nil, http.StatusOK},
		{"SSR 直连（服务令牌）允许明文", strict, "/api/v1/items", map[string]string{HeaderServiceToken: "svc-token"}, http.StatusOK},
		{"BFF 代转发的浏览器请求仍须加密", strict, "/api/v1/items", map[string]string{HeaderServiceToken: "svc-token", HeaderBFFProxy: "1"}, http.StatusBadRequest},
		{"伪造的服务令牌不算 SSR", strict, "/api/v1/items", map[string]string{HeaderServiceToken: "guess"}, http.StatusBadRequest},
		{"未强制时允许明文", loose, "/api/v1/items", nil, http.StatusOK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := do(tt.r, tt.path, tt.headers)
			if w.Code != tt.status {
				t.Fatalf("期望 %d，得到 %d %s", tt.status, w.Code, w.Body.String())
			}
			if tt.status == http.StatusBadRequest && plainCode(t, w) != "ENC_REQUIRED" {
				t.Fatalf("业务码应为 ENC_REQUIRED：%s", w.Body.String())
			}
			if tt.status == http.StatusOK && tt.path == "/api/v1/items" && w.Header().Get("Cache-Control") != "public, s-maxage=60" {
				t.Fatal("明文响应不应被改写缓存头")
			}
		})
	}
}
