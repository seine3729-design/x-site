// Package htmlsan 是富文本与自定义 HTML 的清洗白名单（docs/03 第 7.1 节）：静态页正文、广告与区块的自定义 HTML 共用一套策略，
// 避免两处白名单各自演进。调用方自己判断「清洗后为空」等业务规则并给出对应文案。
package htmlsan

import (
	"strings"

	"github.com/microcosm-cc/bluemonday"
)

// Policy 以 bluemonday 的 UGC 策略为底（常见排版标签、链接、图片、表格；去掉脚本、样式表、iframe、表单、事件属性
// 与 javascript: 等协议），只放行少量排版用的内联样式；站外链接新窗口打开并加 noopener。
// 前台另外只在沙箱 iframe 中渲染自定义 HTML，这里是第二道防线。
var Policy = func() *bluemonday.Policy {
	p := bluemonday.UGCPolicy()
	p.AllowStyles("color", "background-color", "font-size", "font-weight", "font-style", "text-align", "text-decoration",
		"line-height", "width", "max-width", "height", "margin", "padding", "border", "border-radius", "display").Globally()
	p.AddTargetBlankToFullyQualifiedLinks(true)
	return p
}()

// Sanitize 按白名单清洗并去掉首尾空白。全部内容都不合规时返回空串。
func Sanitize(raw string) string { return strings.TrimSpace(Policy.Sanitize(raw)) }
