package pg

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// 内容域迁移（00004，演员表由 00010 删除；docs/02 第 2、8 节）的约束、级联与索引。仓储随 S1-3 实现，这里直接写 SQL。

// pgCode 返回 PostgreSQL 错误码：23505 唯一约束、23514 CHECK、23503 外键。
func pgCode(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code
	}
	return ""
}

// insertID 执行带 RETURNING id 的插入。
func insertID(t *testing.T, pool *pgxpool.Pool, sql string, args ...any) int64 {
	t.Helper()
	var id int64
	if err := pool.QueryRow(context.Background(), sql, args...).Scan(&id); err != nil {
		t.Fatalf("插入失败：%v\n%s", err, sql)
	}
	return id
}

func count(t *testing.T, pool *pgxpool.Pool, sql string, args ...any) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(), sql, args...).Scan(&n); err != nil {
		t.Fatalf("查询失败：%v\n%s", err, sql)
	}
	return n
}

func TestContentSchema_ConstraintsAndCascade(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	mark := fmt.Sprintf("it%d", time.Now().UnixNano())

	dramaID := insertID(t, pool, `INSERT INTO drama (title) VALUES ($1) RETURNING id`, "约束测试"+mark)
	taxA := insertID(t, pool, `INSERT INTO taxonomy (name, slug) VALUES ('类型', $1) RETURNING id`, mark+"-a")
	taxB := insertID(t, pool, `INSERT INTO taxonomy (name, slug) VALUES ('地区', $1) RETURNING id`, mark+"-b")
	t.Cleanup(func() {
		bg := context.Background()
		_, _ = pool.Exec(bg, `DELETE FROM topic WHERE slug LIKE $1`, mark+"%")
		_, _ = pool.Exec(bg, `DELETE FROM drama WHERE id = $1`, dramaID)
		_, _ = pool.Exec(bg, `DELETE FROM tag WHERE slug LIKE $1`, mark+"%")
		_, _ = pool.Exec(bg, `DELETE FROM taxonomy WHERE id IN ($1, $2)`, taxA, taxB)
	})

	// 默认值：新剧为草稿；分集免费、待转封装、视频对象为空、没有首帧图
	epID := insertID(t, pool, `INSERT INTO episode (drama_id, number) VALUES ($1, 1) RETURNING id`, dramaID)
	var status, access, media, renditions, poster string
	var videoObject *int64
	if err := pool.QueryRow(ctx, `SELECT d.status, e.access, e.media_status, e.renditions::text, e.video_object_id, e.poster
		FROM episode e JOIN drama d ON d.id = e.drama_id WHERE e.id = $1`, epID).
		Scan(&status, &access, &media, &renditions, &videoObject, &poster); err != nil {
		t.Fatal(err)
	}
	if status != "draft" || access != "free" || media != "pending" || renditions != "[]" || videoObject != nil || poster != "" {
		t.Fatalf("默认值不符合预期：%s %s %s %s %v %q", status, access, media, renditions, videoObject, poster)
	}
	// 迁移 00007（S1-5）起 video_object_id 指向 media_object，不能写入不存在的对象
	if _, err := pool.Exec(ctx, `UPDATE episode SET video_object_id = 987654321 WHERE id = $1`, epID); pgCode(err) != "23503" {
		t.Fatalf("video_object_id 应有外键约束，得到 %v", err)
	}

	tagID := insertID(t, pool, `INSERT INTO tag (name, slug) VALUES ('甜宠', $1) RETURNING id`, mark+"-tag")
	// 两个维度下同名同 slug 的取值；只有 A 设为频道
	termSlug := mark + "-city"
	termA := insertID(t, pool, `INSERT INTO taxonomy_term (taxonomy_id, name, slug, is_channel) VALUES ($1, '都市', $2, true) RETURNING id`, taxA, termSlug)
	termB := insertID(t, pool, `INSERT INTO taxonomy_term (taxonomy_id, name, slug) VALUES ($1, '都市', $2) RETURNING id`, taxB, termSlug)
	topicID := insertID(t, pool, `INSERT INTO topic (title, slug) VALUES ('专题', $1) RETURNING id`, mark+"-topic")
	for _, r := range []struct {
		sql  string
		args []any
	}{
		{`INSERT INTO drama_tag (drama_id, tag_id) VALUES ($1, $2)`, []any{dramaID, tagID}},
		{`INSERT INTO drama_term (drama_id, term_id) VALUES ($1, $2), ($1, $3)`, []any{dramaID, termA, termB}},
		{`INSERT INTO topic_drama (topic_id, drama_id) VALUES ($1, $2)`, []any{topicID, dramaID}},
	} {
		if _, err := pool.Exec(ctx, r.sql, r.args...); err != nil {
			t.Fatalf("写关联失败：%v\n%s", err, r.sql)
		}
	}

	violations := []struct {
		name string
		code string
		sql  string
		args []any
	}{
		{"同一部剧集号重复", "23505", `INSERT INTO episode (drama_id, number) VALUES ($1, 1)`, []any{dramaID}},
		{"集号必须大于 0", "23514", `INSERT INTO episode (drama_id, number) VALUES ($1, 0)`, []any{dramaID}},
		{"分集所属剧不存在", "23503", `INSERT INTO episode (drama_id, number) VALUES (-1, 1)`, nil},
		{"剧状态只能是三种之一", "23514", `UPDATE drama SET status = 'scheduled' WHERE id = $1`, []any{dramaID}},
		{"剧名不能为空", "23514", `INSERT INTO drama (title) VALUES ('')`, nil},
		{"评分不能为负", "23514", `UPDATE drama SET score = -1 WHERE id = $1`, []any{dramaID}},
		{"谁能看只能是三种之一", "23514", `UPDATE episode SET access = 'paid' WHERE id = $1`, []any{epID}},
		{"码率阶梯必须是数组", "23514", `UPDATE episode SET renditions = '{}' WHERE id = $1`, []any{epID}},
		{"标签不能并入自己", "23514", `UPDATE tag SET merged_into_id = id WHERE id = $1`, []any{tagID}},
		{"取值 slug 在维度内唯一", "23505", `INSERT INTO taxonomy_term (taxonomy_id, name, slug) VALUES ($1, '重复', $2)`, []any{taxA, termSlug}},
		{"频道取值 slug 全站唯一", "23505", `UPDATE taxonomy_term SET is_channel = true WHERE id = $1`, []any{termB}},
		{"FAQ 必须是数组", "23514", `UPDATE topic SET faq_json = '{}' WHERE id = $1`, []any{topicID}},
	}
	for _, v := range violations {
		_, err := pool.Exec(ctx, v.sql, v.args...)
		if got := pgCode(err); got != v.code {
			t.Errorf("%s：期望错误码 %s，得到 %q（%v）", v.name, v.code, got, err)
		}
	}

	// 取值编号：空字符串可以重复，填写后全站唯一
	if _, err := pool.Exec(ctx, `UPDATE taxonomy_term SET code = '' WHERE id IN ($1, $2)`, termA, termB); err != nil {
		t.Fatalf("空编号应允许重复：%v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE taxonomy_term SET code = $1 WHERE id = $2`, mark, termA); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE taxonomy_term SET code = $1 WHERE id = $2`, mark, termB); pgCode(err) != "23505" {
		t.Errorf("取值编号应全站唯一，得到 %v", err)
	}
	// 剧的 slug 可空，空值不冲突
	for range 2 {
		id := insertID(t, pool, `INSERT INTO drama (title) VALUES ($1) RETURNING id`, "无 slug"+mark)
		t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM drama WHERE id = $1`, id) })
	}

	// 删除维度：取值与剧的关联一起删除
	if _, err := pool.Exec(ctx, `DELETE FROM taxonomy WHERE id = $1`, taxB); err != nil {
		t.Fatal(err)
	}
	if n := count(t, pool, `SELECT count(*) FROM drama_term WHERE drama_id = $1`, dramaID); n != 1 {
		t.Errorf("删除维度后只剩另一维度的取值，得到 %d 行", n)
	}

	// 物理删除剧（回收站到期）：分集与全部关联随之删除，标签与专题本身保留
	if _, err := pool.Exec(ctx, `DELETE FROM drama WHERE id = $1`, dramaID); err != nil {
		t.Fatal(err)
	}
	left := count(t, pool, `SELECT (SELECT count(*) FROM episode WHERE drama_id = $1)
		+ (SELECT count(*) FROM drama_tag WHERE drama_id = $1)
		+ (SELECT count(*) FROM drama_term WHERE drama_id = $1)
		+ (SELECT count(*) FROM topic_drama WHERE drama_id = $1)`, dramaID)
	if left != 0 {
		t.Errorf("删除剧后分集与关联应级联删除，还剩 %d 行", left)
	}
	if n := count(t, pool, `SELECT count(*) FROM tag WHERE id = $1`, tagID) +
		count(t, pool, `SELECT count(*) FROM topic WHERE id = $1`, topicID); n != 2 {
		t.Errorf("标签与专题不应随剧删除")
	}
}

// 迁移 00010 删除演员模块：演员表与剧的演员表都不存在。
func TestContentSchema_ActorTablesDropped(t *testing.T) {
	pool := testPool(t)
	for _, table := range []string{"actor", "drama_actor"} {
		var exists bool
		if err := pool.QueryRow(context.Background(), `SELECT to_regclass($1) IS NOT NULL`, table).Scan(&exists); err != nil {
			t.Fatal(err)
		}
		if exists {
			t.Errorf("表 %s 应已删除", table)
		}
	}
}

// 中文搜索：两个字的词能命中剧名与别名，并且走 pg_bigm 索引（docs/02 第 8 节）；三种排序的游标分页走复合索引。
func TestContentSchema_IndexesForSearchAndSort(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	mark := fmt.Sprintf("%d", time.Now().UnixNano())

	target := insertID(t, pool, `INSERT INTO drama (title, aliases, status) VALUES ($1, $2, 'published') RETURNING id`,
		"霸道总裁爱上我"+mark, []string{"心动的信号", "又名" + mark})
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM drama WHERE id = $1 OR title LIKE $2`, target, "排序测试"+mark+"%")
	})
	if _, err := pool.Exec(ctx, `INSERT INTO drama (title, status, hot_score, sort_weight, publish_at)
		SELECT '排序测试' || $1 || g, 'published', g, g, now() - g * interval '1 hour' FROM generate_series(1, 300) g`, mark); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `ANALYZE drama`); err != nil {
		t.Fatal(err)
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	// 禁用顺序扫描，确认查询能用上索引（数据量小时规划器本来就可能选顺序扫描）
	if _, err := tx.Exec(ctx, `SET LOCAL enable_seqscan = off`); err != nil {
		t.Fatal(err)
	}

	search := []struct {
		name, where, word, index string
		hit                      bool
	}{
		{"剧名两字词", "title LIKE likequery($1)", "总裁", "drama_title_bigm_idx", true},
		{"别名两字词", "aliases_text(aliases) LIKE likequery($1)", "心动", "drama_aliases_bigm_idx", true},
		{"剧名不含的词", "title LIKE likequery($1)", "总监", "drama_title_bigm_idx", false},
	}
	for _, s := range search {
		sql := `SELECT id FROM drama WHERE ` + s.where
		ids, err := collectIDs(ctx, tx, sql, s.word)
		if err != nil {
			t.Fatal(err)
		}
		if got := containsID(ids, target); got != s.hit {
			t.Errorf("%s「%s」：命中 = %v，期望 %v", s.name, s.word, got, s.hit)
		}
		assertPlanUses(ctx, t, tx, strings.Replace(sql, "$1", "'"+s.word+"'", 1), s.index)
	}

	for _, s := range []struct{ column, index string }{
		{"hot_score", "drama_status_hot_idx"},
		{"sort_weight", "drama_status_recommend_idx"},
		{"publish_at", "drama_status_publish_idx"},
	} {
		cursor := "100"
		if s.column == "publish_at" {
			cursor = "now()"
		}
		sql := fmt.Sprintf(`SELECT id FROM drama WHERE status = 'published' AND (%[1]s, id) < (%[2]s, 9223372036854775807)
			ORDER BY %[1]s DESC, id DESC LIMIT 20`, s.column, cursor)
		ids, err := collectIDs(ctx, tx, sql)
		if err != nil {
			t.Fatal(err)
		}
		if len(ids) != 20 {
			t.Errorf("%s 游标分页应返回一整页，得到 %d 条", s.column, len(ids))
		}
		assertPlanUses(ctx, t, tx, sql, s.index)
	}
}

func collectIDs(ctx context.Context, tx pgx.Tx, sql string, args ...any) ([]int64, error) {
	rows, err := tx.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[int64])
}

func containsID(ids []int64, id int64) bool {
	for _, x := range ids {
		if x == id {
			return true
		}
	}
	return false
}

func assertPlanUses(ctx context.Context, t *testing.T, tx pgx.Tx, sql, index string) {
	t.Helper()
	rows, err := tx.Query(ctx, "EXPLAIN (COSTS OFF) "+sql)
	if err != nil {
		t.Fatal(err)
	}
	lines, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatal(err)
	}
	plan := strings.Join(lines, "\n")
	if !strings.Contains(plan, index) {
		t.Errorf("查询应使用索引 %s：\n%s\n%s", index, sql, plan)
	}
}
