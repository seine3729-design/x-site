#!/usr/bin/env bash
# 重新生成 deploy/prod/nginx/snippets/cloudflare-ips.conf。
#
#   用法: ./scripts/update-cloudflare-ips.sh
#
# 刻意在仓库里存一份静态清单、而不是在镜像构建或容器启动时去抓：
# 启动时抓网络会让「Cloudflare 的站点打不开」连带导致「自己的 nginx 起不来」。
# 代价是网段变化要人工同步，所以这个脚本存在，升级边缘层镜像时顺手跑一次、看 diff。
set -euo pipefail

cd "$(dirname "$0")/.."
OUT=nginx/snippets/cloudflare-ips.conf

v4=$(curl -fsS --max-time 20 https://www.cloudflare.com/ips-v4)
v6=$(curl -fsS --max-time 20 https://www.cloudflare.com/ips-v6)
[[ -n $v4 && -n $v6 ]] || { echo "拉取 Cloudflare 网段失败，没有改动 $OUT" >&2; exit 1; }

{
  sed -n '1,/^# 最后更新/p' "$OUT" | sed '$d'
  printf '# 最后更新：%s。网段会变，升级边缘层镜像时顺手重跑一次脚本。\n\n' "$(date +%Y-%m-%d)"
  printf '%s\n%s\n' "$v4" "$v6" | sed 's|^|set_real_ip_from |; s|$|;|'
  cat <<'TAIL'

real_ip_header    CF-Connecting-IP;
# 不递归：CF-Connecting-IP 是单个地址，不是链
real_ip_recursive off;
TAIL
} > "$OUT.new"

mv "$OUT.new" "$OUT"
echo "已更新 ${OUT}（$(grep -c set_real_ip_from "$OUT") 个网段）"
