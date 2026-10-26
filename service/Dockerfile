# 生产镜像：多阶段构建，静态二进制 + distroless，非 root 运行（docs/03 第 0.3 节）。
# 构建：docker build -t xsite/service:<版本> service/
FROM golang:1.27.3-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download && go mod verify
COPY . .
ENV CGO_ENABLED=0
# 有 cmd/api/default.pgo 时 go build 会自动启用 PGO
RUN go build -trimpath -ldflags="-s -w" -o /out/api ./cmd/api \
 && go build -trimpath -ldflags="-s -w" -o /out/worker ./cmd/worker \
 && go build -trimpath -ldflags="-s -w" -o /out/admintool ./cmd/admintool

# 转封装 worker（S1-5）：要调用 FFmpeg（libx264、libwebp），不能用 distroless static。
# 构建：docker build --target media-worker -t xsite/media-worker:<版本> service/
# 运行时设 MEDIA_TRANSCODE_CONCURRENCY > 0；通用 worker（下面的默认镜像）设为 0，不消费 media 队列。
FROM debian:bookworm-slim AS media-worker
RUN apt-get update \
 && apt-get install -y --no-install-recommends ffmpeg ca-certificates \
 && rm -rf /var/lib/apt/lists/* \
 && useradd --system --uid 65532 --no-create-home --shell /usr/sbin/nologin nonroot
COPY --from=build /out/worker /app/worker
USER 65532:65532
ENV MEDIA_WORK_DIR=/tmp/xsite-transcode
ENTRYPOINT ["/app/worker"]

# 默认目标（最后一个阶段）：api、通用 worker 与 admintool
FROM gcr.io/distroless/static-debian12:nonroot
# admintool 用于创建首个超级管理员：docker run -i --entrypoint /app/admintool <镜像> create-admin ...
COPY --from=build /out/api /out/worker /out/admintool /app/
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/app/api"]
