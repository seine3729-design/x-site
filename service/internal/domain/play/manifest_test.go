package play

import (
	"errors"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
	"testing"

	"xsite/service/internal/transcode"
)

// 清单改写（S1-8）的单元测试：解析的白名单、地址替换、试看裁剪的边界。夹具与 internal/transcode 写出的格式一致，
// 真实产物的解析与解码在 hls_e2e_test.go。

const testMaster = `#EXTM3U
#EXT-X-VERSION:7
#EXT-X-INDEPENDENT-SEGMENTS
#EXT-X-STREAM-INF:BANDWIDTH=520000,AVERAGE-BANDWIDTH=410000,CODECS="avc1.64001e,mp4a.40.2",RESOLUTION=360x640,FRAME-RATE=29.970
v360.m3u8
#EXT-X-STREAM-INF:BANDWIDTH=1900000,AVERAGE-BANDWIDTH=1500000,CODECS="avc1.64001f",RESOLUTION=720x1280
v720.m3u8
`

// segName 是第 i 个切片的文件名（与转封装一样按内容哈希命名：32 位十六进制）。
func segName(i int) string { return fmt.Sprintf("s/%032x.m4s", i+1) }

const testInit = "s/00000000000000000000000000000abc.mp4"

// mediaFixture 生成与 transcode.writeMediaPlaylist 相同格式的一档清单：每个切片前带自己的 EXT-X-KEY 与 IV。
func mediaFixture(keyVer int32, durs ...float64) string {
	var b strings.Builder
	target := 1
	for _, d := range durs {
		target = max(target, int(math.Round(d)))
	}
	fmt.Fprintf(&b, "#EXTM3U\n#EXT-X-VERSION:7\n#EXT-X-TARGETDURATION:%d\n", target)
	b.WriteString("#EXT-X-MEDIA-SEQUENCE:0\n#EXT-X-PLAYLIST-TYPE:VOD\n#EXT-X-INDEPENDENT-SEGMENTS\n")
	fmt.Fprintf(&b, "#EXT-X-MAP:URI=%q\n", testInit)
	for i, d := range durs {
		fmt.Fprintf(&b, "#EXT-X-KEY:METHOD=AES-128,URI=%q,IV=0x%032x\n", transcode.KeyURI(keyVer), i+1)
		fmt.Fprintf(&b, "#EXTINF:%.6f,\n%s\n", d, segName(i))
	}
	b.WriteString("#EXT-X-ENDLIST\n")
	return b.String()
}

func repeat(d float64, n int) []float64 {
	out := make([]float64, n)
	for i := range out {
		out[i] = d
	}
	return out
}

func TestParseMaster(t *testing.T) {
	got, err := parseMaster([]byte(testMaster))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, []string{"v360.m3u8", "v720.m3u8"}) {
		t.Fatalf("档位 = %v", got)
	}
	// Windows 换行与注释不影响
	crlf := strings.ReplaceAll(testMaster, "\n", "\r\n") + "# 注释\r\n"
	if got, err := parseMaster([]byte(crlf)); err != nil || len(got) != 2 {
		t.Fatalf("CRLF 清单应能解析：%v %v", got, err)
	}

	stream := "#EXT-X-STREAM-INF:BANDWIDTH=1\n"
	for name, data := range map[string]string{
		"空":         "",
		"缺少头":       strings.TrimPrefix(testMaster, "#EXTM3U\n"),
		"没有档位":      "#EXTM3U\n#EXT-X-VERSION:7\n",
		"外部地址":      "#EXTM3U\n" + stream + "https://evil.example.com/v360.m3u8\n",
		"上级目录":      "#EXTM3U\n" + stream + "../v360.m3u8\n",
		"带查询串":      "#EXTM3U\n" + stream + "v360.m3u8?x=1\n",
		"不是档位清单":    "#EXTM3U\n" + stream + "s/abc.m4s\n",
		"缺少档位地址":    "#EXTM3U\n" + stream,
		"连续两个档位描述":  "#EXTM3U\n" + stream + stream + "v360.m3u8\n",
		"地址前没有档位描述": "#EXTM3U\nv360.m3u8\n",
		"音轨分组":      "#EXTM3U\n#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID=\"a\",NAME=\"a\",URI=\"https://evil/a.m3u8\"\n" + stream + "v360.m3u8\n",
		"I 帧清单":     "#EXTM3U\n#EXT-X-I-FRAME-STREAM-INF:BANDWIDTH=1,URI=\"iframe.m3u8\"\n" + stream + "v360.m3u8\n",
		"会话密钥":      "#EXTM3U\n#EXT-X-SESSION-KEY:METHOD=AES-128,URI=\"https://evil/k\"\n" + stream + "v360.m3u8\n",
	} {
		if _, err := parseMaster([]byte(data)); !errors.Is(err, errBadPlaylist) {
			t.Errorf("%s：应拒绝，得到 %v", name, err)
		}
	}
}

func TestParseKeyPlaceholder(t *testing.T) {
	// 与转封装写入的占位符保持一致
	for _, ver := range []int32{1, 3, 1 << 30} {
		if got, ok := parseKeyPlaceholder(transcode.KeyURI(ver)); !ok || got != ver {
			t.Errorf("KeyURI(%d) 应解析回 %d，得到 %d %v", ver, ver, got, ok)
		}
	}
	for _, bad := range []string{"", "xsite-key:", "xsite-key:0", "xsite-key:01", "xsite-key:-1", "xsite-key:+1",
		"xsite-key:2147483648", "xsite-key:1a", "other:1", "https://evil/key"} {
		if v, ok := parseKeyPlaceholder(bad); ok {
			t.Errorf("%q 不是合法占位符，得到 %d", bad, v)
		}
	}
}

func TestParseAttrs(t *testing.T) {
	got, err := parseAttrs(`METHOD=AES-128,URI="a,b=c",IV=0x01`)
	if err != nil {
		t.Fatal(err)
	}
	want := []attr{{"METHOD", "AES-128"}, {"URI", `"a,b=c"`}, {"IV", "0x01"}}
	if !slices.Equal(got, want) {
		t.Fatalf("属性 = %v", got)
	}
	for _, bad := range []string{``, `URI="abc`, `METHOD=AES-128,`, `method=x`, `URI`, `URI="a"IV=1`, `,METHOD=NONE`} {
		if _, err := parseAttrs(bad); !errors.Is(err, errBadPlaylist) {
			t.Errorf("%q：应拒绝，得到 %v", bad, err)
		}
	}
}

func TestParseMedia_Rejects(t *testing.T) {
	ok := mediaFixture(3, 2, 2)
	seg0 := segName(0)
	for name, data := range map[string]string{
		"空":               "",
		"缺少头":             strings.TrimPrefix(ok, "#EXTM3U\n"),
		"缺少结束标记（不是点播）":    strings.Replace(ok, "#EXT-X-ENDLIST\n", "", 1),
		"结束标记后还有内容":       ok + "#EXTINF:2,\n" + seg0 + "\n",
		"切片外部地址":          strings.Replace(ok, seg0, "https://evil.example.com/a.m4s", 1),
		"切片上级目录":          strings.Replace(ok, seg0, "s/../x.m4s", 1),
		"切片不在 s/ 下":       strings.Replace(ok, seg0, "abc.m4s", 1),
		"切片多级目录":          strings.Replace(ok, seg0, "s/a/b.m4s", 1),
		"切片不是媒体文件":        strings.Replace(ok, seg0, "s/master.m3u8", 1),
		"init 段外部地址":      strings.Replace(ok, testInit, "https://evil.example.com/init.mp4", 1),
		"init 段缺少地址":      strings.Replace(ok, `URI="`+testInit+`"`, `BYTERANGE="1@0"`, 1),
		"密钥地址不是占位符":       strings.Replace(ok, `"xsite-key:3"`, `"https://evil.example.com/key"`, 1),
		"密钥版本非法":          strings.Replace(ok, `"xsite-key:3"`, `"xsite-key:0"`, 1),
		"不支持的加密方式":        strings.Replace(ok, "METHOD=AES-128", "METHOD=SAMPLE-AES", 1),
		"不支持的密钥格式":        strings.Replace(ok, "IV=0x", "KEYFORMAT=\"com.apple.streamingkeydelivery\",IV=0x", 1),
		"METHOD=NONE 带地址": strings.Replace(ok, "METHOD=AES-128", "METHOD=NONE", 1),
		"低延迟分片":           strings.Replace(ok, "#EXT-X-ENDLIST", "#EXT-X-PART:DURATION=1,URI=\"s/p.m4s\"\n#EXT-X-ENDLIST", 1),
		"切片前缺少时长":         strings.Replace(ok, "#EXTINF:2.000000,\n"+seg0, seg0, 1),
		"时长为 0":           strings.Replace(ok, "#EXTINF:2.000000,", "#EXTINF:0,", 1),
		"时长为负":            strings.Replace(ok, "#EXTINF:2.000000,", "#EXTINF:-2,", 1),
		"时长不是数字":          strings.Replace(ok, "#EXTINF:2.000000,", "#EXTINF:abc,", 1),
		"时长 NaN":          strings.Replace(ok, "#EXTINF:2.000000,", "#EXTINF:NaN,", 1),
		"时长无穷":            strings.Replace(ok, "#EXTINF:2.000000,", "#EXTINF:+Inf,", 1),
		"连续两个时长":          strings.Replace(ok, "#EXTINF:2.000000,\n"+seg0, "#EXTINF:2.000000,\n#EXTINF:2.000000,\n"+seg0, 1),
		"最后的时长没有切片":       strings.Replace(ok, "#EXT-X-ENDLIST", "#EXTINF:2,\n#EXT-X-ENDLIST", 1),
		"没有切片":            "#EXTM3U\n#EXT-X-VERSION:7\n#EXT-X-ENDLIST\n",
	} {
		if _, err := parseMedia([]byte(data)); !errors.Is(err, errBadPlaylist) {
			t.Errorf("%s：应拒绝，得到 %v", name, err)
		}
	}
	if _, err := parseMedia([]byte(ok)); err != nil {
		t.Fatalf("合法清单应能解析：%v", err)
	}
}

// fakeURL 模拟线路签名：相对地址换成 CDN 上带签名的地址。
func fakeURL(uri string) string { return "https://cdn.example.com/hls/42/j1/" + uri + "?exp=1&s=sig" }

func fakeKeyURL(ver int32) string { return KeyURL("", 42, ver, "k1.tok.sig") }

func TestRewrite_FullPlayback(t *testing.T) {
	src := mediaFixture(3, 2.002, 2.002, 1.5)
	pl, err := parseMedia([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	keep := pl.trialCut(0)
	if keep != 3 {
		t.Fatalf("完整播放应保留全部 3 个切片，得到 %d", keep)
	}
	uris := pl.objectURIs(keep)
	if want := []string{testInit, segName(0), segName(1), segName(2)}; !slices.Equal(uris, want) {
		t.Fatalf("要签名的对象 = %v，期望 %v", uris, want)
	}
	out, err := pl.render(keep, fakeURL, fakeKeyURL)
	if err != nil {
		t.Fatal(err)
	}
	got := string(out)

	// 逐行对照：只有 init 段、切片与密钥地址变化，其余行（含 IV、时长、头部标签）原样保留
	srcLines := strings.Split(strings.TrimSpace(src), "\n")
	outLines := strings.Split(strings.TrimSpace(got), "\n")
	if len(srcLines) != len(outLines) {
		t.Fatalf("行数不同：%d / %d\n%s", len(srcLines), len(outLines), got)
	}
	for i, s := range srcLines {
		o := outLines[i]
		switch {
		case strings.HasPrefix(s, "#EXT-X-MAP:"):
			if o != `#EXT-X-MAP:URI="`+fakeURL(testInit)+`"` {
				t.Errorf("init 段未改写：%s", o)
			}
		case strings.HasPrefix(s, "#EXT-X-KEY:"):
			iv := s[strings.Index(s, ",IV="):]
			if o != `#EXT-X-KEY:METHOD=AES-128,URI="/api/v1/hls/key/42/3?t=k1.tok.sig"`+iv {
				t.Errorf("密钥地址未改写或 IV 变了：%s", o)
			}
		case strings.HasPrefix(s, "s/"):
			if o != fakeURL(s) {
				t.Errorf("切片未改写：%s", o)
			}
		default:
			if o != s {
				t.Errorf("第 %d 行不应变化：%q → %q", i+1, s, o)
			}
		}
	}
	if strings.Contains(got, "xsite-key:") || !strings.HasSuffix(got, "#EXT-X-ENDLIST\n") {
		t.Fatalf("改写后不应残留占位符，且以 EXT-X-ENDLIST 结尾：\n%s", got)
	}
}

func TestTrialCut(t *testing.T) {
	tests := []struct {
		name  string
		durs  []float64
		trial int32
		want  int
	}{
		{"完整播放", repeat(2.002, 30), 0, 30},
		{"2.002 秒切片试看 30 秒：第 15 个切片结束于 30.03 秒，不保留", repeat(2.002, 30), 30, 14},
		{"正好落在切片边界：第 15 个切片结束于 30 秒，保留", repeat(2, 30), 30, 15},
		{"浮点累加误差不影响边界", repeat(0.1, 400), 30, 300},
		{"试看短于第一个切片：至少保留一个", []float64{4, 4}, 1, 1},
		{"试看长于整集：全部保留", repeat(2, 5), 300, 5},
		{"最后一个短切片", []float64{2, 2, 0.5}, 5, 3},
	}
	for _, tt := range tests {
		pl, err := parseMedia([]byte(mediaFixture(1, tt.durs...)))
		if err != nil {
			t.Fatal(err)
		}
		if got := pl.trialCut(tt.trial); got != tt.want {
			t.Errorf("%s：保留 %d 个，期望 %d", tt.name, got, tt.want)
		}
	}
}

// 试看清单只含前 N 秒（11 的验收）：被裁掉的切片地址、它们的密钥标签都不出现，总时长不超过试看秒数。
func TestRewrite_TrialOnlyFirstSeconds(t *testing.T) {
	const trial = 10
	pl, err := parseMedia([]byte(mediaFixture(3, repeat(2.002, 20)...)))
	if err != nil {
		t.Fatal(err)
	}
	keep := pl.trialCut(trial)
	if keep != 4 { // 4 × 2.002 = 8.008；第 5 个结束于 10.01 秒
		t.Fatalf("应保留 4 个切片，得到 %d", keep)
	}
	if uris := pl.objectURIs(keep); len(uris) != 5 || uris[0] != testInit || uris[4] != segName(3) {
		t.Fatalf("只签 init 段与保留的切片：%v", uris)
	}
	out, err := pl.render(keep, fakeURL, fakeKeyURL)
	if err != nil {
		t.Fatal(err)
	}
	got := string(out)
	var total float64
	var segs int
	for _, l := range strings.Split(got, "\n") {
		if v, ok := strings.CutPrefix(l, "#EXTINF:"); ok {
			d, _ := strconv.ParseFloat(strings.TrimSuffix(v, ","), 64)
			total += d
		}
		if strings.HasPrefix(l, "https://cdn.example.com/hls/42/j1/s/") && strings.Contains(l, ".m4s") {
			segs++
		}
	}
	if segs != 4 || total > trial || strings.Count(got, "#EXT-X-KEY:") != 4 {
		t.Fatalf("试看清单应只有 4 个切片、总时长不超过 %d 秒：切片 %d，时长 %.3f\n%s", trial, segs, total, got)
	}
	for i := 4; i < 20; i++ {
		if strings.Contains(got, segName(i)) {
			t.Fatalf("试看之外的切片 %s 不应出现在清单里", segName(i))
		}
	}
	if !strings.HasSuffix(got, segName(3)+"?exp=1&s=sig\n#EXT-X-ENDLIST\n") {
		t.Fatalf("最后一个保留的切片之后直接结束：\n%s", got)
	}
}

func TestRewrite_KeepsUnencryptedAndExtraTags(t *testing.T) {
	src := "#EXTM3U\n#EXT-X-VERSION:7\n#EXT-X-TARGETDURATION:2\n#EXT-X-PLAYLIST-TYPE:VOD\n" +
		"#EXT-X-MAP:URI=\"" + testInit + "\",BYTERANGE=\"720@0\"\n" +
		"#EXT-X-KEY:METHOD=NONE\n#EXTINF:2.000000,\n" + segName(0) + "\n" +
		"#EXT-X-DISCONTINUITY\n#EXT-X-KEY:METHOD=AES-128,URI=\"xsite-key:2\",IV=0x01\n#EXTINF:2.000000,\n" + segName(1) + "\n" +
		"#EXT-X-ENDLIST\n"
	pl, err := parseMedia([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	out, err := pl.render(pl.trialCut(0), fakeURL, fakeKeyURL)
	if err != nil {
		t.Fatal(err)
	}
	got := string(out)
	for _, want := range []string{
		`#EXT-X-MAP:URI="` + fakeURL(testInit) + `",BYTERANGE="720@0"`,
		"#EXT-X-KEY:METHOD=NONE\n",
		"#EXT-X-DISCONTINUITY\n",
		`#EXT-X-KEY:METHOD=AES-128,URI="/api/v1/hls/key/42/2?t=k1.tok.sig",IV=0x01`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("缺少 %q：\n%s", want, got)
		}
	}
}

func TestRender_RejectsBadURLs(t *testing.T) {
	pl, err := parseMedia([]byte(mediaFixture(1, 2)))
	if err != nil {
		t.Fatal(err)
	}
	for name, objectURL := range map[string]func(string) string{
		"没有签名结果": func(string) string { return "" },
		"地址带引号":  func(u string) string { return "https://cdn.example.com/" + u + `?"` },
		"地址带换行":  func(u string) string { return "https://cdn.example.com/" + u + "\n#EXT-X-ENDLIST" },
	} {
		if _, err := pl.render(1, objectURL, fakeKeyURL); err == nil {
			t.Errorf("%s：应拒绝", name)
		}
	}
	if _, err := pl.render(1, fakeURL, func(int32) string { return "" }); err == nil {
		t.Error("密钥地址为空时应拒绝（否则占位符会留在清单里）")
	}
}

func TestKeyURL(t *testing.T) {
	if got := KeyURL("", 42, 3, "k1.eyJlIjo0Mn0.c2ln"); got != "/api/v1/hls/key/42/3?t=k1.eyJlIjo0Mn0.c2ln" {
		t.Fatalf("KeyURL = %q", got)
	}
	// 后台试播用另一组路由（不加密、走后台会话）
	if got := PreviewKeyURL(42, 3, "p1.eyJlIjo0Mn0.c2ln"); got != "/api/admin/preview/key/42/3?t=p1.eyJlIjo0Mn0.c2ln" {
		t.Fatalf("PreviewKeyURL = %q", got)
	}
}

// tsSegName 是新 MPEG-TS profile 的内容哈希切片文件名。
func tsSegName(i int) string { return fmt.Sprintf("s/%032x.ts", i+1) }

func tsMediaFixture(keyVer int32, durs ...float64) string {
	var b strings.Builder
	target := 1
	for _, d := range durs {
		target = max(target, int(math.Round(d)))
	}
	fmt.Fprintf(&b, "#EXTM3U\n#EXT-X-VERSION:4\n#EXT-X-TARGETDURATION:%d\n", target)
	b.WriteString("#EXT-X-MEDIA-SEQUENCE:0\n#EXT-X-PLAYLIST-TYPE:VOD\n")
	for i, d := range durs {
		fmt.Fprintf(&b, "#EXT-X-KEY:METHOD=AES-128,URI=%q,IV=0x%032x\n", transcode.KeyURI(keyVer), i+1)
		fmt.Fprintf(&b, "#EXTINF:%.6f,\n%s\n", d, tsSegName(i))
	}
	b.WriteString("#EXT-X-ENDLIST\n")
	return b.String()
}

func TestRewrite_MPEGTSSegments(t *testing.T) {
	pl, err := parseMedia([]byte(tsMediaFixture(3, 4, 4, 1)))
	if err != nil {
		t.Fatal(err)
	}
	if got := pl.objectURIs(pl.trialCut(0)); !slices.Equal(got, []string{tsSegName(0), tsSegName(1), tsSegName(2)}) {
		t.Fatalf("MPEG-TS 只应签切片，不应虚构 init 段：%v", got)
	}
	out, err := pl.render(2, fakeURL, fakeKeyURL)
	if err != nil {
		t.Fatal(err)
	}
	text := string(out)
	if strings.Contains(text, "#EXT-X-MAP") || strings.Count(text, ".ts?exp=1&s=sig") != 2 || strings.Contains(text, tsSegName(2)) {
		t.Fatalf("MPEG-TS 清单重写或试看裁剪不正确：\n%s", text)
	}
}
