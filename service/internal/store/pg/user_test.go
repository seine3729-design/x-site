package pg

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"xsite/service/internal/domain/user"
	"xsite/service/internal/errs"
)

// 注册用户仓储（S2-1）的 PostgreSQL 集成测试：创建、唯一冲突、按名 / ID 查询、改密、软删后不可见。
func TestUserRepo(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewUserRepo(pool)

	username := fmt.Sprintf("tu%d", time.Now().UnixNano())
	var createdID int64
	t.Cleanup(func() {
		if createdID > 0 {
			_, _ = pool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, createdID)
		}
	})

	u, err := repo.Create(ctx, user.NewUser{Username: username, Nickname: "测试", PasswordHash: "$argon2id$hash"})
	if err != nil {
		t.Fatalf("创建用户：%v", err)
	}
	createdID = u.ID
	if u.ID <= 0 || u.Username != username || u.Status != user.StatusNormal {
		t.Fatalf("创建结果不对：%+v", u)
	}

	// 重复用户名 → CONFLICT
	if _, err := repo.Create(ctx, user.NewUser{Username: username, PasswordHash: "x"}); !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("重复用户名应 CONFLICT，得到 %v", err)
	}

	// 按用户名 / ID 查询，含密码哈希
	byName, err := repo.ByUsername(ctx, username)
	if err != nil || byName.ID != u.ID || byName.PasswordHash != "$argon2id$hash" {
		t.Fatalf("按用户名查询不对：%+v / %v", byName, err)
	}
	if byID, err := repo.ByID(ctx, u.ID); err != nil || byID.Username != username {
		t.Fatalf("按 ID 查询不对：%+v / %v", byID, err)
	}

	// 未知用户名 → NotFound
	if _, err := repo.ByUsername(ctx, username+"x"); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("未知用户名应 NotFound，得到 %v", err)
	}

	// 改密
	if err := repo.UpdatePassword(ctx, u.ID, "$argon2id$new"); err != nil {
		t.Fatal(err)
	}
	if after, _ := repo.ByID(ctx, u.ID); after.PasswordHash != "$argon2id$new" {
		t.Fatalf("改密后哈希应更新：%q", after.PasswordHash)
	}

	// 软删后视为不存在（回收站）
	if _, err := pool.Exec(ctx, `UPDATE "user" SET deleted_at = now() WHERE id = $1`, u.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ByID(ctx, u.ID); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("软删后按 ID 应 NotFound，得到 %v", err)
	}
	if _, err := repo.ByUsername(ctx, username); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("软删后按用户名应 NotFound，得到 %v", err)
	}
}

// 资料读写（S2-3）集成测试：GetProfile 默认值、UpdateProfile 的 PATCH 语义（nil 不改、空串清空）。
func TestUserRepo_Profile(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewUserRepo(pool)

	username := fmt.Sprintf("tp%d", time.Now().UnixNano())
	u, err := repo.Create(ctx, user.NewUser{Username: username, Nickname: "原名", PasswordHash: "$argon2id$h"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM "user" WHERE id=$1`, u.ID) })

	// 默认资料
	p, err := repo.GetProfile(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	if p.Nickname != "原名" || p.Avatar != "" || p.Bio != "" || p.NicknameStatus != "ok" {
		t.Fatalf("默认资料不对：%+v", p)
	}

	ptr := func(s string) *string { return &s }

	// 只改简介：昵称、头像不动
	p, err = repo.UpdateProfile(ctx, u.ID, user.ProfileFields{Bio: ptr("你好")})
	if err != nil {
		t.Fatal(err)
	}
	if p.Nickname != "原名" || p.Bio != "你好" {
		t.Fatalf("只改简介后：%+v", p)
	}

	// 改昵称 + 头像；简介保持
	p, err = repo.UpdateProfile(ctx, u.ID, user.ProfileFields{Nickname: ptr("新名"), Avatar: ptr("avatar/1.png")})
	if err != nil {
		t.Fatal(err)
	}
	if p.Nickname != "新名" || p.Avatar != "avatar/1.png" || p.Bio != "你好" {
		t.Fatalf("改昵称+头像后：%+v", p)
	}

	// 空串清空简介（区别于 nil 不改）
	p, err = repo.UpdateProfile(ctx, u.ID, user.ProfileFields{Bio: ptr("")})
	if err != nil {
		t.Fatal(err)
	}
	if p.Bio != "" || p.Nickname != "新名" {
		t.Fatalf("空串应清空简介、昵称不动：%+v", p)
	}
}
