package blob

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	smithyendpoints "github.com/aws/smithy-go/endpoints"
)

// PresignGetMany 为同一个桶里的一批对象生成预签名下载地址，顺序与 keys 一致（S1-8 清单改写，一档清单几十到几百个切片）。
//
// 端点只解析一次：SDK 每次解析 S3 端点规则都会重新编译分区正则，逐个 PresignGet 时它占了一半以上的耗时。
// 同一个桶、同一个客户端的端点解析结果与对象键无关（端点规则不使用 Key 参数，键由序列化步骤拼在路径上），
// 复用第一次的结果得到的地址与逐个解析相同。
func (b *Bucket) PresignGetMany(ctx context.Context, keys []string, ttl time.Duration) ([]string, error) {
	resolver := &onceEndpoint{}
	pc := s3.NewPresignClient(b.api, func(po *s3.PresignOptions) {
		po.ClientOptions = append(po.ClientOptions, func(o *s3.Options) {
			resolver.setBase(o.EndpointResolverV2)
			o.EndpointResolverV2 = resolver
		})
	})
	out := make([]string, len(keys))
	for i, key := range keys {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		req, err := pc.PresignGetObject(ctx,
			&s3.GetObjectInput{Bucket: aws.String(b.name), Key: aws.String(key)},
			s3.WithPresignExpires(ttl))
		if err != nil {
			return nil, fmt.Errorf("blob: 预签名 %s/%s: %w", b.name, key, err)
		}
		out[i] = req.URL
	}
	return out, nil
}

// onceEndpoint 缓存第一次成功的端点解析结果。返回的是值拷贝；其中的 Headers 与 Properties 在 SDK 里只读。
type onceEndpoint struct {
	mu   sync.Mutex
	base s3.EndpointResolverV2
	done bool
	ep   smithyendpoints.Endpoint
}

func (r *onceEndpoint) setBase(base s3.EndpointResolverV2) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.base == nil {
		r.base = base
	}
}

func (r *onceEndpoint) ResolveEndpoint(ctx context.Context, p s3.EndpointParameters) (smithyendpoints.Endpoint, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.done {
		return r.ep, nil
	}
	ep, err := r.base.ResolveEndpoint(ctx, p)
	if err != nil {
		return ep, err
	}
	r.ep, r.done = ep, true
	return ep, nil
}
