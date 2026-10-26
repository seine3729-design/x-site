// Package domains 是运营后台「域名与证书」接口（docs/07 第 2 节，docs/06 第 38 节，A4-4 / S4-8）。
// 会话、CSRF、审计中间件由 admin.Routes 返回的分组提供，这里按页面声明权限点（domains.view / domains.edit）。
package domains

import (
	"encoding/json"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	dom "xsite/service/internal/domain/domains"
	"xsite/service/internal/errs"
	"xsite/service/internal/http/admin"
	"xsite/service/internal/http/respond"
)

func itoa(id int64) string { return strconv.FormatInt(id, 10) }

// ---------- 请求体 ----------

type domainRequest struct {
	Host          string     `json:"host"`
	Role          string     `json:"role"`
	Weight        int32      `json:"weight"`
	Region        string     `json:"region"`
	CDN           string     `json:"cdn"`
	DNSProvider   string     `json:"dns_provider"`
	CertExpiresAt *time.Time `json:"cert_expires_at"`
	Status        string     `json:"status"`
	Note          string     `json:"note"`
}

func (r domainRequest) input() dom.EntryInput {
	return dom.EntryInput{
		Host: r.Host, Role: dom.Role(r.Role), Weight: r.Weight, Region: r.Region,
		CDN: r.CDN, DNSProvider: r.DNSProvider, CertExpiresAt: r.CertExpiresAt,
		Status: dom.Status(r.Status), Note: r.Note,
	}
}

type genCertRequest struct {
	CommonName string   `json:"common_name"`
	SANs       []string `json:"sans"`
	Days       int      `json:"days"`
	KeyType    string   `json:"key_type"`
}

type certPEMRequest struct {
	CertPEM string `json:"cert_pem"`
}

type publishRequest struct {
	Payload   json.RawMessage `json:"payload"`
	Signature string          `json:"signature"`
	Note      string          `json:"note"`
}

// ---------- 域名池 ----------

// List 是 GET /api/admin/domains：域名池（不分页），可按状态、角色、关键词筛选。
func (h *Handlers) List(c *gin.Context) {
	list, err := h.Svc.List(c.Request.Context(), dom.Filter{
		Status: dom.Status(c.Query("status")), Role: dom.Role(c.Query("role")), Q: c.Query("q"),
	})
	if err != nil {
		respond.Error(c, err)
		return
	}
	respond.OK(c, gin.H{"list": list, "total": len(list)})
}

// Create 是 POST /api/admin/domains。
func (h *Handlers) Create(c *gin.Context) {
	var req domainRequest
	if !admin.Bind(c, &req) {
		return
	}
	e, err := h.Svc.Create(c.Request.Context(), req.input())
	if err != nil {
		respond.Error(c, err)
		return
	}
	admin.SetAudit(c, admin.AuditDetail{Action: "domain.create", TargetType: "domain", TargetID: itoa(e.ID), After: e})
	respond.OK(c, e)
}

// Update 是 PATCH /api/admin/domains/:id：整体保存。
func (h *Handlers) Update(c *gin.Context) {
	id, ok := admin.PathID(c)
	var req domainRequest
	if !ok || !admin.Bind(c, &req) {
		return
	}
	before, after, err := h.Svc.Update(c.Request.Context(), id, req.input())
	if err != nil {
		respond.Error(c, err)
		return
	}
	admin.SetAudit(c, admin.AuditDetail{Action: "domain.update", TargetType: "domain", TargetID: itoa(id), Before: before, After: after})
	respond.OK(c, after)
}

// Delete 是 DELETE /api/admin/domains/:id：不进回收站（设置类数据）。
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
	admin.SetAudit(c, admin.AuditDetail{Action: "domain.delete", TargetType: "domain", TargetID: itoa(id), Before: before})
	respond.OK(c, nil)
}

// ---------- 证书（手动生成 / 上传回填）----------

// GenerateCert 是 POST /api/admin/domain-certificates：现场生成自签证书。
// 响应里的 key_pem 是私钥，只返回这一次；审计绝不记录私钥，只记主题、SAN、指纹与到期。
func (h *Handlers) GenerateCert(c *gin.Context) {
	var req genCertRequest
	if !admin.Bind(c, &req) {
		return
	}
	cert, err := h.Svc.GenerateCert(c.Request.Context(), dom.GenCertInput{
		CommonName: req.CommonName, SANs: req.SANs, Days: req.Days, KeyType: dom.KeyType(req.KeyType),
	})
	if err != nil {
		respond.Error(c, err)
		return
	}
	admin.SetAudit(c, admin.AuditDetail{Action: "domain.generate_cert", TargetType: "domain_cert", TargetID: req.CommonName,
		After: gin.H{"common_name": req.CommonName, "sans": cert.SANs, "key_type": cert.KeyType, "not_after": cert.NotAfter, "fingerprint": cert.Fingerprint}})
	respond.OK(c, cert)
}

// InspectCert 是 POST /api/admin/domain-certificates/inspect：只解析一张证书、不改数据（上传前预览）。
func (h *Handlers) InspectCert(c *gin.Context) {
	var req certPEMRequest
	if !admin.Bind(c, &req) {
		return
	}
	info, err := h.Svc.InspectCert(c.Request.Context(), req.CertPEM)
	if err != nil {
		respond.Error(c, err)
		return
	}
	admin.SetAudit(c, admin.AuditDetail{Action: "domain.inspect_cert", TargetType: "domain_cert", TargetID: info.Fingerprint})
	respond.OK(c, info)
}

// UploadCert 是 POST /api/admin/domains/:id/certificate：解析上传的证书，把到期日回填到该域名。
func (h *Handlers) UploadCert(c *gin.Context) {
	id, ok := admin.PathID(c)
	var req certPEMRequest
	if !ok || !admin.Bind(c, &req) {
		return
	}
	e, info, err := h.Svc.UploadCertForDomain(c.Request.Context(), id, req.CertPEM)
	if err != nil {
		respond.Error(c, err)
		return
	}
	admin.SetAudit(c, admin.AuditDetail{Action: "domain.upload_cert", TargetType: "domain", TargetID: itoa(id),
		After: gin.H{"cert_expires_at": info.NotAfter, "subject": info.Subject, "fingerprint": info.Fingerprint}})
	respond.OK(c, gin.H{"domain": e, "cert": info})
}

// ---------- 健康看板 ----------

// Health 是 GET /api/admin/domain-health：按域名 + 地区聚合的健康看板。
func (h *Handlers) Health(c *gin.Context) {
	ov, err := h.Svc.Health(c.Request.Context())
	if err != nil {
		respond.Error(c, err)
		return
	}
	respond.OK(c, ov)
}

// ---------- 签名清单 ----------

// ListReleases 是 GET /api/admin/domain-releases：发布历史（页码分页）+ 当前线上清单。
func (h *Handlers) ListReleases(c *gin.Context) {
	page, size := pageParams(c)
	list, total, err := h.Svc.ListReleases(c.Request.Context(), page, size)
	if err != nil {
		respond.Error(c, err)
		return
	}
	cur, ok, err := h.Svc.LatestRelease(c.Request.Context())
	if err != nil {
		respond.Error(c, err)
		return
	}
	var current *dom.Release
	if ok {
		current = &cur
	}
	respond.OK(c, gin.H{"list": list, "total": total, "page": page, "page_size": size, "current": current})
}

// PreviewRelease 是 GET /api/admin/domain-releases/preview：生成待离线签名的清单预览。
// 带 from_version 时用该历史版本的内容作为基础（回滚）。
func (h *Handlers) PreviewRelease(c *gin.Context) {
	var fromVersion *int64
	if v := c.Query("from_version"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n <= 0 {
			respond.Error(c, errs.ErrBadRequest.WithMessage("from_version 不正确"))
			return
		}
		fromVersion = &n
	}
	prev, err := h.Svc.PreviewRelease(c.Request.Context(), fromVersion)
	if err != nil {
		respond.Error(c, err)
		return
	}
	respond.OK(c, prev)
}

// PublishRelease 是 POST /api/admin/domain-releases：上传离线签名后的清单。
func (h *Handlers) PublishRelease(c *gin.Context) {
	var req publishRequest
	if !admin.Bind(c, &req) {
		return
	}
	if len(req.Payload) == 0 {
		respond.Error(c, errs.ErrBadRequest.WithMessage("请提供清单内容 payload"))
		return
	}
	actor, _ := admin.CurrentAdmin(c)
	by := actor.AdminID
	rel, err := h.Svc.PublishRelease(c.Request.Context(), dom.PublishInput{
		Payload: req.Payload, Signature: req.Signature, Note: req.Note, PublishedBy: &by,
	})
	if err != nil {
		respond.Error(c, err)
		return
	}
	admin.SetAudit(c, admin.AuditDetail{Action: "domain.publish_release", TargetType: "domain_release", TargetID: itoa(rel.Version),
		After: gin.H{"version": rel.Version, "note": rel.Note}})
	respond.OK(c, rel)
}

// pageParams 解析页码分页参数（与领域层的上下限一致：默认 20，最大 100）。
func pageParams(c *gin.Context) (page, size int) {
	page, _ = strconv.Atoi(c.Query("page"))
	size, _ = strconv.Atoi(c.Query("page_size"))
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 100 {
		size = 20
	}
	return page, size
}
