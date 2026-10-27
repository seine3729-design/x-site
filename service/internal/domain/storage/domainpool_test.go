package storage

import (
	"context"
	"errors"
	"slices"
	"testing"
)

// poolRepo 只实现域名池需要的 ListLines。
type poolRepo struct {
	lines []Line
	err   error
}

func (r *poolRepo) ListLines(_ context.Context, q LineQuery) ([]Line, error) {
	if r.err != nil {
		return nil, r.err
	}
	// 与 pg 实现一致：按 provider_id、priority 倒序、id 排序
	out := make([]Line, 0, len(r.lines))
	for _, l := range r.lines {
		if q.Kind != "" && l.Kind != q.Kind {
			continue
		}
		if q.Status != "" && l.Status != q.Status {
			continue
		}
		out = append(out, l)
	}
	slices.SortStableFunc(out, func(a, b Line) int {
		switch {
		case a.ProviderID != b.ProviderID:
			return int(a.ProviderID - b.ProviderID)
		case a.Priority != b.Priority:
			return int(b.Priority - a.Priority)
		default:
			return int(a.ID - b.ID)
		}
	})
	return out, nil
}

func poolLine(id, provider int64, kind LineKind, base string, priority int32, status LineStatus) Line {
	return Line{ID: id, ProviderID: provider, Kind: kind, BaseURL: base, Priority: priority, Status: status}
}

// 域名池是封面能否显示的前提：图片前缀为空时前台拼不出相对 key 的地址，
// 页面只会画渐变占位图、不发任何请求、控制台也没有报错（docs/04 F）。
func TestDomainPool(t *testing.T) {
	cases := []struct {
		name   string
		lines  []Line
		images []string
		videos []string
	}{
		{
			name:   "没有线路时给空数组而不是 null",
			lines:  nil,
			images: []string{},
			videos: []string{},
		},
		{
			name: "图片给完整前缀（含路径），视频只给域名",
			lines: []Line{
				poolLine(1, 10, LineImage, "https://img.example/x-site", 10, LineActive),
				poolLine(2, 10, LineVideo, "https://v.example/media", 10, LineActive),
			},
			images: []string{"https://img.example/x-site"},
			videos: []string{"https://v.example"},
		},
		{
			name: "跨存储源按优先级倒序排，同优先级按编号升序",
			lines: []Line{
				poolLine(3, 20, LineImage, "https://c.example", 5, LineActive),
				poolLine(1, 30, LineImage, "https://a.example", 50, LineActive),
				poolLine(2, 10, LineImage, "https://b.example", 50, LineActive),
			},
			images: []string{"https://a.example", "https://b.example", "https://c.example"},
			videos: []string{},
		},
		{
			name: "停用的线路不下发，观察中的照常下发",
			lines: []Line{
				poolLine(1, 10, LineImage, "https://off.example", 20, LineDisabled),
				poolLine(2, 10, LineImage, "https://watch.example", 10, LineWatch),
			},
			images: []string{"https://watch.example"},
			videos: []string{},
		},
		{
			name: "直连存储（无 base_url）没有访问域名，不进池",
			lines: []Line{
				poolLine(1, 10, LineImage, "", 10, LineActive),
				poolLine(2, 10, LineVideo, "", 10, LineActive),
			},
			images: []string{},
			videos: []string{},
		},
		{
			name: "去重：末尾斜杠归一，同域名的多条视频线路只留一个 origin",
			lines: []Line{
				poolLine(1, 10, LineImage, "https://img.example/", 10, LineActive),
				poolLine(2, 20, LineImage, "https://img.example", 10, LineActive),
				poolLine(3, 10, LineVideo, "https://cdn.example/a", 10, LineActive),
				poolLine(4, 20, LineVideo, "https://cdn.example/b", 10, LineActive),
			},
			images: []string{"https://img.example"},
			videos: []string{"https://cdn.example"},
		},
		{
			name: "带端口的域名保留端口；不是 http(s) 的地址丢掉（CSP 里写坏值会让整条策略失效）",
			lines: []Line{
				poolLine(1, 10, LineVideo, "http://localhost:18090/media", 10, LineActive),
				poolLine(2, 20, LineVideo, "ftp://bad.example/x", 10, LineActive),
			},
			images: []string{},
			videos: []string{"http://localhost:18090"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := NewPoolService(&poolRepo{lines: tc.lines}).DomainPool(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(got.ImageBases, tc.images) {
				t.Errorf("图片前缀 = %v，期望 %v", got.ImageBases, tc.images)
			}
			if !slices.Equal(got.VideoOrigins, tc.videos) {
				t.Errorf("视频域名 = %v，期望 %v", got.VideoOrigins, tc.videos)
			}
		})
	}
}

// 读线路失败必须透出错误：静默下发空池会让前台一张封面都不显示，而页面上看不出任何异常。
func TestDomainPool_RepoErrorSurfaces(t *testing.T) {
	want := errors.New("连接中断")
	if _, err := NewPoolService(&poolRepo{err: want}).DomainPool(context.Background()); !errors.Is(err, want) {
		t.Fatalf("err = %v，期望 %v", err, want)
	}
}
