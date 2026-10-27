package upload

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	dom "xsite/service/internal/domain/upload"
	"xsite/service/internal/errs"
	"xsite/service/internal/http/respond"
	"xsite/service/internal/imageopt"
)

// OptimizeMaxBytes 是允许提交给优化服务的原图上限。
// 它比上传会话的 15 MiB 宽：这里收的是没压过的原图，压缩结果才受那个上限约束。
const OptimizeMaxBytes = 25 << 20

// ImageOptimizer 由 app 注入；为 nil 表示没有配置优化服务，接口回 503 让浏览器退回本地压缩。
//
// 装配时注意：别把一个为 nil 的 *imageopt.Optimizer 直接赋给这个字段 ——
// 那会得到「非 nil 接口里装着 nil 指针」，下面的 h.Images == nil 判断失效，真正调用时 panic。
type ImageOptimizer interface {
	Optimize(ctx context.Context, src []byte, srcMIME string) (imageopt.Result, error)
}

// OptimizeImage 是 POST /api/admin/images/optimize?purpose=：把浏览器选的原图压缩并转成 WebP 后回传。
//
// 压缩放在服务端的原因：浏览器 canvas 要先解码再有损编码，对已经优化过的 JPEG 常常既掉画质又涨体积，
// 运营看到的就是「压缩后比原图还大」。外部服务的 API Key 只在服务端，不能下发浏览器，所以必须由后端代理。
//
// 返回的是图片二进制而不是 JSON 包：调用方拿到后仍走原来的 uploads 三步上传，上传管线不变。
func (h *Handlers) OptimizeImage(c *gin.Context) {
	purpose := dom.Purpose(strings.TrimSpace(c.Query("purpose")))
	kind, err := dom.KindForPurpose(purpose)
	if err != nil || kind != dom.KindImage {
		respond.Error(c, errs.ErrBadRequest.WithMessage("不支持的图片用途"))
		return
	}
	// 按用途要求对应模块的修改权限：这个接口会消耗外部服务的付费额度，不能只验登录。
	if _, allowed := requirePurpose(c, purpose); !allowed {
		return
	}
	if h.Images == nil {
		respond.Error(c, errs.ErrUnavailable.WithMessage("图片优化服务未配置，已改用浏览器本地压缩"))
		return
	}
	srcMIME := normalizedMIME(c.GetHeader("Content-Type"))
	if !strings.HasPrefix(srcMIME, "image/") {
		respond.Error(c, errs.ErrBadRequest.WithMessage("请提交图片文件"))
		return
	}
	src, err := io.ReadAll(io.LimitReader(c.Request.Body, OptimizeMaxBytes+1))
	if err != nil {
		respond.Error(c, errs.ErrBadRequest.WithMessage("读取图片失败，请重试"))
		return
	}
	if len(src) == 0 {
		respond.Error(c, errs.ErrBadRequest.WithMessage("图片不能为空"))
		return
	}
	if len(src) > OptimizeMaxBytes {
		respond.Error(c, errs.ErrBadRequest.WithMessage("原图不能超过 25 MiB"))
		return
	}

	result, err := h.Images.Optimize(c.Request.Context(), src, srcMIME)
	if err != nil {
		respond.Error(c, optimizeError(err))
		return
	}
	// 压缩结果反而更大时不要硬塞给调用方：让它自己决定是按原图走还是本地再压一次。
	if result.OutputBytes >= int64(len(src)) && srcMIME == imageopt.OutputMIME {
		respond.Error(c, errs.ErrBadRequest.WithMessage("原图已经是更优的 WebP，无需再压缩"))
		return
	}
	c.Header("Cache-Control", "private, no-store")
	c.Header("X-Image-Input-Bytes", strconv.Itoa(len(src)))
	c.Header("X-Image-Output-Bytes", strconv.FormatInt(result.OutputBytes, 10))
	if result.UsedThisMonth > 0 {
		c.Header("X-Image-Quota-Used", strconv.Itoa(result.UsedThisMonth))
	}
	c.Data(http.StatusOK, result.ContentType, result.Data)
}

// optimizeError 把优化服务的失败映射成后台能看懂的业务错误，不透出外部服务的细节。
func optimizeError(err error) error {
	switch {
	case errors.Is(err, imageopt.ErrNotConfigured):
		return errs.ErrUnavailable.WithMessage("图片优化服务未配置，已改用浏览器本地压缩")
	case errors.Is(err, imageopt.ErrQuotaExceeded):
		return errs.ErrUnavailable.WithMessage("图片优化服务本月额度已用尽，已改用浏览器本地压缩")
	case errors.Is(err, imageopt.ErrUnsupported):
		return errs.ErrBadRequest.WithMessage("这张图片无法识别，请换 JPEG、PNG 或 WebP 原图")
	default:
		return err
	}
}
