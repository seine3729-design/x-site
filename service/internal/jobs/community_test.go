package jobs

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
)

type fakeCommunityNotifier struct {
	userID  int64
	typ     string
	payload []byte
	err     error
}

func (f *fakeCommunityNotifier) InsertNotification(_ context.Context, userID int64, typ string, payload []byte) error {
	f.userID, f.typ, f.payload = userID, typ, payload
	return f.err
}

func TestCommunityNotifyWorker(t *testing.T) {
	f := &fakeCommunityNotifier{}
	w := &CommunityNotifyWorker{Notifier: f}
	job := &river.Job[CommunityNotifyArgs]{
		JobRow: &rivertype.JobRow{ID: 1},
		Args:   CommunityNotifyArgs{UserID: 8, Type: "reply", CommentID: 42, EpisodeID: 5},
	}
	if err := w.Work(context.Background(), job); err != nil {
		t.Fatal(err)
	}
	if f.userID != 8 || f.typ != "reply" {
		t.Fatalf("应把收件人与类型交给落库：user=%d type=%q", f.userID, f.typ)
	}
	var p map[string]int64
	if err := json.Unmarshal(f.payload, &p); err != nil || p["comment_id"] != 42 || p["episode_id"] != 5 {
		t.Fatalf("payload 应含 comment_id / episode_id：%s %v", f.payload, err)
	}
	if CommunityNotifyArgs.Kind(CommunityNotifyArgs{}) != "community_notify" {
		t.Fatal("任务类型名")
	}
}

func TestConfigureCommunity(t *testing.T) {
	cfg := Config()
	ConfigureCommunity(cfg, nil) // nil 不注册，不应 panic
	ConfigureCommunity(cfg, &fakeCommunityNotifier{})
}
