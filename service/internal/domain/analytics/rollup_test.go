package analytics

import (
	"context"
	"testing"
	"time"
)

type fakeRollupRepo struct {
	days  []string
	froms []time.Time
	tos   []time.Time
	hours []time.Time
}

func (f *fakeRollupRepo) RollupDay(_ context.Context, day string, from, to time.Time) error {
	f.days = append(f.days, day)
	f.froms = append(f.froms, from)
	f.tos = append(f.tos, to)
	return nil
}
func (f *fakeRollupRepo) RollupHour(_ context.Context, hour time.Time) error {
	f.hours = append(f.hours, hour)
	return nil
}
func (f *fakeRollupRepo) EnsurePartitions(context.Context, time.Time, time.Time) error { return nil }
func (f *fakeRollupRepo) DropPartitionsBefore(context.Context, time.Time) (int, error) { return 0, nil }

func TestRollupDaily_Timezone(t *testing.T) {
	repo := &fakeRollupRepo{}
	r := NewRollup(repo)
	r.now = func() time.Time { return time.Date(2026, 6, 15, 10, 0, 0, 0, time.UTC) } // 北京 6/15 18:00

	if err := r.Daily(context.Background(), 2); err != nil {
		t.Fatal(err)
	}
	if len(repo.days) != 2 || repo.days[0] != "2026-06-15" || repo.days[1] != "2026-06-14" {
		t.Fatalf("应定稿北京 6/15 与 6/14，得 %v", repo.days)
	}
	// 北京 6/15 00:00 == UTC 6/14 16:00；该北京日的 UTC 范围是 [6/14 16:00, 6/15 16:00)
	wantFrom := time.Date(2026, 6, 14, 16, 0, 0, 0, time.UTC)
	wantTo := time.Date(2026, 6, 15, 16, 0, 0, 0, time.UTC)
	if !repo.froms[0].Equal(wantFrom) || !repo.tos[0].Equal(wantTo) {
		t.Errorf("北京 6/15 的 UTC 范围应为 [%v, %v)，得 [%v, %v)", wantFrom, wantTo, repo.froms[0], repo.tos[0])
	}
}

func TestRollupHourly(t *testing.T) {
	repo := &fakeRollupRepo{}
	r := NewRollup(repo)
	r.now = func() time.Time { return time.Date(2026, 6, 15, 10, 30, 0, 0, time.UTC) }

	if err := r.Hourly(context.Background(), 3); err != nil {
		t.Fatal(err)
	}
	if len(repo.hours) != 3 {
		t.Fatalf("应重算 3 个小时，得 %d", len(repo.hours))
	}
	if !repo.hours[0].Equal(time.Date(2026, 6, 15, 10, 0, 0, 0, time.UTC)) {
		t.Errorf("当前小时应为 10:00，得 %v", repo.hours[0])
	}
	if !repo.hours[2].Equal(time.Date(2026, 6, 15, 8, 0, 0, 0, time.UTC)) {
		t.Errorf("往前第 2 个小时应为 08:00，得 %v", repo.hours[2])
	}
}
