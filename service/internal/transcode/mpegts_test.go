package transcode

import (
	"strings"
	"testing"
)

func TestCodecsFromMPEGTS(t *testing.T) {
	// Annex B SPS：profile_idc=100（High）、compatibility=0、level_idc=31。
	data := []byte{0x47, 0x01, 0x02, 0x03, 0, 0, 0, 1, 0x67, 100, 0, 31, 0xaa}
	got, err := codecsFromMPEGTS(data, true)
	if err != nil || got != "avc1.64001f,mp4a.40.2" {
		t.Fatalf("CODECS = %q, %v", got, err)
	}
	got, err = codecsFromMPEGTS(data, false)
	if err != nil || got != "avc1.64001f" {
		t.Fatalf("无音轨 CODECS = %q, %v", got, err)
	}

	for name, bad := range map[string][]byte{
		"没有 SPS":     {0, 0, 1, 0x65, 100, 0, 31},
		"未知 profile": {0, 0, 1, 0x67, 1, 0, 31},
		"无 level":    {0, 0, 1, 0x67, 100, 0, 0},
	} {
		if _, err := codecsFromMPEGTS(bad, true); err == nil || !strings.Contains(err.Error(), "SPS") {
			t.Errorf("%s：应拒绝，得到 %v", name, err)
		}
	}
}
