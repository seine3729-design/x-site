package config

import (
	"fmt"
	"net/url"
	"strings"
	"time"
)

// Image 是后台图片上传的服务端优化配置（docs/03 第 9.6a 节）。
//
// 浏览器里用 canvas 重新编码的结果对已经优化过的 JPEG 常常比原图更大（canvas 先解码再有损编码，
// 两次有损叠加而体积不降），所以压缩交给外部图片优化服务做。留空时接口回「未配置」，浏览器退回本地压缩。
type Image struct {
	// TinifyAPIKey 是 Tinify（TinyPNG / TinyJPG）的 API Key，只在服务端使用，不下发浏览器。
	TinifyAPIKey string `env:"IMAGE_TINIFY_API_KEY"`
	// TinifyEndpoint 是 Tinify 服务地址。只在本地联调时才指向替身服务；返回的结果地址必须与它同源。
	TinifyEndpoint string `env:"IMAGE_TINIFY_ENDPOINT" envDefault:"https://api.tinify.com"`
	// MaxSide 是压缩结果的最长边（像素），超过时按比例缩小。
	MaxSide int `env:"IMAGE_MAX_SIDE" envDefault:"2400"`
	// OptimizeTimeout 是一次优化（压缩 + 转 WebP + 取回）的总时限。
	OptimizeTimeout time.Duration `env:"IMAGE_OPTIMIZE_TIMEOUT" envDefault:"90s"`
}

// OptimizeEnabled 表示服务端图片优化是否可用。
func (i Image) OptimizeEnabled() bool { return strings.TrimSpace(i.TinifyAPIKey) != "" }

// Validate 校验图片优化配置；没有配置 API Key 时只校验取值范围，不阻止启动。
func (i Image) Validate(env string) error {
	var problems []string
	endpoint := strings.TrimSpace(i.TinifyEndpoint)
	if endpoint == "" {
		problems = append(problems, "IMAGE_TINIFY_ENDPOINT 不能为空")
	} else {
		u, err := url.Parse(endpoint)
		switch {
		case err != nil || u.Host == "":
			problems = append(problems, "IMAGE_TINIFY_ENDPOINT 不是合法地址")
		case u.Scheme != "https" && !(u.Scheme == "http" && env != "production"):
			problems = append(problems, "IMAGE_TINIFY_ENDPOINT 必须是 https（production 不允许 http）")
		case u.RawQuery != "" || u.Fragment != "":
			problems = append(problems, "IMAGE_TINIFY_ENDPOINT 不能带查询串或片段")
		}
	}
	if i.MaxSide < 320 || i.MaxSide > 8192 {
		problems = append(problems, "IMAGE_MAX_SIDE 必须在 320 到 8192 之间")
	}
	if i.OptimizeTimeout < 10*time.Second || i.OptimizeTimeout > 10*time.Minute {
		problems = append(problems, "IMAGE_OPTIMIZE_TIMEOUT 必须在 10s 到 10m 之间")
	}
	if len(problems) > 0 {
		return fmt.Errorf("图片优化配置校验失败: %s", strings.Join(problems, "；"))
	}
	return nil
}
