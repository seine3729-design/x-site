package app

import (
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/valkey-io/valkey-go"

	"xsite/service/internal/domain/me"
	"xsite/service/internal/jobs"
	"xsite/service/internal/store/cache"
	"xsite/service/internal/store/pg"
)

// 「我的」域在 Valkey 中的键前缀：API 进程（写续播断点）与 worker（批量落库）都要读写，必须一致。
const historyPrefix = "xsite:history:"

// newMeService 创建「我的」领域服务（收藏、点赞、观看历史；API 进程与 worker 共用同一套装配）。
func newMeService(pool *pgxpool.Pool, vk valkey.Client) *me.Service {
	return me.NewService(pg.NewMeRepo(pool), cache.NewHistoryStore(vk, historyPrefix))
}

// NewMeFlusher 返回 worker 进程中观看历史落库任务的依赖（docs/03 第 6 节）。
func NewMeFlusher(pool *pgxpool.Pool, vk valkey.Client) jobs.HistoryFlusher {
	return newMeService(pool, vk)
}
