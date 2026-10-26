// Package pg 提供 PostgreSQL 连接池、迁移与仓储实现。
package pg

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"xsite/service/db"
)

// NewPool 创建连接池并确认可连通。
// 使用 QueryExecModeCacheDescribe，兼容线上 PgBouncer transaction 模式（docs/00 第 3.5 节）。
func NewPool(ctx context.Context, url string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("解析数据库地址失败: %w", err)
	}
	cfg.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeCacheDescribe
	cfg.MaxConnIdleTime = 5 * time.Minute
	cfg.HealthCheckPeriod = 30 * time.Second

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("创建连接池失败: %w", err)
	}
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("连接数据库失败: %w", err)
	}
	return pool, nil
}

// Migrate 执行嵌入的 goose 迁移（只升不降）。
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	sqlDB := stdlib.OpenDBFromPool(pool)
	defer func(d *sql.DB) { _ = d.Close() }(sqlDB)

	provider, err := goose.NewProvider(goose.DialectPostgres, sqlDB, mustSub(db.Migrations, "migrations"))
	if err != nil {
		return fmt.Errorf("初始化迁移失败: %w", err)
	}
	if _, err := provider.Up(ctx); err != nil {
		return fmt.Errorf("执行迁移失败: %w", err)
	}
	return nil
}

// InTx 在事务中执行 fn；fn 返回错误或 panic 时回滚。
func InTx(ctx context.Context, pool *pgxpool.Pool, fn func(pgx.Tx) error) error {
	return pgx.BeginTxFunc(ctx, pool, pgx.TxOptions{}, fn)
}
