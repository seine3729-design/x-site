package s3probe

import (
	"context"
	"fmt"
	"time"

	"xsite/service/internal/domain/storage"
	"xsite/service/internal/netguard"
	"xsite/service/internal/store/blob"
)

// Presigner 实现 storage.Presigner：用存储源的只读凭据生成 SigV4 预签名下载地址（docs/12 第 3.3 节）。
// 预签名本身不发请求，Egress 只影响后续真正取对象时的出站校验，这里传入保持与连通性测试一致的连接配置。
type Presigner struct {
	Egress netguard.Policy
}

var _ storage.Presigner = (*Presigner)(nil)

// presignTimeout 是生成预签名的超时（只做本地签名计算，不访问网络）。
const presignTimeout = 5 * time.Second

func (p *Presigner) PresignGet(ctx context.Context, in storage.PresignParams) (string, error) {
	client, err := blob.New(blob.Config{
		Endpoint:    in.Conn.Endpoint,
		Region:      in.Conn.Region,
		AccessKey:   in.Cred.AccessKeyID,
		SecretKey:   in.Cred.SecretAccessKey,
		PathStyle:   in.Conn.PathStyle,
		DialContext: p.Egress.DialContext(nil),
	})
	if err != nil {
		return "", fmt.Errorf("创建存储客户端: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, presignTimeout)
	defer cancel()
	return client.Bucket(in.Conn.Bucket).PresignGet(ctx, in.Key, in.TTL)
}
