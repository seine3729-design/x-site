package jobs

import (
	"context"
	"errors"
	"testing"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
)

type fakeHistoryFlusher struct {
	tokens []string
	n      int
	err    error
}

func (f *fakeHistoryFlusher) FlushHistory(_ context.Context, token string) (int, error) {
	f.tokens = append(f.tokens, token)
	return f.n, f.err
}

func TestHistoryFlushWorker(t *testing.T) {
	f := &fakeHistoryFlusher{n: 5}
	w := &HistoryFlushWorker{Flusher: f}
	job := &river.Job[HistoryFlushArgs]{JobRow: &rivertype.JobRow{ID: 9}}
	if err := w.Work(context.Background(), job); err != nil || len(f.tokens) != 1 || f.tokens[0] != "9" {
		t.Fatalf("用任务编号作落库标识：%v %v", f.tokens, err)
	}
	// 重试时 River 用同一个任务编号，落库失败的那一份能被同一个任务再写一次
	f.err = errors.New("db down")
	if err := w.Work(context.Background(), job); err == nil {
		t.Fatal("落库失败应返回错误，由 River 重试")
	}
	if HistoryFlushArgs.Kind(HistoryFlushArgs{}) != "history_flush" {
		t.Fatal("任务类型名")
	}
}

func TestConfigureMe(t *testing.T) {
	cfg := Config()
	before := len(cfg.PeriodicJobs)
	ConfigureMe(cfg, nil) // nil 不注册
	if len(cfg.PeriodicJobs) != before {
		t.Fatalf("flusher 为 nil 不应注册：%d → %d", before, len(cfg.PeriodicJobs))
	}
	ConfigureMe(cfg, &fakeHistoryFlusher{})
	if len(cfg.PeriodicJobs) != before+1 {
		t.Fatalf("应加一条每分钟的周期任务：%d → %d", before, len(cfg.PeriodicJobs))
	}
}
