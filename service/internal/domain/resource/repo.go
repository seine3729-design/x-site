package resource

import "context"

// Repo 读写资源机。查不到时返回 errs.ErrNotFound。传入的 FTPServerInput 已经过校验与补齐默认值。
type Repo interface {
	ListFTPServers(ctx context.Context, q Query) ([]FTPServer, int64, error)
	GetFTPServer(ctx context.Context, id int64) (FTPServer, error)
	CreateFTPServer(ctx context.Context, in FTPServerInput) (FTPServer, error)
	UpdateFTPServer(ctx context.Context, id int64, in FTPServerInput) (FTPServer, error)
	// DeleteFTPServer 删除资源机（设置类数据，不进回收站），返回删除前的记录。
	DeleteFTPServer(ctx context.Context, id int64) (FTPServer, error)
}
