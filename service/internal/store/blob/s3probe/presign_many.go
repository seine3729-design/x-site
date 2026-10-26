package s3probe

import (
	"context"
	"fmt"

	"xsite/service/internal/domain/storage"
	"xsite/service/internal/store/blob"
)

var _ storage.BatchPresigner = (*Presigner)(nil)

// PresignGetMany 为同一存储源上的一批对象生成预签名下载地址（S1-8 清单改写）：只建一次客户端、端点只解析一次
// （blob.Bucket.PresignGetMany），逐个做本地 SigV4 计算，不访问网络。顺序与 Keys 一致。
func (p *Presigner) PresignGetMany(ctx context.Context, in storage.PresignManyParams) ([]string, error) {
	client, err := blob.New(blob.Config{
		Endpoint:    in.Conn.Endpoint,
		Region:      in.Conn.Region,
		AccessKey:   in.Cred.AccessKeyID,
		SecretKey:   in.Cred.SecretAccessKey,
		PathStyle:   in.Conn.PathStyle,
		DialContext: p.Egress.DialContext(nil),
	})
	if err != nil {
		return nil, fmt.Errorf("创建存储客户端: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, presignTimeout)
	defer cancel()
	return client.Bucket(in.Conn.Bucket).PresignGetMany(ctx, in.Keys, in.TTL)
}
