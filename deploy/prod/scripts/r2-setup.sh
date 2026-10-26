#!/usr/bin/env bash
# R2 桶的一次性设置：CORS 与生命周期。换域名或加域名后重跑一次。
#
#   用法: r2-setup.sh
#   环境: R2_ENDPOINT R2_BUCKET AWS_ACCESS_KEY_ID AWS_SECRET_ACCESS_KEY
#         APPS_DOMAIN ADMIN_DOMAIN FEEDBACK_DOMAIN [APPS_ALIASES] [CDN_DOMAIN]
#
# 为什么必须配 CORS：三个前端的入口脚本都是 <script type="module">，而模块脚本**一律**按 CORS
# 模式取（这是规范行为，不是可选项）。R2 不回 Access-Control-Allow-Origin 的话，
# 页面在控制台报跨域、整个应用一行都跑不起来。字体同理。
#
# 生命周期规则：发布时不删远端旧产物（见 cdn-upload.sh 的说明），所以要让 R2 自己过期清理。
# 90 天远大于「还有用户的页面在引用上一版 chunk」的时间窗。
set -euo pipefail

: "${R2_ENDPOINT:?必须设置 R2_ENDPOINT}"
: "${R2_BUCKET:?必须设置 R2_BUCKET}"
: "${AWS_ACCESS_KEY_ID:?必须设置 AWS_ACCESS_KEY_ID}"
: "${AWS_SECRET_ACCESS_KEY:?必须设置 AWS_SECRET_ACCESS_KEY}"
: "${APPS_DOMAIN:?必须设置 APPS_DOMAIN}"
: "${ADMIN_DOMAIN:?必须设置 ADMIN_DOMAIN}"
: "${FEEDBACK_DOMAIN:?必须设置 FEEDBACK_DOMAIN}"
export AWS_DEFAULT_REGION=${AWS_DEFAULT_REGION:-auto}

origins=("https://$APPS_DOMAIN" "https://$ADMIN_DOMAIN" "https://$FEEDBACK_DOMAIN")
for alias in ${APPS_ALIASES:-}; do
  origins+=("https://$alias")
done
# CDN 域名本身也留着：这个桶同时放媒体资源（img/、video/ 前缀），
# 后台填的完整图片地址可能就指向 CDN 域名，那种请求的 Origin 是 CDN 自己。
# put-bucket-cors 是整体替换，不是追加 —— 漏掉任何一个来源都是直接把它拒掉。
if [[ -n ${CDN_DOMAIN:-} ]]; then
  origins+=("https://$CDN_DOMAIN")
fi

origin_json=$(printf '"%s",' "${origins[@]}")
origin_json="[${origin_json%,}]"

cors=$(cat <<JSON
{
  "CORSRules": [
    {
      "AllowedOrigins": $origin_json,
      "AllowedMethods": ["GET", "HEAD"],
      "AllowedHeaders": ["*"],
      "ExposeHeaders": ["Content-Length", "Content-Type", "ETag", "Accept-Ranges", "Content-Range"],
      "MaxAgeSeconds": 86400
    }
  ]
}
JSON
)

lifecycle=$(cat <<'JSON'
{
  "Rules": [
    { "ID": "expire-apps-assets",     "Status": "Enabled", "Filter": { "Prefix": "apps/" },     "Expiration": { "Days": 90 } },
    { "ID": "expire-admin-assets",    "Status": "Enabled", "Filter": { "Prefix": "admin/" },    "Expiration": { "Days": 90 } },
    { "ID": "expire-feedback-assets", "Status": "Enabled", "Filter": { "Prefix": "feedback/" }, "Expiration": { "Days": 90 } },
    { "ID": "abort-incomplete-uploads", "Status": "Enabled", "Filter": { "Prefix": "" },
      "AbortIncompleteMultipartUpload": { "DaysAfterInitiation": 1 } }
  ]
}
JSON
)

echo "R2：设置 CORS（允许来源 ${origins[*]}）"
aws --endpoint-url "$R2_ENDPOINT" s3api put-bucket-cors \
  --bucket "$R2_BUCKET" --cors-configuration "$cors"

echo "R2：设置生命周期（前端产物保留 90 天）"
aws --endpoint-url "$R2_ENDPOINT" s3api put-bucket-lifecycle-configuration \
  --bucket "$R2_BUCKET" --lifecycle-configuration "$lifecycle"

echo "R2：当前 CORS"
aws --endpoint-url "$R2_ENDPOINT" s3api get-bucket-cors --bucket "$R2_BUCKET"
