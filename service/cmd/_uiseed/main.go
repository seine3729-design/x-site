// 一次性程序：给 apps 界面验收造一套演示数据（剧、分集、维度、标签、专题、首页区块、可真实播放的加密 HLS），
// 用完执行 clean 全部删除。数据都带标记：drama.source='uiseed'、slug 前缀 uiseed-、存储源与线路名前缀 uiseed-。
//
//	./dev.sh go run ./cmd/_uiseed seed
//	./dev.sh go run ./cmd/_uiseed clean
//
// 封面与切片写到 /app/tmp/uiseed（宿主机 service/tmp/uiseed），由宿主机上的临时静态服务（端口 18090，带 CORS）提供。
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"xsite/service/internal/config"
	"xsite/service/internal/crypto/mediakey"
	"xsite/service/internal/domain/storage"
	"xsite/service/internal/netguard"
	"xsite/service/internal/store/blob"
	"xsite/service/internal/store/pg"
	"xsite/service/internal/transcode"
)

const (
	outDir   = "/app/tmp/uiseed"
	baseURL  = "http://localhost:18090"
	mark     = "uiseed"
	stateRel = "state.json"
)

type state struct {
	Dramas     []int64 `json:"dramas"`
	Tags       []int64 `json:"tags"`
	Taxonomies []int64 `json:"taxonomies"`
	Topics     []int64 `json:"topics"`
	Blocks     []int64 `json:"blocks"`
	ObjectID   int64   `json:"object_id"`
	Prefix     string  `json:"prefix"`
	SourceKey  string  `json:"source_key"`
	Poster     string  `json:"poster"`
}

var titles = []string{
	"闪婚后，豪门老公宠我上天", "重生之千金归来", "龙王赘婿", "我的总裁老公是大佬", "离婚后她惊艳全球",
	"战神归来", "替嫁新娘的逆袭", "穿越之锦绣医妃", "开局一座小饭馆", "隐世高手在都市",
	"萌宝助攻：爹地快追妈咪", "绝世神医", "她从深渊而来", "首富从退婚开始", "长安夜行录",
	"别惹那个女配", "我在古代当县令", "致命契约", "恋爱倒计时三十天", "全球冰封：我囤了百亿物资",
	"大明第一锦衣卫", "错嫁成婚", "天才萌宝神医娘亲", "逆天改命",
}

func main() {
	if len(os.Args) < 2 {
		fmt.Println("用法：seed | clean")
		os.Exit(2)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	var err error
	switch os.Args[1] {
	case "seed":
		err = seed(ctx)
	case "clean":
		err = clean(ctx)
	default:
		err = fmt.Errorf("未知命令 %s", os.Args[1])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "失败：", err)
		os.Exit(1)
	}
}

func open(ctx context.Context) (*config.Config, *pgxpool.Pool, *blob.Bucket, *blob.Bucket, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, nil, nil, nil, err
	}
	pool, err := pg.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, nil, nil, nil, err
	}
	m := cfg.Media
	client, err := blob.New(blob.Config{Endpoint: m.S3Endpoint, Region: m.S3Region, AccessKey: m.S3AccessKey, SecretKey: m.S3SecretKey, PathStyle: m.S3PathStyle})
	if err != nil {
		return nil, nil, nil, nil, err
	}
	return &cfg, pool, client.Bucket(m.BucketMedia), client.Bucket(m.BucketAssets), nil
}

func ffmpeg(ctx context.Context, args ...string) error {
	out, err := exec.CommandContext(ctx, "ffmpeg", append([]string{"-hide_banner", "-v", "error", "-y"}, args...)...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("ffmpeg %v：%w\n%s", args, err, out)
	}
	return nil
}

func seed(ctx context.Context) error {
	if _, err := os.Stat(filepath.Join(outDir, stateRel)); err == nil {
		return errors.New("已经造过数据（state.json 存在），先执行 clean")
	}
	cfg, pool, media, images, err := open(ctx)
	if err != nil {
		return err
	}
	defer pool.Close()
	st := &state{}
	save := func() error {
		b, _ := json.MarshalIndent(st, "", "  ")
		return os.WriteFile(filepath.Join(outDir, stateRel), b, 0o644)
	}
	if err := os.MkdirAll(filepath.Join(outDir, "img"), 0o755); err != nil {
		return err
	}
	r := rand.New(rand.NewPCG(42, 7))

	// —— 封面：一半的剧有竖图 + 横图（渐变 + 暗角），另一半留空，验证占位图 ——
	palettes := [][2]string{{"0xe11d48", "0x1e1b4b"}, {"0xf59e0b", "0x7c2d12"}, {"0x06b6d4", "0x1e3a8a"}, {"0x8b5cf6", "0x0f172a"},
		{"0x10b981", "0x064e3b"}, {"0xec4899", "0x4c0519"}, {"0x3b82f6", "0x172554"}, {"0xf97316", "0x431407"}}
	for i := range titles {
		if i%2 == 1 {
			continue
		}
		p := palettes[i%len(palettes)]
		for _, v := range []struct{ name, size string }{{fmt.Sprintf("p%02d.jpg", i), "600x800"}, {fmt.Sprintf("l%02d.jpg", i), "1280x720"}} {
			src := fmt.Sprintf("gradients=s=%s:c0=%s:c1=%s:x0=0:y0=0:x1=%s:y1=%s:nb_colors=2:seed=%d", v.size, p[0], p[1],
				strings.Split(v.size, "x")[0], strings.Split(v.size, "x")[1], i)
			if err := ffmpeg(ctx, "-f", "lavfi", "-i", src, "-vf", "vignette=PI/4,noise=alls=12:allf=t", "-frames:v", "1", "-q:v", "3",
				filepath.Join(outDir, "img", v.name)); err != nil {
				return err
			}
		}
	}

	// —— 一段 30 秒竖屏样例视频 → 加密 MPEG-TS HLS（全部演示分集共用这一份产物） ——
	work, err := os.MkdirTemp("", "uiseed")
	if err != nil {
		return err
	}
	defer os.RemoveAll(work)
	srcFile := filepath.Join(work, "source.mp4")
	if err := ffmpeg(ctx, "-f", "lavfi", "-i", "testsrc2=size=540x960:rate=30", "-f", "lavfi", "-i", "sine=frequency=330:sample_rate=48000",
		"-t", "30", "-ac", "2", "-c:v", "libx264", "-preset", "ultrafast", "-c:a", "aac", srcFile); err != nil {
		return err
	}
	jobID := fmt.Sprintf("uiseed%d", time.Now().UnixNano())
	st.SourceKey = "uiseed/" + jobID + "/source.mp4"
	if err := media.PutFile(ctx, st.SourceKey, srcFile, blob.PutOptions{ContentType: "video/mp4"}); err != nil {
		return err
	}

	// 剧与分集先建好：分集编号要用来包装内容密钥
	var dramaIDs []int64
	now := time.Now()
	for i, title := range titles {
		cover, landscape := "", ""
		if i%2 == 0 {
			cover = fmt.Sprintf("%s/img/p%02d.jpg", baseURL, i)
			landscape = fmt.Sprintf("%s/img/l%02d.jpg", baseURL, i)
		}
		badge := []string{"", "独家", "热播", "", "新剧", "", "爆款", ""}[i%8]
		var score any
		if i%5 != 3 {
			score = float64(80+r.IntN(19)) / 10
		}
		var id int64
		err := pool.QueryRow(ctx, `INSERT INTO drama (title, slug, summary, cover_portrait, cover_landscape, badge, score, year, status, publish_at,
				is_finished, sort_weight, view_base, hot_score, source)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 'published', $9, $10, $11, $12, $13, $14) RETURNING id`,
			title, fmt.Sprintf("uiseed-%d", i), summaryFor(title), cover, landscape, badge, score, 2024+i%3,
			now.Add(-time.Duration(i*13)*time.Hour), i%3 != 0, 100-i, int64(5000+r.IntN(9_000_000)), int64(1000-i*30), mark).Scan(&id)
		if err != nil {
			return fmt.Errorf("建剧：%w", err)
		}
		dramaIDs = append(dramaIDs, id)
		st.Dramas = append(st.Dramas, id)
	}
	if err := save(); err != nil {
		return err
	}

	// 转封装（借第一部剧第一集的编号生成产物目录）
	type epRow struct{ id, drama int64 }
	var episodes []epRow
	for di, dramaID := range dramaIDs {
		count := 8 + (di*7)%30
		for n := 1; n <= count; n++ {
			access, trial := "free", 0
			switch {
			case n > 10 && di%3 == 0:
				access, trial = "vip", 8
			case n > 6 && di%4 == 1:
				access = "login"
			}
			var id int64
			if err := pool.QueryRow(ctx, `INSERT INTO episode (drama_id, number, title, duration_sec, access, trial_sec) VALUES ($1, $2, '', 30, $3, $4) RETURNING id`,
				dramaID, n, access, trial).Scan(&id); err != nil {
				return fmt.Errorf("建分集：%w", err)
			}
			episodes = append(episodes, epRow{id, dramaID})
		}
	}
	first := episodes[0].id
	prefix := transcode.OutputPrefix(first, jobID)
	st.Prefix = prefix
	pipeline := &transcode.Pipeline{FFmpeg: "ffmpeg", FFprobe: "ffprobe", Preset: "ultrafast", WorkDir: work, Source: media, Media: media, Images: images}
	ws, err := pipeline.Fetch(ctx, jobID, st.SourceKey)
	if err != nil {
		return err
	}
	contentKey := mediakey.NewContentKey()
	res, err := pipeline.Package(ctx, ws, transcode.Request{JobID: jobID, EpisodeID: first, KeyVer: 1, Key: contentKey}, nil)
	_ = ws.Close()
	if err != nil {
		return err
	}
	st.Poster = res.Poster
	if err := pool.QueryRow(ctx, `INSERT INTO media_object (kind, key, size_bytes, sha256, episode_id) VALUES ('hls_dir', $1, $2, $3, $4) RETURNING id`,
		prefix, res.SizeBytes, res.MasterSHA256, first).Scan(&st.ObjectID); err != nil {
		return err
	}
	if err := save(); err != nil {
		return err
	}
	kek, err := mediakey.ParseKeyring(cfg.Media.KEKs)
	if err != nil {
		return err
	}
	// 每一集都包装同一把内容密钥（密钥接口按分集取），指向同一份产物
	for _, ep := range episodes {
		wrapped, err := kek.Wrap(ep.id, 1, contentKey)
		if err != nil {
			return err
		}
		if _, err := pool.Exec(ctx, `INSERT INTO media_key (episode_id, key_ver, wrapped_key) VALUES ($1, 1, $2)`, ep.id, wrapped); err != nil {
			return err
		}
	}
	if _, err := pool.Exec(ctx, `UPDATE episode SET media_status = 'ready', video_object_id = $1, key_ver = 1 WHERE drama_id = ANY($2)`, st.ObjectID, dramaIDs); err != nil {
		return err
	}
	if _, err := pool.Exec(ctx, `UPDATE drama d SET latest_episode = (SELECT max(number) FROM episode e WHERE e.drama_id = d.id) WHERE d.id = ANY($1)`, dramaIDs); err != nil {
		return err
	}

	// 产物拷到本地目录，由宿主机静态服务当作「CDN」提供切片（SeaweedFS 没有匿名读）
	objects, err := media.List(ctx, prefix+"/")
	if err != nil {
		return err
	}
	for _, obj := range objects {
		k := obj.Key
		rc, _, err := media.Get(ctx, k)
		if err != nil {
			return err
		}
		dst := filepath.Join(outDir, "media", k)
		_ = os.MkdirAll(filepath.Dir(dst), 0o755)
		f, err := os.Create(dst)
		if err != nil {
			rc.Close()
			return err
		}
		_, err = io.Copy(f, rc)
		rc.Close()
		f.Close()
		if err != nil {
			return err
		}
	}

	// 存储源（只读）+ 副本 + 线路（不签名，访问域名指向宿主机静态服务）
	storageKeys, err := cfg.Storage.Keyring()
	if err != nil {
		return err
	}
	repo := pg.NewStorageRepo(pool)
	policy := storage.Policy{Egress: netguard.Policy{AllowPrivate: true}}
	m := cfg.Media
	cred := storage.Credential{AccessKeyID: m.S3AccessKey, SecretAccessKey: m.S3SecretKey}
	provider, err := storage.NewService(repo, storageKeys, nil, policy).Create(ctx, storage.Input{
		Name: mark + "-video", Type: storage.TypeCustom, Subtype: storage.SubtypeS3Compatible, Purpose: storage.PurposeVideo,
		Role: storage.RoleReadonly, Endpoint: m.S3Endpoint, Region: m.S3Region, Bucket: m.BucketMedia, PathStyle: true,
		Status: storage.StatusActive, WriteCred: &cred, ReadCred: &cred,
	})
	if err != nil {
		return fmt.Errorf("建存储源：%w", err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO media_replica (object_id, provider_id, status, replicated_at) VALUES ($1, $2, 'ready', now())`, st.ObjectID, provider.ID); err != nil {
		return err
	}
	if _, err := storage.NewLineService(repo, nil, policy, nil).CreateLine(ctx, storage.LineInput{
		Name: mark + "-本地演示", Kind: storage.LineVideo, RegionScope: storage.ScopeGlobal, ProviderID: provider.ID,
		AccessMode: storage.AccessCDN, SignType: storage.SignNone, BaseURL: baseURL + "/media", TokenTTL: 600,
	}); err != nil {
		return fmt.Errorf("建线路：%w", err)
	}

	// 维度（类型是频道）、标签、专题、首页区块
	taxonomy := func(name, slug string, multi bool, terms [][2]string, channel bool) ([]int64, error) {
		var tid int64
		if err := pool.QueryRow(ctx, `INSERT INTO taxonomy (name, slug, multi_select) VALUES ($1, $2, $3) RETURNING id`, name, slug, multi).Scan(&tid); err != nil {
			return nil, err
		}
		st.Taxonomies = append(st.Taxonomies, tid)
		var ids []int64
		for i, t := range terms {
			var id int64
			if err := pool.QueryRow(ctx, `INSERT INTO taxonomy_term (taxonomy_id, name, slug, is_channel, code, sort) VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`,
				tid, t[0], t[1], channel, t[1], i).Scan(&id); err != nil {
				return nil, err
			}
			ids = append(ids, id)
		}
		return ids, nil
	}
	genres, err := taxonomy("类型", "uiseed-genre", true, [][2]string{{"都市", "uiseed-urban"}, {"甜宠", "uiseed-sweet"}, {"逆袭", "uiseed-rise"}, {"古装", "uiseed-costume"}, {"悬疑", "uiseed-mystery"}, {"穿越", "uiseed-time"}}, true)
	if err != nil {
		return err
	}
	regions, err := taxonomy("地区", "uiseed-region", false, [][2]string{{"国产", "uiseed-cn"}, {"韩剧", "uiseed-kr"}, {"欧美", "uiseed-west"}}, false)
	if err != nil {
		return err
	}
	var tagIDs []int64
	for _, t := range [][2]string{{"霸总", "uiseed-ceo"}, {"重生", "uiseed-reborn"}, {"复仇", "uiseed-revenge"}, {"萌宝", "uiseed-kids"}, {"战神", "uiseed-war"}} {
		var id int64
		if err := pool.QueryRow(ctx, `INSERT INTO tag (name, slug) VALUES ($1, $2) RETURNING id`, t[0], t[1]).Scan(&id); err != nil {
			return err
		}
		tagIDs = append(tagIDs, id)
		st.Tags = append(st.Tags, id)
	}
	for i, id := range dramaIDs {
		for _, term := range []int64{genres[i%len(genres)], genres[(i+2)%len(genres)], regions[i%3%len(regions)]} {
			if _, err := pool.Exec(ctx, `INSERT INTO drama_term (drama_id, term_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, id, term); err != nil {
				return err
			}
		}
		if _, err := pool.Exec(ctx, `INSERT INTO drama_tag (drama_id, tag_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`, id, tagIDs[i%len(tagIDs)]); err != nil {
			return err
		}
	}
	faq, _ := json.Marshal([]map[string]string{{"question": "这个专题会更新吗？", "answer": "会，每周根据热度调整收录的剧集。"}, {"question": "专题里的剧都免费吗？", "answer": "大部分剧集前几集免费，部分会员剧可试看。"}})
	var topicID int64
	if err := pool.QueryRow(ctx, `INSERT INTO topic (title, slug, summary, status, source_type, faq_json) VALUES ('年度爆款逆袭剧', 'uiseed-topic', '看主角一路逆风翻盘，爽感拉满的高分短剧合集。', 'published', 'manual', $1) RETURNING id`, faq).Scan(&topicID); err != nil {
		return err
	}
	st.Topics = append(st.Topics, topicID)
	var topic2 int64
	if err := pool.QueryRow(ctx, `INSERT INTO topic (title, slug, summary, status, source_type) VALUES ('甜宠恋爱季', 'uiseed-topic-2', '心动不止一次，甜到上头的恋爱短剧。', 'published', 'manual') RETURNING id`).Scan(&topic2); err != nil {
		return err
	}
	st.Topics = append(st.Topics, topic2)
	for i, id := range dramaIDs[:10] {
		if _, err := pool.Exec(ctx, `INSERT INTO topic_drama (topic_id, drama_id, sort) VALUES ($1, $2, $3), ($4, $2, $3)`, topicID, id, i, topic2); err != nil {
			return err
		}
	}
	blocks := []struct {
		typ, title, subtitle, source string
		js                           any
		limit, cols                  int
	}{
		{"carousel", "", "", "manual", map[string]any{"drama_ids": dramaIDs[:6]}, 6, 6},
		{"rank", "热播榜", "实时更新的热门短剧", "auto", map[string]any{"rank_board": "drama_week"}, 10, 6},
		{"h_scroll", "猜你喜欢", "", "auto", map[string]any{"sort": "hot"}, 18, 6},
		{"feature", "编辑精选", "", "manual", map[string]any{"drama_ids": dramaIDs[6:10]}, 4, 6},
		{"topic_group", "热门专题", "", "auto", map[string]any{"topic_ids": []int64{topicID, topic2}}, 4, 6},
		{"poster_grid", "最新上架", "每天都有新剧", "auto", map[string]any{"sort": "latest"}, 18, 6},
		{"tag_cloud", "热门标签", "", "auto", map[string]any{"tag_ids": tagIDs}, 10, 6},
	}
	for i, b := range blocks {
		js, _ := json.Marshal(b.js)
		var id int64
		if err := pool.QueryRow(ctx, `INSERT INTO block (page, type, title, subtitle, item_limit, source_type, source_json, cols_desktop, sort) VALUES ('home', $1, $2, $3, $4, $5, $6, $7, $8) RETURNING id`,
			b.typ, b.title, b.subtitle, b.limit, b.source, js, b.cols, i).Scan(&id); err != nil {
			return fmt.Errorf("建区块 %s：%w", b.typ, err)
		}
		st.Blocks = append(st.Blocks, id)
	}
	if err := save(); err != nil {
		return err
	}
	fmt.Printf("已造数：剧 %d 部、分集 %d 集、区块 %d 个、产物 %s（对象 %d）\n", len(dramaIDs), len(episodes), len(st.Blocks), prefix, st.ObjectID)
	fmt.Printf("第一部剧 /d/%d，播放 /play/%d/1\n", dramaIDs[0], dramaIDs[0])
	return nil
}

func summaryFor(title string) string {
	return "《" + title + "》讲述了一段跌宕起伏的故事：误会、背叛与救赎交织，主角在绝境中一步步翻盘，每一集都有意想不到的反转。"
}

func clean(ctx context.Context) error {
	cfg, pool, media, images, err := open(ctx)
	if err != nil {
		return err
	}
	_ = cfg
	defer pool.Close()
	b, err := os.ReadFile(filepath.Join(outDir, stateRel))
	if err != nil {
		return err
	}
	var st state
	if err := json.Unmarshal(b, &st); err != nil {
		return err
	}
	run := func(sql string, args ...any) error {
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			return fmt.Errorf("%s：%w", sql, err)
		}
		return nil
	}
	steps := []func() error{
		func() error { return run(`DELETE FROM block WHERE id = ANY($1)`, st.Blocks) },
		func() error { return run(`DELETE FROM storage_line WHERE name LIKE $1`, mark+"-%") },
		func() error { return run(`DELETE FROM media_replica WHERE object_id = $1`, st.ObjectID) },
		func() error { return run(`DELETE FROM storage_provider WHERE name LIKE $1`, mark+"-%") },
		func() error { return run(`DELETE FROM topic WHERE id = ANY($1)`, st.Topics) },
		func() error { return run(`DELETE FROM drama WHERE id = ANY($1) AND source = $2`, st.Dramas, mark) },
		func() error { return run(`DELETE FROM media_object WHERE id = $1`, st.ObjectID) },
		func() error { return run(`DELETE FROM tag WHERE id = ANY($1)`, st.Tags) },
		func() error { return run(`DELETE FROM taxonomy WHERE id = ANY($1)`, st.Taxonomies) },
	}
	for _, step := range steps {
		if err := step(); err != nil {
			return err
		}
	}
	if st.Prefix != "" {
		if _, err := media.DeletePrefix(ctx, st.Prefix+"/"); err != nil {
			return err
		}
	}
	if st.SourceKey != "" {
		_ = media.Delete(ctx, st.SourceKey)
	}
	if st.Poster != "" {
		_ = images.Delete(ctx, st.Poster)
	}
	var left int64
	_ = pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM drama WHERE source = $1) + (SELECT count(*) FROM tag WHERE slug LIKE 'uiseed-%') + (SELECT count(*) FROM taxonomy WHERE slug LIKE 'uiseed-%') + (SELECT count(*) FROM topic WHERE slug LIKE 'uiseed-%') + (SELECT count(*) FROM storage_provider WHERE name LIKE 'uiseed-%')`, mark).Scan(&left)
	if left != 0 {
		return fmt.Errorf("清理后仍有 %d 条带标记的数据", left)
	}
	if err := os.RemoveAll(outDir); err != nil {
		return err
	}
	fmt.Println("已清理全部演示数据（库内带标记的记录为 0，对象存储产物与本地目录已删除）")
	return nil
}
