// mediatool 是媒体相关的本地开发工具（不部署到线上），在 service 开发容器中执行（镜像带 FFmpeg）：
//
//	./dev.sh go run ./cmd/mediatool fixtures          # X1-1：生成 10 部样例源视频并上传到 xsite-assets/fixtures/
//	./dev.sh go run ./cmd/mediatool verify -episode 12 # 下载某集的转封装产物，按密钥解密并用 FFmpeg 完整解码
//
// verify 会在内存中解开内容密钥，只用于本地验收与排障。
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
)

const usage = `用法：
  mediatool fixtures [-out 目录] [-upload=true] [-prefix fixtures/]
      生成测试素材库（docs/11 X1-1），上传到 S3_BUCKET_ASSETS 的 fixtures/ 前缀，并写 manifest.json
  mediatool verify -episode <分集 ID>
      校验该集当前的转封装产物：主清单 → 各档清单 → 切片，按 media_key 解密后用 FFmpeg 完整解码
  mediatool geoip-sample [-out 路径]
      生成本地开发用的小 GeoIP 数据集（S1-6c）。没有真实 ip2region 文件时用它让按地区选线能跑起来；
      生成后在 .env 里设 GEOIP_XDB_PATH 指向它`

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "错误：", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return errors.New(usage)
	}
	switch args[0] {
	case "fixtures":
		return runFixtures(args[1:])
	case "verify":
		return runVerify(args[1:])
	case "geoip-sample":
		fs := flag.NewFlagSet("geoip-sample", flag.ContinueOnError)
		out := fs.String("out", "deploy/local/geoip/ip2region.xdb", "输出路径")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		return geoipSample(*out)
	default:
		return errors.New(usage)
	}
}
