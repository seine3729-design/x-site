package storage

import (
	"github.com/gin-gonic/gin"

	dom "xsite/service/internal/domain/storage"
	"xsite/service/internal/errs"
	"xsite/service/internal/http/respond"
)

type previewQuery struct {
	IP       string `form:"ip"`
	Country  string `form:"country"`
	Province string `form:"province"`
	ISP      string `form:"isp"`
	ASN      string `form:"asn"`
	Kind     string `form:"kind"`
}

// Preview 是 GET /api/admin/storage/preview：输入 IP 或「国家+省份+运营商」，返回解析出的地区与每条线路的选线评估。
func (h *Handlers) Preview(c *gin.Context) {
	var q previewQuery
	if err := c.ShouldBindQuery(&q); err != nil {
		respond.Error(c, errs.ErrBadRequest.WithMessage("查询参数格式错误"))
		return
	}
	res, err := h.PreviewSvc.Preview(c.Request.Context(), dom.PreviewInput{
		IP:   q.IP,
		Geo:  dom.Geo{Country: q.Country, Province: q.Province, ISP: q.ISP, ASN: q.ASN},
		Kind: dom.LineKind(q.Kind),
	})
	if err != nil {
		respond.Error(c, err)
		return
	}
	respond.OK(c, res)
}
