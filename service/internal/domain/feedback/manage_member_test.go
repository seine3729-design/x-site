package feedback

import (
	"context"
	"errors"
	"testing"

	"xsite/service/internal/errs"
)

// stubMemberRepo 只实现本组测试用到的方法；嵌入接口让误用的方法直接 panic。
type stubMemberRepo struct {
	Repo
	members map[string]Member
	users   map[int64]User
	upserts []Member
}

func newStubMemberRepo() *stubMemberRepo {
	return &stubMemberRepo{members: map[string]Member{}, users: map[int64]User{}}
}

func key(projectID, userID int64) string {
	return string(rune(projectID)) + ":" + string(rune(userID))
}

func (r *stubMemberRepo) Member(_ context.Context, projectID, userID int64) (Member, error) {
	m, ok := r.members[key(projectID, userID)]
	if !ok {
		return Member{}, errs.ErrNotFound
	}
	return m, nil
}

func (r *stubMemberRepo) User(_ context.Context, id int64) (User, error) {
	u, ok := r.users[id]
	if !ok {
		return User{}, errs.ErrNotFound
	}
	return u, nil
}

func (r *stubMemberRepo) UpsertMember(
	_ context.Context, projectID, userID int64, role MemberRole, status string,
) (Member, error) {
	m := Member{ProjectID: projectID, UserID: userID, Role: role, Status: status}
	r.members[key(projectID, userID)] = m
	r.upserts = append(r.upserts, m)
	return m, nil
}

/*
运营把已注册账号拉进项目是这个接口唯一的入口：邀请码只在注册时核销，
已有账号没有别的路进「仅邀请」项目。所以成员行不存在时必须当成「添加成员」，
而不是报 404（feedback/docs/11 第 13.3 节记录的缺口）。
*/
func TestSetMemberRole_AddsMissingMember(t *testing.T) {
	ctx := context.Background()
	repo := newStubMemberRepo()
	repo.users[7] = User{ID: 7, Handle: "xiaoyu", Status: UserActive}
	svc := NewManageService(repo)

	m, err := svc.SetMemberRole(ctx, 1, 7, RoleMember, false)
	if err != nil {
		t.Fatalf("添加成员失败：%v", err)
	}
	if m.UserID != 7 || m.Role != RoleMember || m.Status != "active" {
		t.Fatalf("结果 = %+v，期望 7 号成为 active 的 member", m)
	}
	if len(repo.upserts) != 1 {
		t.Fatalf("写入次数 = %d，期望 1", len(repo.upserts))
	}
}

// 账号不存在时不能写出一条指向幽灵用户的成员记录。
func TestSetMemberRole_RejectsUnknownUser(t *testing.T) {
	repo := newStubMemberRepo()
	svc := NewManageService(repo)

	if _, err := svc.SetMemberRole(context.Background(), 1, 999, RoleMember, false); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("err = %v，期望 ErrNotFound", err)
	}
	if len(repo.upserts) != 0 {
		t.Fatal("账号不存在却写了库")
	}
}

// 移除一个本来就不是成员的人仍然是 404：这是调用方搞错了对象，不该静默成功。
func TestSetMemberRole_RemoveNonMember(t *testing.T) {
	repo := newStubMemberRepo()
	repo.users[7] = User{ID: 7, Status: UserActive}
	svc := NewManageService(repo)

	if _, err := svc.SetMemberRole(context.Background(), 1, 7, RoleMember, true); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("err = %v，期望 ErrNotFound", err)
	}
	if len(repo.upserts) != 0 {
		t.Fatal("不是成员却写了库")
	}
}

// 已有成员改角色仍然正常，而且不会去查用户表（成员行存在就说明账号存在）。
func TestSetMemberRole_UpdatesExisting(t *testing.T) {
	repo := newStubMemberRepo()
	repo.members[key(1, 7)] = Member{ProjectID: 1, UserID: 7, Role: RoleMember, Status: "active"}
	svc := NewManageService(repo)

	m, err := svc.SetMemberRole(context.Background(), 1, 7, RoleOperator, false)
	if err != nil {
		t.Fatal(err)
	}
	if m.Role != RoleOperator {
		t.Fatalf("角色 = %q，期望 operator", m.Role)
	}
}

// 项目所有者不能被直接移除。
func TestSetMemberRole_CannotRemoveOwner(t *testing.T) {
	repo := newStubMemberRepo()
	repo.members[key(1, 7)] = Member{ProjectID: 1, UserID: 7, Role: RoleOwner, Status: "active"}
	svc := NewManageService(repo)

	if _, err := svc.SetMemberRole(context.Background(), 1, 7, RoleOwner, true); !errors.Is(err, errs.ErrConflict) {
		t.Fatalf("err = %v，期望 ErrConflict", err)
	}
}
