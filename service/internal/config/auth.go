package config

import "xsite/service/internal/crypto/authtoken"

// Auth 是用户端身份令牌（docs/03 第 2 节、docs/10 第 4 节，S1-10）的配置。
type Auth struct {
	// SigningKeys 是 Access Token 的 Ed25519 签名密钥 `kid:secret[,kid2:secret2]`：第一把签发，全部用于校验；
	// 轮换时新密钥放第一把，旧密钥保留到它签发的令牌全部过期（15 分钟）后再删。只有 API 进程签发与校验，
	// 缺失或格式错误时 API 启动失败（worker 不需要）。
	SigningKeys string `env:"JWT_SIGNING_KEYS"`
}

// Keyring 解析 Access Token 的签名密钥环；为空或格式错误时返回错误。
func (a Auth) Keyring() (*authtoken.Keyring, error) {
	return authtoken.ParseKeyring(a.SigningKeys)
}
