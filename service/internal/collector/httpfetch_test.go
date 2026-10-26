package collector

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"xsite/service/internal/netguard"
)

func TestHTTPFetcherRejectsRedirectAndOversizeResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/redirect":
			http.Redirect(w, r, "/asset", http.StatusFound)
		case "/large":
			w.Header().Set("Content-Length", "20")
			_, _ = w.Write(make([]byte, 20))
		default:
			w.Header().Set("Content-Type", "application/octet-stream")
			_, _ = w.Write([]byte("asset"))
		}
	}))
	defer server.Close()
	fetcher := NewHTTPFetcher(HTTPFetcherOptions{Egress: netguard.Policy{AllowPrivate: true}, AllowHTTP: true})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := fetcher.Fetch(ctx, server.URL+"/redirect", 100); err == nil {
		t.Fatal("重定向必须拒绝")
	}
	if _, err := fetcher.Fetch(ctx, server.URL+"/large", 10); err == nil {
		t.Fatal("超出 Content-Length 上限必须拒绝")
	}
	fetched, err := fetcher.Fetch(ctx, server.URL+"/asset", 100)
	if err != nil {
		t.Fatal(err)
	}
	defer fetched.Body.Close()
	data, err := io.ReadAll(fetched.Body)
	if err != nil || string(data) != "asset" || fetched.MIME != "application/octet-stream" {
		t.Fatalf("正常下载错误：data=%q mime=%q err=%v", data, fetched.MIME, err)
	}
}
