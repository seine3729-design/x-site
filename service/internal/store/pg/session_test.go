package pg

import (
	"context"
	"crypto/sha256"
	"fmt"
	"testing"
	"time"

	"uuid"

	"xsite/service/internal/domain/identity"

	"github.com/jackc/pgx/v5/pgxpool"
)

// 游客会话（S1-10）的 PostgreSQL 集成测试：真实事务里的令牌轮换与复用检测、过期与未知令牌、登出吊销整族。
func TestSessionRepo_RotateAndReuse(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewSessionRepo(pool)

	device := uuid.NewV7()
	t.Cleanup(func() {
		// 删游客设备即级联删掉它的全部会话
		_, _ = pool.Exec(context.Background(), `DELETE FROM guest_device WHERE id = $1`, pgUUID(device))
	})
	if err := repo.CreateGuest(ctx, identity.GuestDevice{ID: device, Platform: "web", AttestLevel: "none", SourceChannel: "test"}); err != nil {
		t.Fatalf("建游客设备：%v", err)
	}

	nonce := time.Now().UnixNano()
	hash := func(tag string) []byte {
		sum := sha256.Sum256([]byte(fmt.Sprintf("s110-%d-%s", nonce, tag)))
		return sum[:]
	}
	now := time.Now()
	exp := now.Add(identity.GuestRefreshTTL)
	family := uuid.NewV7()

	if err := repo.CreateSession(ctx, identity.NewSession{
		ID: uuid.NewV7(), FamilyID: family, GuestDeviceID: device, RefreshHash: hash("r1"),
		Platform: "web", ExpiresAt: exp,
	}); err != nil {
		t.Fatalf("建会话：%v", err)
	}

	// 轮换：r1 → r2，返回旧行主体
	res, err := repo.Rotate(ctx, hash("r1"), identity.RotateInput{
		NewSessionID: uuid.NewV7(), NewRefreshHash: hash("r2"), GuestExpiresAt: exp,
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	if res.Status != identity.RotateOK || res.GuestDeviceID != device || res.UserID != 0 || res.Platform != "web" {
		t.Fatalf("轮换结果不对：%+v（设备应为 %s）", res, device)
	}
	if n := activeInFamily(t, pool, family); n != 1 {
		t.Fatalf("轮换后族内应只剩 1 个有效会话（r2），实际 %d", n)
	}

	// 复用：再用 r1（已吊销）刷新 → 判为泄露，吊销整族
	reuse, err := repo.Rotate(ctx, hash("r1"), identity.RotateInput{
		NewSessionID: uuid.NewV7(), NewRefreshHash: hash("r3"), GuestExpiresAt: exp,
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	if reuse.Status != identity.RotateReuse {
		t.Fatalf("复用旧令牌应 RotateReuse，得到 %v", reuse.Status)
	}
	if n := activeInFamily(t, pool, family); n != 0 {
		t.Fatalf("复用检测后整族应全部吊销，仍有 %d 个有效", n)
	}

	// 整族已死：新令牌 r2 也不能再轮换
	dead, err := repo.Rotate(ctx, hash("r2"), identity.RotateInput{
		NewSessionID: uuid.NewV7(), NewRefreshHash: hash("r4"), GuestExpiresAt: exp,
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	if dead.Status != identity.RotateReuse {
		t.Fatalf("整族吊销后 r2 也应判为复用，得到 %v", dead.Status)
	}
}

func TestSessionRepo_RotateNotFoundAndExpired(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewSessionRepo(pool)

	device := uuid.NewV7()
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM guest_device WHERE id = $1`, pgUUID(device))
	})
	if err := repo.CreateGuest(ctx, identity.GuestDevice{ID: device, Platform: "web", AttestLevel: "none"}); err != nil {
		t.Fatal(err)
	}
	nonce := time.Now().UnixNano()
	hash := func(tag string) []byte {
		sum := sha256.Sum256([]byte(fmt.Sprintf("s110nf-%d-%s", nonce, tag)))
		return sum[:]
	}
	now := time.Now()

	// 未知刷新令牌
	res, err := repo.Rotate(ctx, hash("missing"), identity.RotateInput{NewSessionID: uuid.NewV7(), NewRefreshHash: hash("x"), GuestExpiresAt: now.Add(time.Hour)}, now)
	if err != nil || res.Status != identity.RotateNotFound {
		t.Fatalf("未知令牌应 RotateNotFound，得到 %v / %v", res.Status, err)
	}

	// 过期会话
	if err := repo.CreateSession(ctx, identity.NewSession{
		ID: uuid.NewV7(), FamilyID: uuid.NewV7(), GuestDeviceID: device, RefreshHash: hash("old"),
		Platform: "web", ExpiresAt: now.Add(-time.Minute),
	}); err != nil {
		t.Fatal(err)
	}
	res, err = repo.Rotate(ctx, hash("old"), identity.RotateInput{NewSessionID: uuid.NewV7(), NewRefreshHash: hash("new"), GuestExpiresAt: now.Add(time.Hour)}, now)
	if err != nil || res.Status != identity.RotateExpired {
		t.Fatalf("过期会话应 RotateExpired，得到 %v / %v", res.Status, err)
	}
}

func TestSessionRepo_LogoutRevokesFamily(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewSessionRepo(pool)

	device := uuid.NewV7()
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM guest_device WHERE id = $1`, pgUUID(device))
	})
	if err := repo.CreateGuest(ctx, identity.GuestDevice{ID: device, Platform: "web", AttestLevel: "none"}); err != nil {
		t.Fatal(err)
	}
	nonce := time.Now().UnixNano()
	h := sha256.Sum256([]byte(fmt.Sprintf("s110lo-%d", nonce)))
	family := uuid.NewV7()
	if err := repo.CreateSession(ctx, identity.NewSession{
		ID: uuid.NewV7(), FamilyID: family, GuestDeviceID: device, RefreshHash: h[:],
		Platform: "web", ExpiresAt: time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	if err := repo.RevokeFamilyByRefresh(ctx, h[:], "logout"); err != nil {
		t.Fatal(err)
	}
	if n := activeInFamily(t, pool, family); n != 0 {
		t.Fatalf("登出后整族应吊销，仍有 %d 个有效", n)
	}
	// 幂等：对不存在的令牌登出不报错、不影响任何行
	if err := repo.RevokeFamilyByRefresh(ctx, sha256Bytes("nope"), "logout"); err != nil {
		t.Fatalf("未知令牌登出应幂等成功：%v", err)
	}
}

func activeInFamily(t *testing.T, pool *pgxpool.Pool, family uuid.UUID) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM user_session WHERE family_id = $1 AND revoked_at IS NULL`, pgUUID(family)).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func sha256Bytes(s string) []byte { sum := sha256.Sum256([]byte(s)); return sum[:] }

// 注册用户会话管理（S2-1）的 PostgreSQL 集成测试：我的设备列表、设备下线（user_id 作用域防越权）、改密吊销全部。
func TestSessionRepo_UserSessions(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	repo := NewSessionRepo(pool)

	nonce := time.Now().UnixNano()
	var u1, u2 int64
	if err := pool.QueryRow(ctx, `INSERT INTO "user" (username, password_hash) VALUES ($1, $2) RETURNING id`,
		fmt.Sprintf("tu%d", nonce), "x").Scan(&u1); err != nil {
		t.Fatalf("建用户 u1：%v", err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO "user" (username, password_hash) VALUES ($1, $2) RETURNING id`,
		fmt.Sprintf("tv%d", nonce), "x").Scan(&u2); err != nil {
		t.Fatalf("建用户 u2：%v", err)
	}
	t.Cleanup(func() {
		// 删用户即级联删其会话
		_, _ = pool.Exec(context.Background(), `DELETE FROM "user" WHERE id = ANY($1)`, []int64{u1, u2})
	})

	hash := func(tag string) []byte {
		sum := sha256.Sum256([]byte(fmt.Sprintf("s21-%d-%s", nonce, tag)))
		return sum[:]
	}
	now := time.Now()
	exp := now.Add(identity.UserRefreshTTL)
	mk := func(user int64, tag string) uuid.UUID {
		sid := uuid.NewV7()
		if err := repo.CreateSession(ctx, identity.NewSession{
			ID: sid, FamilyID: uuid.NewV7(), UserID: user, RefreshHash: hash(tag),
			Platform: "web", ExpiresAt: exp,
		}); err != nil {
			t.Fatalf("建会话 %s：%v", tag, err)
		}
		return sid
	}
	sid1 := mk(u1, "a")
	_ = mk(u1, "b")
	mk(u2, "c")

	// 列表：u1 两个会话
	l1, err := repo.ListUserSessions(ctx, u1, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(l1) != 2 {
		t.Fatalf("u1 应有 2 个会话，得到 %d", len(l1))
	}

	// 防越权：u2 吊销 u1 的会话 → 0 行（带 user_id 作用域）
	if n, err := repo.RevokeSessionByID(ctx, u2, sid1, "user_logout"); err != nil || n != 0 {
		t.Fatalf("不能吊销他人会话：n=%d err=%v", n, err)
	}
	// u1 吊销自己的会话 → 1 行
	if n, err := repo.RevokeSessionByID(ctx, u1, sid1, "user_logout"); err != nil || n != 1 {
		t.Fatalf("吊销自己会话应 1 行：n=%d err=%v", n, err)
	}
	if l, _ := repo.ListUserSessions(ctx, u1, now); len(l) != 1 {
		t.Fatalf("下线一个后 u1 应剩 1 个，得到 %d", len(l))
	}

	// 改密吊销全部
	if err := repo.RevokeAllForUser(ctx, u1, "password_changed"); err != nil {
		t.Fatal(err)
	}
	if l, _ := repo.ListUserSessions(ctx, u1, now); len(l) != 0 {
		t.Fatalf("吊销全部后 u1 应 0 个，得到 %d", len(l))
	}
	// u2 不受影响
	if l, _ := repo.ListUserSessions(ctx, u2, now); len(l) != 1 {
		t.Fatalf("u2 不应受影响，应 1 个，得到 %d", len(l))
	}
}
