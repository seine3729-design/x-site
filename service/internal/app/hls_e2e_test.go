package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"xsite/service/internal/config"
	"xsite/service/internal/crypto/apienc"
	"xsite/service/internal/crypto/mediakey"
	"xsite/service/internal/crypto/playtoken"
	"xsite/service/internal/domain/content"
	"xsite/service/internal/domain/storage"
	"xsite/service/internal/netguard"
	"xsite/service/internal/store/blob"
	"xsite/service/internal/store/pg"
	"xsite/service/internal/transcode"
)

// 清单与密钥（S1-8）的真实链路：用开发镜像里的 FFmpeg 把一段样例视频转封装进本地 SeaweedFS，
// 经 app.NewAPI 装配出的完整路由（真实的报文加密、Postgres、Valkey、存储源凭据与线路签名）
// 选源 → 加密请求取主清单与一档清单 → 加密请求取密钥，再用取回的清单、切片与密钥让 FFmpeg 完整解码。
//
// 会写共享开发库与对象存储（数据带唯一标记，结束时清理），默认跳过。在仓库根目录运行：
//
//	docker compose -f compose.yaml --env-file .env --profile service run --rm --no-deps -T \
//	  -e XSITE_E2E_HLS=1 service go test ./internal/app/ -run TestHLS_EndToEnd -count=1 -v
func TestHLS_EndToEnd(t *testing.T) {
	if os.Getenv("XSITE_E2E_HLS") != "1" {
		t.Skip("设置 XSITE_E2E_HLS=1 运行（需要 compose 的 Postgres、Valkey、SeaweedFS 与开发镜像里的 FFmpeg）")
	}
	for _, bin := range []string{"ffmpeg", "ffprobe"} {
		if _, err := exec.LookPath(bin); err != nil {
			t.Fatalf("没有 %s", bin)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	cfg.AutoMigrate = false // 共享库：不替别人执行迁移
	api, err := NewAPI(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(api.Close)
	srv := httptest.NewServer(api.Handler)
	t.Cleanup(srv.Close)
	pool := api.pool

	mark := fmt.Sprintf("e2e-s18-%d", time.Now().UnixNano())
	device := mark + "-dev"
	m := cfg.Media
	client, err := blob.New(blob.Config{Endpoint: m.S3Endpoint, Region: m.S3Region, AccessKey: m.S3AccessKey, SecretKey: m.S3SecretKey, PathStyle: m.S3PathStyle})
	if err != nil {
		t.Fatal(err)
	}
	media, images := client.Bucket(m.BucketMedia), client.Bucket(m.BucketAssets)

	// ---------- 造数：剧与分集、转封装产物、内容密钥、存储源与副本、线路 ----------
	admin := content.NewService(pg.NewContentRepo(pool))
	drama, err := admin.CreateDrama(ctx, content.DramaInput{Title: mark})
	if err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-time.Hour)
	if _, _, err := admin.UpdateDrama(ctx, drama.ID, content.DramaInput{Title: mark, Status: content.DramaPublished, PublishAt: &past}); err != nil {
		t.Fatal(err)
	}
	ep, err := admin.CreateEpisode(ctx, drama.ID, content.EpisodeInput{Number: 1, Access: content.AccessFree, TrialSec: 6, DurationSec: 12})
	if err != nil {
		t.Fatal(err)
	}
	var objectID int64
	jobID := strings.ReplaceAll(mark, "-", "")
	prefix := transcode.OutputPrefix(ep.ID, jobID)
	sourceKey := "e2e-s18/" + mark + "/source.mp4"
	var posterKey string
	t.Cleanup(func() {
		bg := context.Background()
		run := func(sql string, args ...any) {
			if _, err := pool.Exec(bg, sql, args...); err != nil {
				t.Errorf("清理失败 %s：%v", sql, err)
			}
		}
		run(`DELETE FROM storage_line WHERE name LIKE $1`, mark+"%")
		run(`DELETE FROM media_replica WHERE object_id = $1`, objectID)
		run(`DELETE FROM storage_provider WHERE name LIKE $1`, mark+"%")
		run(`DELETE FROM drama WHERE id = $1`, drama.ID) // 分集与内容密钥级联删除
		run(`DELETE FROM media_object WHERE id = $1`, objectID)
		_, _ = media.DeletePrefix(bg, prefix+"/")
		_ = media.Delete(bg, sourceKey)
		if posterKey != "" {
			_ = images.Delete(bg, posterKey)
		}
		for _, k := range []string{"master.m3u8", "v360.m3u8", "v480.m3u8", "v720.m3u8"} {
			api.valkey.Do(bg, api.valkey.B().Del().Key("xsite:play:manifest:"+prefix+"/"+k).Build())
		}
	})

	src := filepath.Join(t.TempDir(), "source.mp4")
	if out, err := exec.CommandContext(ctx, "ffmpeg", "-hide_banner", "-v", "error", "-y",
		"-f", "lavfi", "-i", "testsrc2=size=360x640:rate=30", "-f", "lavfi", "-i", "sine=frequency=440:sample_rate=48000",
		"-t", "12", "-ac", "2", "-c:v", "libx264", "-preset", "ultrafast", "-c:a", "aac", src).CombinedOutput(); err != nil {
		t.Fatalf("生成样例视频：%v\n%s", err, out)
	}
	if err := media.PutFile(ctx, sourceKey, src, blob.PutOptions{ContentType: "video/mp4"}); err != nil {
		t.Fatal(err)
	}
	pipeline := &transcode.Pipeline{FFmpeg: "ffmpeg", FFprobe: "ffprobe", Preset: "ultrafast", WorkDir: t.TempDir(),
		Source: media, Media: media, Images: images}
	ws, err := pipeline.Fetch(ctx, jobID, sourceKey)
	if err != nil {
		t.Fatal(err)
	}
	contentKey := mediakey.NewContentKey()
	res, err := pipeline.Package(ctx, ws, transcode.Request{JobID: jobID, EpisodeID: ep.ID, KeyVer: 1, Key: contentKey}, nil)
	_ = ws.Close()
	if err != nil {
		t.Fatal(err)
	}
	posterKey = res.Poster
	if res.Prefix != prefix {
		t.Fatalf("产物目录 %s，期望 %s", res.Prefix, prefix)
	}
	kek, err := mediakey.ParseKeyring(m.KEKs)
	if err != nil {
		t.Fatal(err)
	}
	wrapped, err := kek.Wrap(ep.ID, 1, contentKey)
	if err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO media_object (kind, key, size_bytes, sha256, episode_id) VALUES ('hls_dir', $1, $2, $3, $4) RETURNING id`,
		prefix, res.SizeBytes, res.MasterSHA256, ep.ID).Scan(&objectID); err != nil {
		t.Fatal(err)
	}
	mustExec(t, pool.Exec, `INSERT INTO media_key (episode_id, key_ver, wrapped_key) VALUES ($1, 1, $2)`, ep.ID, wrapped)
	mustExec(t, pool.Exec, `UPDATE episode SET media_status = 'ready', video_object_id = $2, key_ver = 1 WHERE id = $1`, ep.ID, objectID)

	storageKeys, err := cfg.Storage.Keyring()
	if err != nil {
		t.Fatal(err)
	}
	storageRepo := pg.NewStorageRepo(pool)
	policy := storage.Policy{Egress: netguard.Policy{AllowPrivate: true}}
	providers := storage.NewService(storageRepo, storageKeys, nil, policy)
	cred := storage.Credential{AccessKeyID: m.S3AccessKey, SecretAccessKey: m.S3SecretKey}
	mkProvider := func(name string) int64 {
		p, err := providers.Create(ctx, storage.Input{
			Name: mark + name, Type: storage.TypeCustom, Subtype: storage.SubtypeS3Compatible, Purpose: storage.PurposeVideo,
			Role: storage.RoleReadonly, Endpoint: m.S3Endpoint, Region: m.S3Region, Bucket: m.BucketMedia, PathStyle: true,
			Status: storage.StatusActive, WriteCred: &cred, ReadCred: &cred,
		})
		if err != nil {
			t.Fatal(err)
		}
		return p.ID
	}
	providerA, providerB := mkProvider("-a"), mkProvider("-b")
	mustExec(t, pool.Exec, `INSERT INTO media_replica (object_id, provider_id, status, replicated_at) VALUES ($1, $2, 'ready', now())`, objectID, providerA)

	lines := storage.NewLineService(storageRepo, nil, policy, nil)
	mkLine := func(name string, in storage.LineInput) storage.Line {
		in.Name, in.Kind, in.RegionScope, in.TokenTTL = mark+name, storage.LineVideo, storage.ScopeGlobal, 300
		if in.ProviderID == 0 {
			in.ProviderID = providerA
		}
		l, err := lines.CreateLine(ctx, in)
		if err != nil {
			t.Fatal(err)
		}
		return l
	}
	// 直连存储（无 base_url，不被健康探测）与目录前缀签名（假 CDN 域名，只校验签名）
	presignLine := mkLine("-presign", storage.LineInput{AccessMode: storage.AccessPresign, SignType: storage.SignS3Presign})
	hmacLine := mkLine("-hmac", storage.LineInput{AccessMode: storage.AccessCDN, SignType: storage.SignHMACToken,
		SignKeyRef: "dev-cdn", BaseURL: "https://cdn-e2e.invalid"})

	enc := newEncClient(t, srv.URL, cfg.APIEncKeys)
	playKeys, err := cfg.Play.Keyring()
	if err != nil {
		t.Fatal(err)
	}
	tokenFor := func(lineID int64, dev string, trial int32, exp time.Time) string {
		tok, err := playKeys.Sign(playtoken.Claims{EpisodeID: ep.ID, LineID: lineID, Device: playtoken.DeviceHash(dev), TrialSec: trial, ExpiresAt: exp})
		if err != nil {
			t.Fatal(err)
		}
		return tok
	}
	devHeader := map[string]string{"X-Device-Id": device}
	masterObject := readObject(t, ctx, media, prefix+"/master.m3u8")

	// ---------- 1. 选源 → 主清单 → 一档清单 → 密钥 → 完整解码 ----------
	urlByLine := func(trial int32) string {
		r := enc.get(t, fmt.Sprintf("/api/v1/episodes/%d/sources", ep.ID), devHeader)
		if r.status != 200 {
			t.Fatalf("选源失败：%d %s", r.status, r.body)
		}
		var body struct {
			Data struct {
				TrialSec int32 `json:"trial_sec"`
				Lines    []struct {
					LineID int64  `json:"line_id"`
					URL    string `json:"url"`
				} `json:"lines"`
			} `json:"data"`
		}
		if err := json.Unmarshal(r.body, &body); err != nil {
			t.Fatal(err)
		}
		if body.Data.TrialSec != trial {
			t.Fatalf("选源的试看秒数 %d，期望 %d", body.Data.TrialSec, trial)
		}
		for _, l := range body.Data.Lines {
			if l.LineID == presignLine.ID {
				return l.URL
			}
		}
		t.Fatalf("选源结果里没有直连存储线路 %d：%s", presignLine.ID, r.body)
		return ""
	}
	manifestURL := urlByLine(0)
	master := enc.get(t, manifestURL, devHeader)
	if master.status != 200 || master.encType != "application/vnd.apple.mpegurl" || !bytes.Equal(master.body, masterObject) {
		t.Fatalf("主清单应原样返回：%d %q\n%s", master.status, master.encType, master.body)
	}
	if master.header.Get("Cache-Control") != "no-store" {
		t.Fatalf("清单不可缓存：%q", master.header.Get("Cache-Control"))
	}
	variants := regexp.MustCompile(`(?m)^v[0-9]+\.m3u8$`).FindAllString(string(master.body), -1)
	if len(variants) == 0 {
		t.Fatalf("主清单没有档位：%s", master.body)
	}
	variantURL := resolve(t, manifestURL, variants[0])
	full := enc.get(t, variantURL, devHeader)
	if full.status != 200 {
		t.Fatalf("取一档清单失败：%d %s", full.status, full.body)
	}
	fullText := string(full.body)
	keyURIs := regexp.MustCompile(`URI="(/api/v1/hls/key/[^"]+)"`).FindAllStringSubmatch(fullText, -1)
	if len(keyURIs) == 0 || strings.Contains(fullText, "xsite-key:") {
		t.Fatalf("密钥地址未改写：\n%s", fullText)
	}
	keyRes := enc.get(t, keyURIs[0][1], devHeader)
	if keyRes.status != 200 || keyRes.encType != "application/octet-stream" || !bytes.Equal(keyRes.body, contentKey) {
		t.Fatalf("密钥不对：%d %q %x", keyRes.status, keyRes.encType, keyRes.body)
	}
	fullFrames, fullDur := decode(t, ctx, fullText, keyRes.body, prefix)
	if fullFrames < 350 || fullDur < 11.9 {
		t.Fatalf("完整播放应解出约 12 秒 360 帧：%d 帧 %.3f 秒", fullFrames, fullDur)
	}
	t.Logf("完整播放：%s 解出 %d 帧，清单时长 %.3f 秒", variants[0], fullFrames, fullDur)

	// 读穿缓存：清单原文进了 Valkey（产物不可变）
	if b, err := api.valkey.Do(ctx, api.valkey.B().Exists().Key("xsite:play:manifest:"+prefix+"/master.m3u8").Build()).AsInt64(); err != nil || b != 1 {
		t.Fatalf("清单原文应进读穿缓存：%d %v", b, err)
	}

	// ---------- 2. 试看清单只含前 N 秒 ----------
	mustExec(t, pool.Exec, `UPDATE episode SET access = 'login', trial_sec = 6 WHERE id = $1`, ep.ID)
	trialManifest := urlByLine(6)
	trial := enc.get(t, resolve(t, trialManifest, variants[0]), devHeader)
	if trial.status != 200 {
		t.Fatalf("取试看清单失败：%d %s", trial.status, trial.body)
	}
	trialText := string(trial.body)
	trialFrames, trialDur := decode(t, ctx, trialText, keyRes.body, prefix)
	if trialDur > 6 || trialDur < 4 || trialFrames > 6*30 || trialFrames < 1 || !strings.HasSuffix(trialText, "#EXT-X-ENDLIST\n") {
		t.Fatalf("试看清单只含前 6 秒：%d 帧 %.3f 秒\n%s", trialFrames, trialDur, trialText)
	}
	for _, seg := range regexp.MustCompile(`s/[0-9a-f]{32}\.ts`).FindAllString(fullText, -1)[trialSegments(trialText):] {
		if strings.Contains(trialText, seg) {
			t.Fatalf("试看之外的切片 %s 出现在试看清单里", seg)
		}
	}
	t.Logf("试看 6 秒：解出 %d 帧，清单时长 %.3f 秒", trialFrames, trialDur)
	mustExec(t, pool.Exec, `UPDATE episode SET access = 'free' WHERE id = $1`, ep.ID)

	// ---------- 3. 目录前缀签名线路：每个地址都能通过边缘校验、指向存在的对象 ----------
	signKeys, err := storage.ParseSignKeyring(cfg.Storage.SignKeys)
	if err != nil {
		t.Fatal(err)
	}
	secret, err := signKeys.Key("dev-cdn")
	if err != nil {
		t.Fatal(err)
	}
	hmacTok := tokenFor(hmacLine.ID, device, 0, time.Now().Add(5*time.Minute))
	hm := enc.get(t, "/api/v1/hls/"+hmacTok+"/"+variants[0], devHeader)
	if hm.status != 200 {
		t.Fatalf("目录前缀签名线路取清单失败：%d %s", hm.status, hm.body)
	}
	signed := regexp.MustCompile(`https://cdn-e2e\.invalid/[^"\s]+`).FindAllString(string(hm.body), -1)
	expectedSigned := strings.Count(fullText, "#EXTINF:")
	if strings.Contains(fullText, "#EXT-X-MAP:") {
		expectedSigned++ // 历史 fMP4 的 init 段也需要签名
	}
	if len(signed) != expectedSigned {
		t.Fatalf("每个切片（及历史 init 段）都应是 CDN 地址：%d 个\n%s", len(signed), hm.body)
	}
	for _, raw := range signed {
		u, err := url.Parse(raw)
		if err != nil {
			t.Fatal(err)
		}
		q := u.Query()
		exp, _ := strconv.ParseInt(q.Get(storage.SignParamExpire), 10, 64)
		objectKey := strings.TrimPrefix(u.Path, "/")
		if q.Get(storage.SignParamPrefix) != prefix+"/s/" || exp < time.Now().Add(299*time.Second).Unix() {
			t.Fatalf("签名前缀应是产物的切片目录、有效期为线路的 token_ttl：%s", raw)
		}
		if err := storage.VerifyHMACToken(secret, objectKey, q.Get(storage.SignParamPrefix), q.Get(storage.SignParamSig), exp, time.Now()); err != nil {
			t.Fatalf("边缘校验不通过：%v（%s）", err, raw)
		}
		if _, err := media.Head(ctx, objectKey); err != nil {
			t.Fatalf("签名地址指向的对象不存在：%s %v", objectKey, err)
		}
	}

	// ---------- 4. 令牌：过期 401、不是本设备 403、其他分集 403；明文 400 ----------
	presignTok := strings.TrimSuffix(strings.TrimPrefix(manifestURL, "/api/v1/hls/"), "/index.m3u8")
	expired := tokenFor(presignLine.ID, device, 0, time.Now().Add(-time.Second))
	for name, c := range map[string]struct {
		uri, dev string
		status   int
		code     string
	}{
		"过期令牌取清单 401":     {"/api/v1/hls/" + expired + "/index.m3u8", device, 401, "TOKEN_EXPIRED"},
		"过期令牌取密钥 401":     {fmt.Sprintf("/api/v1/hls/key/%d/1?t=%s", ep.ID, expired), device, 401, "TOKEN_EXPIRED"},
		"不是本设备的令牌 403":    {manifestURL, device + "-x", 403, "FORBIDDEN"},
		"篡改的令牌 403":       {"/api/v1/hls/" + presignTok[:len(presignTok)-3] + "AAA/index.m3u8", device, 403, "FORBIDDEN"},
		"用其他分集的路径取密钥 403": {fmt.Sprintf("/api/v1/hls/key/%d/1?t=%s", ep.ID+1, presignTok), device, 403, "FORBIDDEN"},
		"不存在的密钥版本 404":    {fmt.Sprintf("/api/v1/hls/key/%d/9?t=%s", ep.ID, presignTok), device, 404, "NOT_FOUND"},
		"不在主清单里的档位 404":   {"/api/v1/hls/" + presignTok + "/v9999.m3u8", device, 404, "NOT_FOUND"},
	} {
		r := enc.get(t, c.uri, map[string]string{"X-Device-Id": c.dev})
		if r.status != c.status || r.code != c.code {
			t.Errorf("%s：得到 %d %s %s", name, r.status, r.code, r.body)
		}
	}
	for _, uri := range []string{manifestURL, variantURL, keyURIs[0][1]} {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+uri, nil)
		req.Header.Set("X-Device-Id", device)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 400 || !strings.Contains(string(body), `"ENC_REQUIRED"`) {
			t.Errorf("明文请求 %s 应 400 ENC_REQUIRED，得到 %d %s", uri, resp.StatusCode, body)
		}
	}

	// ---------- 5. 复核：线路停用、产物不在线路的存储源上 503；下架 404；没有副本记录时读默认存储 ----------
	mustExec(t, pool.Exec, `UPDATE storage_line SET status = 'disabled' WHERE id = $1`, presignLine.ID)
	if r := enc.get(t, manifestURL, devHeader); r.status != 503 || r.code != "UNAVAILABLE" || !strings.Contains(string(r.body), "请切换线路") {
		t.Errorf("线路停用后应 503：%d %s", r.status, r.body)
	}
	mustExec(t, pool.Exec, `UPDATE storage_line SET status = 'active' WHERE id = $1`, presignLine.ID)

	mustExec(t, pool.Exec, `UPDATE media_replica SET provider_id = $2 WHERE object_id = $1`, objectID, providerB)
	if r := enc.get(t, manifestURL, devHeader); r.status != 503 || r.code != "UNAVAILABLE" {
		t.Errorf("产物迁到其他存储源后应 503：%d %s", r.status, r.body)
	}
	// 没有任何副本记录（S1-6f 之前的产物）：不按副本过滤，清单从默认存储读（先清掉读穿缓存）
	mustExec(t, pool.Exec, `DELETE FROM media_replica WHERE object_id = $1`, objectID)
	for _, k := range append([]string{"master.m3u8"}, variants...) {
		api.valkey.Do(ctx, api.valkey.B().Del().Key("xsite:play:manifest:"+prefix+"/"+k).Build())
	}
	if r := enc.get(t, variantURL, devHeader); r.status != 200 || !strings.Contains(string(r.body), "#EXT-X-ENDLIST") {
		t.Errorf("没有副本记录时应从默认存储读清单：%d %s", r.status, r.body)
	}
	mustExec(t, pool.Exec, `INSERT INTO media_replica (object_id, provider_id, status, replicated_at) VALUES ($1, $2, 'ready', now())`, objectID, providerA)

	mustExec(t, pool.Exec, `UPDATE drama SET status = 'offline' WHERE id = $1`, drama.ID)
	for _, uri := range []string{manifestURL, keyURIs[0][1]} {
		if r := enc.get(t, uri, devHeader); r.status != 404 || r.code != "NOT_FOUND" {
			t.Errorf("下架后 %s 应立即 404：%d %s", uri, r.status, r.body)
		}
	}
	mustExec(t, pool.Exec, `UPDATE drama SET status = 'published' WHERE id = $1`, drama.ID)

	// ---------- 6. 密钥限流：每设备每集每分钟 20 次 ----------
	limited := mark + "-limit"
	limitTok := tokenFor(presignLine.ID, limited, 0, time.Now().Add(5*time.Minute))
	keyURL := fmt.Sprintf("/api/v1/hls/key/%d/1?t=%s", ep.ID, limitTok)
	// 跨分钟时计数会归零，从一分钟的开头附近起跑会让结果不稳定，离整分钟太近时等到下一分钟
	if s := time.Now().Second(); s > 50 {
		time.Sleep(time.Duration(61-s) * time.Second)
	}
	for i := range 20 {
		if r := enc.get(t, keyURL, map[string]string{"X-Device-Id": limited}); r.status != 200 {
			t.Fatalf("第 %d 次取密钥应放行：%d %s", i+1, r.status, r.body)
		}
	}
	r := enc.get(t, keyURL, map[string]string{"X-Device-Id": limited})
	if r.status != 429 || r.code != "RATE_LIMITED" || r.header.Get("Retry-After") == "" {
		t.Fatalf("第 21 次应 429：%d %s %q", r.status, r.body, r.header.Get("Retry-After"))
	}
}

// encClient 是与浏览器 / APP 同协议的加密客户端（HPKE，internal/crypto/apienc）。
type encClient struct {
	base string
	pk   apienc.PublicKey
}

type encResponse struct {
	status  int
	header  http.Header
	encType string
	body    []byte
	code    string
}

func newEncClient(t *testing.T, base, keys string) encClient {
	t.Helper()
	kr, err := apienc.ParseKeyring(keys)
	if err != nil {
		t.Fatal(err)
	}
	return encClient{base: base, pk: kr.PublicKeys()[0]}
}

func (c encClient) get(t *testing.T, uri string, headers map[string]string) encResponse {
	t.Helper()
	creq, err := apienc.NewClientRequest(c.pk.Kid, c.pk.PublicKey, time.Now().Unix(), http.MethodGet, uri, nil)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodGet, c.base+uri, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Enc", creq.Header)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.Header.Get("Content-Type") != apienc.ContentType {
		t.Fatalf("%s：响应应为密文，得到 %d %q %s", uri, resp.StatusCode, resp.Header.Get("Content-Type"), raw)
	}
	pt, err := creq.Session.OpenResponse(raw)
	if err != nil {
		t.Fatalf("%s：解不开响应：%v", uri, err)
	}
	out := encResponse{status: resp.StatusCode, header: resp.Header, encType: resp.Header.Get("X-Enc-Type"), body: pt}
	if strings.HasPrefix(out.encType, "application/json") {
		var env struct {
			Code string `json:"code"`
		}
		_ = json.Unmarshal(pt, &env)
		out.code = env.Code
	}
	return out
}

// resolve 按播放器的方式把清单里的相对地址解析成绝对路径。
func resolve(t *testing.T, base, ref string) string {
	t.Helper()
	b, err := url.Parse(base)
	if err != nil {
		t.Fatal(err)
	}
	r, err := url.Parse(ref)
	if err != nil {
		t.Fatal(err)
	}
	return b.ResolveReference(r).String()
}

// decode 下载清单里的历史 init 段（如有）与切片（直连存储的预签名地址），把密钥地址换成本地文件，让 FFmpeg 解密并完整解码。
// 返回视频帧数与清单的总时长。
func decode(t *testing.T, ctx context.Context, playlist string, key []byte, prefix string) (int, float64) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "key.bin"), key, 0o600); err != nil {
		t.Fatal(err)
	}
	var local strings.Builder
	var dur float64
	n := 0
	for _, line := range strings.Split(strings.TrimSpace(playlist), "\n") {
		switch {
		case strings.HasPrefix(line, "#EXT-X-KEY:"):
			line = regexp.MustCompile(`URI="[^"]+"`).ReplaceAllString(line, `URI="key.bin"`)
		case strings.HasPrefix(line, "#EXT-X-MAP:"):
			u := strings.TrimSuffix(strings.TrimPrefix(line, `#EXT-X-MAP:URI="`), `"`)
			line = `#EXT-X-MAP:URI="` + download(t, ctx, u, prefix, dir, "init.mp4") + `"`
		case strings.HasPrefix(line, "#EXTINF:"):
			d, err := strconv.ParseFloat(strings.TrimSuffix(strings.TrimPrefix(line, "#EXTINF:"), ","), 64)
			if err != nil {
				t.Fatal(err)
			}
			dur += d
		case !strings.HasPrefix(line, "#"):
			n++
			u, err := url.Parse(line)
			if err != nil || path.Ext(u.Path) == "" {
				t.Fatalf("切片地址没有可识别的扩展名：%s", line)
			}
			line = download(t, ctx, line, prefix, dir, fmt.Sprintf("seg%04d%s", n, path.Ext(u.Path)))
		}
		local.WriteString(line + "\n")
	}
	if err := os.WriteFile(filepath.Join(dir, "local.m3u8"), []byte(local.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := exec.CommandContext(ctx, "ffprobe", "-v", "error", "-allowed_extensions", "ALL", "-protocol_whitelist", "file,crypto,data",
		"-count_frames", "-select_streams", "v:0", "-show_entries", "stream=nb_read_frames", "-of", "csv=p=0",
		filepath.Join(dir, "local.m3u8")).CombinedOutput()
	if err != nil {
		t.Fatalf("FFmpeg 解码失败：%v\n%s", err, out)
	}
	fields := strings.Fields(string(out))
	if len(fields) == 0 {
		t.Fatalf("读不到帧数：%q", out)
	}
	frames, err := strconv.Atoi(fields[0])
	if err != nil {
		t.Fatalf("读不到帧数：%q", out)
	}
	return frames, dur
}

// download 用清单里的地址下载一个对象（预签名地址直接可用；去掉签名后存储拒绝访问，确认签名真正起作用）。
func download(t *testing.T, ctx context.Context, raw, prefix, dir, name string) string {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil || u.Query().Get("X-Amz-Signature") == "" || !strings.Contains(u.Path, "/"+prefix+"/s/") {
		t.Fatalf("应是产物切片目录下的预签名地址：%s", raw)
	}
	get := func(target string) (int, []byte) {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, body
	}
	status, body := get(raw)
	if status != 200 || len(body) == 0 {
		t.Fatalf("预签名地址下载失败：%d %s", status, raw)
	}
	unsigned := *u
	unsigned.RawQuery = ""
	if status, _ := get(unsigned.String()); status == 200 {
		t.Fatalf("去掉签名后不应能下载：%s", unsigned.String())
	}
	if err := os.WriteFile(filepath.Join(dir, name), body, 0o600); err != nil {
		t.Fatal(err)
	}
	return path.Base(name)
}

// trialSegments 是试看清单里的切片数。
func trialSegments(playlist string) int { return strings.Count(playlist, "#EXTINF:") }

func readObject(t *testing.T, ctx context.Context, b *blob.Bucket, key string) []byte {
	t.Helper()
	body, _, err := b.Get(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	defer body.Close()
	data, err := io.ReadAll(body)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func mustExec[T any](t *testing.T, exec func(context.Context, string, ...any) (T, error), sql string, args ...any) {
	t.Helper()
	if _, err := exec(context.Background(), sql, args...); err != nil {
		t.Fatalf("%s：%v", sql, err)
	}
}
