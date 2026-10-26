package platform

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
)

// fakeKeys 记录收到的模式，便于断言「清了什么」与「没清什么」。
type fakeKeys struct {
	purged  []string
	counted []string
	counts  map[string]int64
	err     error
}

func (f *fakeKeys) Stats(context.Context) (int64, int64, string, error) {
	return 42, 1024, "1.00K", f.err
}

func (f *fakeKeys) Count(_ context.Context, pattern string) (int64, error) {
	if f.err != nil {
		return 0, f.err
	}
	f.counted = append(f.counted, pattern)
	return f.counts[pattern], nil
}

func (f *fakeKeys) Purge(_ context.Context, pattern string) (int64, error) {
	if f.err != nil {
		return 0, f.err
	}
	f.purged = append(f.purged, pattern)
	return 1, nil
}

type fakeSite struct {
	version int64
	bumped  int
	bumpErr error
	getErr  error
}

func (f *fakeSite) Get(context.Context) (SiteConfig, error) {
	if f.getErr != nil {
		return SiteConfig{}, f.getErr
	}
	return SiteConfig{Version: f.version}, nil
}

func (f *fakeSite) BumpVersion(context.Context) (int64, error) {
	if f.bumpErr != nil {
		return 0, f.bumpErr
	}
	f.bumped++
	f.version++
	return f.version, nil
}

type fakeProducts struct {
	prefixes []string
	err      error
}

func (f *fakeProducts) DramaObjectPrefixes(context.Context, int64) ([]string, error) {
	return f.prefixes, f.err
}

func newSvc(keys *fakeKeys, site *fakeSite, products *fakeProducts) *CacheService {
	return NewCacheService(keys, site, products)
}

func TestPurge_ScopePatterns(t *testing.T) {
	cases := []struct {
		scope PurgeScope
		want  []string
	}{
		{ScopeHome, []string{"xsite:content:explore:*"}},
		{ScopeList, []string{"xsite:content:channel:*", "xsite:content:tag:*"}},
		{ScopeTopic, []string{"xsite:content:topic:*", "xsite:content:topics:*"}},
		{ScopeRank, []string{"xsite:ops:rank:*"}},
		{ScopeOps, []string{"xsite:ops:announcements", "xsite:ops:ads", "xsite:ops:hotsearch"}},
		{ScopeSiteConfig, []string{"xsite:site_config:public"}},
	}
	for _, c := range cases {
		t.Run(string(c.scope), func(t *testing.T) {
			keys := &fakeKeys{}
			svc := newSvc(keys, &fakeSite{version: 7}, &fakeProducts{})
			if _, err := svc.Purge(context.Background(), c.scope, 0); err != nil {
				t.Fatalf("清理失败: %v", err)
			}
			if !slices.Equal(keys.purged, c.want) {
				t.Errorf("清了 %v，期望 %v", keys.purged, c.want)
			}
		})
	}
}

// 有状态的键（会话、限流、待落库计数、榜单本体）在任何范围下都不能被清。
func TestPurge_NeverTouchesStatefulKeys(t *testing.T) {
	stateful := []string{
		"xsite:admin:sess:", "xsite:admin:login_fail:", "xsite:fb:sess:", "xsite:fb:login_fail:",
		"xsite:fb:intake:", "xsite:fb:intake_ip:", "xsite:line:health:", "xsite:line:q:",
		"xsite:line:report:", "xsite:play:sources:", "xsite:play:key:", "xsite:ad:", "xsite:hot:", "xsite:rank:",
	}
	for _, s := range scopes {
		keys := &fakeKeys{}
		svc := newSvc(keys, &fakeSite{}, &fakeProducts{prefixes: []string{"media/d1/e1"}})
		if _, err := svc.Purge(context.Background(), s.Scope, 1); err != nil {
			t.Fatalf("范围 %s 清理失败: %v", s.Scope, err)
		}
		for _, pattern := range keys.purged {
			for _, bad := range stateful {
				// 清 xsite:rank: 本体是禁止的，但 xsite:ops:rank:* 是下发缓存、允许。
				if strings.HasPrefix(pattern, bad) {
					t.Errorf("范围 %s 清到了有状态的键：%s（命中前缀 %s）", s.Scope, pattern, bad)
				}
			}
		}
	}
}

// 「全部」不能用 * 或 xsite:* 这种笼统模式一把梭。
func TestPurge_ScopeAllStaysWithinWhitelist(t *testing.T) {
	keys := &fakeKeys{}
	svc := newSvc(keys, &fakeSite{version: 3}, &fakeProducts{})
	res, err := svc.Purge(context.Background(), ScopeAll, 0)
	if err != nil {
		t.Fatalf("清理失败: %v", err)
	}
	for _, p := range keys.purged {
		if p == "*" || p == "xsite:*" || strings.Count(strings.SplitN(p, "*", 2)[0], ":") < 2 {
			t.Errorf("「全部」用了过于笼统的模式：%s", p)
		}
	}
	// 「全部」也要让版本 +1，否则 CDN 与浏览器上的旧配置不会失效
	if res.SiteVersion != 4 {
		t.Errorf("站点版本 = %d，期望 4", res.SiteVersion)
	}
}

func TestPurge_SiteConfigBumpsVersion(t *testing.T) {
	site := &fakeSite{version: 10}
	svc := newSvc(&fakeKeys{}, site, &fakeProducts{})
	res, err := svc.Purge(context.Background(), ScopeSiteConfig, 0)
	if err != nil {
		t.Fatalf("清理失败: %v", err)
	}
	if site.bumped != 1 {
		t.Errorf("BumpVersion 调用了 %d 次，期望 1", site.bumped)
	}
	if res.SiteVersion != 11 {
		t.Errorf("新版本 = %d，期望 11", res.SiteVersion)
	}
}

// 其它范围不该动站点版本：清一次首页就 +1 会让所有客户端白重新拉一遍配置。
func TestPurge_OtherScopesDoNotBumpVersion(t *testing.T) {
	for _, scope := range []PurgeScope{ScopeHome, ScopeList, ScopeTopic, ScopeRank, ScopeOps, ScopeDrama, ScopePlay} {
		site := &fakeSite{version: 5}
		svc := newSvc(&fakeKeys{}, site, &fakeProducts{prefixes: []string{"media/d1/e1"}})
		if _, err := svc.Purge(context.Background(), scope, 1); err != nil {
			t.Fatalf("范围 %s 清理失败: %v", scope, err)
		}
		if site.bumped != 0 {
			t.Errorf("范围 %s 动了站点版本", scope)
		}
	}
}

func TestPurge_DramaScopeNeedsID(t *testing.T) {
	for _, scope := range []PurgeScope{ScopeDrama, ScopePlay} {
		svc := newSvc(&fakeKeys{}, &fakeSite{}, &fakeProducts{})
		_, err := svc.Purge(context.Background(), scope, 0)
		if err == nil {
			t.Fatalf("范围 %s 没填编号却成功了", scope)
		}
		if !strings.Contains(err.Error(), "剧集编号") {
			t.Errorf("范围 %s 的报错没提剧集编号：%v", scope, err)
		}
	}
}

func TestPurge_DramaScopeUsesID(t *testing.T) {
	keys := &fakeKeys{}
	svc := newSvc(keys, &fakeSite{}, &fakeProducts{})
	if _, err := svc.Purge(context.Background(), ScopeDrama, 1024); err != nil {
		t.Fatalf("清理失败: %v", err)
	}
	want := []string{"xsite:content:drama:1024"}
	if !slices.Equal(keys.purged, want) {
		t.Errorf("清了 %v，期望 %v", keys.purged, want)
	}
}

func TestPurge_PlayScopeUsesProductPrefixes(t *testing.T) {
	keys := &fakeKeys{}
	products := &fakeProducts{prefixes: []string{"media/d7/e1", "media/d7/e2"}}
	svc := newSvc(keys, &fakeSite{}, products)
	if _, err := svc.Purge(context.Background(), ScopePlay, 7); err != nil {
		t.Fatalf("清理失败: %v", err)
	}
	want := []string{"xsite:play:manifest:media/d7/e1*", "xsite:play:manifest:media/d7/e2*"}
	if !slices.Equal(keys.purged, want) {
		t.Errorf("清了 %v，期望 %v", keys.purged, want)
	}
}

// 没有转码产物的剧不算错误：运维按编号清一遍是合理操作。
func TestPurge_PlayScopeNoProducts(t *testing.T) {
	keys := &fakeKeys{}
	svc := newSvc(keys, &fakeSite{}, &fakeProducts{})
	res, err := svc.Purge(context.Background(), ScopePlay, 7)
	if err != nil {
		t.Fatalf("清理失败: %v", err)
	}
	if res.Keys != 0 || len(keys.purged) != 0 {
		t.Errorf("没有产物却清了 %v", keys.purged)
	}
}

func TestPurge_UnknownScope(t *testing.T) {
	svc := newSvc(&fakeKeys{}, &fakeSite{}, &fakeProducts{})
	_, err := svc.Purge(context.Background(), "flushall", 0)
	if err == nil {
		t.Fatal("未知范围应当报错")
	}
	// 报错里要列出可选项，便于调用方改对
	if !strings.Contains(err.Error(), "site_config") {
		t.Errorf("报错没列出可选范围：%v", err)
	}
}

func TestPurge_KeyStoreErrorPropagates(t *testing.T) {
	svc := newSvc(&fakeKeys{err: errors.New("valkey 挂了")}, &fakeSite{}, &fakeProducts{})
	if _, err := svc.Purge(context.Background(), ScopeHome, 0); err == nil {
		t.Fatal("底层出错时应当报错")
	}
}

// 清键成功但 version+1 失败时要报错，不能让调用方以为站点设置已经失效了。
func TestPurge_BumpErrorPropagates(t *testing.T) {
	svc := newSvc(&fakeKeys{}, &fakeSite{bumpErr: errors.New("库挂了")}, &fakeProducts{})
	if _, err := svc.Purge(context.Background(), ScopeSiteConfig, 0); err == nil {
		t.Fatal("BumpVersion 失败时应当报错")
	}
}

func TestOverview(t *testing.T) {
	keys := &fakeKeys{counts: map[string]int64{
		"xsite:site_config:public": 1,
		"xsite:content:*":          120,
		"xsite:admin:sess:*":       3,
	}}
	svc := newSvc(keys, &fakeSite{version: 9}, &fakeProducts{})
	out, err := svc.Overview(context.Background())
	if err != nil {
		t.Fatalf("读取概览失败: %v", err)
	}
	if out.SiteVersion != 9 {
		t.Errorf("站点版本 = %d，期望 9", out.SiteVersion)
	}
	if out.TotalKeys != 42 || out.MemoryText != "1.00K" {
		t.Errorf("统计不对：keys=%d memory=%q", out.TotalKeys, out.MemoryText)
	}
	if len(out.Scopes) != len(scopes) {
		t.Errorf("范围数 = %d，期望 %d", len(out.Scopes), len(scopes))
	}

	// 概览要同时列出可清与不可清的前缀，且不可清的必须带原因
	var purgeable, stateful int
	for _, p := range out.Prefixes {
		if p.Purgeable {
			purgeable++
			continue
		}
		stateful++
		if p.Reason == "" {
			t.Errorf("不可清的前缀 %s 没写原因", p.Prefix)
		}
	}
	if purgeable == 0 || stateful == 0 {
		t.Errorf("前缀分类不对：可清 %d 个、不可清 %d 个", purgeable, stateful)
	}
	for _, p := range out.Prefixes {
		if p.Prefix == "xsite:content:*" && p.Keys != 120 {
			t.Errorf("xsite:content:* 的键数 = %d，期望 120", p.Keys)
		}
	}
}

// 每个范围都必须有标题与说明：页面直接展示这两段文字。
func TestScopes_AllDescribed(t *testing.T) {
	for _, s := range Scopes() {
		if s.Title == "" || s.Desc == "" {
			t.Errorf("范围 %s 缺标题或说明", s.Scope)
		}
	}
}
