package pg

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"xsite/service/internal/domain/domains"
	"xsite/service/internal/store/pg/sqlcgen"
)

// DomainRepo 实现 domains.Repo（域名池、签名清单发布、健康日表）。
type DomainRepo struct {
	q *sqlcgen.Queries
}

func NewDomainRepo(pool *pgxpool.Pool) *DomainRepo {
	return &DomainRepo{q: sqlcgen.New(pool)}
}

func domainEntryOf(r sqlcgen.DomainEntry) domains.Entry {
	return domains.Entry{
		ID: r.ID, Host: r.Host, Role: domains.Role(r.Role), Weight: r.Weight,
		Region: r.Region, CDN: r.Cdn, DNSProvider: r.DnsProvider,
		CertExpiresAt: utcPtr(r.CertExpiresAt), Status: domains.Status(r.Status), Note: r.Note,
		CreatedAt: r.CreatedAt.Time.UTC(), UpdatedAt: r.UpdatedAt.Time.UTC(),
	}
}

func (r *DomainRepo) List(ctx context.Context, f domains.Filter) ([]domains.Entry, error) {
	rows, err := r.q.ListDomains(ctx, sqlcgen.ListDomainsParams{
		Status: nullText(string(f.Status)), Role: nullText(string(f.Role)), Pattern: nullText(f.Pattern),
	})
	if err != nil {
		return nil, fmt.Errorf("读取域名列表: %w", err)
	}
	out := make([]domains.Entry, len(rows))
	for i, x := range rows {
		out[i] = domainEntryOf(x)
	}
	return out, nil
}

func (r *DomainRepo) Get(ctx context.Context, id int64) (domains.Entry, error) {
	row, err := r.q.GetDomain(ctx, id)
	if err != nil {
		return domains.Entry{}, dbErr(err, "读取域名")
	}
	return domainEntryOf(row), nil
}

func (r *DomainRepo) Create(ctx context.Context, in domains.EntryInput) (domains.Entry, error) {
	row, err := r.q.CreateDomain(ctx, sqlcgen.CreateDomainParams{
		Host: in.Host, Role: string(in.Role), Weight: in.Weight, Region: in.Region,
		Cdn: in.CDN, DnsProvider: in.DNSProvider, CertExpiresAt: tsOf(in.CertExpiresAt),
		Status: string(in.Status), Note: in.Note,
	})
	if err != nil {
		return domains.Entry{}, dbErr(err, "创建域名")
	}
	return domainEntryOf(row), nil
}

func (r *DomainRepo) Update(ctx context.Context, id int64, in domains.EntryInput) (domains.Entry, error) {
	row, err := r.q.UpdateDomain(ctx, sqlcgen.UpdateDomainParams{
		ID: id, Host: in.Host, Role: string(in.Role), Weight: in.Weight, Region: in.Region,
		Cdn: in.CDN, DnsProvider: in.DNSProvider, CertExpiresAt: tsOf(in.CertExpiresAt),
		Status: string(in.Status), Note: in.Note,
	})
	if err != nil {
		return domains.Entry{}, dbErr(err, "修改域名")
	}
	return domainEntryOf(row), nil
}

func (r *DomainRepo) Delete(ctx context.Context, id int64) (domains.Entry, error) {
	row, err := r.q.DeleteDomain(ctx, id)
	if err != nil {
		return domains.Entry{}, dbErr(err, "删除域名")
	}
	return domainEntryOf(row), nil
}

func (r *DomainRepo) UpdateCertExpiry(ctx context.Context, id int64, expires *time.Time) (domains.Entry, error) {
	row, err := r.q.UpdateDomainCertExpiry(ctx, sqlcgen.UpdateDomainCertExpiryParams{
		ID: id, CertExpiresAt: tsOf(expires),
	})
	if err != nil {
		return domains.Entry{}, dbErr(err, "更新证书到期日")
	}
	return domainEntryOf(row), nil
}

// ---------- 签名清单 ----------

func (r *DomainRepo) ListReleases(ctx context.Context, limit, offset int) ([]domains.Release, int64, error) {
	rows, err := r.q.ListDomainReleases(ctx, sqlcgen.ListDomainReleasesParams{
		PageLimit: int32(limit), PageOffset: int32(offset),
	})
	if err != nil {
		return nil, 0, fmt.Errorf("读取清单发布历史: %w", err)
	}
	total, err := r.q.CountDomainReleases(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("统计清单发布: %w", err)
	}
	out := make([]domains.Release, len(rows))
	for i, x := range rows {
		out[i] = releaseOf(x.Version, x.Payload, x.Signature, x.Note, x.PublishedBy, x.PublishedAt, x.PublishedByName)
	}
	return out, total, nil
}

func (r *DomainRepo) GetRelease(ctx context.Context, version int64) (domains.Release, error) {
	row, err := r.q.GetDomainRelease(ctx, version)
	if err != nil {
		return domains.Release{}, dbErr(err, "读取清单发布")
	}
	return releaseOf(row.Version, row.Payload, row.Signature, row.Note, row.PublishedBy, row.PublishedAt, row.PublishedByName), nil
}

func (r *DomainRepo) LatestRelease(ctx context.Context) (domains.Release, error) {
	row, err := r.q.LatestDomainRelease(ctx)
	if err != nil {
		return domains.Release{}, dbErr(err, "读取最新清单") // 无行 → ErrNotFound
	}
	return releaseOf(row.Version, row.Payload, row.Signature, row.Note, row.PublishedBy, row.PublishedAt, row.PublishedByName), nil
}

func (r *DomainRepo) NextReleaseVersion(ctx context.Context) (int64, error) {
	n, err := r.q.NextReleaseVersion(ctx)
	if err != nil {
		return 0, fmt.Errorf("计算下一清单版本: %w", err)
	}
	return n, nil
}

func (r *DomainRepo) InsertRelease(ctx context.Context, row domains.ReleaseRow) (domains.Release, error) {
	by := pgtype.Int8{}
	if row.PublishedBy != nil {
		by = pgtype.Int8{Int64: *row.PublishedBy, Valid: true}
	}
	out, err := r.q.InsertDomainRelease(ctx, sqlcgen.InsertDomainReleaseParams{
		Version: row.Version, Payload: row.Payload, Signature: row.Signature, Note: row.Note, PublishedBy: by,
	})
	if err != nil {
		return domains.Release{}, dbErr(err, "发布清单")
	}
	return domains.Release{
		Version: out.Version, Payload: json.RawMessage(out.Payload), Signature: out.Signature,
		Note: out.Note, PublishedBy: optInt8Ptr(out.PublishedBy), PublishedAt: out.PublishedAt.Time.UTC(),
	}, nil
}

// releaseOf 组装带发布者名字的清单记录（列表 / 详情 / 最新共用）。
func releaseOf(version int64, payload []byte, signature, note string, by pgtype.Int8, at pgtype.Timestamptz, name pgtype.Text) domains.Release {
	return domains.Release{
		Version: version, Payload: json.RawMessage(payload), Signature: signature, Note: note,
		PublishedBy: optInt8Ptr(by), PublishedByName: name.String, PublishedAt: at.Time.UTC(),
	}
}

// ---------- 健康 ----------

func (r *DomainRepo) ListHealth(ctx context.Context, since time.Time) ([]domains.HealthDaily, error) {
	rows, err := r.q.ListDomainHealth(ctx, pgtype.Date{Time: since, Valid: true})
	if err != nil {
		return nil, fmt.Errorf("读取域名健康: %w", err)
	}
	out := make([]domains.HealthDaily, len(rows))
	for i, x := range rows {
		reasons := map[string]int64{}
		if len(x.FailReasons) > 0 {
			_ = json.Unmarshal(x.FailReasons, &reasons)
		}
		out[i] = domains.HealthDaily{
			Day: x.Day.Time.UTC(), Host: x.Host, Region: x.Region,
			ProbeOK: x.ProbeOk, ProbeFail: x.ProbeFail, ClientFailoverCount: x.ClientFailoverCount,
			FailReasons: reasons,
		}
	}
	return out, nil
}
