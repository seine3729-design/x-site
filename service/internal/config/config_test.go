package config

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

const token32 = "0123456789abcdef0123456789abcdef"

// 测试在 compose 容器中运行时会继承服务的环境变量，先全部清掉，避免结果依赖运行环境。
var configKeys = []string{
	"APP_ENV", "HTTP_ADDR", "LOG_LEVEL", "DATABASE_URL", "DATABASE_RO_URL", "VALKEY_URL",
	"AUTO_MIGRATE", "BFF_SERVICE_TOKEN", "EDGE_SHARED_SECRET", "EDGE_AUTH_REQUIRED",
	"TRUSTED_PROXIES", "SHUTDOWN_TIMEOUT", "API_ENC_KEYS", "API_ENCRYPTION_REQUIRED", "ADMIN_ORIGINS",
	"EGRESS_ALLOW_PRIVATE", "STORAGE_KEKS",
}

func clearEnv(t *testing.T) {
	t.Helper()
	for _, k := range configKeys {
		t.Setenv(k, "") // 注册测试结束后的恢复
		os.Unsetenv(k)
	}
}

func setBase(t *testing.T) {
	t.Helper()
	clearEnv(t)
	t.Setenv("DATABASE_URL", "postgres://u:p@localhost/db")
	t.Setenv("VALKEY_URL", "redis://localhost:6379/0")
	t.Setenv("BFF_SERVICE_TOKEN", token32)
	t.Setenv("API_ENC_KEYS", "k1:"+token32)
	t.Setenv("STORAGE_KEKS", "k1:"+token32)
	t.Setenv("ADMIN_ORIGINS", "https://admin.example.com")
}

func TestLoad_DevelopmentDoesNotRequireEdgeSecret(t *testing.T) {
	setBase(t)
	t.Setenv("APP_ENV", "development")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("期望加载成功，得到 %v", err)
	}
	if cfg.EdgeAuthEnforced() {
		t.Fatal("development 默认不应强制回源鉴权")
	}
}

func TestLoad_ProductionRequiresEdgeSecret(t *testing.T) {
	setBase(t)
	t.Setenv("APP_ENV", "production")

	_, err := Load()
	if err == nil || !strings.Contains(err.Error(), "EDGE_SHARED_SECRET") {
		t.Fatalf("期望提示缺少 EDGE_SHARED_SECRET，得到 %v", err)
	}
}

func TestLoad_MissingRequired(t *testing.T) {
	clearEnv(t)
	t.Setenv("APP_ENV", "development")
	t.Setenv("BFF_SERVICE_TOKEN", token32)

	if _, err := Load(); err == nil {
		t.Fatal("缺少 DATABASE_URL 时应失败")
	}
}

func TestLoad_ProductionRejectsAutoMigrate(t *testing.T) {
	setBase(t)
	t.Setenv("APP_ENV", "production")
	t.Setenv("EDGE_SHARED_SECRET", token32)
	t.Setenv("AUTO_MIGRATE", "true")

	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "AUTO_MIGRATE") {
		t.Fatalf("production 开启 AUTO_MIGRATE 应失败，得到 %v", err)
	}
}

func TestLoad_APIEncryptionDefaults(t *testing.T) {
	setBase(t)
	t.Setenv("APP_ENV", "development")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.APIEncEnforced() {
		t.Fatal("development 默认允许明文，方便 curl 调试")
	}

	setBase(t)
	t.Setenv("APP_ENV", "test")
	t.Setenv("EDGE_SHARED_SECRET", token32)
	if cfg, err = Load(); err != nil || !cfg.APIEncEnforced() {
		t.Fatalf("test 环境默认强制加密，得到 %v %v", cfg.APIEncEnforced(), err)
	}
}

func TestLoad_ProductionCannotDisableEncryption(t *testing.T) {
	setBase(t)
	t.Setenv("APP_ENV", "production")
	t.Setenv("EDGE_SHARED_SECRET", token32)
	t.Setenv("API_ENCRYPTION_REQUIRED", "false")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "API_ENCRYPTION_REQUIRED") {
		t.Fatalf("production 关闭加密应失败，得到 %v", err)
	}
}

func TestLoad_RejectsInvalidEncKeys(t *testing.T) {
	for _, value := range []string{"k1:short", "bad-kid:" + token32, ""} {
		setBase(t)
		t.Setenv("APP_ENV", "development")
		t.Setenv("API_ENC_KEYS", value)
		if _, err := Load(); err == nil || !strings.Contains(err.Error(), "API_ENC_KEYS") {
			t.Errorf("API_ENC_KEYS=%q 应启动失败，得到 %v", value, err)
		}
	}
}

func TestLoad_AdminOrigins(t *testing.T) {
	setBase(t)
	t.Setenv("APP_ENV", "development")
	t.Setenv("ADMIN_ORIGINS", " http://localhost:5173 , http://127.0.0.1:5173")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.AdminOrigins) != 2 || cfg.AdminOrigins[0] != "http://localhost:5173" {
		t.Fatalf("应去掉空白并保留两个来源，得到 %q", cfg.AdminOrigins)
	}

	tests := []struct {
		name, env, value string
	}{
		{"未设置", "development", "<unset>"},
		{"为空", "development", " , "},
		{"带路径", "development", "http://localhost:5173/admin"},
		{"不是 http(s)", "development", "ftp://admin.example.com"},
		{"缺少协议", "development", "admin.example.com"},
		{"production 只允许 https", "production", "http://admin.example.com"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setBase(t)
			t.Setenv("APP_ENV", tt.env)
			t.Setenv("EDGE_SHARED_SECRET", token32)
			if tt.value == "<unset>" {
				os.Unsetenv("ADMIN_ORIGINS")
			} else {
				t.Setenv("ADMIN_ORIGINS", tt.value)
			}
			if _, err := Load(); err == nil || !strings.Contains(err.Error(), "ADMIN_ORIGINS") {
				t.Fatalf("应启动失败并指出 ADMIN_ORIGINS，得到 %v", err)
			}
		})
	}
}

func TestLoad_EgressAllowPrivate(t *testing.T) {
	setBase(t)
	t.Setenv("APP_ENV", "development")
	if cfg, err := Load(); err != nil || !cfg.EgressPrivateAllowed() {
		t.Fatalf("development 默认允许访问内网（本地资源机），得到 %v %v", cfg.EgressPrivateAllowed(), err)
	}

	setBase(t)
	t.Setenv("APP_ENV", "test")
	t.Setenv("EDGE_SHARED_SECRET", token32)
	if cfg, err := Load(); err != nil || cfg.EgressPrivateAllowed() {
		t.Fatalf("test 环境默认禁止，得到 %v %v", cfg.EgressPrivateAllowed(), err)
	}
	t.Setenv("EGRESS_ALLOW_PRIVATE", "true")
	if cfg, err := Load(); err != nil || !cfg.EgressPrivateAllowed() {
		t.Fatalf("test 环境可以显式开启，得到 %v %v", cfg.EgressPrivateAllowed(), err)
	}

	setBase(t)
	t.Setenv("APP_ENV", "production")
	t.Setenv("EDGE_SHARED_SECRET", token32)
	if cfg, err := Load(); err != nil || cfg.EgressPrivateAllowed() {
		t.Fatalf("production 默认禁止，得到 %v %v", cfg.EgressPrivateAllowed(), err)
	}
	t.Setenv("EGRESS_ALLOW_PRIVATE", "true")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "EGRESS_ALLOW_PRIVATE") {
		t.Fatalf("production 开启 EGRESS_ALLOW_PRIVATE 应启动失败，得到 %v", err)
	}
}

func TestParseFTPCredentials(t *testing.T) {
	creds, problems := parseFTPCredentials([]string{
		"FTP_HK1_USER=xsite",
		"FTP_HK1_PASSWORD=p=a=s=s", // 值里可以有等号
		"FTP_JP_EAST_1_USER=jp",    // 凭据标识可以含下划线
		"FTP_JP_EAST_1_PASSWORD=pw",
		"FTP_HALF_USER=only-user",
		"FTP_LONELY_PASSWORD=only-password",
		"FTP_EMPTY_USER=", // 两个都为空视为没有配置
		"FTP_EMPTY_PASSWORD=",
		"FTP_BLANK_USER=u", // 密码为空视为缺少
		"FTP_BLANK_PASSWORD=",
		"FTP_PROXY=http://proxy:3128", // 不是凭据变量
		"ftp_lower_user=x",
		"FTP_1BAD_USER=x",
		"PATH=/usr/bin",
	})
	if u, p, ok := creds.Lookup("HK1"); !ok || u != "xsite" || p != "p=a=s=s" {
		t.Fatalf("HK1 应读到账号密码：%q %q %v", u, p, ok)
	}
	if u, p, ok := creds.Lookup("JP_EAST_1"); !ok || u != "jp" || p != "pw" {
		t.Fatalf("凭据标识可以含下划线：%q %q %v", u, p, ok)
	}
	for _, ref := range []string{"HALF", "LONELY", "EMPTY", "BLANK", "PROXY", "LOWER", "1BAD"} {
		if _, _, ok := creds.Lookup(ref); ok {
			t.Errorf("%s 不应算作已配置", ref)
		}
	}
	if got := strings.Join(creds.Refs(), ","); got != "HK1,JP_EAST_1" {
		t.Fatalf("已配置的凭据标识：%s", got)
	}
	want := []string{
		"已设置 FTP_BLANK_USER 但缺少 FTP_BLANK_PASSWORD（资源机账号与密码要成对配置）",
		"已设置 FTP_HALF_USER 但缺少 FTP_HALF_PASSWORD（资源机账号与密码要成对配置）",
		"已设置 FTP_LONELY_PASSWORD 但缺少 FTP_LONELY_USER（资源机账号与密码要成对配置）",
	}
	if strings.Join(problems, "\n") != strings.Join(want, "\n") {
		t.Fatalf("只配了一半的应逐个指出：\n%s", strings.Join(problems, "\n"))
	}
}

func TestLoad_FTPCredentials(t *testing.T) {
	setBase(t)
	t.Setenv("APP_ENV", "development")
	t.Setenv("FTP_TCFG_USER", "xsite")
	t.Setenv("FTP_TCFG_PASSWORD", "s3cret-value")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if u, p, ok := cfg.FTPCredentials.Lookup("TCFG"); !ok || u != "xsite" || p != "s3cret-value" {
		t.Fatalf("应读到资源机账号密码：%q %q %v", u, p, ok)
	}
	// 配置被整体打印（fmt / slog）时不能带出账号密码
	if s := fmt.Sprintf("%v %+v %#v", cfg, cfg, cfg); strings.Contains(s, "s3cret-value") || strings.Contains(s, "xsite\"") {
		t.Fatalf("打印配置时不应输出账号密码：%s", s)
	}

	t.Setenv("FTP_TCFG2_USER", "only-user")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "FTP_TCFG2_PASSWORD") {
		t.Fatalf("只配了账号应启动失败并指出缺少的变量，得到 %v", err)
	}
}

func TestLoad_RejectsInvalidStorageKEKs(t *testing.T) {
	for _, value := range []string{"", "k1:short", "bad-kid:" + token32} {
		setBase(t)
		t.Setenv("APP_ENV", "development")
		t.Setenv("STORAGE_KEKS", value)
		if _, err := Load(); err == nil || !strings.Contains(err.Error(), "STORAGE_KEKS") {
			t.Errorf("STORAGE_KEKS=%q 应启动失败，得到 %v", value, err)
		}
	}
}
