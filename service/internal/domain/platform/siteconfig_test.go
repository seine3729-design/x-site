package platform

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

// 站点配置服务（S1-9）的单元测试：一级缓存命中与过期、版本 +1 时的失效与广播、收到广播时清本地缓存。
// 跨实例「5 秒内生效」的端到端走 siteconfig_pubsub_test.go（真实 Valkey）。

type fakeSiteRepo struct {
	cfg     SiteConfig
	gets    int
	bumpErr error
	bumped  int
}

func (r *fakeSiteRepo) GetPublicSiteConfig(context.Context) (SiteConfig, error) {
	r.gets++
	return r.cfg, nil
}

func (r *fakeSiteRepo) BumpVersion(context.Context) (int64, error) {
	if r.bumpErr != nil {
		return 0, r.bumpErr
	}
	r.bumped++
	r.cfg.Version++
	return r.cfg.Version, nil
}

// fakeCache 不做真正缓存：每次都调 load（一级缓存在服务里、在它之上），只记录 Delete 次数。
type fakeCache struct{ deleted int }

func (c *fakeCache) GetOrLoadJSON(ctx context.Context, _ string, _ time.Duration, dst any, load func(context.Context) (any, error)) error {
	v, err := load(ctx)
	if err != nil {
		return err
	}
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, dst)
}

func (c *fakeCache) Delete(context.Context, string) error { c.deleted++; return nil }

type fakePublisher struct {
	topics []string
	err    error
}

func (p *fakePublisher) Publish(_ context.Context, topic string) error {
	p.topics = append(p.topics, topic)
	return p.err
}

func newFixture() (*SiteConfigService, *fakeSiteRepo, *fakeCache, *fakePublisher) {
	repo := &fakeSiteRepo{cfg: SiteConfig{Version: 1, Site: json.RawMessage(`{"site_name":"x"}`)}}
	cache := &fakeCache{}
	pub := &fakePublisher{}
	svc := NewSiteConfigService(repo, cache, pub, nil)
	return svc, repo, cache, pub
}

func TestSiteConfig_L1CachesWithinTTL(t *testing.T) {
	svc, repo, _, _ := newFixture()
	now := time.Unix(1_700_000_000, 0)
	svc.now = func() time.Time { return now }
	ctx := context.Background()

	if _, err := svc.Get(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Get(ctx); err != nil {
		t.Fatal(err)
	}
	if repo.gets != 1 {
		t.Fatalf("一级缓存有效期内应只回源一次，实际 %d 次", repo.gets)
	}
	// 过了一级 TTL 再读，回源一次
	now = now.Add(siteConfigL1TTL + time.Millisecond)
	if _, err := svc.Get(ctx); err != nil {
		t.Fatal(err)
	}
	if repo.gets != 2 {
		t.Fatalf("一级缓存过期后应再回源，实际共 %d 次", repo.gets)
	}
}

func TestSiteConfig_ETagFromVersion(t *testing.T) {
	svc, repo, _, _ := newFixture()
	repo.cfg.Version = 7
	cfg, err := svc.Get(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ETag() != `W/"site-7"` {
		t.Fatalf("ETag = %q，期望 W/\"site-7\"", cfg.ETag())
	}
}

func TestSiteConfig_BumpVersionInvalidatesAndPublishes(t *testing.T) {
	svc, repo, cache, pub := newFixture()
	now := time.Unix(1_700_000_000, 0)
	svc.now = func() time.Time { return now }
	ctx := context.Background()

	// 先读一次，填充一级缓存
	if _, err := svc.Get(ctx); err != nil {
		t.Fatal(err)
	}
	if repo.gets != 1 {
		t.Fatalf("预热应回源一次，实际 %d", repo.gets)
	}

	v, err := svc.BumpVersion(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if v != 2 || repo.bumped != 1 {
		t.Fatalf("版本应 +1 到 2，得到 %d（bump %d 次）", v, repo.bumped)
	}
	if cache.deleted != 1 {
		t.Fatalf("版本变更应删除 Valkey 缓存一次，实际 %d", cache.deleted)
	}
	if len(pub.topics) != 1 || pub.topics[0] != SiteConfigTopic {
		t.Fatalf("应广播一次 %q，实际 %v", SiteConfigTopic, pub.topics)
	}
	// 一级缓存已被清：同一时刻再读也会回源，并拿到新版本
	got, err := svc.Get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if repo.gets != 2 || got.Version != 2 {
		t.Fatalf("版本变更后应回源拿到新版本，gets=%d version=%d", repo.gets, got.Version)
	}
}

func TestSiteConfig_BumpVersionSurvivesPublishFailure(t *testing.T) {
	svc, _, cache, pub := newFixture()
	pub.err = errors.New("valkey down")
	v, err := svc.BumpVersion(context.Background())
	if err != nil {
		t.Fatalf("广播失败不应让写入失败：%v", err)
	}
	if v != 2 || cache.deleted != 1 {
		t.Fatalf("广播失败仍应 +1 并清本地缓存，version=%d deleted=%d", v, cache.deleted)
	}
}

func TestSiteConfig_BumpVersionRepoErrorNoInvalidate(t *testing.T) {
	svc, repo, cache, pub := newFixture()
	repo.bumpErr = errors.New("db down")
	if _, err := svc.BumpVersion(context.Background()); err == nil {
		t.Fatal("repo 出错时应返回错误")
	}
	if cache.deleted != 0 || len(pub.topics) != 0 {
		t.Fatalf("版本没写成功就不该失效或广播：deleted=%d topics=%v", cache.deleted, pub.topics)
	}
}

func TestSiteConfig_OnInvalidateClearsLocal(t *testing.T) {
	svc, repo, cache, _ := newFixture()
	now := time.Unix(1_700_000_000, 0)
	svc.now = func() time.Time { return now }
	ctx := context.Background()

	if _, err := svc.Get(ctx); err != nil {
		t.Fatal(err)
	}
	svc.OnInvalidate(ctx) // 模拟收到其它实例的失效广播
	if cache.deleted != 1 {
		t.Fatalf("收到广播应删 Valkey 缓存一次，实际 %d", cache.deleted)
	}
	if _, err := svc.Get(ctx); err != nil {
		t.Fatal(err)
	}
	if repo.gets != 2 {
		t.Fatalf("本地缓存被清后应回源，实际 gets=%d", repo.gets)
	}
}

// fakeMedia 是线路域名池读取器的存根（真实实现是 domain/storage.PoolService）。
type fakeMedia struct {
	pool MediaDomains
	err  error
	gets int
}

func (m *fakeMedia) MediaDomains(context.Context) (MediaDomains, error) {
	m.gets++
	return m.pool, m.err
}

// 域名池随站点配置下发（docs/04 F）：前台用它拼封面的相对 key，并按请求生成 CSP。
func TestSiteConfig_CarriesMediaDomains(t *testing.T) {
	repo := &fakeSiteRepo{cfg: SiteConfig{Version: 1, Site: json.RawMessage(`{}`)}}
	media := &fakeMedia{pool: MediaDomains{
		ImageBases:   []string{"https://img.example/x"},
		VideoOrigins: []string{"https://v.example"},
	}}
	svc := NewSiteConfigService(repo, &fakeCache{}, &fakePublisher{}, media)

	cfg, err := svc.Get(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Media.ImageBases) != 1 || cfg.Media.ImageBases[0] != "https://img.example/x" {
		t.Fatalf("图片前缀 = %v", cfg.Media.ImageBases)
	}
	if len(cfg.Media.VideoOrigins) != 1 || cfg.Media.VideoOrigins[0] != "https://v.example" {
		t.Fatalf("视频域名 = %v", cfg.Media.VideoOrigins)
	}
	// 域名池和配置各段一起进缓存：命中一级缓存时不应再读线路表
	if _, err := svc.Get(context.Background()); err != nil {
		t.Fatal(err)
	}
	if media.gets != 1 {
		t.Fatalf("读线路表 %d 次，期望 1 次（应随配置一起缓存）", media.gets)
	}
}

// 未注入读取器时给空数组：JSON 里不能是 null，否则客户端在 .map() 上直接崩。
func TestSiteConfig_MediaDomainsEmptyArrays(t *testing.T) {
	svc, _, _, _ := newFixture()
	cfg, err := svc.Get(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Media.ImageBases == nil || cfg.Media.VideoOrigins == nil {
		t.Fatalf("域名池不能是 null：%+v", cfg.Media)
	}
	b, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"media":{"image_bases":[],"video_origins":[]}`) {
		t.Fatalf("下发的 media 段不对：%s", b)
	}
}

// 读线路表失败要透出错误，而不是静默下发空池：空池会让前台一张封面都不显示，页面上却看不出异常。
func TestSiteConfig_MediaDomainsErrorFailsRead(t *testing.T) {
	want := errors.New("连接中断")
	repo := &fakeSiteRepo{cfg: SiteConfig{Version: 1, Site: json.RawMessage(`{}`)}}
	svc := NewSiteConfigService(repo, &fakeCache{}, &fakePublisher{}, &fakeMedia{err: want})
	if _, err := svc.Get(context.Background()); !errors.Is(err, want) {
		t.Fatalf("err = %v，期望 %v", err, want)
	}
}
