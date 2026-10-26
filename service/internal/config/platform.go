package config

import (
	"errors"
	"strings"
	"time"
	"xsite/service/internal/crypto/backupsig"
)

// Platform 是系统/平台功能的配置（缓存管理、配置导出导入，S4-4）。
type Platform struct {
	// ConfigBackupKeys 是配置备份的签名密钥 `kid:密钥[,kid2:密钥2]`（docs/06 第 29 节「勿手改备份」）。
	//
	// 备份文件是明文 JSON、可以人工检视，用签名而不是加密来保证「没被改过」。留空时导出不签名、
	// 导入也不验签（本地开发不配密钥也要能用），所以非开发环境应当配置：
	// 否则任何人都能构造一份备份导进来覆盖维度与广告。
	ConfigBackupKeys string `env:"CONFIG_BACKUP_KEYS"`

	// TrashRetention 是回收站的保留期（docs/06 第 35 节：30 天内可还原）。
	// 过期由 worker 的清理任务物理删除，不可恢复，所以本地开发可以调短、生产不建议短于 7 天。
	TrashRetention time.Duration `env:"TRASH_RETENTION" envDefault:"720h"`
	// TrashCleanupInterval 是清理任务的执行间隔。
	TrashCleanupInterval time.Duration `env:"TRASH_CLEANUP_INTERVAL" envDefault:"24h"`
}

// ValidateTrash 校验回收站配置（worker 启动时调用）。清理是物理删除，配置写错的代价高。
func (p Platform) ValidateTrash(appEnv string) error {
	var problems []string
	if p.TrashRetention < time.Hour {
		problems = append(problems, "TRASH_RETENTION 至少 1h（过期内容会被物理删除，不可恢复）")
	}
	if appEnv == "production" && p.TrashRetention < 7*24*time.Hour {
		problems = append(problems, "production 环境 TRASH_RETENTION 不应短于 168h（7 天）")
	}
	if p.TrashCleanupInterval < time.Minute || p.TrashCleanupInterval > 7*24*time.Hour {
		problems = append(problems, "TRASH_CLEANUP_INTERVAL 应在 1m–168h 之间")
	}
	if len(problems) > 0 {
		return errors.New("回收站配置校验失败: " + strings.Join(problems, "；"))
	}
	return nil
}

// BackupSigner 解析配置备份的签名密钥环。留空返回空密钥环（不签名、不验签），格式错误时返回错误。
func (p Platform) BackupSigner() (*backupsig.Keyring, error) {
	return backupsig.ParseKeyring(p.ConfigBackupKeys)
}
