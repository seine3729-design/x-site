# S1-5b MPEG-TS HLS 转封装 Profile

状态：已完成
任务：`docs/11` S1-5b。依赖 S1-5（转封装）、S1-6f（主存储与副本）、S1-7/S1-8（选源、清单与密钥）。

## 目标

新上传的分集和广告 MP4（以及既有支持的 MOV/WebM/MKV 源）由 FFmpeg 转为 HLS MPEG-TS：主清单 + 多档媒体清单 + `.ts` 分片。视频统一 H.264，音频统一 AAC-LC，固定 4 秒切片，并通过现有存储源 / CDN 线路与播放器 ABR 覆盖不同网络速度。

## 范围

| 项目 | 约定 |
| --- | --- |
| 封装 | 新产物为 MPEG-TS HLS；`master.m3u8` 与 `v{短边}.m3u8` 位于产物目录顶层，分片为 `s/{SHA-256 前 32 位}.ts` |
| 切片 | 固定 4 秒；强制关键帧、关闭场景切换插帧、闭 GOP，x264 重复 SPS/PPS，保证每片可独立起播 |
| 清晰度 | 360p（400 kbps）/ 480p（850 kbps）/ 720p（1.6 Mbps）/ 1080p（3.0 Mbps）；按源短边输出、不放大，低于 360p 的源只输出源尺寸一档 |
| 编码 | H.264（x264 兼容档，主清单从实际 SPS 读取 profile/level）+ AAC-LC 96 kbps；源无音轨时补双声道静音 AAC，单声道源保持单声道 |
| 加密 | Go 在 FFmpeg 之后逐 `.ts` 分片作 AES-128-CBC 加密，每片随机 IV；内容密钥只在 Go 内存、`media_key` 与密钥接口中存在 |
| 分发 | worker 只写 `storage_settings` 指定的视频主 `storage_provider`；`storage_line` / CDN 只回源、签名、缓存和分发，不接收浏览器写入凭据 |
| 兼容 | 播放清单解析器同时接受历史 fMP4 的 `EXT-X-MAP` + `.m4s` 与新 `.ts`；已有分集不批量重转、不中断播放 |

范围外：接入某一家 CDN 的媒资直推 API、存量全量重转、独立 APP 回环代理实现、HEVC/AV1、DRM。

## 数据流

```text
后台上传源视频 → 视频主存储 video/source/ → 私有 ingest/ 暂存
  → River media_transcode → FFmpeg 多档 MPEG-TS HLS → Go 加密并哈希命名
  → 主存储 video/episodes/{episode_id}/{job_id}/ → media_replica 异步复制
  → CDN/线路回源并缓存 .ts → /api/v1/hls 清单改写 → hls.js ABR
```

新格式不改变 `media_object`、`media_key`、`transcode_job` 或 `/api/v1` 路径。每次转封装仍写新的任务目录，成功后原子切换 `episode.video_object_id`；失败时旧 ready 产物仍可播放。

## 验收

- `transcodeArgs` 明确使用 `-hls_segment_type mpegts`、4 秒、`.ts` 文件名，不输出 fMP4 init 段。
- 真实 FFmpeg 流水线生成 360p/480p/720p TS 清单；用内容密钥解密后 FFprobe 可完整解码。
- 新 TS 和历史 fMP4 清单都可经过 `/api/v1/hls` 白名单解析、签名改写与试看裁剪。
- 跨存储源复制 `.ts` 时保留 `video/mp2t` 与不可变缓存头；预热按新清单只拉实际请求的前 N 个 `.ts`。
- Web 继续从主清单读取档位、`startLevel=0` 起播并由 hls.js 自动升降档；网络错误沿既有线路候选续播。

## 验证命令

```bash
./dev.sh go test ./internal/transcode -count=1
./dev.sh go test ./internal/domain/play ./internal/domain/resourcewarm -count=1
./dev.sh go test ./internal/store/blob/s3probe ./internal/jobs -count=1
```
