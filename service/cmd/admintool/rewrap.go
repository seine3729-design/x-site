package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"xsite/service/internal/config"
	"xsite/service/internal/crypto/mediakey"
	"xsite/service/internal/store/pg"
)

// rewrapFailure 是一把解不开的内容密钥。
type rewrapFailure struct {
	EpisodeID int64
	KeyVer    int32
	KID       string
	Err       error
}

// rewrapPlan 是一次重新包装的预览：哪些已是当前 KEK、哪些要改、哪些解不开。
type rewrapPlan struct {
	Total    int
	Current  int
	ByKID    map[string]int // 需要重新包装的，按原 kid 统计
	Changes  []pg.MediaKeyReplacement
	Failures []rewrapFailure
}

// planRewrap 逐把判断：target 的当前 KEK 已能解开的不动；其余用 target 或 fallback 解开后改用当前 KEK 包装。
// 只在内存里算，不写库，所以预览与执行用的是同一份计划。
func planRewrap(rows []pg.WrappedMediaKey, target, fallback *mediakey.Keyring) rewrapPlan {
	plan := rewrapPlan{Total: len(rows), ByKID: map[string]int{}}
	for _, row := range rows {
		out, changed, err := mediakey.Rewrap(target, fallback, row.EpisodeID, row.KeyVer, row.Wrapped)
		switch {
		case err != nil:
			plan.Failures = append(plan.Failures, rewrapFailure{EpisodeID: row.EpisodeID, KeyVer: row.KeyVer, KID: kidLabel(row.Wrapped), Err: err})
		case changed:
			plan.ByKID[kidLabel(row.Wrapped)]++
			plan.Changes = append(plan.Changes, pg.MediaKeyReplacement{EpisodeID: row.EpisodeID, KeyVer: row.KeyVer, Old: row.Wrapped, New: out})
		default:
			plan.Current++
		}
	}
	return plan
}

func kidLabel(wrapped []byte) string {
	if kid := mediakey.KIDOf(wrapped); kid != "" {
		return kid
	}
	return "（格式错误）"
}

// rewrapMediaKeys 是 admintool rewrap-media-keys：把内容密钥改用 MEDIA_KEKS 第一把 KEK 重新包装。
//
// 典型场景：本地库导入线上后，旧剧集的内容密钥是本地 KEK 包装的，线上 API 解不开（日志里是
// 「mediakey: 未知的 KEK」或「mediakey: 密文无效」），所有旧分集播放失败。重新包装只换外层密文，
// 内容密钥本身不变，所以 HLS 切片与清单不用重新转码。
//
// 旧 KEK 从标准输入读一行，不出现在命令行参数、进程列表与 shell 历史里；旧 KEK 已经追加在
// MEDIA_KEKS 后面时给空行即可。只需要 DATABASE_URL 与 MEDIA_KEKS 两个环境变量。
func rewrapMediaKeys(args []string, stdin io.Reader, stdout io.Writer) error {
	fs := flag.NewFlagSet("rewrap-media-keys", flag.ContinueOnError)
	apply := fs.Bool("apply", false, "写库；不加只预览")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return errors.New(usage)
	}
	target, err := mediakey.ParseKeyring(os.Getenv("MEDIA_KEKS"))
	if err != nil {
		return fmt.Errorf("MEDIA_KEKS: %w", err)
	}
	oldSpec, err := readLine(stdin)
	if err != nil {
		return fmt.Errorf("读取旧 KEK: %w", err)
	}
	var fallback *mediakey.Keyring
	if strings.TrimSpace(oldSpec) != "" {
		if fallback, err = mediakey.ParseKeyring(oldSpec); err != nil {
			return fmt.Errorf("标准输入里的旧 KEK: %w", err)
		}
	}

	cfg, err := config.LoadMigration()
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	pool, err := pg.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	repo := pg.NewMediaKeyRewrapRepo(pool)

	rows, err := repo.List(ctx)
	if err != nil {
		return err
	}
	plan := planRewrap(rows, target, fallback)
	printRewrapPlan(stdout, plan, target.CurrentKID())

	switch {
	case len(plan.Changes) == 0:
		// 有解不开的时候不说这句：后面的报错才是重点，两句连着读像是互相矛盾
		if len(plan.Failures) == 0 {
			fmt.Fprintln(stdout, "没有需要重新包装的内容密钥。")
		}
	case !*apply:
		fmt.Fprintln(stdout, "预览模式，没有写库。确认无误后加 -apply 执行。")
	default:
		if err := repo.Replace(ctx, plan.Changes); err != nil {
			return err
		}
		fmt.Fprintf(stdout, "已在一个事务里重新包装 %d 把。\n", len(plan.Changes))
		// 复查只用 MEDIA_KEKS、不给旧 KEK：之后线上 API 就是这样解的
		after, err := repo.List(ctx)
		if err != nil {
			return err
		}
		check := planRewrap(after, target, nil)
		if len(check.Changes) == 0 && len(check.Failures) == len(plan.Failures) {
			fmt.Fprintf(stdout, "复查：%d 把里 %d 把都是 %s 包装、只用 MEDIA_KEKS 就能解开；MEDIA_KEKS 里追加过的旧 KEK 现在可以删掉。\n",
				check.Total, check.Current, target.CurrentKID())
		} else {
			fmt.Fprintf(stdout, "复查：仍有 %d 把不是 %s 包装、%d 把解不开，先别删旧 KEK，重新执行一次本命令。\n",
				len(check.Changes), target.CurrentKID(), len(check.Failures))
		}
	}
	if len(plan.Failures) > 0 {
		return fmt.Errorf("有 %d 把内容密钥解不开（见上方列表）：确认旧 KEK 给对了；确实找不回旧 KEK 的分集只能重新上传视频转码", len(plan.Failures))
	}
	return nil
}

func printRewrapPlan(w io.Writer, plan rewrapPlan, currentKID string) {
	fmt.Fprintf(w, "内容密钥共 %d 把；MEDIA_KEKS 当前（第一把）KEK：%s\n", plan.Total, currentKID)
	fmt.Fprintf(w, "  已是 %s 包装：%d 把\n", currentKID, plan.Current)
	kids := make([]string, 0, len(plan.ByKID))
	for kid := range plan.ByKID {
		kids = append(kids, kid)
	}
	sort.Strings(kids)
	parts := make([]string, len(kids))
	for i, kid := range kids {
		parts[i] = fmt.Sprintf("%s %d 把", kid, plan.ByKID[kid])
	}
	detail := ""
	if len(parts) > 0 {
		detail = "（原 KEK：" + strings.Join(parts, "、") + "）"
	}
	fmt.Fprintf(w, "  需要重新包装：%d 把%s\n", len(plan.Changes), detail)
	fmt.Fprintf(w, "  解不开：%d 把\n", len(plan.Failures))
	for i, f := range plan.Failures {
		if i == 10 {
			fmt.Fprintf(w, "    …另有 %d 把\n", len(plan.Failures)-i)
			break
		}
		fmt.Fprintf(w, "    第 %d 集第 %d 版（kid %s）：%v\n", f.EpisodeID, f.KeyVer, f.KID, f.Err)
	}
}
