package storage

import (
	"strconv"

	"github.com/gin-gonic/gin"

	dom "xsite/service/internal/domain/storage"
	"xsite/service/internal/errs"
	"xsite/service/internal/http/admin"
	"xsite/service/internal/http/respond"
)

func itoa(id int64) string { return strconv.FormatInt(id, 10) }

// ---------- 请求 ----------

type listQuery struct {
	Q        string `form:"q"`
	Type     string `form:"type"`
	Status   string `form:"status"`
	Page     int    `form:"page"`
	PageSize int    `form:"page_size"`
}

// credentialReq 是一组凭据。编辑时两者都留空表示不修改该组凭据。
type credentialReq struct {
	AccessKeyID     string `json:"access_key_id"`
	SecretAccessKey string `json:"secret_access_key"`
}

func (c *credentialReq) toCredential() *dom.Credential {
	if c == nil || (c.AccessKeyID == "" && c.SecretAccessKey == "") {
		return nil
	}
	return &dom.Credential{AccessKeyID: c.AccessKeyID, SecretAccessKey: c.SecretAccessKey}
}

type providerReq struct {
	Name          string         `json:"name"`
	Type          string         `json:"type"`
	Subtype       string         `json:"subtype"`
	Purpose       string         `json:"purpose"`
	Role          string         `json:"role"`
	Endpoint      string         `json:"endpoint"`
	Region        string         `json:"region"`
	Bucket        string         `json:"bucket"`
	AccountID     string         `json:"account_id"`
	PathStyle     bool           `json:"path_style"`
	PathPrefix    string         `json:"path_prefix"`
	MaxPresignTTL int32          `json:"max_presign_ttl"`
	Status        string         `json:"status"`
	WriteCred     *credentialReq `json:"write_cred"`
	ReadCred      *credentialReq `json:"read_cred"`
}

func (r providerReq) input() dom.Input {
	return dom.Input{
		Name: r.Name, Type: dom.Type(r.Type), Subtype: dom.Subtype(r.Subtype), Purpose: dom.Purpose(r.Purpose),
		Role: dom.Role(r.Role), Endpoint: r.Endpoint, Region: r.Region, Bucket: r.Bucket, AccountID: r.AccountID,
		PathStyle: r.PathStyle, PathPrefix: r.PathPrefix, MaxPresignTTL: r.MaxPresignTTL, Status: dom.Status(r.Status),
		WriteCred: r.WriteCred.toCredential(), ReadCred: r.ReadCred.toCredential(),
	}
}

// ---------- 响应 ----------

type pageDTO[T any] struct {
	List     []T   `json:"list"`
	Total    int64 `json:"total"`
	Page     int   `json:"page"`
	PageSize int   `json:"page_size"`
}

// testAuditDTO 是连通性测试的审计内容：只记结果与失败步骤，不记端点等细节。
type testAuditDTO struct {
	OK         bool   `json:"ok"`
	FailedStep string `json:"failed_step"`
}

func testAuditOf(r dom.ProbeResult) testAuditDTO {
	a := testAuditDTO{OK: r.OK}
	for _, s := range r.Steps {
		if !s.OK {
			a.FailedStep = string(s.Name)
		}
	}
	return a
}

// ---------- handler ----------

// List 是 GET /api/admin/storage/providers：搜名称或桶名、按类型与状态筛选，页码分页。
func (h *Handlers) List(c *gin.Context) {
	var q listQuery
	if err := c.ShouldBindQuery(&q); err != nil {
		respond.Error(c, errs.ErrBadRequest.WithMessage("查询参数格式错误"))
		return
	}
	page, err := h.Svc.List(c.Request.Context(), dom.Filter{
		Q: q.Q, Type: dom.Type(q.Type), Status: dom.Status(q.Status), Page: q.Page, PageSize: q.PageSize,
	})
	if err != nil {
		respond.Error(c, err)
		return
	}
	respond.OK(c, pageDTO[dom.Provider]{List: page.List, Total: page.Total, Page: page.Page, PageSize: page.PageSize})
}

// Create 是 POST /api/admin/storage/providers。
func (h *Handlers) Create(c *gin.Context) {
	var req providerReq
	if !admin.Bind(c, &req) {
		return
	}
	prov, err := h.Svc.Create(c.Request.Context(), req.input())
	if err != nil {
		respond.Error(c, err)
		return
	}
	admin.SetAudit(c, admin.AuditDetail{Action: "storage_provider.create", TargetType: "storage_provider", TargetID: itoa(prov.ID), After: prov})
	respond.OK(c, prov)
}

// Update 是 PATCH /api/admin/storage/providers/:id：整体保存（凭据留空表示不修改）。
func (h *Handlers) Update(c *gin.Context) {
	id, ok := admin.PathID(c)
	var req providerReq
	if !ok || !admin.Bind(c, &req) {
		return
	}
	before, after, err := h.Svc.Update(c.Request.Context(), id, req.input())
	if err != nil {
		respond.Error(c, err)
		return
	}
	admin.SetAudit(c, admin.AuditDetail{Action: "storage_provider.update", TargetType: "storage_provider", TargetID: itoa(id), Before: before, After: after})
	respond.OK(c, after)
}

// Delete 是 DELETE /api/admin/storage/providers/:id：不进回收站（有副本引用时由外键阻止，S1-6f 起需先 draining 迁移）。
func (h *Handlers) Delete(c *gin.Context) {
	id, ok := admin.PathID(c)
	if !ok {
		return
	}
	before, err := h.Svc.Delete(c.Request.Context(), id)
	if err != nil {
		respond.Error(c, err)
		return
	}
	admin.SetAudit(c, admin.AuditDetail{Action: "storage_provider.delete", TargetType: "storage_provider", TargetID: itoa(id), Before: before})
	respond.OK(c, nil)
}

// Test 是 POST /api/admin/storage/providers/:id/test：连通性测试，失败也返回 200（结果写在步骤里）。
func (h *Handlers) Test(c *gin.Context) {
	id, ok := admin.PathID(c)
	if !ok {
		return
	}
	res, err := h.Svc.Test(c.Request.Context(), id)
	if err != nil {
		respond.Error(c, err)
		return
	}
	admin.SetAudit(c, admin.AuditDetail{Action: "storage_provider.test", TargetType: "storage_provider", TargetID: itoa(id), After: testAuditOf(res)})
	respond.OK(c, res)
}
