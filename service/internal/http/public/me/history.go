package me

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	meDom "xsite/service/internal/domain/me"
	"xsite/service/internal/errs"
	"xsite/service/internal/http/respond"
)

// ListHistory 是 GET /api/v1/me/history：续播列表（最近在前）。
func (h *Handlers) ListHistory(c *gin.Context) {
	c.Header("Cache-Control", noStore)
	items, err := h.Me.ListHistory(c.Request.Context(), currentUserID(c))
	if err != nil {
		respond.Error(c, err)
		return
	}
	if items == nil {
		items = []meDom.HistoryView{}
	}
	respond.OK(c, gin.H{"items": items})
}

type historyReportRequest struct {
	Items []historyEntryDTO `json:"items"`
}

type historyEntryDTO struct {
	DramaID       int64 `json:"drama_id"`
	EpisodeID     int64 `json:"episode_id"`
	EpisodeNumber int   `json:"episode_number"`
	PositionSec   int   `json:"position_sec"`
	DurationSec   int   `json:"duration_sec"`
}

// ReportHistory 是 POST /api/v1/me/history：续播断点上报（支持批量）。暂停记录的用户静默忽略。
func (h *Handlers) ReportHistory(c *gin.Context) {
	c.Header("Cache-Control", noStore)
	var req historyReportRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respond.Error(c, errs.ErrBadRequest.WithMessage("请求体格式错误"))
		return
	}
	entries := make([]meDom.HistoryEntry, len(req.Items))
	for i, it := range req.Items {
		entries[i] = meDom.HistoryEntry{
			DramaID: it.DramaID, EpisodeID: it.EpisodeID, EpisodeNumber: it.EpisodeNumber,
			PositionSec: it.PositionSec, DurationSec: it.DurationSec,
		}
	}
	if err := h.Me.ReportHistory(c.Request.Context(), currentUserID(c), entries); err != nil {
		respond.Error(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// DeleteHistory 是 DELETE /api/v1/me/history：删除指定剧（?drama_ids=1,2,3）或清空（?all=1）。
func (h *Handlers) DeleteHistory(c *gin.Context) {
	c.Header("Cache-Control", noStore)
	uid := currentUserID(c)
	if c.Query("all") == "1" {
		if err := h.Me.ClearHistory(c.Request.Context(), uid); err != nil {
			respond.Error(c, err)
			return
		}
		c.Status(http.StatusNoContent)
		return
	}
	ids, err := parseIDList(c.Query("drama_ids"))
	if err != nil {
		respond.Error(c, err)
		return
	}
	if err := h.Me.DeleteHistory(c.Request.Context(), uid, ids); err != nil {
		respond.Error(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// parseIDList 解析逗号分隔的正整数编号列表；为空或含非法值时返回 400。
func parseIDList(s string) ([]int64, error) {
	parts := strings.Split(s, ",")
	ids := make([]int64, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		id, err := strconv.ParseInt(p, 10, 64)
		if err != nil || id <= 0 {
			return nil, errs.ErrBadRequest.WithMessage("drama_ids 含非法编号")
		}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return nil, errs.ErrBadRequest.WithMessage("请提供要删除的 drama_ids 或 all=1")
	}
	return ids, nil
}
