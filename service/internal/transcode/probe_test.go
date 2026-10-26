package transcode

import (
	"strings"
	"testing"
)

func TestParseProbe(t *testing.T) {
	tests := []struct {
		name string
		json string
		want SourceInfo
	}{
		{
			name: "竖屏 1080p30 带立体声",
			json: `{"streams":[{"codec_type":"video","codec_name":"h264","width":1080,"height":1920,"sample_aspect_ratio":"1:1",
				"avg_frame_rate":"30/1","r_frame_rate":"30/1","duration":"60.0"},{"codec_type":"audio","codec_name":"aac","channels":2}],
				"format":{"duration":"60.023","bit_rate":"6100000"}}`,
			want: SourceInfo{Width: 1080, Height: 1920, FPSNum: 30, FPSDen: 1, Duration: 60.023, VideoCodec: "h264", HasAudio: true, AudioChannels: 2, BitRate: 6100000},
		},
		{
			name: "手机竖拍：Display Matrix 逆时针 -90 度，显示尺寸互换",
			json: `{"streams":[{"codec_type":"video","codec_name":"hevc","width":1920,"height":1080,"avg_frame_rate":"30000/1001",
				"side_data_list":[{"side_data_type":"Display Matrix","rotation":-90}]}],"format":{"duration":"10"}}`,
			want: SourceInfo{Width: 1080, Height: 1920, FPSNum: 30000, FPSDen: 1001, Duration: 10, VideoCodec: "hevc", Rotation: 90},
		},
		{
			name: "FFmpeg 5.1 的 ffprobe：DisplayMatrix（无空格），旋转 90 度",
			json: `{"streams":[{"codec_type":"video","codec_name":"h264","width":1920,"height":1080,"avg_frame_rate":"30/1",
				"side_data_list":[{"side_data_type":"DisplayMatrix","displaymatrix":"...","rotation":90}]}],"format":{"duration":"10"}}`,
			want: SourceInfo{Width: 1080, Height: 1920, FPSNum: 30, FPSDen: 1, Duration: 10, VideoCodec: "h264", Rotation: 270},
		},
		{
			name: "旧文件的 rotate 标签",
			json: `{"streams":[{"codec_type":"video","codec_name":"h264","width":1280,"height":720,"avg_frame_rate":"25/1","tags":{"rotate":"270"}}],
				"format":{"duration":"5"}}`,
			want: SourceInfo{Width: 720, Height: 1280, FPSNum: 25, FPSDen: 1, Duration: 5, VideoCodec: "h264", Rotation: 270},
		},
		{
			name: "非方形像素按 SAR 换算显示宽度",
			json: `{"streams":[{"codec_type":"video","codec_name":"mpeg2video","width":720,"height":576,"sample_aspect_ratio":"16:15","avg_frame_rate":"25/1"}],
				"format":{"duration":"3"}}`,
			want: SourceInfo{Width: 768, Height: 576, FPSNum: 25, FPSDen: 1, Duration: 3, VideoCodec: "mpeg2video"},
		},
		{
			name: "跳过封面图（attached_pic），平均帧率异常时用 r_frame_rate，单声道",
			json: `{"streams":[{"codec_type":"video","codec_name":"mjpeg","width":600,"height":600,"disposition":{"attached_pic":1}},
				{"codec_type":"video","codec_name":"h264","width":720,"height":1280,"avg_frame_rate":"0/0","r_frame_rate":"24/1","duration":"8.5"},
				{"codec_type":"audio","channels":1}],"format":{}}`,
			want: SourceInfo{Width: 720, Height: 1280, FPSNum: 24, FPSDen: 1, Duration: 8.5, VideoCodec: "h264", HasAudio: true, AudioChannels: 1},
		},
		{
			name: "帧率都不可用时为 0（保持源时间戳）",
			json: `{"streams":[{"codec_type":"video","codec_name":"h264","width":720,"height":1280,"avg_frame_rate":"1000/1","r_frame_rate":"0/0"}],
				"format":{"duration":"8"}}`,
			want: SourceInfo{Width: 720, Height: 1280, Duration: 8, VideoCodec: "h264"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseProbe([]byte(tt.json))
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("\n得到 %+v\n期望 %+v", got, tt.want)
			}
		})
	}
}

func TestParseProbe_Rejects(t *testing.T) {
	tests := []struct{ name, json, want string }{
		{"没有视频轨", `{"streams":[{"codec_type":"audio"}],"format":{"duration":"3"}}`, "没有视频轨"},
		{"只有封面图", `{"streams":[{"codec_type":"video","width":600,"height":600,"disposition":{"attached_pic":1}}],"format":{"duration":"3"}}`, "没有视频轨"},
		{"读不到时长", `{"streams":[{"codec_type":"video","width":720,"height":1280}],"format":{}}`, "时长"},
		{"超过 4 小时", `{"streams":[{"codec_type":"video","width":720,"height":1280}],"format":{"duration":"14401"}}`, "超过上限"},
		{"分辨率太小", `{"streams":[{"codec_type":"video","width":32,"height":48}],"format":{"duration":"3"}}`, "分辨率"},
		{"分辨率太大", `{"streams":[{"codec_type":"video","width":4320,"height":8000}],"format":{"duration":"3"}}`, "分辨率"},
		{"不是 JSON", `not json`, "ffprobe"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := parseProbe([]byte(tt.json)); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("应报错并包含 %q，得到 %v", tt.want, err)
			}
		})
	}
}
