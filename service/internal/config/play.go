package config

import "xsite/service/internal/crypto/playtoken"

// Play 是播放服务（docs/03 第 4 节）的配置。
type Play struct {
	// TokenKeys 是播放令牌的签名密钥 `kid:secret[,kid2:secret2]`（docs/10 第 4、8 节）：第一把签发，全部用于校验；
	// 轮换时新密钥放第一把，旧密钥保留到它签发的令牌全部过期（10 分钟）后再删。只有 API 进程签发与校验
	// （选源 S1-7、清单与密钥 S1-8），不下发给 CDN，所以与线路签名密钥 LINE_SIGN_KEYS 分开。缺失时 API 启动失败。
	TokenKeys string `env:"PLAY_TOKEN_KEYS"`
}

// Keyring 解析播放令牌的密钥环；为空或格式错误时返回错误。
func (p Play) Keyring() (*playtoken.Keyring, error) {
	return playtoken.ParseKeyring(p.TokenKeys)
}
