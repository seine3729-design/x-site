package ops

import (
	"time"

	"github.com/gin-gonic/gin"

	dom "xsite/service/internal/domain/ops"
	"xsite/service/internal/errs"
	"xsite/service/internal/http/admin"
	"xsite/service/internal/http/respond"
)

type rankOverrideDTO struct {
	DramaID     int64     `json:"drama_id"`
	Title       string    `json:"title"`
	DramaStatus string    `json:"drama_status"`
	InTrash     bool      `json:"in_trash"`
	Pinned      bool      `json:"pinned"`
	Blocked     bool      `json:"blocked"`
	Boost       int64     `json:"boost"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func rankOverrideOf(o dom.RankOverrideView) rankOverrideDTO {
	return rankOverrideDTO{
		DramaID: o.DramaID, Title: o.Title, DramaStatus: o.DramaStatus, InTrash: o.InTrash,
		Pinned: o.Pinned, Blocked: o.Blocked, Boost: o.Boost, UpdatedAt: o.UpdatedAt,
	}
}

type rankRowDTO struct {
	Rank    int     `json:"rank"`
	DramaID int64   `json:"drama_id"`
	Title   string  `json:"title"`
	Heat    float64 `json:"heat"`
	Pinned  bool    `json:"pinned"`
}

func rankRowOf(r dom.RankRow) rankRowDTO {
	return rankRowDTO{Rank: r.Rank, DramaID: r.DramaID, Title: r.Title, Heat: r.Heat, Pinned: r.Pinned}
}

// rankBoardDTO 是后台一张榜的页面数据。live 是前台正在显示的（上次重新排名的结果），preview 按当前系数与调整现算。
type rankBoardDTO struct {
	Board      string            `json:"board"`
	Overrides  []rankOverrideDTO `json:"overrides"`
	ComputedAt *time.Time        `json:"computed_at"`
	Live       []rankRowDTO      `json:"live"`
	Preview    []rankRowDTO      `json:"preview"`
}

type rankOverrideInput struct {
	DramaID int64 `json:"drama_id"`
	Pinned  bool  `json:"pinned"`
	Blocked bool  `json:"blocked"`
	Boost   int64 `json:"boost"`
}

// rankOverridesRequest 是一张榜的整体保存。overrides 必须显式传（清空传空数组），没传不能当作清空。
type rankOverridesRequest struct {
	Overrides *[]rankOverrideInput `json:"overrides"`
}

// rankWeightsRequest 要求四个系数都传，缺了不能当作 0。
type rankWeightsRequest struct {
	Play   *float64 `json:"play"`
	Follow *float64 `json:"follow"`
	Like   *float64 `json:"like"`
	Finish *float64 `json:"finish"`
}

type rankWeightsDTO struct {
	Play   float64 `json:"play"`
	Follow float64 `json:"follow"`
	Like   float64 `json:"like"`
	Finish float64 `json:"finish"`
}

func rankWeightsOf(w dom.RankWeights) rankWeightsDTO {
	return rankWeightsDTO{Play: w.Play, Follow: w.Follow, Like: w.Like, Finish: w.Finish}
}

func (h *Handlers) writeRankBoard(c *gin.Context, b dom.RankBoard) {
	v, err := h.Ranks.AdminBoard(c.Request.Context(), b)
	if err != nil {
		respond.Error(c, err)
		return
	}
	respond.OK(c, rankBoardDTO{
		Board: string(v.Board), Overrides: mapSlice(v.Overrides, rankOverrideOf), ComputedAt: v.ComputedAt,
		Live: mapSlice(v.Live, rankRowOf), Preview: mapSlice(v.Preview, rankRowOf),
	})
}

// GetRankBoard 是 GET /api/admin/ranks/:board：人工调整、线上榜单与按当前设置的预览。
func (h *Handlers) GetRankBoard(c *gin.Context) {
	h.writeRankBoard(c, dom.RankBoard(c.Param("board")))
}

// SetRankOverrides 是 PUT /api/admin/ranks/:board：整体保存一张榜的人工调整（要重新排名才生效），返回新的页面数据。
func (h *Handlers) SetRankOverrides(c *gin.Context) {
	board := dom.RankBoard(c.Param("board"))
	var req rankOverridesRequest
	if !admin.Bind(c, &req) {
		return
	}
	if req.Overrides == nil {
		respond.Error(c, errs.ErrBadRequest.WithMessage("请提供 overrides（清空所有调整请传空数组）"))
		return
	}
	list := make([]dom.RankOverride, len(*req.Overrides))
	for i, o := range *req.Overrides {
		list[i] = dom.RankOverride{DramaID: o.DramaID, Pinned: o.Pinned, Blocked: o.Blocked, Boost: o.Boost}
	}
	before, after, err := h.Ranks.SetOverrides(c.Request.Context(), board, list)
	if err != nil {
		respond.Error(c, err)
		return
	}
	admin.SetAudit(c, admin.AuditDetail{
		Action: "rank.update", TargetType: "rank", TargetID: string(board),
		Before: mapSlice(before, rankOverrideOf), After: mapSlice(after, rankOverrideOf),
	})
	h.writeRankBoard(c, board)
}

// RecomputeRanks 是 POST /api/admin/ranks/recompute：马上重新排名（两张榜）。done=false 表示已交给 worker、还没算完。
func (h *Handlers) RecomputeRanks(c *gin.Context) {
	done, err := h.Ranks.RecomputeNow(c.Request.Context())
	if err != nil {
		respond.Error(c, err)
		return
	}
	admin.SetAudit(c, admin.AuditDetail{Action: "rank.recompute", TargetType: "rank", After: gin.H{"done": done}})
	respond.OK(c, gin.H{"done": done})
}

// GetRankingConfig 是 GET /api/admin/ranking-config：计分系数。
func (h *Handlers) GetRankingConfig(c *gin.Context) {
	w, err := h.Ranks.Weights(c.Request.Context())
	if err != nil {
		respond.Error(c, err)
		return
	}
	respond.OK(c, rankWeightsOf(w))
}

// SetRankingConfig 是 PUT /api/admin/ranking-config：保存计分系数（要重新排名才生效）。
func (h *Handlers) SetRankingConfig(c *gin.Context) {
	var req rankWeightsRequest
	if !admin.Bind(c, &req) {
		return
	}
	if req.Play == nil || req.Follow == nil || req.Like == nil || req.Finish == nil {
		respond.Error(c, errs.ErrBadRequest.WithMessage("播放、追剧、点赞、看完四个系数都要填"))
		return
	}
	before, after, err := h.Ranks.SetWeights(c.Request.Context(), dom.RankWeights{Play: *req.Play, Follow: *req.Follow, Like: *req.Like, Finish: *req.Finish})
	if err != nil {
		respond.Error(c, err)
		return
	}
	out := rankWeightsOf(after)
	admin.SetAudit(c, admin.AuditDetail{
		Action: "ranking_config.update", TargetType: "site_config", TargetID: "ranking", Before: rankWeightsOf(before), After: out,
	})
	respond.OK(c, out)
}
