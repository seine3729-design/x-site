package upload

import (
	"context"
	"strings"
	"testing"
	"time"

	"uuid"
)

func (f *fakeRepo) SetMultipart(_ context.Context, _ uuid.UUID, _ int64, uploadID string) error {
	f.asset.MultipartUploadID = uploadID
	return nil
}

func (f *fakeRepo) TouchPart(_ context.Context, _ uuid.UUID, _ int64, expiresAt time.Time) error {
	f.asset.Status, f.asset.ExpiresAt = StatusUploading, expiresAt
	return nil
}

// 测试里的「现在」与 newServiceForTest 一致。
var multipartNow = time.Date(2026, 10, 25, 0, 0, 0, 0, time.UTC)

func multipartAsset(size int64) Asset {
	return Asset{
		ID: uuid.NewV7(), AdminID: 7, ProviderID: 1, Purpose: PurposeEpisodeVideo, Kind: KindVideo, MIME: "video/mp4",
		SizeBytes: size, PartSize: VideoPartSize, MultipartUploadID: "mp-1", Status: StatusCreated,
		ExpiresAt: multipartNow.Add(time.Minute),
	}
}

func TestInitMultipartOnlyForVideo(t *testing.T) {
	repo := &fakeRepo{}
	s := newServiceForTest(repo)
	video, err := s.Init(context.Background(), 7, InitInput{
		ProviderID: 1, ProviderBucket: "videos", Purpose: PurposeEpisodeVideo, MIME: "video/mp4",
		SizeBytes: 30 << 20, EpisodeID: 11, Multipart: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !video.Multipart() || video.PartSize != VideoPartSize || video.PartCount() != 6 {
		t.Fatalf("30 MiB 视频应按 5 MiB 切成 6 片：%+v", video)
	}
	plain, err := s.Init(context.Background(), 7, InitInput{
		ProviderID: 1, ProviderBucket: "videos", Purpose: PurposeAdVideo, MIME: "video/mp4", SizeBytes: 1 << 20,
	})
	if err != nil || plain.Multipart() {
		t.Fatalf("不声明分片时保持整段上传：%+v err=%v", plain, err)
	}
	if _, err := s.Init(context.Background(), 7, InitInput{
		ProviderID: 1, ProviderBucket: "images", Purpose: PurposeDramaCover, MIME: "image/webp", ImageFormat: FormatWebP,
		SizeBytes: 1024, Multipart: true,
	}); err == nil {
		t.Fatal("图片不应接受分片上传")
	}
}

func TestPartLayout(t *testing.T) {
	part := VideoPartSize
	for _, tc := range []struct {
		name  string
		size  int64
		count int32
		last  int64
	}{
		{"不足一片", 3 << 20, 1, 3 << 20},
		{"恰好一片", part, 1, part},
		{"整片倍数", 3 * part, 3, part},
		{"有余数", 2*part + 7, 3, 7},
		{"4 GiB 上限", videoMaxBytes, 820, videoMaxBytes - 819*part},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := Asset{SizeBytes: tc.size, PartSize: part}
			if got := a.PartCount(); got != tc.count {
				t.Fatalf("PartCount() = %d，want %d", got, tc.count)
			}
			if got := a.PartLength(tc.count); got != tc.last {
				t.Fatalf("最后一片 = %d，want %d", got, tc.last)
			}
			var sum int64
			for n := int32(1); n <= tc.count; n++ {
				sum += a.PartLength(n)
			}
			if sum != tc.size {
				t.Fatalf("各片之和 %d 不等于文件大小 %d", sum, tc.size)
			}
			if a.PartLength(0) != 0 || a.PartLength(tc.count+1) != 0 {
				t.Fatal("越界片号应返回 0")
			}
		})
	}
	if (Asset{SizeBytes: 100}).PartCount() != 0 {
		t.Fatal("整段上传的会话没有分片")
	}
}

func TestClaimPartValidatesAndSlidesExpiry(t *testing.T) {
	size := 2*VideoPartSize + 100
	for _, tc := range []struct {
		name   string
		mutate func(*Asset)
		number int32
		length int64
		want   string // 为空表示应通过
	}{
		{"第一片", nil, 1, VideoPartSize, ""},
		{"最后一片按余数", nil, 3, 100, ""},
		{"上传中可以继续传", func(a *Asset) { a.Status = StatusUploading }, 2, VideoPartSize, ""},
		{"片号为 0", nil, 0, VideoPartSize, "分片序号"},
		{"片号越界", nil, 4, 100, "分片序号"},
		{"长度不符", nil, 1, VideoPartSize - 1, "应为"},
		{"最后一片按整片提交", nil, 3, VideoPartSize, "应为"},
		{"整段上传的会话", func(a *Asset) { a.PartSize = 0 }, 1, VideoPartSize, "不是分片上传"},
		{"还没登记分片上传", func(a *Asset) { a.MultipartUploadID = "" }, 1, VideoPartSize, "不是分片上传"},
		{"已经上传完成", func(a *Asset) { a.Status = StatusUploaded }, 1, VideoPartSize, "已经结束"},
		{"已过期", func(a *Asset) { a.ExpiresAt = multipartNow.Add(-time.Second) }, 1, VideoPartSize, "过期"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := multipartAsset(size)
			if tc.mutate != nil {
				tc.mutate(&a)
			}
			repo := &fakeRepo{asset: a}
			s := newServiceForTest(repo)
			got, err := s.ClaimPart(context.Background(), a.ID, 7, tc.number, tc.length)
			if tc.want != "" {
				if err == nil || !strings.Contains(err.Error(), tc.want) {
					t.Fatalf("应拒绝并提示「%s」，得到 %v", tc.want, err)
				}
				if repo.asset.ExpiresAt != a.ExpiresAt {
					t.Fatal("被拒绝的分片不应顺延会话")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			wantExpiry := multipartNow.Add(sessionTTL)
			if got.Status != StatusUploading || !got.ExpiresAt.Equal(wantExpiry) || !repo.asset.ExpiresAt.Equal(wantExpiry) {
				t.Fatalf("通过后应为 uploading 并从现在起顺延 %s：%+v", sessionTTL, got)
			}
		})
	}
	other := multipartAsset(size)
	other.AdminID = 8
	if _, err := newServiceForTest(&fakeRepo{asset: other}).ClaimPart(context.Background(), other.ID, 7, 1, VideoPartSize); err == nil {
		t.Fatal("不能往其他管理员的会话里写分片")
	}
}

func TestCheckPartsDetectsMissingExtraAndWrongLength(t *testing.T) {
	a := multipartAsset(2*VideoPartSize + 100)
	full := []UploadedPart{{1, VideoPartSize}, {2, VideoPartSize}, {3, 100}}
	if err := a.CheckParts(full); err != nil {
		t.Fatalf("齐全的分片应通过：%v", err)
	}
	for _, tc := range []struct {
		name  string
		parts []UploadedPart
		want  string
	}{
		{"缺中间一片", []UploadedPart{{1, VideoPartSize}, {3, 100}}, "还有 1 片没有上传完成（第 2 片）"},
		{"一片都没有", nil, "还有 3 片"},
		{"多出一片", append(full, UploadedPart{4, 1}), "越界"},
		{"长度不符", []UploadedPart{{1, VideoPartSize}, {2, VideoPartSize - 1}, {3, 100}}, "第 2 片的长度"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := a.CheckParts(tc.parts); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("应提示「%s」，得到 %v", tc.want, err)
			}
		})
	}
	many := multipartAsset(9 * VideoPartSize)
	if err := many.CheckParts([]UploadedPart{{1, VideoPartSize}}); err == nil || !strings.Contains(err.Error(), "第 2、3、4、5、6、… 片") {
		t.Fatalf("缺片很多时只列前 5 个片号：%v", err)
	}
}

// 分片会话不能再走整段 PUT：两条路径写同一个暂存 key，合并时会互相覆盖。
func TestClaimWriteRejectsMultipartSession(t *testing.T) {
	a := multipartAsset(VideoPartSize)
	repo := &fakeRepo{asset: a}
	if _, err := newServiceForTest(repo).ClaimWrite(context.Background(), a.ID, 7); err == nil || !strings.Contains(err.Error(), "分片上传") {
		t.Fatalf("分片会话应拒绝整段写入，得到 %v", err)
	}
	if repo.asset.Status != StatusCreated {
		t.Fatalf("被拒绝时不应改状态：%s", repo.asset.Status)
	}
}
