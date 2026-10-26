// Package db 把迁移文件嵌入二进制，便于发布流程与本地 AUTO_MIGRATE 执行。
package db

import "embed"

//go:embed migrations/*.sql
var Migrations embed.FS
