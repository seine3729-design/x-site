package pg

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"xsite/service/internal/domain/storage"
	"xsite/service/internal/errs"
)

// 存储源（S1-6a）走真实 PostgreSQL：仓储增删改查、凭据密文（bytea）往返、搜索与筛选、表约束。
func TestStorageRepo_CRUD(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	mark := fmt.Sprintf("it%d", time.Now().UnixNano())
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM storage_provider WHERE name LIKE $1`, "%"+mark)
	})
	repo := NewStorageRepo(pool)

	writeEnc := []byte{0x00, 0x01, 'k', '1', ':', 0xde, 0xad, 0xbe, 0xef}
	r2, err := repo.Create(ctx, storage.Row{
		Name: "R2 主存储" + mark, Type: storage.TypeR2, Purpose: storage.PurposeBoth, Role: storage.RolePrimary,
		Endpoint: "https://38cf99d986ef8a3c19ea71e9fd2099c9.r2.cloudflarestorage.com", Region: "auto",
		Bucket: "bk" + mark, AccountID: "38cf99d986ef8a3c19ea71e9fd2099c9",
		CredentialEnc: writeEnc, CredTail: "839e", MaxPresignTTL: 600, Status: storage.StatusActive,
	})
	if err != nil {
		t.Fatal(err)
	}
	if r2.ID == 0 || r2.CreatedAt.IsZero() || r2.CreatedAt.Location() != time.UTC {
		t.Fatalf("新建结果不符合预期：%+v", r2)
	}
	s3, err := repo.Create(ctx, storage.Row{
		Name: "S3 镜像" + mark, Type: storage.TypeS3, Purpose: storage.PurposeImage, Role: storage.RoleMirror,
		Region: "ap-east-1", Bucket: "xsite-assets", MaxPresignTTL: 600, Status: storage.StatusDisabled,
	})
	if err != nil {
		t.Fatal(err)
	}

	// 密文（bytea）原样往返，只读凭据未配置时为 nil
	got, err := repo.Get(ctx, r2.ID)
	if err != nil || !bytes.Equal(got.CredentialEnc, writeEnc) || got.ReadCredentialEnc != nil {
		t.Fatalf("密文应原样读回、未配置只读凭据为 nil：%v len=%d read=%v", err, len(got.CredentialEnc), got.ReadCredentialEnc)
	}

	ids := func(f storage.Query) ([]int64, int64) {
		t.Helper()
		rows, total, err := repo.List(ctx, f)
		if err != nil {
			t.Fatal(err)
		}
		out := make([]int64, len(rows))
		for i, r := range rows {
			out[i] = r.ID
		}
		return out, total
	}
	// 按名称搜索，按编号倒序
	if list, total := ids(storage.Query{Pattern: "%" + mark, Limit: 20}); len(list) != 2 || list[0] != s3.ID || list[1] != r2.ID || total != 2 {
		t.Fatalf("按标记搜索应命中两条、按编号倒序：%v %d", list, total)
	}
	// 按桶名搜索
	if list, _ := ids(storage.Query{Pattern: "bk" + mark, Limit: 20}); len(list) != 1 || list[0] != r2.ID {
		t.Fatalf("按桶名搜索：%v", list)
	}
	// 按类型、状态筛选
	if list, total := ids(storage.Query{Pattern: "%" + mark, Type: storage.TypeR2, Limit: 20}); len(list) != 1 || list[0] != r2.ID || total != 1 {
		t.Fatalf("按类型筛选：%v %d", list, total)
	}
	if list, _ := ids(storage.Query{Pattern: "%" + mark, Status: storage.StatusDisabled, Limit: 20}); len(list) != 1 || list[0] != s3.ID {
		t.Fatalf("按状态筛选：%v", list)
	}

	// 整体保存：改状态并替换密文，updated_at 刷新
	time.Sleep(10 * time.Millisecond)
	newEnc := []byte("k1:newcipher")
	after, err := repo.Update(ctx, r2.ID, storage.Row{
		Name: r2.Name, Type: storage.TypeR2, Purpose: storage.PurposeVideo, Role: storage.RolePrimary,
		Endpoint: r2.Endpoint, Region: "auto", Bucket: r2.Bucket, AccountID: r2.AccountID,
		CredentialEnc: newEnc, CredTail: "aaaa", MaxPresignTTL: 1200, Status: storage.StatusDraining,
	})
	if err != nil || after.Purpose != storage.PurposeVideo || after.Status != storage.StatusDraining ||
		after.MaxPresignTTL != 1200 || !bytes.Equal(after.CredentialEnc, newEnc) || after.CredTail != "aaaa" ||
		!after.UpdatedAt.After(after.CreatedAt) {
		t.Fatalf("保存结果不符合预期：%+v %v", after, err)
	}

	// 删除
	if del, err := repo.Delete(ctx, s3.ID); err != nil || del.ID != s3.ID {
		t.Fatalf("删除应返回删除前记录：%+v %v", del, err)
	}
	if _, err := repo.Get(ctx, s3.ID); !errors.Is(err, errs.ErrNotFound) {
		t.Fatalf("删除后应 404，得到 %v", err)
	}

	// 表约束兜底：绕过领域层写入非法枚举值会被拒绝
	for _, sql := range []string{
		`INSERT INTO storage_provider (name, type, purpose, role) VALUES ($1, 'oss', 'both', 'primary')`,
		`INSERT INTO storage_provider (name, type, purpose, role) VALUES ($1, 's3', 'audio', 'primary')`,
		`INSERT INTO storage_provider (name, type, purpose, role) VALUES ($1, 's3', 'both', 'leader')`,
		`INSERT INTO storage_provider (name, type, purpose, role, status) VALUES ($1, 's3', 'both', 'primary', 'deleted')`,
		`INSERT INTO storage_provider (name, type, purpose, role, max_presign_ttl) VALUES ($1, 's3', 'both', 'primary', 10)`,
		`INSERT INTO storage_provider (name, type, purpose, role, subtype) VALUES ($1, 'custom', 'both', 'primary', 'ftp')`,
	} {
		if _, err := pool.Exec(ctx, sql, "bad"+mark); pgCode(err) != "23514" {
			t.Errorf("应违反 CHECK 约束（23514），得到 %v\n%s", err, sql)
		}
	}
	// 空名也违反长度 CHECK
	if _, err := pool.Exec(ctx, `INSERT INTO storage_provider (name, type, purpose, role) VALUES ('', 's3', 'both', 'primary')`); pgCode(err) != "23514" {
		t.Errorf("空名应违反 CHECK 约束（23514），得到 %v", err)
	}
}
