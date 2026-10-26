package collector

import (
	"testing"
	"time"
)

func validManifest() Manifest {
	return Manifest{
		Source: Source{
			ID:            "demo-source",
			Authorization: Authorization{Kind: AuthorizationPublicDomain, Reference: "PD-TEST-001"},
			Origins:       []Origin{{Host: "example.test", RobotsURL: "https://example.test/robots.txt"}},
		},
		Dramas: []Drama{{
			ID: "drama-1", Title: "测试短剧", Summary: "只用于自动化测试。",
			Cover: Asset{URL: "https://example.test/covers/one.webp", MIME: "image/webp"},
			Episodes: []Episode{{
				ID: "episode-1", Number: 1, Title: "第一集", Video: Asset{URL: "https://example.test/videos/one.mp4", MIME: "video/mp4"},
			}},
		}},
	}
}

func TestValidateManifestAcceptsAuthorizedExactOrigins(t *testing.T) {
	in, err := ValidateManifest(validManifest())
	if err != nil {
		t.Fatal(err)
	}
	if in.Source.Origins[0].Host != "example.test" || in.Dramas[0].Cover.MIME != "image/webp" {
		t.Fatalf("规范化结果错误：%+v", in)
	}
}

func TestValidateManifestRejectsUnsafeOrUnlicensedInput(t *testing.T) {
	cases := []struct {
		name string
		edit func(*Manifest)
	}{
		{"缺少授权引用", func(m *Manifest) { m.Source.Authorization.Reference = "" }},
		{"许可已到期", func(m *Manifest) {
			expired := time.Now().Add(-time.Hour)
			m.Source.Authorization = Authorization{Kind: AuthorizationLicensed, Reference: "contract-1", ExpiresAt: &expired}
		}},
		{"封面不在授权域名", func(m *Manifest) { m.Dramas[0].Cover.URL = "https://other.test/cover.webp" }},
		{"封面格式不支持", func(m *Manifest) { m.Dramas[0].Cover.MIME = "image/jpeg" }},
		{"HTTP 不允许", func(m *Manifest) { m.Dramas[0].Episodes[0].Video.URL = "http://example.test/video.mp4" }},
		{"robots 跨域", func(m *Manifest) { m.Source.Origins[0].RobotsURL = "https://robots.other.test/robots.txt" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := validManifest()
			tc.edit(&m)
			if _, err := ValidateManifest(m); err == nil {
				t.Fatal("应拒绝不合规 manifest")
			}
		})
	}
}

func TestValidateManifestAllowsHTTPOnlyForInjectedLocalTestMode(t *testing.T) {
	m := validManifest()
	m.Source.Origins[0].Host = "127.0.0.1"
	m.Source.Origins[0].RobotsURL = "http://127.0.0.1/robots.txt"
	m.Dramas[0].Cover.URL = "http://127.0.0.1/cover.webp"
	m.Dramas[0].Episodes[0].Video.URL = "http://127.0.0.1/video.mp4"
	if _, err := validateManifest(m, true, time.Now()); err != nil {
		t.Fatalf("仅测试注入的 HTTP 应可通过：%v", err)
	}
}
