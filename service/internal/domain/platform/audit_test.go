package platform

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"
)

type memAuditRepo struct {
	mu      sync.Mutex
	entries []AuditEntry
	block   chan struct{} // 非空时每次写入前等待，用来制造队列积压
}

func (m *memAuditRepo) InsertAudit(_ context.Context, e AuditEntry) error {
	if m.block != nil {
		<-m.block
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.entries = append(m.entries, e)
	return nil
}

func (m *memAuditRepo) count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.entries)
}

func TestAuditRecorder_CloseDrainsQueue(t *testing.T) {
	repo := &memAuditRepo{}
	rec := NewAuditRecorder(repo, 16)
	for i := range 10 {
		rec.Record(AuditEntry{AdminID: int64(i + 1), Action: "drama.update"})
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := rec.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if repo.count() != 10 {
		t.Fatalf("关闭前提交的 10 条应全部写入，得到 %d", repo.count())
	}

	// 关闭后仍提交的记录同步写入，不丢失、不 panic
	rec.Record(AuditEntry{AdminID: 99, Action: "late"})
	if repo.count() != 11 {
		t.Fatal("关闭后的记录应同步写入")
	}
}

func TestAuditRecorder_FullQueueWritesSynchronously(t *testing.T) {
	repo := &memAuditRepo{block: make(chan struct{})}
	rec := NewAuditRecorder(repo, 1)

	rec.Record(AuditEntry{Action: "first"}) // 被写入协程取走，阻塞在 InsertAudit
	for len(rec.queue) > 0 {                // 等写入协程取走第一条
		time.Sleep(time.Millisecond)
	}
	rec.Record(AuditEntry{Action: "second"}) // 占满队列

	done := make(chan struct{})
	go func() {
		rec.Record(AuditEntry{Action: "third"}) // 队列已满：同步写入，等待 InsertAudit 放行
		close(done)
	}()
	select {
	case <-done:
		t.Fatal("队列满时应同步写入（此时仓储阻塞，Record 不应立即返回）")
	case <-time.After(50 * time.Millisecond):
	}
	close(repo.block)
	<-done

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := rec.Close(ctx); err != nil {
		t.Fatal(err)
	}
	if repo.count() != 3 {
		t.Fatalf("三条都应写入，得到 %d", repo.count())
	}
}

func TestRedactJSON(t *testing.T) {
	in := map[string]any{
		"name":           "R2-主存储",
		"password":       "p@ss",
		"credential_enc": "v1:abc",
		"nested":         map[string]any{"Secret_Access_Key": "sk", "region": "auto"},
		"items":          []any{map[string]any{"api_key": "k", "id": 1}},
		"sign_key_ref":   "line-key-1",
	}
	raw, err := RedactJSON(in)
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	for _, leaked := range []string{"p@ss", "v1:abc", `"sk"`, `"k"`} {
		if strings.Contains(s, leaked) {
			t.Fatalf("敏感值 %s 不应出现在审计内容中：%s", leaked, s)
		}
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if out["name"] != "R2-主存储" || out["sign_key_ref"] != "line-key-1" || out["password"] != "***" {
		t.Fatalf("非敏感字段应保留、敏感字段应替换：%s", s)
	}
	if out["nested"].(map[string]any)["region"] != "auto" {
		t.Fatalf("嵌套对象的普通字段应保留：%s", s)
	}

	if raw, err := RedactJSON(nil); err != nil || raw != nil {
		t.Fatalf("nil 应返回 nil，得到 %s %v", raw, err)
	}
}
