package s3probe

import (
	"testing"

	"xsite/service/internal/store/blob"
)

func TestPutOptionsForMPEGTSSegment(t *testing.T) {
	opt := putOptionsFor("video/episodes/42/job/s/segment.ts", blob.ObjectInfo{})
	if opt.ContentType != "video/mp2t" || opt.CacheControl != immutableCache {
		t.Fatalf("MPEG-TS 副本响应头 = %+v", opt)
	}
}
