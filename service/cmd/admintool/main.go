// admintool 是运维命令行，在能连数据库的环境（service 容器）中执行。首个超级管理员只能用它创建。
//
//	printf '%s\n' "$ADMIN_PASSWORD" | admintool create-admin -username root -role superadmin
//	admintool migrate
//
// 本地开发：printf '%s\n' "$PW" | ./dev.sh go run ./cmd/admintool create-admin -username root -role superadmin
// 密码只从标准输入读取，不出现在命令行参数、进程列表与 shell 历史中。
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"xsite/service/internal/config"
	"xsite/service/internal/crypto/password"
	"xsite/service/internal/domain/admin"
	"xsite/service/internal/errs"
	"xsite/service/internal/store/pg"
)

const usage = `用法：
  admintool create-admin -username <用户名> -role <角色编码>
  角色编码为后台「角色」页中的编码；内置只有 superadmin，其余角色在后台新建
  密码从标准输入读取一行（8–128 位）

  admintool migrate
  执行嵌入在本二进制里的 goose 迁移（只升不降）。production 禁用 AUTO_MIGRATE，
  发布流程用这条命令在 API 启动之前把库升到位（docs/11 X0-6）`

func main() {
	if err := run(os.Args[1:], os.Stdin); err != nil {
		var e *errs.Error
		if errors.As(err, &e) {
			err = errors.New(e.Message)
		}
		fmt.Fprintln(os.Stderr, "错误：", err)
		os.Exit(1)
	}
}

func run(args []string, stdin io.Reader) error {
	if len(args) == 0 {
		return errors.New(usage)
	}
	switch args[0] {
	case "create-admin":
		return createAdmin(args, stdin)
	case "migrate":
		return migrate(args)
	default:
		return errors.New(usage)
	}
}

// migrate 执行嵌入的迁移。迁移二进制与 API 二进制来自同一次构建，所以库的 schema 一定和即将启动的
// 代码配套 —— 这是不用单独做一个带 goose 的镜像的原因。
func migrate(args []string) error {
	if len(args) > 1 {
		return errors.New(usage)
	}
	// 迁移只需要 DATABASE_URL。不能加载 API/worker 的完整 Config：BFF、Valkey、S3、
	// 报文加密或后台二次验证的任意配置没填好，都不该阻止数据库 schema 升级。
	cfg, err := config.LoadMigration()
	if err != nil {
		return err
	}
	// 大表迁移可能很慢，给足时间；失败时发布流程会停住，不会让 API 带着不兼容的 schema 起来
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	pool, err := pg.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	if err := pg.Migrate(ctx, pool); err != nil {
		return err
	}
	fmt.Println("迁移已执行到最新版本")
	return nil
}

func createAdmin(args []string, stdin io.Reader) error {
	fs := flag.NewFlagSet("create-admin", flag.ContinueOnError)
	username := fs.String("username", "", "用户名，3–32 位字母、数字、下划线、点、连字符")
	role := fs.String("role", "", "角色编码，如 superadmin 或后台新建的角色编码")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if *username == "" || *role == "" {
		return errors.New(usage)
	}
	pw, err := readLine(stdin)
	if err != nil {
		return fmt.Errorf("读取密码: %w", err)
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pg.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	id, err := admin.CreateAccountWithRoleCode(ctx, pg.NewAdminRepo(pool), password.NewHasher(password.DefaultParams, 1), *username, pw, *role)
	if err != nil {
		return err
	}
	fmt.Printf("已创建管理员 #%d %s（%s），状态 unused，首次登录后变为 active\n", id, admin.NormalizeUsername(*username), *role)
	return nil
}

// readLine 读取一行并去掉行尾换行；不 trim 空格，密码中的空格是有效字符。
func readLine(r io.Reader) (string, error) {
	line, err := bufio.NewReader(r).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}
