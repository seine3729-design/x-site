package collector

import "testing"

func TestRobotsAllowedUsesMostSpecificMatchingRule(t *testing.T) {
	robots, err := ParseRobots([]byte(`
User-agent: xsite-test-collector
Disallow: /private
Allow: /private/allowed

User-agent: *
Disallow: /all
`), "xsite-test-collector")
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		url  string
		want bool
	}{
		{"https://example.test/public/a.webp", true},
		{"https://example.test/private/a.webp", false},
		{"https://example.test/private/allowed/a.webp", true},
		{"https://example.test/all/a.webp", true}, // 精确 User-Agent 组覆盖 * 组。
	}
	for _, tc := range cases {
		if got := robots.Allowed(tc.url); got != tc.want {
			t.Errorf("Allowed(%q)=%v，want %v", tc.url, got, tc.want)
		}
	}
}

func TestRobotsAllowedFallsBackToWildcard(t *testing.T) {
	robots, err := ParseRobots([]byte("User-agent: *\nDisallow: /blocked\n"), "xsite-test-collector")
	if err != nil {
		t.Fatal(err)
	}
	if robots.Allowed("https://example.test/blocked/a.mp4") {
		t.Fatal("通配 robots 拒绝规则必须生效")
	}
}

func TestParseRobotsFailsClosedForUnsupportedWildcardRule(t *testing.T) {
	if _, err := ParseRobots([]byte("User-agent: xsite-test-collector\nDisallow: /*.mp4\n"), "xsite-test-collector"); err == nil {
		t.Fatal("不支持的 robots 通配规则应拒绝采集")
	}
}

func TestParseRobotsMatchesUserAgentProductToken(t *testing.T) {
	robots, err := ParseRobots([]byte("User-agent: xsite-test-collector\nDisallow: /video\n"), "xsite-test-collector/1.0")
	if err != nil {
		t.Fatal(err)
	}
	if robots.Allowed("https://example.test/video/one.mp4") {
		t.Fatal("robots 的产品 token 应匹配带版本号的采集器 User-Agent")
	}
}
