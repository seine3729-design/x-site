#!/bin/sh
# 把镜像里的后台静态产物（含 .br / .gz 预压缩文件）同步到挂载出来的卷，
# 供边缘层直接从磁盘发文件（brotli_static / gzip_static）。
#
# 后台页面不经过反向代理再套一层 nginx：少一跳，而且边缘层那个 nginx 带 ngx_brotli，
# 能把构建期压好的 .br 原样送出去（官方 nginx 镜像没有这个模块）。
#
# 换目录用两次 rename 完成，不是「先删后拷」：
# nginx 正在从同一个卷读文件，先删会出现一段时间所有请求 404。
set -eu

DEST=${EXPORT_DIR:-/export}
mkdir -p "$DEST"

rm -rf "$DEST/.staging" "$DEST/.old"
cp -a /app/dist "$DEST/.staging"
if [ -e "$DEST/current" ]; then
    mv "$DEST/current" "$DEST/.old"
fi
mv "$DEST/.staging" "$DEST/current"
rm -rf "$DEST/.old"

echo "后台静态产物已导出：$(find "$DEST/current" -type f | wc -l) 个文件，其中预压缩 $(find "$DEST/current" -name '*.br' | wc -l) 个 .br"
