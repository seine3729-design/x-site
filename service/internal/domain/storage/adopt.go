package storage

import (
	"context"
	"fmt"
	"log/slog"
)

// 接管历史产物（docs/12 第 5 节末，S1-6f）：S1-6f 之前转封装产物直接写 config.Media 的默认桶，
// 没有 media_replica 记录，因此选源看不到它们、复制任务也找不到复制源（no_source）。
//
// 把默认桶登记成存储源后（docs/12 第 10 节：本地 SeaweedFS 就是一个 custom/s3_compatible 存储源），
// 用这里的接管操作给这些历史对象补上副本记录。
//
// **必须先校验对象在该存储源上真实存在**：盲目登记会让选源把线路下发给实际没有文件的存储源，
// 用户拿到的就是 404。校验不通过的对象跳过并记日志，不写副本。

// VerifyResult 是对象存在性校验的结果。
type VerifyResult struct {
	Exists  bool
	Objects int   // hls_dir 下的文件数；file 为 1
	Bytes   int64 // 总字节数
}

// ObjectVerifier 检查一个对象是否存在于某个存储源（实现在 store/blob）。
type ObjectVerifier interface {
	VerifyObject(ctx context.Context, conn Conn, cred Credential, kind ObjectKind, key string) (VerifyResult, error)
}

// AdoptEnqueuer 把接管任务入队。
type AdoptEnqueuer interface {
	EnqueueAdopt(ctx context.Context, objectID, providerID int64) error
}

// AdoptRequest 是接管历史产物的请求。
type AdoptRequest struct {
	// ProviderID 是这些历史产物实际所在的存储源（通常是把原默认桶登记成的那个存储源）。
	ProviderID int64
}

// AdoptService 把没有副本记录的历史对象登记到指定存储源。
type AdoptService struct {
	repo     Repo
	creds    WriteCredentialOpener
	verifier ObjectVerifier
	enqueue  AdoptEnqueuer
}

func NewAdoptService(repo Repo, creds WriteCredentialOpener, verifier ObjectVerifier, enqueue AdoptEnqueuer) *AdoptService {
	return &AdoptService{repo: repo, creds: creds, verifier: verifier, enqueue: enqueue}
}

// Adopt 扫描还没有任何副本记录的媒体对象，逐个入队接管任务（任务里校验存在性再登记副本）。
func (s *AdoptService) Adopt(ctx context.Context, req AdoptRequest) (EnqueueResult, error) {
	if req.ProviderID <= 0 {
		return EnqueueResult{}, badRequest("请指定历史产物所在的存储源")
	}
	row, err := s.repo.Get(ctx, req.ProviderID)
	if err != nil {
		return EnqueueResult{}, err
	}
	if isHTTPOrigin(row.Type, row.Subtype) {
		return EnqueueResult{}, badRequest("只读 HTTP 源不能登记为副本所在的存储源")
	}
	if s.enqueue == nil {
		return EnqueueResult{}, badRequest("当前进程不能入队接管任务")
	}

	var res EnqueueResult
	var cursor int64
	for {
		ids, err := s.repo.ListOrphanObjectIDs(ctx, cursor, objectScanBatch)
		if err != nil {
			return res, err
		}
		if len(ids) == 0 {
			return res, nil
		}
		res.Scanned += len(ids)
		for _, id := range ids {
			if res.Objects >= maxEnqueuePerCall {
				res.Capped = true
				return res, nil
			}
			if err := s.enqueue.EnqueueAdopt(ctx, id, req.ProviderID); err != nil {
				return res, fmt.Errorf("入队对象 %d: %w", id, err)
			}
			res.Objects++
		}
		cursor = ids[len(ids)-1]
		if len(ids) < objectScanBatch {
			return res, nil
		}
	}
}

// AdoptOne 校验一个对象在指定存储源上存在，存在则登记 ready 副本（worker 侧执行）。
// 对象不存在时跳过并返回 nil：历史数据里可能有已经被清理掉文件的记录，不该让任务反复重试。
func (s *AdoptService) AdoptOne(ctx context.Context, objectID, providerID int64) error {
	obj, err := s.repo.GetObject(ctx, objectID)
	if err != nil {
		return fmt.Errorf("读取媒体对象 %d: %w", objectID, err)
	}
	// 已经有副本记录就不再接管（可能是并发触发或产物已重新转封装）
	replicas, err := s.repo.ListReplicas(ctx, objectID)
	if err != nil {
		return err
	}
	if len(replicas) > 0 {
		slog.InfoContext(ctx, "接管跳过：对象已有副本记录", "object_id", objectID)
		return nil
	}

	row, err := s.repo.Get(ctx, providerID)
	if err != nil {
		return err
	}
	cred, err := s.creds.WriteCredential(row)
	if err != nil {
		return err
	}
	got, err := s.verifier.VerifyObject(ctx, row.Conn(), cred, obj.Kind, obj.Key)
	if err != nil {
		return fmt.Errorf("校验对象 %d 是否在存储源 %d 上: %w", objectID, providerID, err)
	}
	if !got.Exists {
		slog.WarnContext(ctx, "接管跳过：对象不在该存储源上",
			"object_id", objectID, "provider_id", providerID, "key", obj.Key)
		return nil
	}
	if _, err := s.repo.SaveReplica(ctx, objectID, providerID, ReplicaReady, ""); err != nil {
		return fmt.Errorf("登记副本: %w", err)
	}
	slog.InfoContext(ctx, "已接管历史产物", "object_id", objectID, "provider_id", providerID,
		"files", got.Objects, "bytes", got.Bytes)
	return nil
}
