package transcode

import "fmt"

// codecsFromMPEGTS 从 MPEG-TS 中首个 H.264 SPS 读取 RFC 6381 编码标识。
// 转码时要求 x264 在每个独立切片重复 SPS/PPS，因此不需要依赖 fMP4 init 段；音频由流水线固定编码为 AAC-LC。
func codecsFromMPEGTS(data []byte, hasAudio bool) (string, error) {
	for i := 0; i < len(data); i++ {
		start := -1
		switch {
		case i+4 < len(data) && data[i] == 0 && data[i+1] == 0 && data[i+2] == 0 && data[i+3] == 1:
			start = i + 4
		case i+3 < len(data) && data[i] == 0 && data[i+1] == 0 && data[i+2] == 1:
			start = i + 3
		}
		if start < 0 {
			continue
		}
		if start+3 >= len(data) || data[start]&0x1f != 7 {
			i = start
			continue
		}
		profile, compatibility, level := data[start+1], data[start+2], data[start+3]
		if !knownH264Profile(profile) || level == 0 {
			i = start
			continue
		}
		out := fmt.Sprintf("avc1.%02x%02x%02x", profile, compatibility, level)
		if hasAudio {
			out += ",mp4a.40.2"
		}
		return out, nil
	}
	return "", fmt.Errorf("MPEG-TS 切片缺少 H.264 SPS")
}

func knownH264Profile(profile byte) bool {
	switch profile {
	case 66, 77, 88, 100, 110, 122, 244:
		return true
	default:
		return false
	}
}
