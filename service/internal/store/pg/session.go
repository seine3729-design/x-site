package pg

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"uuid"

	"xsite/service/internal/domain/identity"
	"xsite/service/internal/store/pg/sqlcgen"
)

// SessionRepo 实现 identity.SessionRepo（S1-10 游客设备与会话族）。
type SessionRepo struct {
	pool *pgxpool.Pool
	q    *sqlcgen.Queries
}

func NewSessionRepo(pool *pgxpool.Pool) *SessionRepo {
	return &SessionRepo{pool: pool, q: sqlcgen.New(pool)}
}

func (r *SessionRepo) CreateGuest(ctx context.Context, d identity.GuestDevice) error {
	err := r.q.CreateGuestDevice(ctx, sqlcgen.CreateGuestDeviceParams{
		ID: pgUUID(d.ID), SourceChannel: d.SourceChannel, Platform: d.Platform, AttestLevel: d.AttestLevel,
	})
	return dbErr(err, "创建游客设备")
}

func (r *SessionRepo) CreateSession(ctx context.Context, s identity.NewSession) error {
	p := sqlcgen.CreateSessionParams{
		ID: pgUUID(s.ID), FamilyID: pgUUID(s.FamilyID), RefreshHash: s.RefreshHash,
		UaDigest: s.UADigest, IpPrefix: s.IPPrefix, Platform: s.Platform, ExpiresAt: pgTime(s.ExpiresAt),
	}
	// 恰好绑定一个主体：注册用户用 user_id，游客用 guest_device_id（与迁移的 CHECK 一致）
	if s.UserID > 0 {
		p.UserID = pgtype.Int8{Int64: s.UserID, Valid: true}
	} else {
		p.GuestDeviceID = pgUUID(s.GuestDeviceID)
	}
	return dbErr(r.q.CreateSession(ctx, p), "创建会话")
}

func (r *SessionRepo) RevokeFamilyByRefresh(ctx context.Context, hash []byte, reason string) error {
	_, err := r.q.RevokeFamilyByRefreshHash(ctx, sqlcgen.RevokeFamilyByRefreshHashParams{RefreshHash: hash, RevokeReason: reason})
	return dbErr(err, "吊销会话族")
}

// Rotate 在一个事务内完成轮换与复用检测：FOR UPDATE 锁住命中的会话行，串行化同一刷新令牌的并发刷新。
func (r *SessionRepo) Rotate(ctx context.Context, oldHash []byte, in identity.RotateInput, now time.Time) (identity.RotateResult, error) {
	var out identity.RotateResult
	err := InTx(ctx, r.pool, func(tx pgx.Tx) error {
		q := r.q.WithTx(tx)
		old, err := q.LockSessionByRefreshHash(ctx, oldHash)
		if errors.Is(err, pgx.ErrNoRows) {
			out.Status = identity.RotateNotFound
			return nil
		}
		if err != nil {
			return err
		}
		if old.RevokedAt.Valid {
			// 复用：旧令牌已在上次轮换时吊销，仍被拿来刷新 → 判定泄露，吊销整族
			if _, err := q.RevokeFamily(ctx, sqlcgen.RevokeFamilyParams{FamilyID: old.FamilyID, RevokeReason: "reuse_detected"}); err != nil {
				return err
			}
			out.Status = identity.RotateReuse
			return nil
		}
		if !old.ExpiresAt.Time.After(now) {
			out.Status = identity.RotateExpired
			return nil
		}
		if _, err := q.RevokeSession(ctx, sqlcgen.RevokeSessionParams{ID: old.ID, RevokeReason: "rotated"}); err != nil {
			return err
		}
		// 新行继承旧行的会话族与主体，平台沿用；UA / IP 用本次请求更新。
		// 过期时间按主体选：注册用户续 UserRefreshTTL（30 天），游客续 GuestRefreshTTL（180 天）。
		exp := in.GuestExpiresAt
		if old.UserID.Valid {
			exp = in.UserExpiresAt
		}
		if err := q.CreateSession(ctx, sqlcgen.CreateSessionParams{
			ID: pgUUID(in.NewSessionID), FamilyID: old.FamilyID, UserID: old.UserID, GuestDeviceID: old.GuestDeviceID,
			RefreshHash: in.NewRefreshHash, UaDigest: in.UADigest, IpPrefix: in.IPPrefix,
			Platform: old.Platform, ExpiresAt: pgTime(exp),
		}); err != nil {
			return err
		}
		out = identity.RotateResult{
			Status: identity.RotateOK, Platform: old.Platform,
			UserID: int64FromPg(old.UserID), GuestDeviceID: uuidFromPg(old.GuestDeviceID),
		}
		return nil
	})
	if err != nil {
		return identity.RotateResult{}, dbErr(err, "轮换会话")
	}
	return out, nil
}

// ListUserSessions 返回注册用户所有未吊销且未过期的会话（「我的设备」，S2-1）。
func (r *SessionRepo) ListUserSessions(ctx context.Context, userID int64, now time.Time) ([]identity.SessionInfo, error) {
	rows, err := r.q.ListUserSessions(ctx, sqlcgen.ListUserSessionsParams{
		UserID: pgtype.Int8{Int64: userID, Valid: true}, ExpiresAt: pgTime(now),
	})
	if err != nil {
		return nil, dbErr(err, "查询会话列表")
	}
	out := make([]identity.SessionInfo, len(rows))
	for i, row := range rows {
		out[i] = identity.SessionInfo{
			ID: uuidFromPg(row.ID), UADigest: row.UaDigest, IPPrefix: row.IpPrefix,
			Platform: row.Platform, CreatedAt: row.CreatedAt.Time, LastSeenAt: row.LastSeenAt.Time,
		}
	}
	return out, nil
}

// RevokeSessionByID 吊销该用户名下的指定会话（设备下线）；带 user_id 作用域防越权。
func (r *SessionRepo) RevokeSessionByID(ctx context.Context, userID int64, sessionID uuid.UUID, reason string) (int64, error) {
	n, err := r.q.RevokeUserSessionByID(ctx, sqlcgen.RevokeUserSessionByIDParams{
		ID: pgUUID(sessionID), UserID: pgtype.Int8{Int64: userID, Valid: true}, RevokeReason: reason,
	})
	return n, dbErr(err, "吊销会话")
}

// RevokeAllForUser 吊销该用户的全部会话（改密 / 封禁）。
func (r *SessionRepo) RevokeAllForUser(ctx context.Context, userID int64, reason string) error {
	_, err := r.q.RevokeAllForUser(ctx, sqlcgen.RevokeAllForUserParams{
		UserID: pgtype.Int8{Int64: userID, Valid: true}, RevokeReason: reason,
	})
	return dbErr(err, "吊销用户会话")
}

func pgTime(t time.Time) pgtype.Timestamptz { return pgtype.Timestamptz{Time: t, Valid: true} }

func int64FromPg(v pgtype.Int8) int64 {
	if v.Valid {
		return v.Int64
	}
	return 0
}

func uuidFromPg(v pgtype.UUID) uuid.UUID {
	if v.Valid {
		return uuid.UUID(v.Bytes)
	}
	return uuid.Nil()
}
