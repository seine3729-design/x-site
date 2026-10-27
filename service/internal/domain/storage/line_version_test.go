package storage

import (
	"context"
	"testing"

	"xsite/service/internal/errs"
)

// 线路域名池随站点配置下发（docs/04 F），所以线路与地区规则的每次改动都要令站点配置版本 +1，
// 否则后台换了图片 / 视频线路，前台与 CDN 仍按旧 ETag 用旧域名：封面与视频会一起挂。

type lineVersionRepo struct {
	Repo
	provider Row
	line     Line
	created  bool
	updated  bool
	deleted  bool
	rules    bool
}

func (r *lineVersionRepo) Get(_ context.Context, id int64) (Row, error) {
	if id != r.provider.ID {
		return Row{}, errs.ErrNotFound
	}
	return r.provider, nil
}

func (r *lineVersionRepo) GetLine(_ context.Context, id int64) (Line, error) {
	if id != r.line.ID {
		return Line{}, errs.ErrNotFound
	}
	return r.line, nil
}

func (r *lineVersionRepo) ListRulesByLine(context.Context, int64) ([]Rule, error) { return nil, nil }

func (r *lineVersionRepo) CreateLine(_ context.Context, in LineInput) (Line, error) {
	r.created = true
	return Line{ID: 7, ProviderID: in.ProviderID, Kind: in.Kind, BaseURL: in.BaseURL}, nil
}

func (r *lineVersionRepo) UpdateLine(_ context.Context, id int64, in LineInput) (Line, error) {
	r.updated = true
	return Line{ID: id, ProviderID: r.line.ProviderID, Kind: in.Kind, BaseURL: in.BaseURL}, nil
}

func (r *lineVersionRepo) DeleteLine(_ context.Context, id int64) (Line, error) {
	r.deleted = true
	return Line{ID: id}, nil
}

func (r *lineVersionRepo) ReplaceLineRules(context.Context, int64, []RuleInput) ([]Rule, error) {
	r.rules = true
	return []Rule{}, nil
}

type countingBumper struct {
	calls int
	err   error
}

func (b *countingBumper) BumpVersion(context.Context) (int64, error) {
	b.calls++
	return int64(b.calls), b.err
}

func newLineVersionFixture() (*LineService, *lineVersionRepo, *countingBumper) {
	repo := &lineVersionRepo{
		provider: Row{ID: 3, Name: "R2", Purpose: PurposeBoth, Status: StatusActive},
		line:     Line{ID: 5, ProviderID: 3, Kind: LineImage, BaseURL: "https://old.example", Status: LineActive},
	}
	bumper := &countingBumper{}
	return NewLineService(repo, nil, Policy{}, nil, bumper), repo, bumper
}

func imageLineInput(base string) LineInput {
	return LineInput{
		ProviderID: 3, Kind: LineImage, Name: "图片线路", BaseURL: base,
		AccessMode: AccessCDN, SignType: SignNone, RegionScope: ScopeGlobal,
	}
}

func TestLineService_BumpsSiteVersionOnWrite(t *testing.T) {
	ctx := context.Background()

	t.Run("新建线路", func(t *testing.T) {
		svc, repo, bumper := newLineVersionFixture()
		if _, err := svc.CreateLine(ctx, imageLineInput("https://new.example")); err != nil {
			t.Fatal(err)
		}
		if !repo.created || bumper.calls != 1 {
			t.Fatalf("created=%v bump=%d，期望 true/1", repo.created, bumper.calls)
		}
	})

	t.Run("改线路（换域名正是要让前台立刻跟上的场景）", func(t *testing.T) {
		svc, repo, bumper := newLineVersionFixture()
		if _, _, err := svc.UpdateLine(ctx, 5, imageLineInput("https://new.example")); err != nil {
			t.Fatal(err)
		}
		if !repo.updated || bumper.calls != 1 {
			t.Fatalf("updated=%v bump=%d，期望 true/1", repo.updated, bumper.calls)
		}
	})

	t.Run("删线路", func(t *testing.T) {
		svc, repo, bumper := newLineVersionFixture()
		if _, err := svc.DeleteLine(ctx, 5); err != nil {
			t.Fatal(err)
		}
		if !repo.deleted || bumper.calls != 1 {
			t.Fatalf("deleted=%v bump=%d，期望 true/1", repo.deleted, bumper.calls)
		}
	})

	t.Run("整体替换地区规则", func(t *testing.T) {
		svc, repo, bumper := newLineVersionFixture()
		if _, err := svc.SetRules(ctx, 5, []RuleInput{{Country: "CN", Effect: EffectInclude}}); err != nil {
			t.Fatal(err)
		}
		if !repo.rules || bumper.calls != 1 {
			t.Fatalf("rules=%v bump=%d，期望 true/1", repo.rules, bumper.calls)
		}
	})
}

// 写失败时不该刷版本：线路没变，白白作废全站配置的 ETag 与 CDN 缓存。
func TestLineService_NoBumpWhenWriteRejected(t *testing.T) {
	svc, repo, bumper := newLineVersionFixture()
	in := imageLineInput("") // access_mode=cdn 必须填访问域名
	if _, err := svc.CreateLine(context.Background(), in); err == nil {
		t.Fatal("期望校验失败")
	}
	if repo.created || bumper.calls != 0 {
		t.Fatalf("created=%v bump=%d，期望 false/0", repo.created, bumper.calls)
	}
}

// 刷版本失败不回滚线路：线路本身已经写进库了，前台最多等一个缓存 TTL。
func TestLineService_BumpFailureDoesNotFailWrite(t *testing.T) {
	repo := &lineVersionRepo{
		provider: Row{ID: 3, Name: "R2", Purpose: PurposeBoth, Status: StatusActive},
		line:     Line{ID: 5, ProviderID: 3, Kind: LineImage, BaseURL: "https://old.example"},
	}
	bumper := &countingBumper{err: errs.ErrInternal}
	svc := NewLineService(repo, nil, Policy{}, nil, bumper)

	line, err := svc.CreateLine(context.Background(), imageLineInput("https://new.example"))
	if err != nil {
		t.Fatalf("刷版本失败不应让新建失败：%v", err)
	}
	if line.ID == 0 || !repo.created {
		t.Fatal("线路应已写入")
	}
}

// 未注入 bumper（一次性脚本、测试）时不应 panic。
func TestLineService_NilBumper(t *testing.T) {
	repo := &lineVersionRepo{provider: Row{ID: 3, Purpose: PurposeBoth, Status: StatusActive}}
	svc := NewLineService(repo, nil, Policy{}, nil, nil)
	if _, err := svc.CreateLine(context.Background(), imageLineInput("https://new.example")); err != nil {
		t.Fatal(err)
	}
}
