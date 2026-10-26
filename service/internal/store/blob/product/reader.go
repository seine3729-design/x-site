// Package product 读取转封装产物里的清单原文（S1-8，docs/specs/S1-8.md），实现 play.ProductFiles。
//
// 产物有就绪副本时从副本所在的存储源读（用存储源的只读凭据，出站地址经 netguard 校验），按调用方给的顺序逐个尝试；
// 没有任何副本记录时（S1-6f 之前、或没有配置主存储时写入的产物）读默认存储 S3_BUCKET_MEDIA。
// 产物目录按转封装任务区分、写入后不再变化，清单原文进读穿缓存；改写后的清单含签名与令牌，由调用方负责不缓存。
package product

import (
	"context"
	"errors"
	"fmt"
	"io"
	"regexp"
	"time"

	"xsite/service/internal/domain/play"
	"xsite/service/internal/domain/storage"
	"xsite/service/internal/netguard"
	"xsite/service/internal/store/blob"
)

const (
	// MaxFileSize 是单个清单的大小上限：几千个切片的清单也只有几百 KB，超出说明不是我们的产物。
	MaxFileSize = 4 << 20
	// CacheTTL 是清单原文的缓存时长（产物不可变，缓存只为减少回源；过期后从存储重新读）。
	CacheTTL = time.Hour
	// readTimeout 是从一个存储源读一个文件的超时：超时后换下一个存储源。
	readTimeout = 10 * time.Second
)

// fileNameRe 是可读取的文件名：产物目录下的单个文件（master.m3u8、v{短边}.m3u8），不含目录。
var fileNameRe = regexp.MustCompile(`^[A-Za-z0-9_-]+\.m3u8$`)

// ProviderRepo 读取存储源（domain/storage.Repo）。
type ProviderRepo interface {
	Get(ctx context.Context, id int64) (storage.Row, error)
}

// Cache 是读穿缓存（store/cache.Store）。
type Cache interface {
	GetOrLoadJSON(ctx context.Context, key string, ttl time.Duration, dst any, load func(context.Context) (any, error)) error
}

// ObjectGetter 读取一个对象（blob.Bucket）。
type ObjectGetter interface {
	Get(ctx context.Context, key string) (io.ReadCloser, blob.ObjectInfo, error)
}

// Reader 读取产物里的清单原文。并发安全。
type Reader struct {
	providers ProviderRepo
	creds     storage.CredentialOpener
	fallback  ObjectGetter
	cache     Cache
	// open 按存储源的连接参数与凭据建立读取客户端（测试可替换）。
	open func(conn storage.Conn, cred storage.Credential) (ObjectGetter, error)
}

var _ play.ProductFiles = (*Reader)(nil)

// NewReader 创建读取器。fallback 是默认存储（没有配置时传 nil，此时没有副本记录的产物读不到）；
// cache 为 nil 时不缓存。egress 是连接存储源时的出站校验策略。
func NewReader(providers ProviderRepo, creds storage.CredentialOpener, egress netguard.Policy, fallback ObjectGetter, cache Cache) *Reader {
	return &Reader{
		providers: providers, creds: creds, fallback: fallback, cache: cache,
		open: func(conn storage.Conn, cred storage.Credential) (ObjectGetter, error) {
			client, err := blob.New(blob.Config{
				Endpoint: conn.Endpoint, Region: conn.Region, AccessKey: cred.AccessKeyID,
				SecretKey: cred.SecretAccessKey, PathStyle: conn.PathStyle, DialContext: egress.DialContext(nil),
			})
			if err != nil {
				return nil, err
			}
			return client.Bucket(conn.Bucket), nil
		},
	}
}

// ReadFile 读取产物目录下的一个清单。同一文件并发只回源一次（读穿缓存的 singleflight）。
func (r *Reader) ReadFile(ctx context.Context, p play.Product, name string) ([]byte, error) {
	if p.Key == "" || !fileNameRe.MatchString(name) {
		return nil, fmt.Errorf("product: 无效的产物文件 %q/%q", p.Key, name)
	}
	key := p.Key + "/" + name
	if r.cache == nil {
		return r.load(ctx, p, key)
	}
	var data []byte
	err := r.cache.GetOrLoadJSON(ctx, key, CacheTTL, &data, func(ctx context.Context) (any, error) {
		return r.load(ctx, p, key)
	})
	if err != nil {
		return nil, err
	}
	return data, nil
}

// load 从存储读取：有副本时按顺序逐个存储源尝试，没有副本时读默认存储。
func (r *Reader) load(ctx context.Context, p play.Product, key string) ([]byte, error) {
	if len(p.Providers) == 0 {
		if r.fallback == nil {
			return nil, errors.New("product: 产物没有就绪副本，且没有配置默认存储")
		}
		data, err := readObject(ctx, r.fallback, key)
		if err != nil {
			return nil, fmt.Errorf("product: 默认存储: %w", err)
		}
		return data, nil
	}
	var errs []error
	for _, id := range p.Providers {
		data, err := r.fromProvider(ctx, id, key)
		if err == nil {
			return data, nil
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		errs = append(errs, fmt.Errorf("存储源 %d: %w", id, err))
	}
	return nil, fmt.Errorf("product: %w", errors.Join(errs...))
}

// fromProvider 用存储源的只读凭据读取（HTTP 源只有公开地址、没有 S3 接口，跳过）。
func (r *Reader) fromProvider(ctx context.Context, id int64, key string) ([]byte, error) {
	row, err := r.providers.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if row.Type == storage.TypeCustom && row.Subtype == storage.SubtypeHTTPOrigin {
		return nil, errors.New("HTTP 源不支持读取产物清单")
	}
	cred, err := r.creds.ReadCredential(row)
	if err != nil {
		return nil, err
	}
	obj, err := r.open(row.Conn(), cred)
	if err != nil {
		return nil, fmt.Errorf("创建存储客户端: %w", err)
	}
	return readObject(ctx, obj, storage.JoinPrefix(row.PathPrefix, key))
}

// readObject 读取一个不超过 MaxFileSize 的对象。
func readObject(ctx context.Context, g ObjectGetter, key string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, readTimeout)
	defer cancel()
	body, info, err := g.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	defer body.Close()
	if info.Size > MaxFileSize {
		return nil, fmt.Errorf("%s 有 %d 字节，超过上限 %d", key, info.Size, MaxFileSize)
	}
	data, err := io.ReadAll(io.LimitReader(body, MaxFileSize+1))
	if err != nil {
		return nil, fmt.Errorf("读取 %s: %w", key, err)
	}
	if len(data) > MaxFileSize {
		return nil, fmt.Errorf("%s 超过上限 %d 字节", key, MaxFileSize)
	}
	return data, nil
}
