package upload

import (
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	dom "xsite/service/internal/domain/upload"
	"xsite/service/internal/errs"
	"xsite/service/internal/http/respond"
	"xsite/service/internal/store/blob"
)

const (
	// imageKeyPrefix 是允许预览的唯一前缀。放开到别的前缀就等于把私有视频源与 HLS 产物也变成可下载。
	imageKeyPrefix = "img/"
	// imageKeyMaxLen 与 openapi 里媒体字段的长度上限一致。
	imageKeyMaxLen = 512
	// previewMaxBytes 是单张预览图的上限。后台图片经上传管线压缩后远小于此，
	// 这个上限只是防止有人把超大对象塞进 img/ 前缀后由后台代理反复拉取。
	previewMaxBytes = 20 << 20
	// previewCacheControl：图片 key 带 uuid、内容不会变，让浏览器自己缓存一天，
	// 否则列表页每次重渲染都会把所有缩略图重新回源一遍。private 是因为这是带后台会话的响应。
	previewCacheControl = "private, max-age=86400"
)

// PreviewImage 是 GET /api/admin/images/preview?key=：把 img/ 下的对象 key 还原成浏览器能直接显示的图片。
//
// 后台此前只把 key 当文本显示，运营看不到已经保存的封面、配图与 Logo。图片线路（CDN）是面向访客的读取通道，
// 后台不一定配好、也可能带签名，所以这里直接从当前图片主存储读取并同源返回：一个地址适用于所有模块的图片字段。
//
// 权限只要求已登录的后台会话，不绑定某个页面权限点：这些对象本身就是 `public, immutable` 的公开读图片，
// 经 CDN 面向全部访客分发，后台预览不构成额外暴露；而图片字段散落在剧集、广告、公告、静态页、白标等模块，
// 绑定单一权限点会让其他模块的运营看不到自己的图。
func (h *Handlers) PreviewImage(c *gin.Context) {
	key := strings.TrimSpace(c.Query("key"))
	if !validImageKey(key) {
		respond.Error(c, errs.ErrBadRequest.WithMessage("图片路径无效"))
		return
	}
	target, err := h.Stores.Primary(c.Request.Context(), dom.KindImage)
	if err != nil {
		respond.Error(c, err)
		return
	}
	body, info, err := target.Store.Get(c.Request.Context(), key)
	if err != nil {
		if errors.Is(err, blob.ErrNotFound) {
			respond.Error(c, errs.ErrNotFound.WithMessage("图片不存在或已被删除"))
			return
		}
		respond.Error(c, err)
		return
	}
	defer body.Close()

	// 只回图片类型：img/ 下如果混进 HTML 或 SVG，原样回给浏览器就是一个同源的脚本执行点。
	mime := normalizedMIME(info.ContentType)
	if !strings.HasPrefix(mime, "image/") || mime == "image/svg+xml" {
		respond.Error(c, errs.ErrBadRequest.WithMessage("该对象不是可预览的图片"))
		return
	}
	if info.Size > previewMaxBytes {
		respond.Error(c, errs.ErrBadRequest.WithMessage("图片过大，无法预览"))
		return
	}
	c.Header("Cache-Control", previewCacheControl)
	if info.ETag != "" && respond.NotModified(c, info.ETag) {
		return
	}
	c.DataFromReader(http.StatusOK, info.Size, mime, io.LimitReader(body, previewMaxBytes), nil)
}

// validImageKey 只接受 img/ 下的相对路径。拒绝绝对路径、`.`/`..` 路径段、空段、反斜杠与控制字符，
// 避免通过 key 跳出前缀读到私有对象。
func validImageKey(key string) bool {
	if key == "" || len(key) > imageKeyMaxLen || !strings.HasPrefix(key, imageKeyPrefix) {
		return false
	}
	if strings.ContainsAny(key, "\\?#") {
		return false
	}
	for _, r := range key {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	for _, segment := range strings.Split(key, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return false
		}
	}
	return true
}
