#!/usr/bin/env bash
# 生成 .env 里全部密钥字段，输出到标准输出。
#
#   用法: ./scripts/gen-secrets.sh            # 打印出来，自己粘到 .env
#         ./scripts/gen-secrets.sh >> .env    # 直接追加（追加后记得删掉 .env 里原来的空值行）
#
# 为什么要有这个脚本，而不是在文档里写一行 openssl：
# 最容易犯的错是「生成一个值，粘到所有字段」。那样一把密钥泄露就等于全部泄露，
# 而且轮换任何一项都要同时动其他所有项。这里每个字段单独生成。
#
# 刻意**不**自动写进 .env、不打印到日志：
# - 直接覆盖 .env 会把已经填好的数据库密码冲掉；
# - 这些值一旦进了 CI 日志或终端记录就等于泄露，必须由人决定去向。
set -euo pipefail

command -v openssl >/dev/null || { echo "需要 openssl" >&2; exit 1; }

# 裸密钥：长度 64 个十六进制字符，满足所有「至少 32 字符」的启动校验
plain=(
  BFF_SERVICE_TOKEN     # apps / feedback 的服务端调后端时携带
  EDGE_SHARED_SECRET    # 边缘层回源时携带 x-edge-secret
)

# `引用:密钥` 形式。第一把用于签发，列表里全部用于校验；
# 轮换时在最前面加一把新的、保留旧的一段时间
keyed=(
  API_ENC_KEYS          # 用户端接口报文加密
  JWT_SIGNING_KEYS      # 用户端 Access Token 签名
  PLAY_TOKEN_KEYS       # 播放令牌签名
  MEDIA_KEKS            # HLS 内容密钥的包装密钥
  STORAGE_KEKS          # 存储源凭据的包装密钥
  CONFIG_BACKUP_KEYS    # 配置备份的签名密钥
  ADMIN_TOTP_KEKS       # 后台二次验证 TOTP 种子的包装密钥
)

# 数据库与缓存口令：没有格式要求，但同样一个字段一个值
passwords=(
  POSTGRES_PASSWORD
  XSITE_APP_PASSWORD
  XSITE_RO_PASSWORD
  VALKEY_PASSWORD
)

echo "# 由 scripts/gen-secrets.sh 生成于 $(date '+%Y-%m-%d %H:%M:%S')。"
echo "# 每个字段都是独立随机值。粘进 .env 后 chmod 600，不要提交、不要贴进聊天或工单。"
echo

for k in "${passwords[@]}"; do
  printf '%s=%s\n' "$k" "$(openssl rand -hex 24)"
done
echo
for k in "${plain[@]}"; do
  printf '%s=%s\n' "$k" "$(openssl rand -hex 32)"
done
echo
for k in "${keyed[@]}"; do
  printf '%s=k1:%s\n' "$k" "$(openssl rand -hex 32)"
done
echo
# 线路签名密钥要同时下发给 CDN 边缘，引用名用得上语义，所以单独给
printf 'LINE_SIGN_KEYS=cdn1:%s\n' "$(openssl rand -hex 32)"
echo
echo "# 还要自己填：数据库连接串里的口令（用上面 XSITE_APP_PASSWORD / XSITE_RO_PASSWORD 的值）、"
echo "# VALKEY_URL / APPS_VALKEY_URL 里的口令、S3_* 存储凭据、域名与 CERTBOT_EMAIL。"
