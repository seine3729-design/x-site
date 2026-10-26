package platform

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"time"
)

// 审计哈希链（S4-7，docs/06 第 34 节）。
//
// 每条记录的哈希覆盖「上一条的哈希 + 本条全部业务字段」，所以改动任何一行、或者删掉中间一行，
// 后面所有记录的链接都对不上。这挡的是「改完库再抹掉痕迹」，不是防止有库权限的人写入新记录 ——
// 后者需要把哈希外发到别处对账，不在本任务范围。
//
// 刻意不把 id 放进哈希：id 由数据库在插入时生成，放进去会变成循环依赖。链的顺序由 prev_hash 表达。

// chainSep 是哈希输入里的字段分隔符（ASCII 单元分隔符）。
// 用它而不是直接拼接：否则 ("ab","c") 与 ("a","bc") 会算出同一个哈希。
const chainSep = "\x1f"

// AuditHash 计算一条审计记录的哈希。prev 是上一条的哈希（第一条为空串）。
//
// 两处细节决定了「写入时算的哈希」与「读回来重算的哈希」能对上：
//
//   - 时间截断到微秒：PostgreSQL 的 timestamptz 只存到微秒，Go 的 time.Time 带纳秒。
//     不截断的话，写入时用纳秒算、读回来是微秒，每一条都会对不上。
//   - diff 走 canonicalJSON：jsonb 会把 JSON 规范化（重排键、去掉空格），
//     读回来的字节和写入时不一样。两边都先规范化，格式差异就被抹平了。
func AuditHash(prev string, e AuditEntry) string {
	h := sha256.New()
	write := func(s string) {
		h.Write([]byte(s))
		h.Write([]byte(chainSep))
	}
	write(prev)
	write(strconv.FormatInt(e.AdminID, 10))
	write(e.Action)
	write(e.TargetType)
	write(e.TargetID)
	write(canonicalJSON(e.Before))
	write(canonicalJSON(e.After))
	write(e.IPPrefix)
	write(e.RequestID)
	// 固定用 UTC：同一时刻在不同时区的写法不同，但哈希必须一致
	write(e.CreatedAt.UTC().Truncate(time.Microsecond).Format(time.RFC3339Nano))
	return hex.EncodeToString(h.Sum(nil))
}

// canonicalJSON 把 JSON 规范化成「键有序、无多余空格」的形式，抹平 jsonb 往返带来的格式差异。
// 空值返回空串；解析不了的（理论上不会有，写入前都经过 RedactJSON）按原文处理。
func canonicalJSON(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return string(raw)
	}
	// encoding/json 序列化 map 时按键排序，且输出紧凑
	b, err := json.Marshal(v)
	if err != nil {
		return string(raw)
	}
	return string(b)
}

// AuditChainRepo 读取审计链用于校验。
type AuditChainRepo interface {
	// ChainPage 按 id 升序返回带哈希的记录（afterID 之后的，最多 limit 条）。
	ChainPage(ctx context.Context, afterID int64, limit int) ([]AuditRecord, error)
	// ChainStats 返回带哈希的记录数与时间范围，以及启用哈希链之前的记录数。
	ChainStats(ctx context.Context) (AuditChainStats, error)
}

// AuditRecord 是一条完整的审计记录（含哈希字段），校验时逐条重算比对。
type AuditRecord struct {
	ID       int64
	Entry    AuditEntry
	PrevHash string
	Hash     string
}

// AuditChainStats 是链的概览。
type AuditChainStats struct {
	Chained   int64     `json:"chained"`   // 带哈希、参与校验的记录数
	Unchained int64     `json:"unchained"` // 启用哈希链之前的记录数（不参与校验）
	FirstAt   time.Time `json:"first_at"`
	LastAt    time.Time `json:"last_at"`
}

// AuditVerifyResult 是一次校验的结果。
type AuditVerifyResult struct {
	OK      bool  `json:"ok"`
	Checked int64 `json:"checked"` // 实际校验过的记录数
	AuditChainStats

	// Broken 是第一处对不上的记录。OK 为真时为 nil。
	// 只报第一处：链一旦断开，后面每条的 prev_hash 都会对不上，全列出来没有意义。
	Broken *AuditBreak `json:"broken"`
}

// AuditBreak 描述一处断链。
type AuditBreak struct {
	ID     int64     `json:"id"`
	Reason string    `json:"reason"`
	Action string    `json:"action"`
	At     time.Time `json:"at"`
}

// 单次校验扫描的上限：审计表会一直长，全表扫描在后台接口里不合适。
const (
	auditVerifyBatch = 1000
	auditVerifyMax   = 50000
)

// VerifyAuditChain 从头按 id 升序走链，返回第一处对不上的位置。
//
// 两种对不上：
//   - 本条的 prev_hash 不等于上一条的 hash（中间有记录被删掉或被插入）
//   - 本条的 hash 不等于按内容重算的结果（内容被改过）
func (s *AuditService) VerifyAuditChain(ctx context.Context, max int) (AuditVerifyResult, error) {
	if max <= 0 || max > auditVerifyMax {
		max = auditVerifyMax
	}
	stats, err := s.chain.ChainStats(ctx)
	if err != nil {
		return AuditVerifyResult{}, err
	}
	out := AuditVerifyResult{OK: true, AuditChainStats: stats}

	var (
		afterID int64
		prev    string
		first   = true
	)
	for out.Checked < int64(max) {
		batch := min(auditVerifyBatch, max-int(out.Checked))
		records, err := s.chain.ChainPage(ctx, afterID, batch)
		if err != nil {
			return AuditVerifyResult{}, err
		}
		if len(records) == 0 {
			break
		}
		for _, r := range records {
			// 第一条的 prev_hash 允许是任意值：链可能是在已有记录之后才启用的，
			// 那时第一条带哈希的记录没有「上一条」可以指向。
			if !first && r.PrevHash != prev {
				out.OK = false
				out.Broken = &AuditBreak{
					ID: r.ID, Action: r.Entry.Action, At: r.Entry.CreatedAt,
					Reason: "与上一条的哈希对不上：中间有记录被删除或插入",
				}
				return out, nil
			}
			if want := AuditHash(r.PrevHash, r.Entry); want != r.Hash {
				out.OK = false
				out.Broken = &AuditBreak{
					ID: r.ID, Action: r.Entry.Action, At: r.Entry.CreatedAt,
					Reason: "内容与哈希对不上：这条记录被改动过",
				}
				return out, nil
			}
			prev = r.Hash
			afterID = r.ID
			first = false
			out.Checked++
		}
		if len(records) < batch {
			break
		}
	}
	return out, nil
}
