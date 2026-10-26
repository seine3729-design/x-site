// Package observe 负责日志等可观测能力。
package observe

import (
	"log/slog"
	"os"
	"strings"
)

// SetupLogger 设置全局 slog：JSON 输出到 stdout，级别来自配置。
func SetupLogger(level, service, env string) {
	var lv slog.Level
	switch strings.ToLower(level) {
	case "debug":
		lv = slog.LevelDebug
	case "warn":
		lv = slog.LevelWarn
	case "error":
		lv = slog.LevelError
	default:
		lv = slog.LevelInfo
	}
	h := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: lv})
	slog.SetDefault(slog.New(h).With("service", service, "env", env))
}
