package content

import (
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"strconv"
	"strings"
	"time"

	"xsite/service/internal/errs"
)

// 游标是不透明字符串：base64url("<排序>|<种子>|<排序值>|<编号>")。
// 翻页在 SQL 里按 (排序列, id) 行值比较（迁移 00004 为此建了复合索引），不用 OFFSET，深翻页也不变慢。
// 客户端只能原样回传上一页的 next_cursor；解不开或与本次排序不一致时报错，避免把游标当偏移量误用。

const cursorFields = 4

// sortTopics 是专题列表的「排序」标记：专题只按编号倒序，游标里只有编号有意义。
const sortTopics DramaSort = "topics"

// randomSeedWindow 是随机排序的种子窗口：同一窗口内翻页顺序稳定，下一个窗口换一批。
const randomSeedWindow = 10 * time.Minute

var errBadCursor = errs.ErrBadRequest.WithMessage("分页参数无效，请重新从第一页开始")

// cursorPos 是解析后的游标位置。
type cursorPos struct {
	Sort  DramaSort
	Seed  int64
	Value string
	ID    int64
}

func (p cursorPos) encode() string {
	raw := string(p.Sort) + "|" + strconv.FormatInt(p.Seed, 10) + "|" + p.Value + "|" + strconv.FormatInt(p.ID, 10)
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

// decodeCursor 解析游标，并要求它与本次请求的排序一致。空游标返回零值（第一页）。
func decodeCursor(s string, sort DramaSort) (cursorPos, error) {
	if s == "" {
		return cursorPos{}, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return cursorPos{}, errBadCursor
	}
	parts := strings.Split(string(raw), "|")
	if len(parts) != cursorFields {
		return cursorPos{}, errBadCursor
	}
	p := cursorPos{Sort: DramaSort(parts[0]), Value: parts[2]}
	if p.Sort != sort {
		return cursorPos{}, errBadCursor
	}
	if p.Seed, err = strconv.ParseInt(parts[1], 10, 64); err != nil {
		return cursorPos{}, errBadCursor
	}
	if p.ID, err = strconv.ParseInt(parts[3], 10, 64); err != nil || p.ID <= 0 {
		return cursorPos{}, errBadCursor
	}
	return p, nil
}

// dramaCursor 把游标位置转为仓储用的比较值。
func (p cursorPos) dramaCursor() (DramaCursor, error) {
	if p.ID == 0 {
		return DramaCursor{}, nil
	}
	c := DramaCursor{ID: p.ID}
	switch p.Sort {
	case SortHot, SortRecommend:
		n, err := strconv.ParseInt(p.Value, 10, 64)
		if err != nil {
			return DramaCursor{}, errBadCursor
		}
		c.Num = n
	case SortRandom:
		if len(p.Value) != hex.EncodedLen(md5.Size) {
			return DramaCursor{}, errBadCursor
		}
		if _, err := hex.DecodeString(p.Value); err != nil {
			return DramaCursor{}, errBadCursor
		}
		c.Text = p.Value
	default: // latest
		// 微秒与 PostgreSQL 的 timestamptz 精度一致：精度低了会漏掉同一毫秒内的其他剧。
		us, err := strconv.ParseInt(p.Value, 10, 64)
		if err != nil {
			return DramaCursor{}, errBadCursor
		}
		c.At = time.UnixMicro(us).UTC()
	}
	return c, nil
}

// nextCursor 返回下一页的游标：取到的条数不足一页时没有下一页。
func nextCursor(sort DramaSort, seed int64, list []DramaCard, limit int) *string {
	if len(list) < limit {
		return nil
	}
	last := list[len(list)-1]
	p := cursorPos{Sort: sort, Seed: seed, ID: last.ID}
	switch sort {
	case SortHot:
		p.Value = strconv.FormatInt(last.HotScore, 10)
	case SortRecommend:
		p.Value = strconv.FormatInt(int64(last.SortWeight), 10)
	case SortRandom:
		p.Value = randomKey(last.ID, seed)
	default: // latest
		p.Value = strconv.FormatInt(latestKey(last).UnixMicro(), 10)
	}
	s := p.encode()
	return &s
}

// nextTopicCursor 返回专题列表下一页的游标（只按编号倒序）。
func nextTopicCursor(list []TopicCard, limit int) *string {
	if len(list) < limit {
		return nil
	}
	p := cursorPos{Sort: sortTopics, ID: list[len(list)-1].ID}
	s := p.encode()
	return &s
}

// latestKey 是「最新」排序的排序键，与 SQL 的 coalesce(publish_at, created_at) 一致。
func latestKey(c DramaCard) time.Time {
	if c.PublishAt != nil {
		return *c.PublishAt
	}
	return c.CreatedAt
}

// randomKey 是随机排序的排序键，必须与 SQL 的 md5(id::text || '|' || 种子) 一致。
// 这里的 MD5 只用于打乱顺序，不涉及安全性。
func randomKey(id, seed int64) string {
	sum := md5.Sum([]byte(strconv.FormatInt(id, 10) + "|" + strconv.FormatInt(seed, 10)))
	return hex.EncodeToString(sum[:])
}

// randomSeed 返回当前 10 分钟窗口的种子。
func randomSeed(now time.Time) int64 {
	return now.Unix() / int64(randomSeedWindow/time.Second)
}
