package resource

import (
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	dom "xsite/service/internal/domain/resource"
	"xsite/service/internal/errs"
	"xsite/service/internal/http/admin"
	"xsite/service/internal/http/respond"
)

func itoa(id int64) string { return strconv.FormatInt(id, 10) }

// ---------- 请求与响应 ----------

type listQuery struct {
	Q        string `form:"q"`
	Status   string `form:"status"`
	Page     int    `form:"page"`
	PageSize int    `form:"page_size"`
}

type ftpServerRequest struct {
	Name    string `json:"name"`
	Host    string `json:"host"`
	Port    int32  `json:"port"`
	TLS     string `json:"tls"`
	Passive *bool  `json:"passive"` // 未传时为 true
	CredRef string `json:"cred_ref"`
	Status  string `json:"status"`
}

func (r ftpServerRequest) input() dom.FTPServerInput {
	passive := true
	if r.Passive != nil {
		passive = *r.Passive
	}
	return dom.FTPServerInput{
		Name: r.Name, Host: r.Host, Port: r.Port, TLS: dom.TLSMode(r.TLS), Passive: passive,
		CredRef: r.CredRef, Status: dom.Status(r.Status),
	}
}

type listDirRequest struct {
	Path string `json:"path"`
}

// credEnvDTO 是 worker 读取账号密码的两个环境变量名（不是账号密码本身）。
type credEnvDTO struct {
	User     string `json:"user"`
	Password string `json:"password"`
}

type ftpServerDTO struct {
	ID        int64      `json:"id"`
	Name      string     `json:"name"`
	Host      string     `json:"host"`
	Port      int32      `json:"port"`
	TLS       string     `json:"tls"`
	Passive   bool       `json:"passive"`
	CredRef   string     `json:"cred_ref"`
	CredEnv   credEnvDTO `json:"cred_env"`
	Status    string     `json:"status"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

func ftpServerOf(s dom.FTPServer) ftpServerDTO {
	user, password := dom.CredEnv(s.CredRef)
	return ftpServerDTO{
		ID: s.ID, Name: s.Name, Host: s.Host, Port: s.Port, TLS: string(s.TLS), Passive: s.Passive,
		CredRef: s.CredRef, CredEnv: credEnvDTO{User: user, Password: password}, Status: string(s.Status),
		CreatedAt: s.CreatedAt, UpdatedAt: s.UpdatedAt,
	}
}

type pageDTO[T any] struct {
	List     []T   `json:"list"`
	Total    int64 `json:"total"`
	Page     int   `json:"page"`
	PageSize int   `json:"page_size"`
}

type probeStepDTO struct {
	Name    string `json:"name"`
	OK      bool   `json:"ok"`
	Ms      int64  `json:"ms"`
	Message string `json:"message"`
}

type ftpEntryDTO struct {
	Name       string     `json:"name"`
	Type       string     `json:"type"`
	Size       uint64     `json:"size"`
	ModifiedAt *time.Time `json:"modified_at"`
	Target     string     `json:"target"`
}

type probeResultDTO struct {
	OK        bool           `json:"ok"`
	ElapsedMs int64          `json:"elapsed_ms"`
	Steps     []probeStepDTO `json:"steps"`
	Path      string         `json:"path"`
	Entries   []ftpEntryDTO  `json:"entries"`
	Truncated bool           `json:"truncated"`
}

func probeResultOf(r dom.ProbeResult) probeResultDTO {
	out := probeResultDTO{
		OK: r.OK, ElapsedMs: r.ElapsedMs, Path: r.Path, Truncated: r.Truncated,
		Steps: make([]probeStepDTO, len(r.Steps)), Entries: make([]ftpEntryDTO, len(r.Entries)),
	}
	for i, s := range r.Steps {
		out.Steps[i] = probeStepDTO{Name: string(s.Name), OK: s.OK, Ms: s.Ms, Message: s.Message}
	}
	for i, e := range r.Entries {
		out.Entries[i] = ftpEntryDTO{Name: e.Name, Type: string(e.Type), Size: e.Size, ModifiedAt: e.ModifiedAt, Target: e.Target}
	}
	return out
}

// probeAuditDTO 是测连与列目录的审计内容：只记结果与路径，不记目录内容。
type probeAuditDTO struct {
	OK   bool   `json:"ok"`
	Path string `json:"path"`
	Step string `json:"step"` // 失败的那一步，成功时为空
}

func probeAuditOf(r dom.ProbeResult) probeAuditDTO {
	a := probeAuditDTO{OK: r.OK, Path: r.Path}
	for _, s := range r.Steps {
		if !s.OK {
			a.Step = string(s.Name)
		}
	}
	return a
}

// ---------- handler ----------

// ListFTPServers 是 GET /api/admin/ftp：搜名称或地址、按状态筛选，页码分页。
func (h *Handlers) ListFTPServers(c *gin.Context) {
	var q listQuery
	if err := c.ShouldBindQuery(&q); err != nil {
		respond.Error(c, errs.ErrBadRequest.WithMessage("查询参数格式错误"))
		return
	}
	page, err := h.Svc.ListFTPServers(c.Request.Context(), dom.Filter{
		Q: q.Q, Status: dom.Status(q.Status), Page: q.Page, PageSize: q.PageSize,
	})
	if err != nil {
		respond.Error(c, err)
		return
	}
	list := make([]ftpServerDTO, len(page.List))
	for i, s := range page.List {
		list[i] = ftpServerOf(s)
	}
	respond.OK(c, pageDTO[ftpServerDTO]{List: list, Total: page.Total, Page: page.Page, PageSize: page.PageSize})
}

// CreateFTPServer 是 POST /api/admin/ftp。
func (h *Handlers) CreateFTPServer(c *gin.Context) {
	var req ftpServerRequest
	if !admin.Bind(c, &req) {
		return
	}
	s, err := h.Svc.CreateFTPServer(c.Request.Context(), req.input())
	if err != nil {
		respond.Error(c, err)
		return
	}
	out := ftpServerOf(s)
	admin.SetAudit(c, admin.AuditDetail{Action: "ftp_server.create", TargetType: "ftp_server", TargetID: itoa(s.ID), After: out})
	respond.OK(c, out)
}

// UpdateFTPServer 是 PATCH /api/admin/ftp/:id：整体保存。
func (h *Handlers) UpdateFTPServer(c *gin.Context) {
	id, ok := admin.PathID(c)
	var req ftpServerRequest
	if !ok || !admin.Bind(c, &req) {
		return
	}
	before, after, err := h.Svc.UpdateFTPServer(c.Request.Context(), id, req.input())
	if err != nil {
		respond.Error(c, err)
		return
	}
	out := ftpServerOf(after)
	admin.SetAudit(c, admin.AuditDetail{Action: "ftp_server.update", TargetType: "ftp_server", TargetID: itoa(id), Before: ftpServerOf(before), After: out})
	respond.OK(c, out)
}

// DeleteFTPServer 是 DELETE /api/admin/ftp/:id：不进回收站。
func (h *Handlers) DeleteFTPServer(c *gin.Context) {
	id, ok := admin.PathID(c)
	if !ok {
		return
	}
	before, err := h.Svc.DeleteFTPServer(c.Request.Context(), id)
	if err != nil {
		respond.Error(c, err)
		return
	}
	admin.SetAudit(c, admin.AuditDetail{Action: "ftp_server.delete", TargetType: "ftp_server", TargetID: itoa(id), Before: ftpServerOf(before)})
	respond.OK(c, nil)
}

// TestFTPServer 是 POST /api/admin/ftp/:id/test：由 worker 测连，返回每一步的结果（失败也是 200）。
func (h *Handlers) TestFTPServer(c *gin.Context) {
	id, ok := admin.PathID(c)
	if !ok {
		return
	}
	res, err := h.Svc.TestFTPServer(c.Request.Context(), id)
	if err != nil {
		respond.Error(c, err)
		return
	}
	admin.SetAudit(c, admin.AuditDetail{Action: "ftp_server.test", TargetType: "ftp_server", TargetID: itoa(id), After: probeAuditOf(res)})
	respond.OK(c, probeResultOf(res))
}

// ListFTPDir 是 POST /api/admin/ftp/:id/list：打开目录，请求体 { path } 可省略（登录目录）。
func (h *Handlers) ListFTPDir(c *gin.Context) {
	id, ok := admin.PathID(c)
	if !ok {
		return
	}
	var req listDirRequest
	if !bindOptional(c, &req) {
		return
	}
	res, err := h.Svc.ListFTPDir(c.Request.Context(), id, req.Path)
	if err != nil {
		respond.Error(c, err)
		return
	}
	admin.SetAudit(c, admin.AuditDetail{Action: "ftp_server.list", TargetType: "ftp_server", TargetID: itoa(id), After: probeAuditOf(res)})
	respond.OK(c, probeResultOf(res))
}

// bindOptional 解析可以省略的 JSON 请求体（没有请求体时保持零值）；格式错误时已写出 400。
func bindOptional(c *gin.Context, dst any) bool {
	if c.Request.ContentLength == 0 || c.Request.Body == nil || c.Request.Body == http.NoBody {
		return true
	}
	if err := c.ShouldBindJSON(dst); err != nil && !errors.Is(err, io.EOF) {
		respond.Error(c, errs.ErrBadRequest)
		return false
	}
	return true
}
