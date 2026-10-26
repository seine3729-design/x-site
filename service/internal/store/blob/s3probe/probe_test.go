package s3probe

import (
	"context"
	"os"
	"testing"

	"xsite/service/internal/domain/storage"
	"xsite/service/internal/netguard"
)

// 连通性测试走真实的 S3 兼容服务（本地 compose 的 SeaweedFS）：写入 → 读取 → 预签名访问 → 删除全部通过（S1-6a 验收）。
// 在 service 容器中执行（./dev.sh go test）时由 compose 注入 S3_* 环境变量；无 S3 环境时跳过。
func TestProbe_SeaweedFS(t *testing.T) {
	endpoint, ak, sk := os.Getenv("S3_ENDPOINT"), os.Getenv("S3_ACCESS_KEY"), os.Getenv("S3_SECRET_KEY")
	bucket := os.Getenv("S3_BUCKET_MEDIA")
	if endpoint == "" || ak == "" || sk == "" || bucket == "" {
		t.Skip("无 S3_ENDPOINT/S3_ACCESS_KEY/S3_SECRET_KEY/S3_BUCKET_MEDIA，跳过连通性集成测试")
	}
	// SeaweedFS 在 compose 内网，开发环境允许内网出站
	p := &Prober{Egress: netguard.Policy{AllowPrivate: true}}
	res := p.Probe(context.Background(), storage.ProbeRequest{
		Type: storage.TypeCustom, Subtype: storage.SubtypeS3Compatible,
		Endpoint: endpoint, Region: "us-east-1", Bucket: bucket, PathStyle: true,
		PathPrefix: "_conntest", TTL: 600,
		Write: storage.Credential{AccessKeyID: ak, SecretAccessKey: sk},
		Read:  storage.Credential{AccessKeyID: ak, SecretAccessKey: sk},
	})
	if !res.OK {
		t.Fatalf("SeaweedFS 连通性测试应全部通过，结果：%+v", res.Steps)
	}
	// 自定义端点：resolve + put + get + presign + delete 共 5 步
	if len(res.Steps) != 5 {
		t.Errorf("应有 5 个步骤，得到 %d：%+v", len(res.Steps), res.Steps)
	}
	for _, s := range res.Steps {
		if !s.OK {
			t.Errorf("步骤 %s 失败：%s", s.Name, s.Message)
		}
	}
}

// 内网地址在禁止内网出站时应在 resolve 步被拦下（SSRF 防护）。
func TestProbe_BlocksPrivateWhenDisallowed(t *testing.T) {
	p := &Prober{Egress: netguard.Policy{AllowPrivate: false}}
	res := p.Probe(context.Background(), storage.ProbeRequest{
		Type: storage.TypeCustom, Subtype: storage.SubtypeS3Compatible,
		Endpoint: "http://10.1.2.3:9000", Region: "us-east-1", Bucket: "xsite-media", PathStyle: true, TTL: 600,
		Write: storage.Credential{AccessKeyID: "ak", SecretAccessKey: "sk"},
		Read:  storage.Credential{AccessKeyID: "ak", SecretAccessKey: "sk"},
	})
	if res.OK || len(res.Steps) != 1 || res.Steps[0].Name != storage.StepResolve || res.Steps[0].OK {
		t.Fatalf("内网端点应在 resolve 步被拦下：%+v", res.Steps)
	}
}
