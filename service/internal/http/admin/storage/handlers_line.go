package storage

import (
	"github.com/gin-gonic/gin"

	dom "xsite/service/internal/domain/storage"
	"xsite/service/internal/errs"
	"xsite/service/internal/http/admin"
	"xsite/service/internal/http/respond"
)

// ---------- 请求 ----------

type listLineQuery struct {
	ProviderID int64  `form:"provider_id"`
	Kind       string `form:"kind"`
	Status     string `form:"status"`
}

type lineReq struct {
	ProviderID   int64  `json:"provider_id"` // 仅新建用
	Kind         string `json:"kind"`
	Name         string `json:"name"`
	BaseURL      string `json:"base_url"`
	AccessMode   string `json:"access_mode"`
	SignType     string `json:"sign_type"`
	SignKeyRef   string `json:"sign_key_ref"`
	TokenTTL     int32  `json:"token_ttl"`
	Priority     int32  `json:"priority"`
	Weight       int32  `json:"weight"`
	RegionScope  string `json:"region_scope"`
	VideoProcess string `json:"video_process"`
	ProbePath    string `json:"probe_path"`
	Status       string `json:"status"`
	Note         string `json:"note"`
}

func (r lineReq) input() dom.LineInput {
	return dom.LineInput{
		ProviderID: r.ProviderID, Kind: dom.LineKind(r.Kind), Name: r.Name, BaseURL: r.BaseURL,
		AccessMode: dom.AccessMode(r.AccessMode), SignType: dom.SignType(r.SignType), SignKeyRef: r.SignKeyRef,
		TokenTTL: r.TokenTTL, Priority: r.Priority, Weight: r.Weight, RegionScope: dom.RegionScope(r.RegionScope),
		VideoProcess: dom.VideoProcess(r.VideoProcess), ProbePath: r.ProbePath, Status: dom.LineStatus(r.Status), Note: r.Note,
	}
}

type ruleReq struct {
	Country  string `json:"country"`
	Province string `json:"province"`
	ISP      string `json:"isp"`
	ASN      string `json:"asn"`
	Effect   string `json:"effect"`
	Boost    int32  `json:"boost"`
}

type rulesReq struct {
	Rules []ruleReq `json:"rules"`
}

func (r rulesReq) input() []dom.RuleInput {
	out := make([]dom.RuleInput, len(r.Rules))
	for i, x := range r.Rules {
		out[i] = dom.RuleInput{Country: x.Country, Province: x.Province, ISP: x.ISP, ASN: x.ASN, Effect: dom.RuleEffect(x.Effect), Boost: x.Boost}
	}
	return out
}

// ---------- 响应 ----------

type lineListDTO struct {
	List  []dom.Line `json:"list"`
	Total int        `json:"total"`
}

type rulesDTO struct {
	Rules []dom.Rule `json:"rules"`
}

// lineAuditDTO 是线路写操作的审计内容（不含完整规则列表，只记关键字段）。
type lineAuditDTO struct {
	ID         int64  `json:"id"`
	ProviderID int64  `json:"provider_id"`
	Kind       string `json:"kind"`
	Name       string `json:"name"`
	BaseURL    string `json:"base_url"`
	Status     string `json:"status"`
}

func lineAuditOf(l dom.Line) lineAuditDTO {
	return lineAuditDTO{ID: l.ID, ProviderID: l.ProviderID, Kind: string(l.Kind), Name: l.Name, BaseURL: l.BaseURL, Status: string(l.Status)}
}

// ---------- handler ----------

// ListLines 是 GET /api/admin/storage/lines：按存储源、类型、状态筛选，含地区规则。
func (h *Handlers) ListLines(c *gin.Context) {
	var q listLineQuery
	if err := c.ShouldBindQuery(&q); err != nil {
		respond.Error(c, errs.ErrBadRequest.WithMessage("查询参数格式错误"))
		return
	}
	lines, err := h.LineSvc.ListLines(c.Request.Context(), dom.LineFilter{
		ProviderID: q.ProviderID, Kind: dom.LineKind(q.Kind), Status: dom.LineStatus(q.Status),
	})
	if err != nil {
		respond.Error(c, err)
		return
	}
	respond.OK(c, lineListDTO{List: lines, Total: len(lines)})
}

// CreateLine 是 POST /api/admin/storage/lines。
func (h *Handlers) CreateLine(c *gin.Context) {
	var req lineReq
	if !admin.Bind(c, &req) {
		return
	}
	line, err := h.LineSvc.CreateLine(c.Request.Context(), req.input())
	if err != nil {
		respond.Error(c, err)
		return
	}
	admin.SetAudit(c, admin.AuditDetail{Action: "storage_line.create", TargetType: "storage_line", TargetID: itoa(line.ID), After: lineAuditOf(line)})
	respond.OK(c, line)
}

// UpdateLine 是 PATCH /api/admin/storage/lines/:id：整体保存（不改所属存储源）。
func (h *Handlers) UpdateLine(c *gin.Context) {
	id, ok := admin.PathID(c)
	var req lineReq
	if !ok || !admin.Bind(c, &req) {
		return
	}
	before, after, err := h.LineSvc.UpdateLine(c.Request.Context(), id, req.input())
	if err != nil {
		respond.Error(c, err)
		return
	}
	admin.SetAudit(c, admin.AuditDetail{Action: "storage_line.update", TargetType: "storage_line", TargetID: itoa(id), Before: lineAuditOf(before), After: lineAuditOf(after)})
	respond.OK(c, after)
}

// DeleteLine 是 DELETE /api/admin/storage/lines/:id：级联删除其地区规则。
func (h *Handlers) DeleteLine(c *gin.Context) {
	id, ok := admin.PathID(c)
	if !ok {
		return
	}
	before, err := h.LineSvc.DeleteLine(c.Request.Context(), id)
	if err != nil {
		respond.Error(c, err)
		return
	}
	admin.SetAudit(c, admin.AuditDetail{Action: "storage_line.delete", TargetType: "storage_line", TargetID: itoa(id), Before: lineAuditOf(before)})
	respond.OK(c, nil)
}

// SetLineRules 是 PUT /api/admin/storage/lines/:id/rules：整体替换地区规则。
func (h *Handlers) SetLineRules(c *gin.Context) {
	id, ok := admin.PathID(c)
	var req rulesReq
	if !ok || !admin.Bind(c, &req) {
		return
	}
	rules, err := h.LineSvc.SetRules(c.Request.Context(), id, req.input())
	if err != nil {
		respond.Error(c, err)
		return
	}
	admin.SetAudit(c, admin.AuditDetail{Action: "storage_line.set_rules", TargetType: "storage_line", TargetID: itoa(id), After: rulesDTO{Rules: rules}})
	respond.OK(c, rulesDTO{Rules: rules})
}

// TestLine 是 POST /api/admin/storage/lines/:id/test：HTTP 探测，失败也返回 200（原因写在 message）。
func (h *Handlers) TestLine(c *gin.Context) {
	id, ok := admin.PathID(c)
	if !ok {
		return
	}
	res, err := h.LineSvc.TestLine(c.Request.Context(), id)
	if err != nil {
		respond.Error(c, err)
		return
	}
	admin.SetAudit(c, admin.AuditDetail{Action: "storage_line.test", TargetType: "storage_line", TargetID: itoa(id), After: res})
	respond.OK(c, res)
}
