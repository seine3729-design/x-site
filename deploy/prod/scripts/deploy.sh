#!/usr/bin/env bash
# 在服务器上执行一次发布。由 GitHub Actions 经 SSH 调用，也可以人工执行。
#
#   用法: deploy.sh [镜像标签]
#   位置: 服务器上的 scripts/ 子目录，脚本自己会切到上一级（compose.yaml 与 .env 所在的目录）。
#         工作流同步时去掉了 prod/ 这一层，所以服务器上的布局是
#         $DEPLOY_PATH/{compose.yaml,.env,scripts/,nginx/,certbot/,geoip/}，默认 $DEPLOY_PATH=/opt/xsite
#
# 顺序是刻意的：
#   拉镜像 → 迁移 → 起服务 → 重载 nginx → 验活
#
#   先迁移再起服务：production 禁用 AUTO_MIGRATE（启动校验会拒），库必须在新代码起来之前就位。
#   迁移失败就停在这里，旧版本还在跑，不会出现「一半实例写不兼容的数据」。
#
#   最后重载 nginx：开源版 nginx 只在启动/重载时解析 upstream 里的服务名，
#   重建过的容器 IP 变了而 nginx 不知道，会一直 502（见 nginx.conf 里的说明）。
set -euo pipefail

cd "$(dirname "$0")/.."

[[ -f compose.yaml ]] || { echo "找不到 compose.yaml，请在 deploy/prod 下执行" >&2; exit 1; }
[[ -f .env ]] || { echo "找不到 .env，先从 .env.example 复制并填值（chmod 600）" >&2; exit 1; }

compose() { docker compose -f compose.yaml --env-file .env "$@"; }

tag=${1:-}
if [[ -n $tag ]]; then
  # 把本次的标签写回 .env，这样人工 docker compose 命令和脚本看到的是同一个版本
  if grep -q '^RELEASE_TAG=' .env; then
    sed -i -E "s|^RELEASE_TAG=.*|RELEASE_TAG=${tag}|" .env
  else
    printf 'RELEASE_TAG=%s\n' "$tag" >> .env
  fi
fi
tag=$(grep -E '^RELEASE_TAG=' .env | head -1 | cut -d= -f2-)
echo "发布：镜像标签 ${tag:-latest}"

# GeoIP 数据目录（挂进 service / worker）。没有文件时地区按未知处理，不影响启动
mkdir -p geoip

echo "发布：拉取镜像"
compose pull --quiet

echo "发布：执行数据库迁移"
compose --profile release run --rm migrate

echo "发布：启动/更新服务"
compose up -d --remove-orphans

echo "发布：重载边缘层（让 nginx 重新解析上游容器地址）"
compose exec -T nginx nginx -s reload

echo "发布：验活"
fail=0
for i in $(seq 1 30); do
  if compose exec -T service /app/api -healthcheck >/dev/null 2>&1; then
    break
  fi
  if [[ $i -eq 30 ]]; then
    echo "后端 /api/v1/health 在 60 秒内没有就绪" >&2
    fail=1
  fi
  sleep 2
done

for name in apps feedback nginx; do
  state=$(compose ps --format '{{.State}}' "$name" 2>/dev/null | head -1)
  printf '  %-9s %s\n' "$name" "${state:-未运行}"
  [[ $state == running ]] || fail=1
done

# 后台与反馈的静态产物是一次性容器导出到卷的，确认卷里确实有东西
for pair in "admin:/srv/admin/current/index.html" "feedback:/srv/feedback/current"; do
  name=${pair%%:*}
  path=${pair#*:}
  if compose exec -T nginx test -e "$path"; then
    printf '  %-9s 静态产物已就位\n' "$name"
  else
    printf '  %-9s 静态产物缺失（%s）\n' "$name" "$path"
    fail=1
  fi
done

if [[ $fail -ne 0 ]]; then
  echo "发布：有服务未就绪，下面是最近日志" >&2
  compose logs --tail 60 service apps admin feedback nginx >&2 || true
  exit 1
fi

# 保留上一个版本的镜像便于回滚（docker image prune 只清没有任何标签的层）
docker image prune -f >/dev/null 2>&1 || true

echo "发布：完成。回滚执行 ./scripts/deploy.sh <上一个标签>"
