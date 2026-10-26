package play

import (
	"errors"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

// 清单改写（S1-8，docs/specs/S1-8.md）：主清单列出各档 v{短边}.m3u8；新 MPEG-TS 产物的切片位于
// s/ 下按内容哈希命名的 .ts 文件，历史 fMP4 产物保留可选 init 段与 .m4s 文件。每个切片前的 EXT-X-KEY
// 地址是占位符 xsite-key:{key_ver}。返回给播放器前把切片（及历史 init 段）换成线路上的签名地址，
// 把占位符换成带令牌的密钥接口地址，试看令牌只保留前 N 秒。
//
// 解析是白名单式的：地址只认上面这几种形态，带 URI 属性的其他标签（音轨分组、I 帧清单、低延迟分片等）一律拒绝，
// 不把产物目录里的任意内容或外部地址透传给播放器.

const (
	// IndexFile 是主清单在接口中的文件名（选源返回的地址），对应产物里的 master.m3u8。
	IndexFile  = "index.m3u8"
	masterFile = "master.m3u8"
	// segmentDir 是产物目录里 init 段与切片所在的子目录（transcode.Pipeline 写入），目录前缀签名覆盖它。
	segmentDir = "s/"
	// keyScheme 是清单里内容密钥地址的占位符前缀（transcode.KeyURI）。
	keyScheme = "xsite-key:"
	// trialEpsilon 吸收切片时长累加的浮点误差：结束时间恰好等于试看秒数的切片要保留。
	trialEpsilon = 1e-3
)

var (
	// variantNameRe 是主清单里各档清单的地址：同目录的 v{短边}.m3u8（transcode.Rendition.Name）。
	variantNameRe = regexp.MustCompile(`^v[0-9]{1,5}\.m3u8$`)
	// segmentURIRe 是历史 init 段与新旧切片的地址：s/ 下的单个文件（内容哈希 + .mp4 / .m4s / .ts），不含 .. 或多级目录。
	segmentURIRe = regexp.MustCompile(`^s/[A-Za-z0-9_-]{1,128}\.(?:mp4|m4s|ts)$`)
	// attrNameRe 是属性名（RFC 8216 第 4.2 节：大写字母、数字与连字符）。
	attrNameRe = regexp.MustCompile(`^[A-Z0-9-]+$`)
)

// errBadPlaylist 标记产物清单不是预期的形态（产物损坏或格式变化），调用方按产物不可用处理。
var errBadPlaylist = errors.New("产物清单格式不受支持")

func badPlaylist(format string, args ...any) error {
	return fmt.Errorf("%w: %s", errBadPlaylist, fmt.Sprintf(format, args...))
}

// playlistLines 按行切分清单：去掉行首尾空白与空行，第一行必须是 #EXTM3U。注释（# 开头但不是 #EXT）丢弃。
func playlistLines(data []byte) ([]string, error) {
	var out []string
	for raw := range strings.SplitSeq(string(data), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		if len(out) == 0 {
			if line != "#EXTM3U" {
				return nil, badPlaylist("缺少 #EXTM3U")
			}
			out = append(out, line)
			continue
		}
		if strings.HasPrefix(line, "#") && !strings.HasPrefix(line, "#EXT") {
			continue
		}
		out = append(out, line)
	}
	if len(out) == 0 {
		return nil, badPlaylist("清单为空")
	}
	return out, nil
}

// hasURIAttr 表示标签带地址类属性（URI、SERVER-URI 等）：这类标签需要改写，未支持的一律拒绝。
func hasURIAttr(line string) bool { return strings.Contains(line, "URI=") }

// tagName 是标签名（冒号之前），用于错误信息。
func tagName(line string) string {
	name, _, _ := strings.Cut(line, ":")
	return name
}

// parseMaster 校验主清单并返回各档清单的文件名（按出现顺序）。主清单本身原样返回给播放器：
// 各档地址是相对地址，播放器按清单接口的地址解析后同样经清单接口获取。
func parseMaster(data []byte) ([]string, error) {
	lines, err := playlistLines(data)
	if err != nil {
		return nil, err
	}
	var variants []string
	expectURI := false
	for _, l := range lines[1:] {
		switch {
		case strings.HasPrefix(l, "#EXT-X-STREAM-INF:"):
			if expectURI {
				return nil, badPlaylist("EXT-X-STREAM-INF 后缺少档位地址")
			}
			expectURI = true
		case strings.HasPrefix(l, "#"):
			if hasURIAttr(l) {
				return nil, badPlaylist("主清单含不支持的标签 %s", tagName(l))
			}
		default:
			if !expectURI {
				return nil, badPlaylist("档位地址 %q 前缺少 EXT-X-STREAM-INF", l)
			}
			if !variantNameRe.MatchString(l) {
				return nil, badPlaylist("档位地址 %q 不是同目录的 v{短边}.m3u8", l)
			}
			variants = append(variants, l)
			expectURI = false
		}
	}
	if expectURI {
		return nil, badPlaylist("最后一个 EXT-X-STREAM-INF 后缺少档位地址")
	}
	if len(variants) == 0 {
		return nil, badPlaylist("主清单没有档位")
	}
	return variants, nil
}

// ---------- 各档清单 ----------

type lineKind int

const (
	kindTag lineKind = iota // 原样输出的标签（含 EXTINF）
	kindMap                 // #EXT-X-MAP：URI 换成线路地址
	kindKey                 // #EXT-X-KEY：AES-128 的 URI 换成密钥接口地址
	kindURI                 // 切片地址：换成线路地址
)

// attr 是属性列表中的一项；value 保留原文（带引号的字符串含引号）。
type attr struct {
	name, value string
}

// plLine 是解析后的一行。
type plLine struct {
	kind   lineKind
	text   string  // kindTag 的原文
	uri    string  // kindMap / kindURI：产物目录内的相对地址
	attrs  []attr  // kindMap / kindKey 的属性（按原顺序）
	keyVer int32   // kindKey：密钥版本；0 表示 METHOD=NONE（原样输出）
	dur    float64 // EXTINF 行的切片时长
}

// mediaPlaylist 是解析后的一档清单。lines 不含 #EXTM3U 与 #EXT-X-ENDLIST（输出时补上）。
type mediaPlaylist struct {
	lines    []plLine
	segments int
}

// parseMedia 解析并校验一档清单。
func parseMedia(data []byte) (mediaPlaylist, error) {
	lines, err := playlistLines(data)
	if err != nil {
		return mediaPlaylist{}, err
	}
	var pl mediaPlaylist
	ended, pending := false, false
	for _, l := range lines[1:] {
		if ended {
			return mediaPlaylist{}, badPlaylist("EXT-X-ENDLIST 之后还有内容")
		}
		switch {
		case l == "#EXT-X-ENDLIST":
			ended = true
		case strings.HasPrefix(l, "#EXT-X-MAP:"):
			attrs, err := parseAttrs(strings.TrimPrefix(l, "#EXT-X-MAP:"))
			if err != nil {
				return mediaPlaylist{}, err
			}
			uri, ok := attrValue(attrs, "URI")
			if !ok || !segmentURIRe.MatchString(uri) {
				return mediaPlaylist{}, badPlaylist("init 段地址 %q 不是产物目录 s/ 下的文件", uri)
			}
			pl.lines = append(pl.lines, plLine{kind: kindMap, uri: uri, attrs: attrs})
		case strings.HasPrefix(l, "#EXT-X-KEY:"):
			kl, err := parseKey(strings.TrimPrefix(l, "#EXT-X-KEY:"))
			if err != nil {
				return mediaPlaylist{}, err
			}
			pl.lines = append(pl.lines, kl)
		case strings.HasPrefix(l, "#EXTINF:"):
			v, _, _ := strings.Cut(strings.TrimPrefix(l, "#EXTINF:"), ",")
			d, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
			if err != nil || d <= 0 || math.IsInf(d, 0) || math.IsNaN(d) {
				return mediaPlaylist{}, badPlaylist("切片时长 %q 无效", v)
			}
			if pending {
				return mediaPlaylist{}, badPlaylist("连续两个 EXTINF 之间缺少切片地址")
			}
			pending = true
			pl.lines = append(pl.lines, plLine{kind: kindTag, text: l, dur: d})
		case strings.HasPrefix(l, "#"):
			if hasURIAttr(l) {
				return mediaPlaylist{}, badPlaylist("清单含不支持的标签 %s", tagName(l))
			}
			pl.lines = append(pl.lines, plLine{kind: kindTag, text: l})
		default:
			if !pending {
				return mediaPlaylist{}, badPlaylist("切片 %q 前缺少 EXTINF", l)
			}
			if !segmentURIRe.MatchString(l) {
				return mediaPlaylist{}, badPlaylist("切片地址 %q 不是产物目录 s/ 下的文件", l)
			}
			pending = false
			pl.segments++
			pl.lines = append(pl.lines, plLine{kind: kindURI, uri: l})
		}
	}
	switch {
	case pending:
		return mediaPlaylist{}, badPlaylist("最后一个 EXTINF 后缺少切片地址")
	case pl.segments == 0:
		return mediaPlaylist{}, badPlaylist("清单没有切片")
	case !ended:
		return mediaPlaylist{}, badPlaylist("清单缺少 EXT-X-ENDLIST（不是点播清单）")
	}
	return pl, nil
}

// parseKey 解析 EXT-X-KEY：METHOD=NONE 原样保留；AES-128 的地址必须是占位符 xsite-key:{key_ver}。
func parseKey(list string) (plLine, error) {
	attrs, err := parseAttrs(list)
	if err != nil {
		return plLine{}, err
	}
	method, _ := attrValue(attrs, "METHOD")
	uri, hasURI := attrValue(attrs, "URI")
	switch method {
	case "NONE":
		if hasURI {
			return plLine{}, badPlaylist("METHOD=NONE 的 EXT-X-KEY 不应带 URI")
		}
		return plLine{kind: kindKey, attrs: attrs}, nil
	case "AES-128":
	default:
		return plLine{}, badPlaylist("不支持的加密方式 %q", method)
	}
	if f, ok := attrValue(attrs, "KEYFORMAT"); ok && f != "identity" {
		return plLine{}, badPlaylist("不支持的密钥格式 %q", f)
	}
	ver, ok := parseKeyPlaceholder(uri)
	if !hasURI || !ok {
		return plLine{}, badPlaylist("密钥地址 %q 不是占位符 %s{key_ver}", uri, keyScheme)
	}
	return plLine{kind: kindKey, attrs: attrs, keyVer: ver}, nil
}

// parseKeyPlaceholder 解析 xsite-key:{key_ver}：版本是没有前导零与符号的正 int32。
func parseKeyPlaceholder(uri string) (int32, bool) {
	s, ok := strings.CutPrefix(uri, keyScheme)
	if !ok || s == "" || s[0] < '1' || s[0] > '9' {
		return 0, false
	}
	v, err := strconv.ParseInt(s, 10, 32)
	if err != nil || v <= 0 {
		return 0, false
	}
	return int32(v), true
}

// parseAttrs 解析属性列表（RFC 8216 第 4.2 节）：NAME=VALUE 以逗号分隔，带引号的字符串里可以有逗号。
func parseAttrs(list string) ([]attr, error) {
	var out []attr
	for {
		name, rest, ok := strings.Cut(list, "=")
		if !ok || !attrNameRe.MatchString(name) {
			return nil, badPlaylist("属性列表格式错误")
		}
		var value string
		if strings.HasPrefix(rest, `"`) {
			end := strings.IndexByte(rest[1:], '"')
			if end < 0 {
				return nil, badPlaylist("属性 %s 的引号未闭合", name)
			}
			value, rest = rest[:end+2], rest[end+2:]
		} else if i := strings.IndexByte(rest, ','); i >= 0 {
			value, rest = rest[:i], rest[i:]
		} else {
			value, rest = rest, ""
		}
		out = append(out, attr{name: name, value: value})
		if rest == "" {
			return out, nil
		}
		if rest[0] != ',' || len(rest) == 1 {
			return nil, badPlaylist("属性 %s 之后的分隔符错误", name)
		}
		list = rest[1:]
	}
}

// attrValue 取属性值（去掉引号）。
func attrValue(attrs []attr, name string) (string, bool) {
	for _, a := range attrs {
		if a.name == name {
			return strings.Trim(a.value, `"`), true
		}
	}
	return "", false
}

// trialCut 返回要保留的切片数：试看只保留结束时间不超过 trialSec 的切片（至少保留第一个），trialSec 为 0 表示完整播放。
// 宁可早结束一点（不超过一个切片时长），也不让试看之外的内容出现在清单里。
func (pl mediaPlaylist) trialCut(trialSec int32) int {
	if trialSec <= 0 {
		return pl.segments
	}
	limit := float64(trialSec) + trialEpsilon
	var end float64
	kept := 0
	for _, l := range pl.lines {
		switch l.kind {
		case kindTag:
			end += l.dur // 只有 EXTINF 行的 dur 非零
		case kindURI:
			if kept > 0 && end > limit {
				return kept
			}
			kept++
		}
	}
	return kept
}

// objectURIs 返回前 keep 个切片（及其之前的 init 段）的相对地址，按出现顺序去重：这些是要签名的对象。
// 与 render 的截断方式一致。
func (pl mediaPlaylist) objectURIs(keep int) []string {
	seen := map[string]bool{}
	var out []string
	n := 0
	for _, l := range pl.lines {
		if n == keep {
			break
		}
		if l.kind != kindMap && l.kind != kindURI {
			continue
		}
		if !seen[l.uri] {
			seen[l.uri] = true
			out = append(out, l.uri)
		}
		if l.kind == kindURI {
			n++
		}
	}
	return out
}

// render 输出改写后的清单：保留前 keep 个切片（之后的标签都属于被裁掉的切片，一并去掉），
// init 段与切片换成 objectURL 给出的地址，AES-128 的密钥地址换成 keyURL 给出的地址（IV 等其他属性不变），
// 末尾补 #EXT-X-ENDLIST。
func (pl mediaPlaylist) render(keep int, objectURL func(uri string) string, keyURL func(ver int32) string) ([]byte, error) {
	var b strings.Builder
	b.WriteString("#EXTM3U\n")
	n := 0
	for _, l := range pl.lines {
		if n == keep {
			break
		}
		switch l.kind {
		case kindTag:
			b.WriteString(l.text)
		case kindMap:
			u := objectURL(l.uri)
			if u == "" {
				return nil, fmt.Errorf("init 段 %s 没有线路地址", l.uri)
			}
			if err := writeAttrTag(&b, "#EXT-X-MAP:", l.attrs, u); err != nil {
				return nil, err
			}
		case kindKey:
			u := ""
			if l.keyVer > 0 { // METHOD=NONE 原样输出
				if u = keyURL(l.keyVer); u == "" {
					return nil, fmt.Errorf("密钥版本 %d 没有地址", l.keyVer)
				}
			}
			if err := writeAttrTag(&b, "#EXT-X-KEY:", l.attrs, u); err != nil {
				return nil, err
			}
		case kindURI:
			u := objectURL(l.uri)
			if u == "" || strings.ContainsAny(u, "\r\n") {
				return nil, fmt.Errorf("切片 %s 的线路地址无效", l.uri)
			}
			b.WriteString(u)
			n++
		}
		b.WriteByte('\n')
	}
	b.WriteString("#EXT-X-ENDLIST\n")
	return []byte(b.String()), nil
}

// writeAttrTag 输出带属性列表的标签；uri 非空时替换 URI 属性的值（引号字符串，HLS 不支持转义）。
func writeAttrTag(b *strings.Builder, prefix string, attrs []attr, uri string) error {
	replace := uri != ""
	if replace && strings.ContainsAny(uri, "\"\r\n") {
		return fmt.Errorf("地址不能放进带引号的属性: %q", uri)
	}
	b.WriteString(prefix)
	for i, a := range attrs {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(a.name)
		b.WriteByte('=')
		if a.name == "URI" && replace {
			b.WriteString(`"` + uri + `"`)
			continue
		}
		b.WriteString(a.value)
	}
	return nil
}
