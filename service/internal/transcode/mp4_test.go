package transcode

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"
)

func box(typ string, parts ...[]byte) []byte {
	body := bytes.Join(parts, nil)
	out := binary.BigEndian.AppendUint32(nil, uint32(8+len(body)))
	return append(append(out, typ...), body...)
}

// box64 用 64 位长度写盒子头（size=1）。
func box64(typ string, body []byte) []byte {
	out := binary.BigEndian.AppendUint32(nil, 1)
	out = append(out, typ...)
	out = binary.BigEndian.AppendUint64(out, uint64(16+len(body)))
	return append(out, body...)
}

func stsd(entries ...[]byte) []byte {
	hdr := make([]byte, 8)
	binary.BigEndian.PutUint32(hdr[4:], uint32(len(entries)))
	return box("stsd", append([][]byte{hdr}, entries...)...)
}

func trak(entry []byte) []byte {
	return box("trak", box("tkhd", make([]byte, 20)), box("mdia", box("mdhd", make([]byte, 24)),
		box("minf", box("stbl", stsd(entry), box("stts", make([]byte, 8))))))
}

func avc1(profile, compat, level byte) []byte {
	return box("avc1", make([]byte, 78), box("avcC", []byte{1, profile, compat, level, 0xff}), box("pasp", make([]byte, 8)))
}

var mp4a = box("mp4a", make([]byte, 28), box("esds", make([]byte, 20)))

func TestCodecsFromInit(t *testing.T) {
	init := append(box("ftyp", []byte("iso6")), box("moov", box("mvhd", make([]byte, 100)), trak(avc1(0x64, 0, 0x1f)), trak(mp4a),
		box("mvex", box("trex", make([]byte, 24))))...)
	got, err := codecsFromInit(init)
	if err != nil || got != "avc1.64001f,mp4a.40.2" {
		t.Fatalf("得到 %q %v", got, err)
	}

	// 没有音轨；moov 用 64 位长度
	noAudio := box64("moov", trak(avc1(0x64, 0, 0x28)))
	if got, err := codecsFromInit(noAudio); err != nil || got != "avc1.640028" {
		t.Fatalf("得到 %q %v", got, err)
	}

	bad := map[string][]byte{
		"截断":      init[:len(init)-10],
		"没有视频轨":   box("moov", trak(mp4a)),
		"缺少 avcC": box("moov", trak(box("avc1", make([]byte, 78)))),
		"不支持的编码":  box("moov", trak(box("hvc1", make([]byte, 78)))),
		"长度越界":    append(binary.BigEndian.AppendUint32(nil, 9999), "moov"...),
	}
	for name, data := range bad {
		if _, err := codecsFromInit(data); err == nil {
			t.Errorf("%s：应报错", name)
		} else if strings.Contains(err.Error(), "panic") {
			t.Errorf("%s：%v", name, err)
		}
	}
}
