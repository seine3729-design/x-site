package analytics

import (
	"context"
	"strings"
	"time"
)

// Repo 批量写入原始事件。
type Repo interface {
	InsertEvents(ctx context.Context, events []Event) (int, error)
}

// Service 是埋点采集。
type Service struct {
	repo Repo
	now  func() time.Time
}

// NewService 构造采集服务。
func NewService(repo Repo) *Service {
	return &Service{repo: repo, now: time.Now}
}

// Collect 校验并写入一批事件，返回实际入库条数。
//
// 采集宽容：未知类型、props 过大的单条静默丢弃，不因一条脏数据拒整批；只有「超过批量上限」才返回 400。
// occurred_at 一律用服务端时间（docs/03 第 8.1 节：服务端打时间戳，防客户端时钟作弊）。
func (s *Service) Collect(ctx context.Context, in CollectInput) (int, error) {
	if len(in.Events) == 0 {
		return 0, nil
	}
	if len(in.Events) > MaxBatch {
		return 0, badRequest("单次最多上报 %d 条事件", MaxBatch)
	}
	now := s.now().UTC()
	client := in.Client
	if !allowedClients[client] {
		client = ""
	}
	device := truncate(in.DeviceID, maxDeviceLen)
	region := truncate(in.Region, maxRegionLen)
	isp := truncate(in.ISP, maxISPLen)

	out := make([]Event, 0, len(in.Events))
	for _, it := range in.Events {
		t := strings.TrimSpace(it.Type)
		if !allowedType(t) {
			continue
		}
		props, ok := marshalProps(it.Props)
		if !ok {
			continue
		}
		out = append(out, Event{
			OccurredAt: now, Type: t, DeviceID: device, UserID: in.UserID,
			DramaID: it.DramaID, EpisodeID: it.EpisodeID,
			Client: client, Region: region, ISP: isp, Props: props,
		})
	}
	if len(out) == 0 {
		return 0, nil
	}
	return s.repo.InsertEvents(ctx, out)
}
