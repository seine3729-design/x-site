#!/usr/bin/env bash
# 本地开发一键脚本。用法见 ./dev.sh help
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ENV_FILE="$ROOT/.env"
COMPOSE_FILE="$ROOT/compose.yaml"
INFRA=(postgres valkey seaweedfs)

c_info() { printf '\033[36m%s\033[0m\n' "$*"; }
c_ok()   { printf '\033[32m%s\033[0m\n' "$*"; }
c_warn() { printf '\033[33m%s\033[0m\n' "$*" >&2; }
c_err()  { printf '\033[31m%s\033[0m\n' "$*" >&2; }

compose() { docker compose -f "$COMPOSE_FILE" --env-file "$ENV_FILE" "$@"; }

has_service_code() { [[ -f "$ROOT/service/go.mod" ]]; }

env_get() {
  # 读取 .env 中的值（不 source，避免执行任意内容）
  local key="$1" default="${2:-}"
  local line
  line="$(grep -E "^${key}=" "$ENV_FILE" 2>/dev/null | tail -1 || true)"
  if [[ -n "$line" ]]; then printf '%s' "${line#*=}"; else printf '%s' "$default"; fi
}

ensure_docker() {
  if ! command -v docker >/dev/null 2>&1; then
    c_err "没有找到 docker 命令。请先安装 OrbStack 或 Docker Desktop。"
    exit 1
  fi
  if docker info >/dev/null 2>&1; then return; fi

  c_info "Docker 未运行，尝试启动…"
  if command -v orb >/dev/null 2>&1; then
    orb start >/dev/null 2>&1 || true
  elif [[ "$(uname)" == "Darwin" ]]; then
    open -a Docker >/dev/null 2>&1 || true
  fi
  for _ in $(seq 1 60); do
    docker info >/dev/null 2>&1 && return
    sleep 2
  done
  c_err "Docker 启动超时，请手动启动后重试。"
  exit 1
}

ensure_env() {
  if [[ -f "$ENV_FILE" ]]; then
    fill_missing_env
    return
  fi
  c_info "首次运行：从 .env.example 生成 .env，并为所有 change-me 生成随机值"
  local tmp
  tmp="$(mktemp)"
  while IFS= read -r line || [[ -n "$line" ]]; do
    if [[ "$line" == *=change-me ]]; then
      printf '%s=%s\n' "${line%%=*}" "$(openssl rand -hex 16)" >>"$tmp"
    else
      printf '%s\n' "$line" >>"$tmp"
    fi
  done <"$ROOT/.env.example"
  mv "$tmp" "$ENV_FILE"
  chmod 600 "$ENV_FILE"
}

# .env.example 新增变量后，给已有的 .env 补上（change-me 同样换成随机值），已有的值不动
fill_missing_env() {
  local line key added=()
  # 末行没有换行时先补一个，避免新变量接到上一行后面
  if [[ -s "$ENV_FILE" && -n "$(tail -c1 "$ENV_FILE")" ]]; then printf '\n' >>"$ENV_FILE"; fi
  while IFS= read -r line || [[ -n "$line" ]]; do
    [[ "$line" =~ ^[A-Z0-9_]+= ]] || continue
    key="${line%%=*}"
    grep -qE "^${key}=" "$ENV_FILE" && continue
    if [[ "$line" == *=change-me ]]; then
      printf '%s=%s\n' "$key" "$(openssl rand -hex 16)" >>"$ENV_FILE"
    else
      printf '%s\n' "$line" >>"$ENV_FILE"
    fi
    added+=("$key")
  done <"$ROOT/.env.example"
  if [[ ${#added[@]} -gt 0 ]]; then
    c_info ".env 已补充新增变量：${added[*]}"
  fi
}

# 根据参数组装 profile
build_profiles() {
  local target="$1" obs="$2"
  PROFILES=()
  case "$target" in
    infra) ;;
    web) PROFILES+=(--profile web) ;;
    service) PROFILES+=(--profile service) ;;
    all)
      PROFILES+=(--profile web)
      if has_service_code; then
        PROFILES+=(--profile service)
      else
        c_warn "service/go.mod 不存在，暂不启动 Go 后端（完成任务 S0-1 后自动启用）。"
      fi
      ;;
    *) c_err "未知目标：${target}（可选 all | infra | web | service）"; exit 1 ;;
  esac
  if [[ "$target" == "service" ]] && ! has_service_code; then
    c_err "service/go.mod 不存在，无法启动后端。"
    exit 1
  fi
  if [[ "$obs" == "1" ]]; then PROFILES+=(--profile obs); fi
}

# 等待容器健康；没有 healthcheck 的容器只要求在运行
wait_healthy() {
  local timeout="${1:-300}" start now ids id name state health pending
  start="$(date +%s)"
  while true; do
    pending=()
    ids="$(compose ${PROFILES[@]+"${PROFILES[@]}"} ps -q 2>/dev/null || true)"
    for id in $ids; do
      name="$(docker inspect -f '{{index .Config.Labels "com.docker.compose.service"}}' "$id")"
      state="$(docker inspect -f '{{.State.Status}}' "$id")"
      health="$(docker inspect -f '{{if .State.Health}}{{.State.Health.Status}}{{end}}' "$id")"
      if [[ "$name" == "s3-init" ]]; then continue; fi
      if [[ "$state" != "running" ]]; then
        c_err "容器 $name 状态为 ${state}，最近日志："
        compose ${PROFILES[@]+"${PROFILES[@]}"} logs --tail 40 "$name" >&2 || true
        return 1
      fi
      if [[ -n "$health" && "$health" != "healthy" ]]; then pending+=("$name($health)"); fi
    done
    if [[ ${#pending[@]} -eq 0 ]]; then return 0; fi
    now="$(date +%s)"
    if (( now - start > timeout )); then
      c_warn "等待超时，仍未就绪：${pending[*]}"
      c_warn "首次启动需要安装依赖，可用 ./dev.sh logs <服务名> 查看进度。"
      return 0
    fi
    printf '\r等待就绪：%s        ' "${pending[*]}"
    sleep 3
  done
}

print_urls() {
  local bind
  bind="$(env_get BIND_ADDR 127.0.0.1)"
  [[ "$bind" == "0.0.0.0" ]] && bind="localhost"
  echo
  c_ok "本地开发环境已启动"
  cat <<EOF
  Web 前台        http://${bind}:$(env_get APPS_PORT 3001)
  运营后台        http://${bind}:$(env_get ADMIN_PORT 5173)
  反馈社区前台    http://${bind}:$(env_get FEEDBACK_PORT 3002)（仅 ./dev.sh up --feedback 时启动）
  Go 后端         http://${bind}:$(env_get SERVICE_PORT 8080)/api/v1/health
  PostgreSQL     ${bind}:$(env_get PG_PORT 5432)  库 $(env_get XSITE_DB xsite)  账号 $(env_get XSITE_APP_USER xsite_app)（密码见 .env）
  Valkey         ${bind}:$(env_get VALKEY_PORT 6379)（密码见 .env）
  S3(SeaweedFS)  http://${bind}:$(env_get S3_PORT 8333)  桶 $(env_get S3_BUCKET_MEDIA) / $(env_get S3_BUCKET_ASSETS)
  Jaeger         http://${bind}:$(env_get JAEGER_UI_PORT 16686)（--obs 时）

  APP 调试：Android 模拟器访问 http://10.0.2.2:$(env_get SERVICE_PORT 8080)，iOS 模拟器访问 http://localhost:$(env_get SERVICE_PORT 8080)
EOF
}

cmd_up() {
  local target="all" obs="0" ftp="0" feedback="0" build=()
  for arg in "$@"; do
    case "$arg" in
      --obs) obs="1" ;;
      --ftp) ftp="1" ;;
      --feedback) feedback="1" ;;
      --build) build=(--build) ;;
      all|infra|web|service) target="$arg" ;;
      *) c_err "未知参数：$arg"; exit 1 ;;
    esac
  done
  ensure_docker
  ensure_env
  build_profiles "$target" "$obs"
  if [[ "$obs" == "1" ]]; then export OTEL_SDK_DISABLED=false; fi
  # 本地资源机只在 compose 网络内可访问，由容器里的 worker 连接，因此不随 infra 启动
  if [[ "$ftp" == "1" ]]; then
    if [[ "$target" == "infra" ]]; then c_warn "--ftp 需要容器内的 worker，infra 模式下忽略"; else PROFILES+=(--profile ftp); fi
  fi
  # 反馈社区是独立产品、单独吃约 1.6G 内存，默认不随 all/web 启动，用 --feedback 显式开启
  if [[ "$feedback" == "1" ]]; then
    if [[ "$target" == "infra" ]]; then c_warn "--feedback 需要 web 服务，infra 模式下忽略"; else PROFILES+=(--profile feedback); fi
  fi

  c_info "启动：$target ${PROFILES[*]:-}"
  if [[ "$target" == "infra" ]]; then
    compose up -d ${build[@]+"${build[@]}"} "${INFRA[@]}" s3-init
  else
    compose ${PROFILES[@]+"${PROFILES[@]}"} up -d ${build[@]+"${build[@]}"}
  fi
  wait_healthy 420
  echo
  print_urls
}

cmd_reset() {
  local yes="${1:-}"
  ensure_docker
  if [[ "$yes" != "--yes" ]]; then
    c_warn "将删除本地数据库、对象存储、依赖缓存等所有卷，数据不可恢复。"
    read -r -p "确认请输入 reset：" answer
    [[ "$answer" == "reset" ]] || { echo "已取消"; exit 0; }
  fi
  compose --profile web --profile service --profile obs --profile ftp --profile feedback down -v --remove-orphans
  c_ok "已清空。下次 ./dev.sh up 会重新初始化数据库。"
}

cmd_doctor() {
  ensure_docker
  ensure_env
  c_info "Docker：$(docker version --format '{{.Server.Version}}')  Compose：$(docker compose version --short)"
  local key port
  for key in PG_PORT VALKEY_PORT S3_PORT SERVICE_PORT APPS_PORT ADMIN_PORT FEEDBACK_PORT; do
    port="$(env_get "$key")"
    # -Fc 只输出进程名（每行 c<name>），排除 Docker/OrbStack 自己的端口转发进程
    if lsof -nP -iTCP:"$port" -sTCP:LISTEN -Fc 2>/dev/null | sed -n 's/^c//p' \
        | grep -qv -E '^(com\.docke|docker|OrbStack|vpnkit|Docker)'; then
      c_warn "端口 ${port}（${key}）已被其他进程占用，可在 .env 中修改"
    else
      echo "端口 ${port}（${key}）可用或由 Docker 占用"
    fi
  done
  has_service_code && echo "service：已初始化" || c_warn "service：尚未初始化（service/go.mod 不存在）"
  [[ -d "$ROOT/mobile/lib" ]] && echo "mobile：已初始化" || c_warn "mobile：尚未初始化（APP 在宿主机用 flutter run 调试，不进容器）"
}

usage() {
  cat <<'EOF'
用法：./dev.sh <命令> [参数]

  up [all|infra|web|service] [--obs] [--ftp] [--feedback] [--build]
                     启动环境。默认 all = 基础设施 + apps + admin + service(已初始化时)
                     infra 只起数据库等依赖，适合在宿主机直接跑 bun/pnpm/go
                     --obs 启动 Jaeger；--ftp 启动本地资源机；--feedback 启动反馈社区前台（默认不起，单独吃 ~1.6G 内存）；--build 重新构建本地镜像
  down               停止并删除容器（保留数据卷）
  stop               只停止容器
  restart [服务…]     重启指定服务
  logs [服务…]        跟随查看日志
  ps                 查看容器状态
  urls               打印访问地址
  psql [--ro|--root] 进入数据库（默认应用账号）
  valkey             进入 valkey-cli
  sh <服务>           进入容器 shell
  go <参数…>          在 service 开发镜像中执行 go（例：./dev.sh go test ./...）
  run <服务> <命令…>   在服务容器中执行命令（例：./dev.sh run apps bun run check）
  flutter <参数…>     在 mobile/ 目录执行本机 flutter（例：./dev.sh flutter test）
  migrate [参数…]     在 service 容器中执行 goose（默认 up）
  sqlc               在 service 容器中执行 sqlc generate
  reset [--yes]      删除所有本地数据卷（危险）
  doctor             检查 Docker、端口与工程状态
EOF
}

main() {
  local cmd="${1:-help}"
  shift || true
  case "$cmd" in
    up) cmd_up "$@" ;;
    down) ensure_docker; compose --profile web --profile service --profile obs --profile ftp --profile feedback down --remove-orphans ;;
    stop) ensure_docker; compose --profile web --profile service --profile obs --profile ftp --profile feedback stop ;;
    restart) ensure_docker; compose --profile web --profile service --profile obs --profile ftp --profile feedback restart "$@" ;;
    logs) ensure_docker; compose --profile web --profile service --profile obs --profile ftp --profile feedback logs -f --tail 200 "$@" ;;
    ps) ensure_docker; compose --profile web --profile service --profile obs --profile ftp --profile feedback ps ;;
    urls) ensure_env; print_urls ;;
    psql)
      ensure_docker
      case "${1:-}" in
        --root) compose exec postgres psql -U postgres -d "$(env_get XSITE_DB xsite)" ;;
        --ro) compose exec -e PGPASSWORD="$(env_get XSITE_RO_PASSWORD)" postgres \
                psql -h 127.0.0.1 -U "$(env_get XSITE_RO_USER)" -d "$(env_get XSITE_DB xsite)" ;;
        *) compose exec -e PGPASSWORD="$(env_get XSITE_APP_PASSWORD)" postgres \
             psql -h 127.0.0.1 -U "$(env_get XSITE_APP_USER)" -d "$(env_get XSITE_DB xsite)" ;;
      esac
      ;;
    valkey) ensure_docker; compose exec -e REDISCLI_AUTH="$(env_get VALKEY_PASSWORD)" valkey valkey-cli ;;
    sh)
      ensure_docker
      [[ -n "${1:-}" ]] || { c_err "请指定服务名"; exit 1; }
      compose --profile web --profile service --profile ftp --profile feedback exec "$1" sh
      ;;
    migrate)
      ensure_docker
      has_service_code || { c_err "service 尚未初始化"; exit 1; }
      local args=("$@")
      [[ ${#args[@]} -eq 0 ]] && args=(up)
      compose --profile service run --rm --no-deps service \
        sh -c 'goose -dir db/migrations postgres "$DATABASE_URL" "$@"' goose ${args[@]+"${args[@]}"}
      ;;
    sqlc)
      ensure_docker
      has_service_code || { c_err "service 尚未初始化"; exit 1; }
      compose --profile service run --rm --no-deps service sqlc generate
      ;;
    go)
      # 在 service 开发镜像中运行 go（保证 Go 1.27.3，与本机 Go 版本无关）
      ensure_docker
      ensure_env
      compose --profile service run --rm --no-deps -T service go "$@"
      ;;
    run)
      # 在指定服务容器中执行命令：容器在运行就 exec，否则起一次性容器
      ensure_docker
      ensure_env
      [[ -n "${1:-}" ]] || { c_err "用法：./dev.sh run <服务> <命令…>"; exit 1; }
      local svc="$1"
      shift
      if [[ -n "$(compose --profile web --profile service --profile feedback ps -q --status running "$svc" 2>/dev/null)" ]]; then
        compose --profile web --profile service --profile feedback exec -T "$svc" "$@"
      else
        compose --profile web --profile service --profile feedback run --rm --no-deps -T "$svc" "$@"
      fi
      ;;
    flutter)
      # APP 在宿主机运行 Flutter（需要接模拟器）；版本低于文档基线时给出提示
      command -v flutter >/dev/null 2>&1 || { c_err "未安装 Flutter，请先安装 3.47.x"; exit 1; }
      local fv
      fv="$(flutter --version --machine 2>/dev/null | sed -n 's/.*"frameworkVersion": *"\([^"]*\)".*/\1/p')"
      if [[ -n "$fv" ]] && [[ "$(printf '%s\n3.47.0\n' "$fv" | sort -V | head -1)" != "3.47.0" ]]; then
        c_warn "本机 Flutter ${fv} 低于文档基线 3.47，建议执行 flutter upgrade"
      fi
      (cd "$ROOT/mobile" && flutter "$@")
      ;;
    reset) cmd_reset "${1:-}" ;;
    doctor) cmd_doctor ;;
    help|-h|--help) usage ;;
    *) c_err "未知命令：$cmd"; usage; exit 1 ;;
  esac
}

main "$@"
