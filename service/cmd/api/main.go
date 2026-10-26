// api 是 HTTP 进程入口：用户端 /api/v1、后台 /api/admin、开放平台 /open-api。
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"xsite/service/internal/app"
	"xsite/service/internal/config"
	"xsite/service/internal/observe"
)

func main() {
	healthcheck := flag.Bool("healthcheck", false, "探测本进程的 /api/v1/health 后立即退出（容器健康检查用）")
	flag.Parse()
	if *healthcheck {
		if err := probeHealth(); err != nil {
			fmt.Fprintln(os.Stderr, "健康检查失败：", err)
			os.Exit(1)
		}
		return
	}
	if err := run(); err != nil {
		slog.Error("api 退出", "err", err)
		os.Exit(1)
	}
}

// probeHealth 让生产镜像自己做容器健康检查。
//
// 生产镜像是 distroless，里面没有 curl、wget，也没有 shell，Docker 的 HEALTHCHECK 只能执行
// 镜像里已有的二进制 —— 也就是这个进程自己。
//
// 刻意不走 config.Load()：健康检查不该因为某个无关的配置项缺失而报「不健康」，
// 只读 HTTP_ADDR 一个值。
func probeHealth() error {
	addr := strings.TrimSpace(os.Getenv("HTTP_ADDR"))
	if addr == "" {
		addr = ":8080"
	}
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("HTTP_ADDR %q 无法解析出端口: %w", addr, err)
	}
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get("http://127.0.0.1:" + port + "/api/v1/health")
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("/api/v1/health 返回 %d", resp.StatusCode)
	}
	return nil
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	observe.SetupLogger(cfg.LogLevel, "api", cfg.AppEnv)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	a, err := app.NewAPI(ctx, cfg)
	if err != nil {
		return err
	}
	defer a.Close()

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           a.Handler,
		ReadHeaderTimeout: 5 * time.Second,
		// 这两个超时覆盖整个请求，是防慢速读写攻击的默认值。后台大文件上传与 SSE 进度流会超过它们，
		// 由 middleware.LongRequest 在读请求体之前按路由放宽（否则连接被掐断，经 nginx 表现为 502）。
		ReadTimeout:    15 * time.Second,
		WriteTimeout:   30 * time.Second,
		IdleTimeout:    90 * time.Second,
		MaxHeaderBytes: 32 << 10,
	}

	errCh := make(chan error, 1)
	go func() {
		slog.Info("api 启动", "addr", cfg.HTTPAddr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
		close(errCh)
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}

	slog.Info("收到退出信号，等待在途请求结束", "timeout", cfg.ShutdownTimeout)
	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}
