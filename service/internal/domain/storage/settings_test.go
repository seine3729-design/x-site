package storage

import (
	"context"
	"strings"
	"testing"
)

// deliveryRepo 是交付自检需要的存根：存储源、线路与设置。其余 Repo 方法不会被调用。
type deliveryRepo struct {
	Repo
	rows     []Row
	lines    []Line
	settings Settings
}

func (r *deliveryRepo) List(_ context.Context, q Query) ([]Row, int64, error) {
	var out []Row
	for _, row := range r.rows {
		if q.Status != "" && row.Status != q.Status {
			continue
		}
		out = append(out, row)
	}
	// 与 pg 实现一致：按编号倒序
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, int64(len(out)), nil
}

func (r *deliveryRepo) Get(_ context.Context, id int64) (Row, error) {
	for _, row := range r.rows {
		if row.ID == id {
			return row, nil
		}
	}
	return Row{}, badRequest("存储源 %d 不存在", id)
}

func (r *deliveryRepo) GetSettings(context.Context) (Settings, error) { return r.settings, nil }

func (r *deliveryRepo) ListLines(_ context.Context, q LineQuery) ([]Line, error) {
	var out []Line
	for _, l := range r.lines {
		if q.ProviderID != 0 && l.ProviderID != q.ProviderID {
			continue
		}
		if q.Kind != "" && l.Kind != q.Kind {
			continue
		}
		if q.Status != "" && l.Status != q.Status {
			continue
		}
		out = append(out, l)
	}
	return out, nil
}

func store(id int64, name string, purpose Purpose) Row {
	return Row{ID: id, Name: name, Type: TypeS3, Role: RolePrimary, Purpose: purpose, Status: StatusActive, Bucket: "b", Region: "us-east-1"}
}

func provLine(id, providerID int64, kind LineKind, status LineStatus) Line {
	return Line{ID: id, ProviderID: providerID, Kind: kind, Name: "线路", Status: status, Priority: 10, Weight: 100}
}

// 主存储上有可用线路时自检通过；Issues 永不为 nil（后台直接 .map 渲染）。
func TestCheckDelivery_OK(t *testing.T) {
	repo := &deliveryRepo{
		rows:     []Row{store(28, "R2-测试", PurposeBoth)},
		lines:    []Line{provLine(1, 28, LineVideo, LineActive), provLine(2, 28, LineImage, LineWatch)},
		settings: Settings{PrimaryVideoProviderID: 28, PrimaryImageProviderID: 28},
	}
	got := NewSettingsService(repo).CheckDelivery(context.Background())
	if !got.OK {
		t.Fatalf("应通过自检，得到 %+v", got.Issues)
	}
	if got.Issues == nil {
		t.Error("Issues 不能是 nil，后台会直接遍历")
	}
}

// 线路全挂在别的存储源上（线上 503 的真实配置）：主存储一条线路都没有，自检要报出来。
func TestCheckDelivery_LinesOnAnotherProvider(t *testing.T) {
	repo := &deliveryRepo{
		rows:     []Row{store(28, "R2-测试", PurposeBoth), store(701, "uiseed-video", PurposeVideo)},
		lines:    []Line{provLine(370, 701, LineVideo, LineActive)},
		settings: Settings{PrimaryVideoProviderID: 28, PrimaryImageProviderID: 28},
	}
	got := NewSettingsService(repo).CheckDelivery(context.Background())
	if got.OK || len(got.Issues) != 2 {
		t.Fatalf("视频与图片都该报，得到 ok=%v %+v", got.OK, got.Issues)
	}
	video := got.Issues[0]
	if video.Kind != LineVideo || video.Code != IssueNoLineOnPrimary {
		t.Errorf("视频应报 %s，得到 %+v", IssueNoLineOnPrimary, video)
	}
	if !strings.Contains(video.Message, "R2-测试") || !strings.Contains(video.Message, "暂无可用线路") {
		t.Errorf("提示要点出存储源名与前台现象：%q", video.Message)
	}
	// 图片一条线路都没有，属于全站缺失
	if img := got.Issues[1]; img.Kind != LineImage || img.Code != IssueNoLineAtAll {
		t.Errorf("图片应报 %s，得到 %+v", IssueNoLineAtAll, img)
	}
}

// 线路在主存储上但被停用：下发不会用它，等同于没有。
func TestCheckDelivery_AllLinesDisabled(t *testing.T) {
	repo := &deliveryRepo{
		rows:     []Row{store(28, "R2-测试", PurposeBoth)},
		lines:    []Line{provLine(1, 28, LineVideo, LineDisabled), provLine(2, 28, LineImage, LineActive)},
		settings: Settings{PrimaryVideoProviderID: 28, PrimaryImageProviderID: 28},
	}
	got := NewSettingsService(repo).CheckDelivery(context.Background())
	if len(got.Issues) != 1 || got.Issues[0].Kind != LineVideo {
		t.Fatalf("只该报视频，得到 %+v", got.Issues)
	}
	if !strings.Contains(got.Issues[0].Message, "已停用") {
		t.Errorf("要说明线路是被停用的：%q", got.Issues[0].Message)
	}
}

// 连主存储都解析不出来：上传本身就会失败，报 no_primary。
func TestCheckDelivery_NoPrimary(t *testing.T) {
	repo := &deliveryRepo{rows: nil, settings: Settings{}}
	got := NewSettingsService(repo).CheckDelivery(context.Background())
	if len(got.Issues) != 2 {
		t.Fatalf("视频与图片都该报，得到 %+v", got.Issues)
	}
	for _, is := range got.Issues {
		if is.Code != IssueNoPrimary {
			t.Errorf("应报 %s，得到 %+v", IssueNoPrimary, is)
		}
	}
}

// Overview 在设置之外带上自检结果。
func TestSettingsOverview(t *testing.T) {
	repo := &deliveryRepo{
		rows:     []Row{store(28, "R2-测试", PurposeBoth)},
		lines:    []Line{provLine(1, 28, LineVideo, LineActive)},
		settings: Settings{CandidateCount: 3, DefaultTTL: 600, PrimaryVideoProviderID: 28, PrimaryImageProviderID: 28},
	}
	view, err := NewSettingsService(repo).Overview(context.Background())
	if err != nil {
		t.Fatalf("Overview: %v", err)
	}
	if view.CandidateCount != 3 || view.DefaultTTL != 600 {
		t.Errorf("设置字段应原样带出：%+v", view.Settings)
	}
	if view.Delivery.OK {
		t.Errorf("图片主存储没有图片线路，应报出来：%+v", view.Delivery)
	}
}
