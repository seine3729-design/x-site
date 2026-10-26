#!/bin/sh
# 把 .next/static（含 scripts/precompress.mjs 压好的 .br / .gz）同步到挂载出来的卷，
# 供边缘层直接从磁盘发文件（brotli_static / gzip_static）。
#
# 为什么不让 Next.js 自己发：next start 不会去找 .br / .gz，只会按请求现压（而且只有 gzip）。
# 构建期压好的是 Brotli 11，边缘层直接送出去既最小又不耗 CPU。
#
# public/ 刻意不导出：那几个文件很小，而且 /sw.js 必须是 no-store、next.config.ts 里
# 已经设好了对应的响应头，交给 Next.js 发更不容易出错。
#
# 换目录用两次 rename 完成，不是「先删后拷」：
# nginx 正在从同一个卷读文件，先删会出现一段时间静态资源全部 404。
set -eu

DEST=${EXPORT_DIR:-/export}
mkdir -p "$DEST"

rm -rf "$DEST/.staging" "$DEST/.old"
cp -a /app/.next/static "$DEST/.staging"
if [ -e "$DEST/current" ]; then
    mv "$DEST/current" "$DEST/.old"
fi
mv "$DEST/.staging" "$DEST/current"
rm -rf "$DEST/.old"

echo "反馈社区静态产物已导出：$(find "$DEST/current" -type f | wc -l) 个文件，其中预压缩 $(find "$DEST/current" -name '*.br' | wc -l) 个 .br"
