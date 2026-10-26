package play

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"xsite/service/internal/crypto/mediakey"
	"xsite/service/internal/crypto/playtoken"
	"xsite/service/internal/domain/content"
	"xsite/service/internal/domain/storage"
	"xsite/service/internal/errs"
)

// 清单与密钥（S1-8）的领域层测试：令牌校验、可见性与线路复核、清单读取与改写的编排、密钥解包。
// SQL 在 store/pg 的集成测试里，HTTP 层（只收加密请求、限流）在 http/public/play。

const testObjectKey = "hls/42/j1"

type fakeHLSRepo struct {
	fakeRepo
	keys map[[2]int64][]byte // (分集, 版本) → 密文
}

func (r fakeHLSRepo) MediaKey(_ context.Context, episodeID int64, keyVer int32) ([]byte, error) {
	k, ok := r.keys[[2]int64{episodeID, int64(keyVer)}]
	if !ok {
		return nil, errs.ErrNotFound
	}
	return k, nil
}

type fakeLineLookup struct {
	lines map[int64]storage.Line
	ready map[int64][]int64
	err   error
}

func (f *fakeLineLookup) GetLine(_ context.Context, id int64) (storage.Line, error) {
	l, ok := f.lines[id]
	if !ok {
		return storage.Line{}, errs.ErrNotFound
	}
	return l, nil
}

func (f *fakeLineLookup) ReadyProviders(_ context.Context, objectID int64) ([]int64, error) {
	return f.ready[objectID], f.err
}

type fakeFiles struct {
	files map[string]string
	err   error
	calls []Product
	names []string
}

func (f *fakeFiles) ReadFile(_ context.Context, p Product, name string) ([]byte, error) {
	f.calls = append(f.calls, p)
	f.names = append(f.names, name)
	if f.err != nil {
		return nil, f.err
	}
	data, ok := f.files[name]
	if !ok {
		return nil, errors.New("对象不存在")
	}
	return []byte(data), nil
}

type fakeSigner struct {
	err    error
	line   storage.Line
	prefix string
	keys   []string
}

func (f *fakeSigner) SignSegments(_ context.Context, line storage.Line, prefix string, keys []string, _ time.Time) ([]string, error) {
	f.line, f.prefix, f.keys = line, prefix, keys
	if f.err != nil {
		return nil, f.err
	}
	out := make([]string, len(keys))
	for i, k := range keys {
		out[i] = "https://cdn.example.com/" + k + "?s=sig"
	}
	return out, nil
}

type hlsFixture struct {
	svc     *HLSService
	tokens  *playtoken.Keyring
	media   *mediakey.Keyring
	lines   *fakeLineLookup
	files   *fakeFiles
	signer  *fakeSigner
	content map[int32][]byte // 版本 → 内容密钥原文
}

func newHLSFixture(t *testing.T) hlsFixture {
	t.Helper()
	tokens, err := playtoken.ParseKeyring("k1:0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	media, err := mediakey.ParseKeyring("m1:fedcba9876543210fedcba9876543210")
	if err != nil {
		t.Fatal(err)
	}
	contentKeys := map[int32][]byte{2: bytes.Repeat([]byte{0x22}, 16), 3: bytes.Repeat([]byte{0x33}, 16)}
	wrapped := map[[2]int64][]byte{}
	for ver, k := range contentKeys {
		w, err := media.Wrap(42, ver, k)
		if err != nil {
			t.Fatal(err)
		}
		wrapped[[2]int64{42, int64(ver)}] = w
	}
	repo := fakeHLSRepo{
		fakeRepo: fakeRepo{episodes: map[int64]Episode{
			42: {ID: 42, DramaID: 9, Access: content.AccessVIP, TrialSec: 10, VideoObjectID: 501, ObjectKey: testObjectKey},
		}},
		keys: wrapped,
	}
	lines := &fakeLineLookup{
		lines: map[int64]storage.Line{
			12: {ID: 12, ProviderID: 3, Kind: storage.LineVideo, Status: storage.LineActive, SignType: storage.SignHMACToken},
			13: {ID: 13, ProviderID: 4, Kind: storage.LineVideo, Status: storage.LineWatch},
			14: {ID: 14, ProviderID: 3, Kind: storage.LineVideo, Status: storage.LineDisabled},
			15: {ID: 15, ProviderID: 3, Kind: storage.LineImage, Status: storage.LineActive},
			16: {ID: 16, ProviderID: 5, Kind: storage.LineVideo, Status: storage.LineActive},
		},
		ready: map[int64][]int64{501: {3, 4}},
	}
	files := &fakeFiles{files: map[string]string{
		"master.m3u8": testMaster,
		"v360.m3u8":   mediaFixture(3, repeat(2.002, 20)...),
		"v720.m3u8":   mediaFixture(3, repeat(2.002, 20)...),
	}}
	signer := &fakeSigner{}
	svc := NewHLSService(tokens, repo, lines, files, signer, media)
	svc.now = func() time.Time { return testNow }
	return hlsFixture{svc: svc, tokens: tokens, media: media, lines: lines, files: files, signer: signer, content: contentKeys}
}

// token 签发一个令牌（默认：第 42 集、线路 12、绑定 dev-1、完整播放、5 分钟后过期）。
func (f hlsFixture) token(t *testing.T, edit func(*playtoken.Claims)) string {
	t.Helper()
	c := playtoken.Claims{EpisodeID: 42, LineID: 12, Device: playtoken.DeviceHash("dev-1"), ExpiresAt: testNow.Add(5 * time.Minute)}
	if edit != nil {
		edit(&c)
	}
	tok, err := f.tokens.Sign(c)
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

func TestPlaylist_Master(t *testing.T) {
	f := newHLSFixture(t)
	got, err := f.svc.Playlist(context.Background(), PlaylistInput{Token: f.token(t, nil), File: IndexFile, DeviceID: "dev-1"})
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != testMaster {
		t.Fatalf("主清单应原样返回：\n%s", got)
	}
	// 从产物目录读，线路 12 的存储源 3 排第一，其余就绪副本随后
	p := f.files.calls[0]
	if p.ObjectID != 501 || p.Key != testObjectKey || !slices.Equal(p.Providers, []int64{3, 4}) || f.files.names[0] != "master.m3u8" {
		t.Fatalf("读取位置不对：%+v %v", p, f.files.names)
	}
	// 观察中的线路照常可用；它的存储源排第一
	f.files.calls = nil
	if _, err := f.svc.Playlist(context.Background(), PlaylistInput{
		Token: f.token(t, func(c *playtoken.Claims) { c.LineID = 13 }), File: IndexFile, DeviceID: "dev-1",
	}); err != nil {
		t.Fatal(err)
	}
	if got := f.files.calls[0].Providers; !slices.Equal(got, []int64{4, 3}) {
		t.Fatalf("应优先从线路自己的存储源读：%v", got)
	}
}

func TestPlaylist_VariantRewritten(t *testing.T) {
	f := newHLSFixture(t)
	tok := f.token(t, nil)
	got, err := f.svc.Playlist(context.Background(), PlaylistInput{Token: tok, File: "v720.m3u8", DeviceID: "dev-1"})
	if err != nil {
		t.Fatal(err)
	}
	if f.files.names[1] != "v720.m3u8" {
		t.Fatalf("应读取请求的档位：%v", f.files.names)
	}
	// 目录前缀签名：前缀是产物的 s/ 目录，对象是 init 段与全部切片的完整路径
	if f.signer.line.ID != 12 || f.signer.prefix != testObjectKey+"/s/" || len(f.signer.keys) != 21 ||
		f.signer.keys[0] != testObjectKey+"/"+testInit || f.signer.keys[20] != testObjectKey+"/"+segName(19) {
		t.Fatalf("签名输入不对：线路 %d 前缀 %q 对象 %d 个 %v", f.signer.line.ID, f.signer.prefix, len(f.signer.keys), f.signer.keys[:2])
	}
	out := string(got)
	if !strings.Contains(out, `#EXT-X-MAP:URI="https://cdn.example.com/`+testObjectKey+"/"+testInit+`?s=sig"`) ||
		!strings.Contains(out, "\nhttps://cdn.example.com/"+testObjectKey+"/"+segName(19)+"?s=sig\n") ||
		strings.Count(out, `URI="/api/v1/hls/key/42/3?t=`+tok+`"`) != 20 || strings.Contains(out, "xsite-key:") {
		t.Fatalf("清单未正确改写：\n%s", out)
	}
}

func TestPlaylist_TrialOnlyFirstSeconds(t *testing.T) {
	f := newHLSFixture(t)
	tok := f.token(t, func(c *playtoken.Claims) { c.TrialSec = 10 })
	got, err := f.svc.Playlist(context.Background(), PlaylistInput{Token: tok, File: "v360.m3u8", DeviceID: "dev-1"})
	if err != nil {
		t.Fatal(err)
	}
	// 2.002 秒切片试看 10 秒：只签 init 段与前 4 个切片，后面的切片不出现、也不签名
	if len(f.signer.keys) != 5 || slices.Contains(f.signer.keys, testObjectKey+"/"+segName(4)) {
		t.Fatalf("试看只签前 4 个切片：%v", f.signer.keys)
	}
	if n := strings.Count(string(got), "#EXTINF:"); n != 4 || strings.Contains(string(got), segName(4)) {
		t.Fatalf("试看清单只含前 10 秒：%d 个切片\n%s", n, got)
	}

	// 主清单不裁剪（各档清单才有切片）
	master, err := f.svc.Playlist(context.Background(), PlaylistInput{Token: tok, File: IndexFile, DeviceID: "dev-1"})
	if err != nil || string(master) != testMaster {
		t.Fatalf("试看令牌的主清单原样返回：%v", err)
	}
}

func TestPlaylist_TokenErrors(t *testing.T) {
	f := newHLSFixture(t)
	other, _ := playtoken.ParseKeyring("k2:zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz")
	forged, _ := other.Sign(playtoken.Claims{EpisodeID: 42, LineID: 12, ExpiresAt: testNow.Add(time.Minute)})
	valid := f.token(t, nil)
	tests := []struct {
		name   string
		token  string
		device string
		want   *errs.Error
	}{
		{"过期 401", f.token(t, func(c *playtoken.Claims) { c.ExpiresAt = testNow }), "dev-1", ErrPlayTokenExpired},
		{"空令牌", "", "dev-1", ErrPlayTokenInvalid},
		{"格式错误", "not-a-token", "dev-1", ErrPlayTokenInvalid},
		{"签名被篡改", valid[:len(valid)-2] + "AA", "dev-1", ErrPlayTokenInvalid},
		{"其他密钥签发", forged, "dev-1", ErrPlayTokenInvalid},
		{"不是签给本设备的", valid, "dev-2", ErrPlayTokenInvalid},
		{"签给设备的令牌不能不带设备标识用", valid, "", ErrPlayTokenInvalid},
	}
	for _, tt := range tests {
		_, err := f.svc.Playlist(context.Background(), PlaylistInput{Token: tt.token, File: IndexFile, DeviceID: tt.device})
		if !errors.Is(err, tt.want) || errs.From(err).Status != tt.want.Status || errs.From(err).Message != tt.want.Message {
			t.Errorf("%s：期望 %d %s，得到 %v", tt.name, tt.want.Status, tt.want.Code, err)
		}
	}
	if errs.From(ErrPlayTokenExpired).Status != 401 || errs.From(ErrPlayTokenInvalid).Status != 403 {
		t.Fatal("过期 401、其余 403")
	}
	if len(f.files.calls) != 0 {
		t.Fatal("令牌不对时不应读取产物")
	}

	// 签发时没有设备标识的令牌不限设备
	unbound := f.token(t, func(c *playtoken.Claims) { c.Device = "" })
	if _, err := f.svc.Playlist(context.Background(), PlaylistInput{Token: unbound, File: IndexFile, DeviceID: "any"}); err != nil {
		t.Fatalf("不绑定设备的令牌应可用：%v", err)
	}
}

func TestPlaylist_NotFoundAndUnavailable(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name  string
		file  string
		edit  func(*playtoken.Claims)
		setup func(f hlsFixture)
		want  *errs.Error
	}{
		{"不在主清单里的档位", "v1080.m3u8", nil, nil, errs.ErrNotFound},
		{"不能直接取产物主清单", "master.m3u8", nil, nil, errs.ErrNotFound},
		{"不能借此读其他文件", "s.m3u8", nil, nil, errs.ErrNotFound},
		{"分集已下架或删除", IndexFile, func(c *playtoken.Claims) { c.EpisodeID = 404 }, nil, errs.ErrNotFound},
		{"线路已删除", IndexFile, func(c *playtoken.Claims) { c.LineID = 99 }, nil, ErrLineUnavailable},
		{"线路已停用", IndexFile, func(c *playtoken.Claims) { c.LineID = 14 }, nil, ErrLineUnavailable},
		{"不是视频线路", IndexFile, func(c *playtoken.Claims) { c.LineID = 15 }, nil, ErrLineUnavailable},
		{"产物不在线路的存储源上", IndexFile, func(c *playtoken.Claims) { c.LineID = 16 }, nil, ErrLineUnavailable},
		{"读不到清单", IndexFile, nil, func(f hlsFixture) { f.files.err = errors.New("timeout") }, ErrLineUnavailable},
		{"主清单损坏", IndexFile, nil, func(f hlsFixture) { f.files.files["master.m3u8"] = "<html>" }, ErrLineUnavailable},
		{"档位清单损坏", "v720.m3u8", nil, func(f hlsFixture) { f.files.files["v720.m3u8"] = "#EXTM3U\nhttps://evil/a.m4s\n" }, ErrLineUnavailable},
		{"签不出地址（线路配置不全）", "v720.m3u8", nil, func(f hlsFixture) { f.signer.err = storage.ErrSignKeyMissing }, ErrLineUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newHLSFixture(t)
			if tt.setup != nil {
				tt.setup(f)
			}
			_, err := f.svc.Playlist(ctx, PlaylistInput{Token: f.token(t, tt.edit), File: tt.file, DeviceID: "dev-1"})
			if !errors.Is(err, tt.want) || errs.From(err).Message != tt.want.Message {
				t.Fatalf("期望 %s「%s」，得到 %v", tt.want.Code, tt.want.Message, err)
			}
		})
	}

	// 没有任何副本记录的老产物：不按副本过滤，读默认存储（Providers 为空）
	f := newHLSFixture(t)
	f.lines.ready = nil
	if _, err := f.svc.Playlist(ctx, PlaylistInput{Token: f.token(t, func(c *playtoken.Claims) { c.LineID = 16 }), File: IndexFile, DeviceID: "dev-1"}); err != nil {
		t.Fatal(err)
	}
	if p := f.files.calls[0]; len(p.Providers) != 0 {
		t.Fatalf("没有副本记录时读默认存储：%+v", p)
	}

	// 查副本失败是内部错误（不是线路问题）
	f = newHLSFixture(t)
	f.lines.err = errors.New("db down")
	if _, err := f.svc.Playlist(ctx, PlaylistInput{Token: f.token(t, nil), File: IndexFile, DeviceID: "dev-1"}); errs.From(err) != errs.ErrInternal {
		t.Fatalf("查副本失败应是内部错误，得到 %v", err)
	}
}

func TestKey(t *testing.T) {
	ctx := context.Background()
	f := newHLSFixture(t)
	tok := f.token(t, func(c *playtoken.Claims) { c.TrialSec = 10 })
	for _, ver := range []int32{3, 2} { // 当前版本与仍在库里的旧版本
		got, err := f.svc.Key(ctx, KeyInput{Token: tok, EpisodeID: 42, KeyVer: ver, DeviceID: "dev-1"})
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, f.content[ver]) || len(got) != mediakey.KeySize {
			t.Fatalf("第 %d 版密钥不对：%x", ver, got)
		}
	}

	tests := []struct {
		name string
		in   KeyInput
		want *errs.Error
	}{
		{"过期 401", KeyInput{Token: f.token(t, func(c *playtoken.Claims) { c.ExpiresAt = testNow.Add(-time.Second) }), EpisodeID: 42, KeyVer: 3, DeviceID: "dev-1"}, ErrPlayTokenExpired},
		{"其他分集的令牌", KeyInput{Token: f.token(t, func(c *playtoken.Claims) { c.EpisodeID = 43 }), EpisodeID: 42, KeyVer: 3, DeviceID: "dev-1"}, ErrPlayTokenInvalid},
		{"不是签给本设备的", KeyInput{Token: tok, EpisodeID: 42, KeyVer: 3, DeviceID: "dev-2"}, ErrPlayTokenInvalid},
		{"没有令牌", KeyInput{EpisodeID: 42, KeyVer: 3, DeviceID: "dev-1"}, ErrPlayTokenInvalid},
		{"版本不存在", KeyInput{Token: tok, EpisodeID: 42, KeyVer: 9, DeviceID: "dev-1"}, errs.ErrNotFound},
		{"版本非法", KeyInput{Token: tok, EpisodeID: 42, KeyVer: 0, DeviceID: "dev-1"}, errs.ErrNotFound},
	}
	for _, tt := range tests {
		if _, err := f.svc.Key(ctx, tt.in); !errors.Is(err, tt.want) || errs.From(err).Message != tt.want.Message {
			t.Errorf("%s：期望 %s，得到 %v", tt.name, tt.want.Code, err)
		}
	}

	// 下架立即生效：令牌未过期也取不到密钥
	f.svc.repo = fakeHLSRepo{fakeRepo: fakeRepo{episodes: map[int64]Episode{}}, keys: f.svc.repo.(fakeHLSRepo).keys}
	if _, err := f.svc.Key(ctx, KeyInput{Token: tok, EpisodeID: 42, KeyVer: 3, DeviceID: "dev-1"}); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("下架后应 404，得到 %v", err)
	}

	// 密文解不开（包装密钥轮换时漏配旧 kid）是内部错误，不能返回任何字节
	f = newHLSFixture(t)
	other, _ := mediakey.ParseKeyring("m2:00000000000000000000000000000000")
	f.svc.keys = other
	got, err := f.svc.Key(ctx, KeyInput{Token: tok, EpisodeID: 42, KeyVer: 3, DeviceID: "dev-1"})
	if errs.From(err) != errs.ErrInternal || got != nil {
		t.Fatalf("解不开时应是内部错误，得到 %x %v", got, err)
	}
}

func TestPlaylist_MPEGTSSegments(t *testing.T) {
	f := newHLSFixture(t)
	f.files.files["v360.m3u8"] = tsMediaFixture(3, 4, 4, 1)
	tok := f.token(t, nil)
	got, err := f.svc.Playlist(context.Background(), PlaylistInput{Token: tok, File: "v360.m3u8", DeviceID: "dev-1"})
	if err != nil {
		t.Fatal(err)
	}
	if len(f.signer.keys) != 3 || f.signer.keys[0] != testObjectKey+"/"+tsSegName(0) || f.signer.keys[2] != testObjectKey+"/"+tsSegName(2) {
		t.Fatalf("MPEG-TS 应只签三个切片：%v", f.signer.keys)
	}
	text := string(got)
	if strings.Contains(text, "#EXT-X-MAP") || !strings.Contains(text, "https://cdn.example.com/"+testObjectKey+"/"+tsSegName(2)+"?s=sig") || strings.Contains(text, "xsite-key:") {
		t.Fatalf("MPEG-TS 清单未正确改写：\n%s", text)
	}
}
