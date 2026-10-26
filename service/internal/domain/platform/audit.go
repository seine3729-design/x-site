package platform

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
)

// AuditEntry 是一条后台写操作的审计记录（docs/02 第 6.2 节、docs/03 第 9.8 节）。
// Before / After 必须是已脱敏的 JSON（见 RedactJSON）。哈希链在 S4-7 加入。
type AuditEntry struct {
	AdminID    int64
	Action     string
	TargetType string
	TargetID   string
	Before     json.RawMessage
	After      json.RawMessage
	IPPrefix   string
	RequestID  string
	CreatedAt  time.Time
}

// AuditRepo 写入审计记录。
type AuditRepo interface {
	InsertAudit(ctx context.Context, e AuditEntry) error
}

// 单条审计写库的超时。
const auditWriteTimeout = 5 * time.Second

// AuditRecorder 在请求结束后异步写审计，不拖慢 handler。
// 审计不能丢：队列满或已关闭时改为同步写入；Close 会等队列写完。
type AuditRecorder struct {
	repo   AuditRepo
	queue  chan AuditEntry
	mu     sync.RWMutex
	closed bool
	done   chan struct{}
}

// NewAuditRecorder 创建并启动写入协程，buffer 为队列长度。
func NewAuditRecorder(repo AuditRepo, buffer int) *AuditRecorder {
	r := &AuditRecorder{repo: repo, queue: make(chan AuditEntry, buffer), done: make(chan struct{})}
	go r.run()
	return r
}

// Record 提交一条审计记录。
func (r *AuditRecorder) Record(e AuditEntry) {
	if r.enqueue(e) {
		return
	}
	r.write(e)
}

// Close 停止接收新记录，等待队列中的记录写完或 ctx 到期。
func (r *AuditRecorder) Close(ctx context.Context) error {
	r.mu.Lock()
	if !r.closed {
		r.closed = true
		close(r.queue)
	}
	r.mu.Unlock()
	select {
	case <-r.done:
		return nil
	case <-ctx.Done():
		return fmt.Errorf("审计队列未写完: %w", ctx.Err())
	}
}

func (r *AuditRecorder) enqueue(e AuditEntry) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.closed {
		return false
	}
	select {
	case r.queue <- e:
		return true
	default:
		return false
	}
}

func (r *AuditRecorder) run() {
	defer close(r.done)
	for e := range r.queue {
		r.write(e)
	}
}

func (r *AuditRecorder) write(e AuditEntry) {
	// 请求已经结束，不用请求的 ctx
	ctx, cancel := context.WithTimeout(context.Background(), auditWriteTimeout)
	defer cancel()
	if err := r.repo.InsertAudit(ctx, e); err != nil {
		slog.Error("审计日志写入失败", "action", e.Action, "admin_id", e.AdminID, "request_id", e.RequestID, "err", err)
	}
}

// 字段名（小写）包含这些片段时，值在写入审计前替换为 "***"（docs/10 第 9 节：密码、密钥原文不记）。
var sensitiveKeyParts = []string{"password", "secret", "token", "credential", "private_key", "api_key", "apikey", "access_key"}

// RedactJSON 把任意值编码为 JSON，并把敏感字段的值替换为 "***"（递归处理对象与数组）。v 为 nil 时返回 nil。
func RedactJSON(v any) (json.RawMessage, error) {
	if v == nil {
		return nil, nil
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("审计内容编码失败: %w", err)
	}
	var tree any
	if err := json.Unmarshal(raw, &tree); err != nil {
		return nil, fmt.Errorf("审计内容解析失败: %w", err)
	}
	return json.Marshal(redact(tree))
}

func redact(v any) any {
	switch t := v.(type) {
	case map[string]any:
		for k, child := range t {
			if isSensitiveKey(k) {
				t[k] = "***"
				continue
			}
			t[k] = redact(child)
		}
	case []any:
		for i, child := range t {
			t[i] = redact(child)
		}
	}
	return v
}

func isSensitiveKey(k string) bool {
	k = strings.ToLower(k)
	if strings.HasSuffix(k, "_enc") || strings.HasSuffix(k, "_hash") {
		return true
	}
	for _, part := range sensitiveKeyParts {
		if strings.Contains(k, part) {
			return true
		}
	}
	return false
}
