#!/usr/bin/env bash
# 把一个前端的静态产物同步到 Cloudflare R2（前端静态资源 CDN）。
#
#   用法: cdn-upload.sh <apps|admin|feedback> <本地产物目录>
#   环境: R2_ENDPOINT R2_BUCKET AWS_ACCESS_KEY_ID AWS_SECRET_ACCESS_KEY AWS_DEFAULT_REGION=auto
#
# 三个前端的资源前缀与构建期注入的变量必须对上，否则页面会去一个不存在的地址取脚本：
#   apps      CDN_ASSET_PREFIX=<CDN>/apps        → 上传 apps/build/client  到 apps/
#   admin     ADMIN_ASSET_BASE=<CDN>/admin/      → 上传 admin/dist         到 admin/
#   feedback  NEXT_ASSET_PREFIX=<CDN>/feedback   → 上传 feedback/.next/static 到 feedback/_next/static/
#
# 刻意不上传三类文件：
#   *.br / *.gz  预压缩文件在 R2 上是独立的对象键，CDN 不会按 Accept-Encoding 在不同键之间选，
#                上去只会变成「一个把 Brotli 字节当 JS 发出去」的坑。Cloudflare 边缘自己会压。
#                这些文件留给边缘层的 brotli_static / gzip_static 用（nginx 直接读本地产物时）。
#   *.map        admin 是 sourcemap: 'hidden'，产物里不引用；公开出去等于把源码送人。
#   index.html   必须由各自的站点域名发出（后台还要和 /api/admin 同源），不能从 CDN 裸域取。
#
# 不删远端多余对象（没有 --delete）：带内容哈希的旧产物要留着，
# 否则刚发布的瞬间，老页面还在引用的 chunk 就 404 了。清理靠 R2 的生命周期规则。
set -euo pipefail

target=${1:?用法: cdn-upload.sh <apps|admin|feedback> <本地产物目录>}
src=${2:?用法: cdn-upload.sh <apps|admin|feedback> <本地产物目录>}

: "${R2_ENDPOINT:?必须设置 R2_ENDPOINT}"
: "${R2_BUCKET:?必须设置 R2_BUCKET}"
: "${AWS_ACCESS_KEY_ID:?必须设置 AWS_ACCESS_KEY_ID}"
: "${AWS_SECRET_ACCESS_KEY:?必须设置 AWS_SECRET_ACCESS_KEY}"
export AWS_DEFAULT_REGION=${AWS_DEFAULT_REGION:-auto}

[[ -d $src ]] || { echo "产物目录不存在：$src" >&2; exit 1; }

# 带内容哈希的子树（改内容必然换文件名）与其余文件的缓存策略不同
case $target in
  apps)     prefix=apps;              immutable_dir=_app/immutable ;;
  admin)    prefix=admin;             immutable_dir=assets ;;
  feedback) prefix=feedback/_next/static; immutable_dir= ;;   # 整棵树都带哈希
  *) echo "未知目标：${target}（只支持 apps / admin / feedback）" >&2; exit 1 ;;
esac

IMMUTABLE='public, max-age=31536000, immutable'
SHORT='public, max-age=300, stale-while-revalidate=86400'

sync() {
  aws --endpoint-url "$R2_ENDPOINT" s3 sync "$1" "s3://$R2_BUCKET/$2" \
    --cache-control "$3" \
    --exclude '*.br' --exclude '*.gz' --exclude '*.map' \
    --exclude 'index.html' --exclude 'service-worker.js' \
    --no-progress \
    "${@:4}"
}

echo "CDN：$target → s3://$R2_BUCKET/$prefix/"

if [[ -z $immutable_dir ]]; then
  sync "$src" "$prefix/" "$IMMUTABLE"
else
  # 先传不可变的那棵子树，再传其余（其余里排掉它，免得用短缓存覆盖掉刚写的头）
  if [[ -d "$src/$immutable_dir" ]]; then
    sync "$src/$immutable_dir" "$prefix/$immutable_dir/" "$IMMUTABLE"
  else
    echo "警告：$src/$immutable_dir 不存在，跳过不可变子树" >&2
  fi
  sync "$src" "$prefix/" "$SHORT" --exclude "$immutable_dir/*"
fi

echo "CDN：$target 同步完成"
