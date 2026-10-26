package storage

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
)

// 复制服务（docs/12 第 5 节第 3 条，S1-6f）：把主存储上的对象复制到全部镜像存储源。
// 由 worker 的 River 任务调用；每个镜像独立成败，失败的记 failed 并带原因，由 River 重试整个任务。

// ReplicateService 执行一个对象到各镜像存储源的复制。
type ReplicateService struct {
	repo      Repo
	placement *Placement
	creds     WriteCredentialOpener
	copier    Copier
}

// WriteCredentialOpener 解密存储源的读写凭据（由 Service 实现）。
type WriteCredentialOpener interface {
	WriteCredential(row Row) (Credential, error)
}

func NewReplicateService(repo Repo, placement *Placement, creds WriteCredentialOpener, copier Copier) *ReplicateService {
	return &ReplicateService{repo: repo, placement: placement, creds: creds, copier: copier}
}

// ReplicateResult 是一次复制任务的结果。
type ReplicateResult struct {
	ObjectID int64
	Copied   int // 本次复制成功的镜像数
	Failed   int // 本次因临时原因失败的镜像数（网络、存储服务抖动等，重试可能成功）
	// Permanent 是因配置问题失败的镜像数（镜像没配凭据、目标不可写等）：重试解决不了，
	// 副本记为 failed 等运营修好配置后重新触发，不让任务无限重试。
	Permanent int
	Skipped   int // 已经是 ready、本次跳过的镜像数
	// NoSource 表示这个对象没有任何可用作复制源的副本（例如 S1-6f 之前写入默认桶、还没登记副本的历史产物）。
	// 这不是失败，重试也不会变好，需要先把产物登记为主存储副本。
	NoSource bool
}

// permanentError 标记「重试解决不了」的失败（配置类）。
type permanentError struct{ err error }

func (e permanentError) Error() string { return e.err.Error() }
func (e permanentError) Unwrap() error { return e.err }

func permanent(err error) error { return permanentError{err: err} }

// IsPermanentCopyError 表示这次复制失败是配置问题，重试无用。
func IsPermanentCopyError(err error) bool {
	var p permanentError
	return errors.As(err, &p)
}

// Replicate 把对象复制到全部镜像存储源。对象在主存储上必须已有 ready 副本（由产物写入时记录）。
// 返回 error 只表示整个任务没能执行（对象不存在、没有源副本）；个别镜像失败记在副本状态里，不影响其他镜像。
func (s *ReplicateService) Replicate(ctx context.Context, objectID int64) (ReplicateResult, error) {
	res := ReplicateResult{ObjectID: objectID}
	obj, err := s.repo.GetObject(ctx, objectID)
	if err != nil {
		return res, fmt.Errorf("读取媒体对象 %d: %w", objectID, err)
	}
	replicas, err := s.repo.ListReplicas(ctx, objectID)
	if err != nil {
		return res, err
	}
	// 源：任何一个 ready 的副本（通常是主存储）
	var source Row
	byProvider := make(map[int64]ReplicaStatus, len(replicas))
	for _, rep := range replicas {
		byProvider[rep.ProviderID] = rep.Status
		if rep.Status == ReplicaReady && source.ID == 0 {
			if row, err := s.repo.Get(ctx, rep.ProviderID); err == nil {
				source = row
			}
		}
	}
	if source.ID == 0 {
		// 历史产物还没登记副本：重试没有意义，交由「把历史产物登记为主存储副本」的运维动作处理
		slog.InfoContext(ctx, "复制跳过：对象没有可用作复制源的副本", "object_id", objectID)
		res.NoSource = true
		return res, nil
	}
	sourceCred, err := s.creds.WriteCredential(source)
	if err != nil {
		// 源存储源没配读写凭据是配置问题，重试无用
		res.Permanent++
		slog.WarnContext(ctx, "复制跳过：复制源没有可用凭据", "object_id", objectID, "provider_id", source.ID, "err", err)
		return res, nil
	}

	mirrors, err := s.placement.Mirrors(ctx, kindOfObject(obj), source.ID)
	if err != nil {
		return res, err
	}
	for _, mirror := range mirrors {
		if byProvider[mirror.ID] == ReplicaReady {
			res.Skipped++
			continue
		}
		if err := s.copyTo(ctx, obj, source, sourceCred, mirror); err != nil {
			if IsPermanentCopyError(err) {
				res.Permanent++
			} else {
				res.Failed++
			}
			slog.WarnContext(ctx, "复制到镜像存储源失败", "object_id", objectID, "provider_id", mirror.ID,
				"permanent", IsPermanentCopyError(err), "err", err)
			if _, saveErr := s.repo.SaveReplica(ctx, objectID, mirror.ID, ReplicaFailed, publicCopyError(err)); saveErr != nil {
				slog.ErrorContext(ctx, "记录副本失败状态出错", "object_id", objectID, "provider_id", mirror.ID, "err", saveErr)
			}
			continue
		}
		if _, err := s.repo.SaveReplica(ctx, objectID, mirror.ID, ReplicaReady, ""); err != nil {
			return res, fmt.Errorf("记录副本就绪: %w", err)
		}
		res.Copied++
	}
	return res, nil
}

// copyTo 把对象复制到一个镜像存储源：先登记 pending（让看板能看出正在复制），再执行复制。
func (s *ReplicateService) copyTo(ctx context.Context, obj MediaObject, source Row, sourceCred Credential, mirror Row) error {
	// 目标不可写、没配凭据都是配置问题：标记为永久失败，不让 River 反复重试
	if err := checkWritable(mirror, kindOfObject(obj)); err != nil {
		return permanent(err)
	}
	mirrorCred, err := s.creds.WriteCredential(mirror)
	if err != nil {
		return permanent(err)
	}
	if _, err := s.repo.SaveReplica(ctx, obj.ID, mirror.ID, ReplicaPending, ""); err != nil {
		return fmt.Errorf("登记副本: %w", err)
	}
	_, err = s.copier.CopyObject(ctx, CopyRequest{
		Kind: obj.Kind, Key: obj.Key,
		From: source.Conn(), FromCred: sourceCred,
		To: mirror.Conn(), ToCred: mirrorCred,
		SameStore: sameStore(source, sourceCred, mirror, mirrorCred),
	})
	return err
}

// RegisterPrimary 为一个对象登记主存储副本（产物写入成功后调用）。
func (s *ReplicateService) RegisterPrimary(ctx context.Context, objectID, providerID int64) error {
	_, err := s.repo.SaveReplica(ctx, objectID, providerID, ReplicaReady, "")
	return err
}

// kindOfObject 判断一个对象属于视频还是图片：HLS 目录是视频，单文件按图片处理（首帧图、封面）。
func kindOfObject(obj MediaObject) LineKind {
	if obj.Kind == ObjectHLSDir {
		return LineVideo
	}
	return LineImage
}

// sameStore 判断两个存储源是否同一存储服务（同端点 + 同凭据）：
// 这时可以用服务端复制 API，不必把数据拉到本进程再写回去。
func sameStore(a Row, ac Credential, b Row, bc Credential) bool {
	return a.Endpoint != "" && a.Endpoint == b.Endpoint &&
		ac.AccessKeyID == bc.AccessKeyID && ac.SecretAccessKey == bc.SecretAccessKey
}

// publicCopyError 是写进副本记录、显示在后台的失败原因：截断并去掉可能出现的签名参数。
func publicCopyError(err error) string {
	msg := err.Error()
	if i := strings.Index(msg, "X-Amz-"); i >= 0 {
		msg = msg[:i] + "…"
	}
	if len(msg) > 300 {
		msg = msg[:300] + "…"
	}
	return msg
}

// ErrNoPrimary 表示没有配置可用的主存储（上传方据此回退或报错）。
var ErrNoPrimary = errors.New("没有可用的主存储")
