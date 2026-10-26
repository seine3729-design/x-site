package platform

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

// fakeChain 用内存里的一条链模拟仓储，便于直接改动某一条来验证校验能不能发现。
type fakeChain struct {
	records   []AuditRecord
	unchained int64
	err       error
}

func (f *fakeChain) ChainPage(_ context.Context, afterID int64, limit int) ([]AuditRecord, error) {
	if f.err != nil {
		return nil, f.err
	}
	var out []AuditRecord
	for _, r := range f.records {
		if r.ID > afterID {
			out = append(out, r)
			if len(out) == limit {
				break
			}
		}
	}
	return out, nil
}

func (f *fakeChain) ChainStats(context.Context) (AuditChainStats, error) {
	if f.err != nil {
		return AuditChainStats{}, f.err
	}
	return AuditChainStats{Chained: int64(len(f.records)), Unchained: f.unchained}, nil
}

type nopAuditRepo struct{}

func (nopAuditRepo) ListAudit(context.Context, AuditQuery) ([]AuditItem, int64, error) {
	return nil, 0, nil
}
func (nopAuditRepo) GetAudit(context.Context, int64) (AuditDetail, error) { return AuditDetail{}, nil }
func (nopAuditRepo) AuditFacets(context.Context) (AuditFacets, error)     { return AuditFacets{}, nil }

// buildChain 造一条 n 条记录的合法链。
func buildChain(n int) *fakeChain {
	base := time.Date(2026, 10, 24, 10, 0, 0, 0, time.UTC)
	f := &fakeChain{}
	prev := ""
	for i := 1; i <= n; i++ {
		e := AuditEntry{
			AdminID: int64(i%3 + 1), Action: "drama.update", TargetType: "drama",
			TargetID: string(rune('0' + i%10)), IPPrefix: "203.0.0.0/16",
			RequestID: "req-" + string(rune('a'+i%26)),
			After:     json.RawMessage(`{"title":"剧` + string(rune('0'+i%10)) + `"}`),
			CreatedAt: base.Add(time.Duration(i) * time.Second),
		}
		h := AuditHash(prev, e)
		f.records = append(f.records, AuditRecord{ID: int64(i), Entry: e, PrevHash: prev, Hash: h})
		prev = h
	}
	return f
}

func newAuditSvc(chain *fakeChain) *AuditService {
	return NewAuditService(nopAuditRepo{}, chain)
}

func TestAuditHash_Deterministic(t *testing.T) {
	e := AuditEntry{AdminID: 7, Action: "role.update", CreatedAt: time.Now()}
	if AuditHash("prev", e) != AuditHash("prev", e) {
		t.Error("同样的输入应当算出同样的哈希")
	}
	if AuditHash("prev", e) == AuditHash("other", e) {
		t.Error("前驱不同时哈希必须不同")
	}
}

// 时区不影响哈希：同一时刻在不同时区的写法不同，但指的是同一瞬间。
func TestAuditHash_TimezoneIndependent(t *testing.T) {
	utc := time.Date(2026, 10, 24, 10, 0, 0, 0, time.UTC)
	shanghai := utc.In(time.FixedZone("CST", 8*3600))
	a := AuditHash("p", AuditEntry{Action: "x", CreatedAt: utc})
	b := AuditHash("p", AuditEntry{Action: "x", CreatedAt: shanghai})
	if a != b {
		t.Error("同一时刻的不同时区表示应当算出同样的哈希")
	}
}

// 字段之间有分隔符：挪动字段边界不能撞出同一个哈希。
func TestAuditHash_NoFieldAmbiguity(t *testing.T) {
	now := time.Now()
	a := AuditHash("p", AuditEntry{Action: "ab", TargetType: "c", CreatedAt: now})
	b := AuditHash("p", AuditEntry{Action: "a", TargetType: "bc", CreatedAt: now})
	if a == b {
		t.Error("(ab,c) 与 (a,bc) 算出了同一个哈希")
	}
}

// 每个被哈希的字段都要真的参与计算，漏掉任何一个都意味着改它不会被发现。
func TestAuditHash_CoversEveryField(t *testing.T) {
	base := AuditEntry{
		AdminID: 1, Action: "a", TargetType: "t", TargetID: "1",
		Before: json.RawMessage(`{"x":1}`), After: json.RawMessage(`{"x":2}`),
		IPPrefix: "203.0.0.0/16", RequestID: "req", CreatedAt: time.Unix(1_790_000_000, 0),
	}
	want := AuditHash("p", base)
	mutations := map[string]func(*AuditEntry){
		"admin_id":    func(e *AuditEntry) { e.AdminID = 2 },
		"action":      func(e *AuditEntry) { e.Action = "b" },
		"target_type": func(e *AuditEntry) { e.TargetType = "u" },
		"target_id":   func(e *AuditEntry) { e.TargetID = "2" },
		"before":      func(e *AuditEntry) { e.Before = json.RawMessage(`{"x":9}`) },
		"after":       func(e *AuditEntry) { e.After = json.RawMessage(`{"x":9}`) },
		"ip_prefix":   func(e *AuditEntry) { e.IPPrefix = "198.51.0.0/16" },
		"request_id":  func(e *AuditEntry) { e.RequestID = "req2" },
		"created_at":  func(e *AuditEntry) { e.CreatedAt = e.CreatedAt.Add(time.Second) },
	}
	for field, mutate := range mutations {
		e := base
		mutate(&e)
		if AuditHash("p", e) == want {
			t.Errorf("改动 %s 后哈希没变：这个字段没有参与计算", field)
		}
	}
}

func TestVerify_CleanChain(t *testing.T) {
	svc := newAuditSvc(buildChain(50))
	res, err := svc.VerifyAuditChain(context.Background(), 0)
	if err != nil {
		t.Fatalf("校验失败: %v", err)
	}
	if !res.OK || res.Broken != nil {
		t.Errorf("干净的链应当通过：%+v", res)
	}
	if res.Checked != 50 {
		t.Errorf("校验了 %d 条，期望 50", res.Checked)
	}
}

// 这是 S4-7 的验收标准：篡改一行后校验报警。
func TestVerify_DetectsTamperedRow(t *testing.T) {
	for _, pos := range []int{0, 7, 49} {
		chain := buildChain(50)
		// 只改内容、不动哈希，模拟有人直接改库
		chain.records[pos].Entry.After = json.RawMessage(`{"title":"被改过"}`)

		res, err := newAuditSvc(chain).VerifyAuditChain(context.Background(), 0)
		if err != nil {
			t.Fatalf("校验失败: %v", err)
		}
		if res.OK || res.Broken == nil {
			t.Fatalf("第 %d 条被改动后校验仍然通过", pos+1)
		}
		if res.Broken.ID != int64(pos+1) {
			t.Errorf("报告的位置是 #%d，期望 #%d", res.Broken.ID, pos+1)
		}
		if !strings.Contains(res.Broken.Reason, "被改动过") {
			t.Errorf("断链原因应当说明内容被改：%s", res.Broken.Reason)
		}
	}
}

// 删掉中间一行：后面那条的 prev_hash 会指向一个不存在的前驱。
func TestVerify_DetectsDeletedRow(t *testing.T) {
	chain := buildChain(20)
	chain.records = append(chain.records[:9], chain.records[10:]...) // 删掉第 10 条

	res, err := newAuditSvc(chain).VerifyAuditChain(context.Background(), 0)
	if err != nil {
		t.Fatalf("校验失败: %v", err)
	}
	if res.OK || res.Broken == nil {
		t.Fatal("删掉一行后校验仍然通过")
	}
	if res.Broken.ID != 11 {
		t.Errorf("报告的位置是 #%d，期望 #11（被删那条的下一条）", res.Broken.ID)
	}
	if !strings.Contains(res.Broken.Reason, "删除或插入") {
		t.Errorf("断链原因应当说明链接对不上：%s", res.Broken.Reason)
	}
}

// 伪造一条插进中间：它的哈希是自洽的，但下一条的 prev_hash 对不上。
func TestVerify_DetectsInsertedRow(t *testing.T) {
	chain := buildChain(10)
	forged := AuditEntry{
		AdminID: 99, Action: "admin.create", TargetType: "admin", TargetID: "999",
		CreatedAt: time.Date(2026, 10, 24, 10, 0, 5, 500, time.UTC),
	}
	prev := chain.records[4].Hash
	inserted := AuditRecord{ID: 55, Entry: forged, PrevHash: prev, Hash: AuditHash(prev, forged)}
	chain.records = append(chain.records[:5], append([]AuditRecord{inserted}, chain.records[5:]...)...)

	res, err := newAuditSvc(chain).VerifyAuditChain(context.Background(), 0)
	if err != nil {
		t.Fatalf("校验失败: %v", err)
	}
	if res.OK {
		t.Fatal("插入一条伪造记录后校验仍然通过")
	}
}

// 只改哈希不改内容，同样要被发现。
func TestVerify_DetectsForgedHash(t *testing.T) {
	chain := buildChain(10)
	chain.records[3].Hash = strings.Repeat("0", 64)
	res, _ := newAuditSvc(chain).VerifyAuditChain(context.Background(), 0)
	if res.OK {
		t.Fatal("哈希被替换后校验仍然通过")
	}
	if res.Broken.ID != 4 {
		t.Errorf("报告的位置是 #%d，期望 #4", res.Broken.ID)
	}
}

// 链的第一条允许没有前驱：哈希链可能是在已有记录之后才启用的。
func TestVerify_FirstRecordNeedsNoPredecessor(t *testing.T) {
	chain := buildChain(5)
	chain.unchained = 596 // 启用之前的历史记录
	res, err := newAuditSvc(chain).VerifyAuditChain(context.Background(), 0)
	if err != nil {
		t.Fatalf("校验失败: %v", err)
	}
	if !res.OK {
		t.Errorf("启用链之后的记录自成一条链，应当通过：%+v", res.Broken)
	}
	// 历史记录数要报告出来，不能假装它们在链上
	if res.Unchained != 596 {
		t.Errorf("未入链的记录数 = %d，期望 596", res.Unchained)
	}
}

func TestVerify_EmptyChain(t *testing.T) {
	res, err := newAuditSvc(&fakeChain{}).VerifyAuditChain(context.Background(), 0)
	if err != nil {
		t.Fatalf("空链校验失败: %v", err)
	}
	if !res.OK || res.Checked != 0 {
		t.Errorf("空链应当通过且校验 0 条：%+v", res)
	}
}

// 分页扫描要能跨批次把链接上（批大小 1000，这里造 2500 条）。
func TestVerify_AcrossBatches(t *testing.T) {
	chain := buildChain(2500)
	res, err := newAuditSvc(chain).VerifyAuditChain(context.Background(), 0)
	if err != nil {
		t.Fatalf("校验失败: %v", err)
	}
	if !res.OK || res.Checked != 2500 {
		t.Errorf("跨批次校验不对：ok=%v checked=%d broken=%+v", res.OK, res.Checked, res.Broken)
	}
	// 跨批次的边界上改一条也要能发现
	chain2 := buildChain(2500)
	chain2.records[1000].Entry.Action = "被改过"
	res2, _ := newAuditSvc(chain2).VerifyAuditChain(context.Background(), 0)
	if res2.OK || res2.Broken.ID != 1001 {
		t.Errorf("批次边界上的改动没被发现：%+v", res2.Broken)
	}
}

func TestVerify_RespectsMax(t *testing.T) {
	res, err := newAuditSvc(buildChain(500)).VerifyAuditChain(context.Background(), 100)
	if err != nil {
		t.Fatalf("校验失败: %v", err)
	}
	if res.Checked != 100 {
		t.Errorf("校验了 %d 条，期望受 max 限制为 100", res.Checked)
	}
}

func TestVerify_RepoError(t *testing.T) {
	svc := newAuditSvc(&fakeChain{err: errors.New("库挂了")})
	if _, err := svc.VerifyAuditChain(context.Background(), 0); err == nil {
		t.Fatal("底层出错时应当报错")
	}
}

// ---------- 列表筛选 ----------

func TestCleanAuditFilter(t *testing.T) {
	q, page, size, err := cleanAuditFilter(AuditFilter{})
	if err != nil {
		t.Fatalf("空筛选应当可用: %v", err)
	}
	if page != 1 || size != auditDefaultPageSize || q.Pattern != "" {
		t.Errorf("默认值不对: page=%d size=%d pattern=%q", page, size, q.Pattern)
	}

	// 搜索词里的 LIKE 通配符要按字面匹配
	q, _, _, err = cleanAuditFilter(AuditFilter{Q: "100%_a"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(q.Pattern, `\%`) || !strings.Contains(q.Pattern, `\_`) {
		t.Errorf("通配符没有转义: %q", q.Pattern)
	}

	// 控制字符要去掉
	q, _, _, _ = cleanAuditFilter(AuditFilter{Q: "a\x00b"})
	if strings.ContainsRune(q.Pattern, 0) {
		t.Errorf("控制字符没有去掉: %q", q.Pattern)
	}

	if _, _, _, err := cleanAuditFilter(AuditFilter{Q: strings.Repeat("字", 101)}); err == nil {
		t.Error("超长搜索词应当报错")
	}
	if _, _, _, err := cleanAuditFilter(AuditFilter{Page: 0, PageSize: 101}); err == nil {
		t.Error("超过上限的每页条数应当报错")
	}
	if _, _, _, err := cleanAuditFilter(AuditFilter{Page: 10001}); err == nil {
		t.Error("超过上限的页码应当报错")
	}

	// 结束时间要晚于开始时间
	from := time.Date(2026, 10, 24, 0, 0, 0, 0, time.UTC)
	to := from.Add(-time.Hour)
	if _, _, _, err := cleanAuditFilter(AuditFilter{From: &from, To: &to}); err == nil {
		t.Error("结束时间早于开始时间应当报错")
	}

	// 分页换算
	q, page, size, _ = cleanAuditFilter(AuditFilter{Page: 3, PageSize: 50})
	if q.Limit != 50 || q.Offset != 100 || page != 3 || size != 50 {
		t.Errorf("分页换算不对: limit=%d offset=%d", q.Limit, q.Offset)
	}
}

// 哈希必须能跨「写入 → 存库 → 读回」往返。两个真实的坑：
// PostgreSQL 的 timestamptz 只存到微秒，jsonb 会重排键并去掉空格。
func TestAuditHash_SurvivesDatabaseRoundTrip(t *testing.T) {
	// 写入侧：带纳秒的时间 + 键无序带空格的 JSON
	written := AuditEntry{
		AdminID: 7, Action: "drama.update", TargetType: "drama", TargetID: "1024",
		Before:    json.RawMessage(`{"title":"旧","sort":1}`),
		After:     json.RawMessage(`{"title": "新", "sort": 2}`),
		IPPrefix:  "203.0.0.0/16",
		RequestID: "req-1",
		CreatedAt: time.Date(2026, 10, 24, 10, 0, 0, 123456789, time.UTC),
	}
	// 读回侧：PG 截断到微秒，jsonb 规范化后键有序、带空格
	readBack := AuditEntry{
		AdminID: 7, Action: "drama.update", TargetType: "drama", TargetID: "1024",
		Before:    json.RawMessage(`{"sort": 1, "title": "旧"}`),
		After:     json.RawMessage(`{"sort": 2, "title": "新"}`),
		IPPrefix:  "203.0.0.0/16",
		RequestID: "req-1",
		CreatedAt: time.Date(2026, 10, 24, 10, 0, 0, 123456000, time.UTC),
	}
	if AuditHash("p", written) != AuditHash("p", readBack) {
		t.Error("写入时与读回后的哈希对不上：链在真实库里会每条都报断")
	}
}

// 规范化不能把不同的内容抹成一样。
func TestCanonicalJSON_StillDistinguishesContent(t *testing.T) {
	a := AuditHash("p", AuditEntry{After: json.RawMessage(`{"a":1}`)})
	b := AuditHash("p", AuditEntry{After: json.RawMessage(`{"a":2}`)})
	if a == b {
		t.Error("规范化后不同的内容算出了同一个哈希")
	}
	// 空与非空要区分
	empty := AuditHash("p", AuditEntry{})
	withNull := AuditHash("p", AuditEntry{After: json.RawMessage(`null`)})
	if empty == withNull {
		t.Error("没有 diff 与 diff 为 null 应当区分")
	}
}
