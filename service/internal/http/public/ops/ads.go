package ops

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
	"uuid"

	"github.com/gin-gonic/gin"

	dom "xsite/service/internal/domain/ops"
	"xsite/service/internal/errs"
	"xsite/service/internal/http/respond"
	"xsite/service/internal/privacy"
)

const headerDeviceID = "X-Device-Id"

type adsQuery struct {
	Slots  string `form:"slots"`
	N      int    `form:"n"`
	Device string `form:"device"`
	Locale string `form:"locale"`
}

type adEventBody struct {
	AdID int64  `json:"ad_id"`
	Kind string `json:"kind"`
}

// ListAds 是 GET /api/v1/ads?slots=&n=&device=&locale=：每个位置按权重随机取 n 条（端定向、投放语言、频控、结束时间）。
func (h *Handlers) ListAds(c *gin.Context) {
	var q adsQuery
	if err := c.ShouldBindQuery(&q); err != nil {
		respond.Error(c, errs.ErrBadRequest.WithMessage("查询参数格式错误"))
		return
	}
	list, err := h.Ads.Serve(c.Request.Context(), dom.ServeRequest{
		Slots: q.Slots, N: q.N, Device: q.Device, Locale: q.Locale, DeviceKey: deviceKey(c),
	})
	if err != nil {
		respond.Error(c, err)
		return
	}
	if list == nil {
		list = []dom.PublicAd{}
	}
	// 按设备频控、每次随机挑选，任何缓存都不能复用
	c.Header("Cache-Control", "private, no-store")
	respond.OK(c, gin.H{"list": list})
}

// AdEvent 是 POST /api/v1/ads/event：上报一次曝光或点击，成功返回 204（重复或不在投放中的广告也返回 204，只是不计数）。
func (h *Handlers) AdEvent(c *gin.Context) {
	var b adEventBody
	if err := c.ShouldBindJSON(&b); err != nil {
		respond.Error(c, errs.ErrBadRequest.WithMessage("请求内容格式错误"))
		return
	}
	err := h.Ads.Record(c.Request.Context(), dom.AdEvent{AdID: b.AdID, Kind: dom.AdEventKind(b.Kind), DeviceKey: deviceKey(c)})
	if err != nil {
		respond.Error(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// deviceKey 返回广告频控与事件去重用的设备标识摘要（docs/specs/A3-4.md 假设 1）：优先用客户端生成并存在本机的
// X-Device-Id（UUID）；没有或格式不对时退化为「IP 网段 + UA 摘要」（同网段、同浏览器的人会被当作同一台设备）。
// 游客身份（S1-10）上线后改用游客令牌中的设备 ID。只输出摘要，Valkey 里不出现设备 ID 或 IP。
func deviceKey(c *gin.Context) string {
	var src string
	if u, err := uuid.Parse(strings.TrimSpace(c.GetHeader(headerDeviceID))); err == nil {
		src = "id:" + u.String()
	} else {
		src = "ipua:" + privacy.IPPrefix(c.ClientIP()) + ":" + privacy.UADigest(c.Request.UserAgent())
	}
	sum := sha256.Sum256([]byte("xsite-ad-device:" + src))
	return hex.EncodeToString(sum[:12])
}
