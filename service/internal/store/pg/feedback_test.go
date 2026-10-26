package pg

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	dom "xsite/service/internal/domain/feedback"
)

/*
TestGetVisibleFbProject_JoinedNeverNull 守一个真实踩过的坑。

GetVisibleFbProject 用 LEFT JOIN 关联成员行，`joined` 列曾经写成
`(m.status = 'active')::boolean`。匿名访客与未加入的用户没有成员行，m.status 是 NULL，
而 SQL 三值逻辑下 `NULL = 'active'` 的结果是 **NULL 而不是 false** ——
这一列返回 NULL，Go 侧按 bool 扫描时报 `cannot scan NULL into *bool`，
于是**所有项目详情页整个 500**，而且顺带让「申请加入」也挂了（join 成功后要回读项目）。

这个 bug 从 HTTP 状态码上看不出来：前台那几个路由是部分预渲染，
静态外壳照样返回 200，错误边界在流式内容里接管，表现为「页面能开但内容是报错卡片」。

所以这里直接压三种视角，任何一种报错或 joined 取值不对都算回归。
*/
func TestGetVisibleFbProject_JoinedNeverNull(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("未设置 DATABASE_URL，跳过数据库集成测试")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pool, err := NewPool(ctx, url)
	if err != nil {
		t.Skipf("数据库不可用，跳过：%v", err)
	}
	defer pool.Close()
	if err := Migrate(ctx, pool); err != nil {
		t.Fatalf("迁移失败：%v", err)
	}

	// 自建数据而不是依赖种子：种子会被重置，测试不该跟着一起坏。
	suffix := time.Now().UnixNano()
	slug := fmt.Sprintf("jointest-%d", suffix)
	// key_prefix 有唯一约束，格式 ^[A-Z][A-Z0-9]{1,7}$（长 2–8）。
	// 用固定值会让第二次运行直接撞唯一键，所以从时间戳取 7 位。
	keyPrefix := fmt.Sprintf("J%07d", suffix%10_000_000)

	var projectID int64
	err = pool.QueryRow(ctx, `
		INSERT INTO fb_project (slug, key_prefix, name, visibility, status)
		VALUES ($1, $2, '加入状态回归用项目', 'public', 'active')
		RETURNING id`, slug, keyPrefix).Scan(&projectID)
	if err != nil {
		t.Fatalf("建项目失败：%v", err)
	}

	newUser := func(handle string) int64 {
		var id int64
		err := pool.QueryRow(ctx, `
			INSERT INTO fb_user (handle, handle_norm, password_hash)
			VALUES ($1, lower($1), 'h:placeholder')
			RETURNING id`, handle).Scan(&id)
		if err != nil {
			t.Fatalf("建用户 %s 失败：%v", handle, err)
		}
		return id
	}
	activeUser := newUser(fmt.Sprintf("jt_active_%d", suffix))
	pendingUser := newUser(fmt.Sprintf("jt_pending_%d", suffix))
	strangerUser := newUser(fmt.Sprintf("jt_stranger_%d", suffix))

	for _, m := range []struct {
		userID int64
		status string
	}{{activeUser, "active"}, {pendingUser, "pending"}} {
		if _, err := pool.Exec(ctx, `
			INSERT INTO fb_project_member (project_id, user_id, role, status)
			VALUES ($1, $2, 'member', $3)`, projectID, m.userID, m.status); err != nil {
			t.Fatalf("建成员（%s）失败：%v", m.status, err)
		}
	}

	/*
	 * 清理用 defer 而不是 t.Cleanup。
	 *
	 * t.Cleanup 的回调在测试函数**返回之后**才执行，那时上面的
	 * `defer pool.Close()` 已经把连接池关了，清理语句全部失败。
	 * defer 是 LIFO：这里注册得比 pool.Close() 晚，所以会先执行，池还开着。
	 *
	 * 错误也不能静默吞掉。清不掉就意味着库里留下一个占着唯一 key_prefix 的项目，
	 * 下次运行会直接撞唯一键失败 —— 而那个失败看起来跟被测逻辑毫无关系，很难查。
	 * （这两点都是实际踩过的。）
	 */
	defer func() {
		bg := context.Background()
		for _, step := range []struct {
			what string
			sql  string
			args []any
		}{
			{"成员", `DELETE FROM fb_project_member WHERE project_id = $1`, []any{projectID}},
			{"项目", `DELETE FROM fb_project WHERE id = $1`, []any{projectID}},
			{"用户", `DELETE FROM fb_user WHERE id = ANY($1)`,
				[]any{[]int64{activeUser, pendingUser, strangerUser}}},
		} {
			if _, err := pool.Exec(bg, step.sql, step.args...); err != nil {
				t.Errorf("清理%s失败（会污染后续运行）：%v", step.what, err)
			}
		}
	}()

	repo := NewFeedbackRepo(pool)

	cases := []struct {
		name         string
		viewer       dom.Viewer
		wantJoined   bool
		wantStatus   string
		wantRoleKind string
	}{
		// 这一条就是原来会 500 的那个：匿名访客没有成员行
		{"匿名访客", dom.Viewer{}, false, "", "无角色"},
		// 登录了但没加入这个项目，同样没有成员行
		{"陌生用户", dom.Viewer{UserID: strangerUser}, false, "", "无角色"},
		{"已加入成员", dom.Viewer{UserID: activeUser}, true, "active", "member"},
		// 申请中：能看到自己的 pending 状态，但 joined 必须还是 false
		{"申请中用户", dom.Viewer{UserID: pendingUser}, false, "pending", "member"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			view, err := repo.VisibleProject(ctx, c.viewer, slug)
			if err != nil {
				t.Fatalf("读取项目失败（这正是该 bug 的症状）：%v", err)
			}
			if view.Joined != c.wantJoined {
				t.Errorf("joined = %v，期望 %v", view.Joined, c.wantJoined)
			}
			if view.MemberStatus != c.wantStatus {
				t.Errorf("member_status = %q，期望 %q", view.MemberStatus, c.wantStatus)
			}
			if c.wantRoleKind == "无角色" {
				if view.MemberRole != "" {
					t.Errorf("没有成员行时 member_role 应为空串，得到 %q", view.MemberRole)
				}
			} else if view.MemberRole == "" {
				t.Errorf("成员的 member_role 不该为空")
			}
			if view.Project.Slug != slug {
				t.Errorf("取回的项目不对：%q", view.Project.Slug)
			}
		})
	}
}
