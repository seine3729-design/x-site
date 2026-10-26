package config

import (
	"os"
	"testing"
)

func TestPlay_TokenKeys(t *testing.T) {
	setBase(t)
	t.Setenv("APP_ENV", "development")
	t.Setenv("PLAY_TOKEN_KEYS", "")
	os.Unsetenv("PLAY_TOKEN_KEYS")

	// worker 也加载同一份配置但不需要播放令牌：缺失时 Load 照常成功，由 API 进程在装配时校验
	cfg, err := Load()
	if err != nil {
		t.Fatalf("缺少 PLAY_TOKEN_KEYS 不应影响 Load：%v", err)
	}
	if _, err := cfg.Play.Keyring(); err == nil {
		t.Fatal("缺少 PLAY_TOKEN_KEYS 时 API 应拿不到密钥环（启动失败）")
	}

	for _, bad := range []string{"k1:short", "k1", "k-1:" + token32} {
		t.Setenv("PLAY_TOKEN_KEYS", bad)
		cfg, err := Load()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := cfg.Play.Keyring(); err == nil {
			t.Errorf("PLAY_TOKEN_KEYS=%q 应无效", bad)
		}
	}

	t.Setenv("PLAY_TOKEN_KEYS", "k2:"+token32+"-new,k1:"+token32)
	cfg, err = Load()
	if err != nil {
		t.Fatal(err)
	}
	kr, err := cfg.Play.Keyring()
	if err != nil || kr.CurrentKID() != "k2" {
		t.Fatalf("应解析出密钥环且第一把为当前密钥：%v", err)
	}
}
