# syntax=docker/dockerfile:1.7

ARG GO_VERSION=1.26
ARG BOOKWORM_TAG=bookworm
ARG ALPINE_TAG=3.20

FROM --platform=$BUILDPLATFORM golang:${GO_VERSION}-${BOOKWORM_TAG} AS builder
WORKDIR /src

ARG HTTP_PROXY
ARG HTTPS_PROXY
ARG ALL_PROXY
ARG NO_PROXY
ARG http_proxy
ARG https_proxy
ARG all_proxy
ARG no_proxy
ARG GOPROXY=https://proxy.golang.org,direct
ARG GOSUMDB=sum.golang.org
ENV HTTP_PROXY=${HTTP_PROXY} \
    HTTPS_PROXY=${HTTPS_PROXY} \
    ALL_PROXY=${ALL_PROXY} \
    NO_PROXY=${NO_PROXY} \
    http_proxy=${http_proxy} \
    https_proxy=${https_proxy} \
    all_proxy=${all_proxy} \
    no_proxy=${no_proxy} \
    GOPROXY=${GOPROXY} \
    GOSUMDB=${GOSUMDB}

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
ARG COMMIT_SHA=unknown
ARG BUILD_TIME=unknown
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath \
      -ldflags "-s -w -X main.versionName=${VERSION} -X main.commitSHA=${COMMIT_SHA} -X main.buildTime=${BUILD_TIME}" \
      -o /out/MusicBot-Go .

FROM alpine:${ALPINE_TAG} AS runtime-base
ARG HTTP_PROXY
ARG HTTPS_PROXY
ARG ALL_PROXY
ARG NO_PROXY
ARG http_proxy
ARG https_proxy
ARG all_proxy
ARG no_proxy
ENV HTTP_PROXY=${HTTP_PROXY} \
    HTTPS_PROXY=${HTTPS_PROXY} \
    ALL_PROXY=${ALL_PROXY} \
    NO_PROXY=${NO_PROXY} \
    http_proxy=${http_proxy} \
    https_proxy=${https_proxy} \
    all_proxy=${all_proxy} \
    no_proxy=${no_proxy}
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
COPY --from=builder /out/MusicBot-Go /app/MusicBot-Go
COPY config_example.ini /app/config_example.ini
RUN mkdir -p /app/workdir
ENTRYPOINT ["/app/MusicBot-Go"]
CMD ["-c", "/app/config.ini"]

FROM runtime-base AS lite
LABEL org.opencontainers.image.description="MusicBot-Go lightweight image without /recognize dependencies"

FROM runtime-base AS full
LABEL org.opencontainers.image.description="MusicBot-Go full image with /recognize dependencies and Apple Music support"
# ffmpeg：/recognize 听歌识曲解码音频为 PCM。指纹编码已由内置 afp.wasm（纯 Go
# wazero 驱动）完成，不再需要 Node.js 运行时。
RUN apk add --no-cache ffmpeg
# afp.wasm 听歌识曲指纹编码器（由 bot 通过 wazero 在进程内执行）。
COPY plugins/netease/recognize/wasm/afp.wasm /app/plugins/netease/recognize/wasm/afp.wasm

# Apple Music：AAC 256k 由 bot 内置原生解密（零配置）。无损 / Hi-Res / Atmos
# 需要 FairPlay wrapper，它作为独立服务运行（见 docker-compose.yml）——需要
# --privileged + 安卓 userland 以及它自己的 Apple ID 登录，因此特意不打包进本镜像。
# 在 config.ini 中用 `wrapper_host = wrapper` 让 bot 指向它。
CMD ["-c", "/app/config.ini"]

