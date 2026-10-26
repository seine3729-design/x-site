package me

import (
	"context"
	"encoding/json"
	"time"

	"xsite/service/internal/errs"
)

// maxHistoryBatch 是单次续播上报的最多条数（docs/03 第 6 节：支持批量）。
const maxHistoryBatch = 100

// HistoryEntry 是客户端上报的一条续播断点。
type HistoryEntry struct {
	DramaID       int64
	EpisodeID     int64
	EpisodeNumber int
	PositionSec   int
	DurationSec   int
}

// historyPayload 是存入 Valkey / 落库的续播断点（含上报时间，用于合并时保留较新的）。
type historyPayload struct {
	EpisodeID     int64 `json:"e"`
	EpisodeNumber int   `json:"n"`
	PositionSec   int   `json:"p"`
	DurationSec   int   `json:"d"`
	TS            int64 `json:"t"` // unix 秒
}

// PendingHistory 是从 Valkey 取出的一条待落库断点（store 层返回，领域层解析 payload 后落库）。
type PendingHistory struct {
	UserID  int64
	DramaID int64
	Payload string
}

// HistoryRow 是解析后要 UPSERT 的一行。
type HistoryRow struct {
	UserID        int64
	DramaID       int64
	EpisodeID     int64
	EpisodeNumber int
	PositionSec   int
	DurationSec   int
	UpdatedAt     time.Time
}

// HistoryView 是续播列表的一项（含剧卡片展示字段）。
type HistoryView struct {
	DramaID       int64     `json:"drama_id"`
	Title         string    `json:"title"`
	Cover         string    `json:"cover"`
	EpisodeID     int64     `json:"episode_id"`
	EpisodeNumber int       `json:"episode_number"`
	PositionSec   int       `json:"position_sec"`
	DurationSec   int       `json:"duration_sec"`
	LatestEpisode int       `json:"latest_episode"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// HistoryStore 是观看历史的 Valkey 合并写缓冲（实现见 internal/store/cache）。
type HistoryStore interface {
	Report(ctx context.Context, uid, dramaID int64, payload string) error
	TakePending(ctx context.Context, token string) ([]PendingHistory, error)
	DonePending(ctx context.Context, token string) error
	Clear(ctx context.Context, uid int64, dramaIDs []int64) error
	ClearAll(ctx context.Context, uid int64) error
}

// ReportHistory 把一批续播断点写入 Valkey 合并缓冲（worker 每分钟批量落库）。
// 开了「暂停记录」的用户静默忽略（既尊重开关，也防客户端绕过）。无效条目跳过、不报错。
func (s *Service) ReportHistory(ctx context.Context, uid int64, entries []HistoryEntry) error {
	if len(entries) == 0 {
		return nil
	}
	if len(entries) > maxHistoryBatch {
		return errs.ErrBadRequest.WithMessage("单次上报的续播记录过多")
	}
	paused, err := s.repo.HistoryPaused(ctx, uid)
	if err != nil {
		return err
	}
	if paused {
		return nil
	}
	now := time.Now().Unix()
	for _, e := range entries {
		if e.DramaID <= 0 || e.EpisodeID <= 0 || e.PositionSec < 0 {
			continue
		}
		b, err := json.Marshal(historyPayload{
			EpisodeID: e.EpisodeID, EpisodeNumber: e.EpisodeNumber,
			PositionSec: e.PositionSec, DurationSec: e.DurationSec, TS: now,
		})
		if err != nil {
			continue
		}
		if err := s.history.Report(ctx, uid, e.DramaID, string(b)); err != nil {
			return err
		}
	}
	return nil
}

// ListHistory 返回续播列表（最近在前，上限由仓储控制）。
func (s *Service) ListHistory(ctx context.Context, uid int64) ([]HistoryView, error) {
	return s.repo.ListHistory(ctx, uid, historyListLimit)
}

// DeleteHistory 删除指定剧的观看历史，并同步清掉 Valkey 里还没落库的断点。
func (s *Service) DeleteHistory(ctx context.Context, uid int64, dramaIDs []int64) error {
	if len(dramaIDs) == 0 {
		return nil
	}
	if err := s.history.Clear(ctx, uid, dramaIDs); err != nil {
		return err
	}
	return s.repo.DeleteHistory(ctx, uid, dramaIDs)
}

// ClearHistory 清空某用户的全部观看历史（含 Valkey 未落库部分）。
func (s *Service) ClearHistory(ctx context.Context, uid int64) error {
	if err := s.history.ClearAll(ctx, uid); err != nil {
		return err
	}
	return s.repo.DeleteAllHistory(ctx, uid)
}

// FlushHistory 把 Valkey 待落库的续播断点批量 UPSERT 到数据库（worker 每分钟调用）。
// token 相同时重试取同一批；UPSERT 失败不清 taken，由 River 重试同一任务，不丢不重。
func (s *Service) FlushHistory(ctx context.Context, token string) (int, error) {
	pending, err := s.history.TakePending(ctx, token)
	if err != nil {
		return 0, err
	}
	if len(pending) == 0 {
		return 0, nil
	}
	rows := make([]HistoryRow, 0, len(pending))
	for _, p := range pending {
		var hp historyPayload
		if err := json.Unmarshal([]byte(p.Payload), &hp); err != nil {
			continue
		}
		rows = append(rows, HistoryRow{
			UserID: p.UserID, DramaID: p.DramaID, EpisodeID: hp.EpisodeID, EpisodeNumber: hp.EpisodeNumber,
			PositionSec: hp.PositionSec, DurationSec: hp.DurationSec, UpdatedAt: time.Unix(hp.TS, 0).UTC(),
		})
	}
	if len(rows) > 0 {
		if err := s.repo.UpsertHistory(ctx, rows); err != nil {
			return 0, err // 不清 taken，下轮重试
		}
	}
	_ = s.history.DonePending(ctx, token) // 尽力而为：清理失败只会下轮重落同一批，UPSERT 幂等
	return len(rows), nil
}
