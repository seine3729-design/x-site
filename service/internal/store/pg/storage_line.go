package pg

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"xsite/service/internal/domain/storage"
	"xsite/service/internal/store/pg/sqlcgen"
)

func lineOf(r sqlcgen.StorageLine) storage.Line {
	return storage.Line{
		ID: r.ID, ProviderID: r.ProviderID, Kind: storage.LineKind(r.Kind), Name: r.Name, BaseURL: r.BaseUrl,
		AccessMode: storage.AccessMode(r.AccessMode), SignType: storage.SignType(r.SignType), SignKeyRef: r.SignKeyRef,
		TokenTTL: r.TokenTtl, Priority: r.Priority, Weight: r.Weight, RegionScope: storage.RegionScope(r.RegionScope),
		VideoProcess: storage.VideoProcess(r.VideoProcess), ProbePath: r.ProbePath, Status: storage.LineStatus(r.Status),
		Note: r.Note, CreatedAt: r.CreatedAt.Time.UTC(), UpdatedAt: r.UpdatedAt.Time.UTC(),
	}
}

func ruleOf(r sqlcgen.StorageLineRule) storage.Rule {
	return storage.Rule{
		ID: r.ID, LineID: r.LineID, Country: r.Country, Province: r.Province, ISP: r.Isp, ASN: r.Asn,
		Effect: storage.RuleEffect(r.Effect), Boost: r.Boost,
	}
}

func (r *StorageRepo) ListLines(ctx context.Context, q storage.LineQuery) ([]storage.Line, error) {
	rows, err := r.q.ListStorageLines(ctx, sqlcgen.ListStorageLinesParams{
		ProviderID: nullID(q.ProviderID), Kind: nullText(string(q.Kind)), Status: nullText(string(q.Status)),
	})
	if err != nil {
		return nil, fmt.Errorf("读取线路列表: %w", err)
	}
	out := make([]storage.Line, len(rows))
	for i, x := range rows {
		out[i] = lineOf(x)
	}
	return out, nil
}

func (r *StorageRepo) GetLine(ctx context.Context, id int64) (storage.Line, error) {
	row, err := r.q.GetStorageLine(ctx, id)
	if err != nil {
		return storage.Line{}, dbErr(err, "读取线路")
	}
	return lineOf(row), nil
}

func (r *StorageRepo) CreateLine(ctx context.Context, in storage.LineInput) (storage.Line, error) {
	row, err := r.q.CreateStorageLine(ctx, sqlcgen.CreateStorageLineParams{
		ProviderID: in.ProviderID, Kind: string(in.Kind), Name: in.Name, BaseUrl: in.BaseURL,
		AccessMode: string(in.AccessMode), SignType: string(in.SignType), SignKeyRef: in.SignKeyRef,
		TokenTtl: in.TokenTTL, Priority: in.Priority, Weight: in.Weight, RegionScope: string(in.RegionScope),
		VideoProcess: string(in.VideoProcess), ProbePath: in.ProbePath, Status: string(in.Status), Note: in.Note,
	})
	if err != nil {
		return storage.Line{}, dbErr(err, "创建线路")
	}
	return lineOf(row), nil
}

func (r *StorageRepo) UpdateLine(ctx context.Context, id int64, in storage.LineInput) (storage.Line, error) {
	row, err := r.q.UpdateStorageLine(ctx, sqlcgen.UpdateStorageLineParams{
		ID: id, Kind: string(in.Kind), Name: in.Name, BaseUrl: in.BaseURL, AccessMode: string(in.AccessMode),
		SignType: string(in.SignType), SignKeyRef: in.SignKeyRef, TokenTtl: in.TokenTTL, Priority: in.Priority,
		Weight: in.Weight, RegionScope: string(in.RegionScope), VideoProcess: string(in.VideoProcess),
		ProbePath: in.ProbePath, Status: string(in.Status), Note: in.Note,
	})
	if err != nil {
		return storage.Line{}, dbErr(err, "修改线路")
	}
	return lineOf(row), nil
}

func (r *StorageRepo) DeleteLine(ctx context.Context, id int64) (storage.Line, error) {
	row, err := r.q.DeleteStorageLine(ctx, id)
	if err != nil {
		return storage.Line{}, dbErr(err, "删除线路")
	}
	return lineOf(row), nil
}

func (r *StorageRepo) ListRulesByProvider(ctx context.Context, providerID int64) ([]storage.Rule, error) {
	rows, err := r.q.ListLineRulesByProvider(ctx, providerID)
	if err != nil {
		return nil, fmt.Errorf("读取线路规则: %w", err)
	}
	out := make([]storage.Rule, len(rows))
	for i, x := range rows {
		out[i] = ruleOf(x)
	}
	return out, nil
}

func (r *StorageRepo) ListRulesByLine(ctx context.Context, lineID int64) ([]storage.Rule, error) {
	rows, err := r.q.ListLineRules(ctx, lineID)
	if err != nil {
		return nil, fmt.Errorf("读取线路规则: %w", err)
	}
	out := make([]storage.Rule, len(rows))
	for i, x := range rows {
		out[i] = ruleOf(x)
	}
	return out, nil
}

func (r *StorageRepo) ReplaceLineRules(ctx context.Context, lineID int64, rules []storage.RuleInput) ([]storage.Rule, error) {
	out := make([]storage.Rule, 0, len(rules))
	err := InTx(ctx, r.pool, func(tx pgx.Tx) error {
		q := sqlcgen.New(tx)
		if err := q.DeleteLineRules(ctx, lineID); err != nil {
			return err
		}
		out = out[:0]
		for _, ru := range rules {
			row, err := q.CreateLineRule(ctx, sqlcgen.CreateLineRuleParams{
				LineID: lineID, Country: ru.Country, Province: ru.Province, Isp: ru.ISP, Asn: ru.ASN,
				Effect: string(ru.Effect), Boost: ru.Boost,
			})
			if err != nil {
				return err
			}
			out = append(out, ruleOf(row))
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("替换线路规则: %w", err)
	}
	return out, nil
}
