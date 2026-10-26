package transcode

import (
	"encoding/binary"
	"fmt"
	"strings"
)

// codecsFromInit 从 fMP4 的 init 段读出主清单 CODECS 属性（RFC 6381），如 "avc1.64001f,mp4a.40.2"。
// 视频取 avcC 中的 profile、兼容标志与 level；音频固定为 AAC-LC（转码时用 -profile:a aac_low）。
func codecsFromInit(data []byte) (string, error) {
	var video, audio []string
	err := walkBoxes(data, func(path, typ string, body []byte) error {
		if path != "moov/trak/mdia/minf/stbl/stsd" {
			return nil
		}
		switch typ {
		case "avc1", "avc3":
			c, err := avcCodec(typ, body)
			if err != nil {
				return err
			}
			video = append(video, c)
		case "mp4a":
			audio = append(audio, "mp4a.40.2")
		default:
			return fmt.Errorf("不支持的编码 %q", typ)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if len(video) != 1 || len(audio) > 1 {
		return "", fmt.Errorf("init 段应有 1 条视频轨、至多 1 条音频轨，得到视频 %d、音频 %d", len(video), len(audio))
	}
	return strings.Join(append(video, audio...), ","), nil
}

// 容器盒子：向下遍历它们的子盒子。stsd 的子盒子是样本描述（前面有 8 字节的版本与条目数）。
var containers = map[string]int{"moov": 0, "trak": 0, "mdia": 0, "minf": 0, "stbl": 0, "stsd": 8}

// walkBoxes 遍历 ISO BMFF 盒子树，对容器（含 stsd）内的每个子盒子回调 (父路径, 类型, 盒子内容)。
func walkBoxes(data []byte, fn func(path, typ string, body []byte) error) error {
	var walk func(buf []byte, path string) error
	walk = func(buf []byte, path string) error {
		for len(buf) > 0 {
			if len(buf) < 8 {
				return fmt.Errorf("盒子头不完整")
			}
			size := uint64(binary.BigEndian.Uint32(buf))
			typ := string(buf[4:8])
			hdr := uint64(8)
			switch size {
			case 0:
				size = uint64(len(buf))
			case 1:
				if len(buf) < 16 {
					return fmt.Errorf("盒子 %s 的 64 位长度不完整", typ)
				}
				size, hdr = binary.BigEndian.Uint64(buf[8:]), 16
			}
			if size < hdr || size > uint64(len(buf)) {
				return fmt.Errorf("盒子 %s 长度 %d 越界", typ, size)
			}
			body := buf[hdr:size]
			if path != "" {
				if err := fn(path, typ, body); err != nil {
					return err
				}
			}
			if skip, ok := containers[typ]; ok {
				if len(body) < skip {
					return fmt.Errorf("盒子 %s 内容不完整", typ)
				}
				child := typ
				if path != "" {
					child = path + "/" + typ
				}
				if err := walk(body[skip:], child); err != nil {
					return err
				}
			}
			buf = buf[size:]
		}
		return nil
	}
	return walk(data, "")
}

// avcCodec 读 VisualSampleEntry（78 字节固定字段）之后的 avcC：配置版本、profile、兼容标志、level。
func avcCodec(typ string, entry []byte) (string, error) {
	const visualFields = 78
	if len(entry) < visualFields {
		return "", fmt.Errorf("%s 样本描述不完整", typ)
	}
	for buf := entry[visualFields:]; len(buf) >= 8; {
		size := int(binary.BigEndian.Uint32(buf))
		if size < 8 || size > len(buf) {
			return "", fmt.Errorf("%s 的子盒子长度 %d 越界", typ, size)
		}
		if string(buf[4:8]) == "avcC" {
			b := buf[8:size]
			if len(b) < 4 || b[0] != 1 {
				return "", fmt.Errorf("avcC 无效")
			}
			return fmt.Sprintf("%s.%02x%02x%02x", typ, b[1], b[2], b[3]), nil
		}
		buf = buf[size:]
	}
	return "", fmt.Errorf("%s 缺少 avcC", typ)
}
