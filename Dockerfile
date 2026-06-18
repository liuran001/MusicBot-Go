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
COPY plugins/netease/recognize/embindlib/go.mod plugins/netease/recognize/embindlib/go.sum ./plugins/netease/recognize/embindlib/
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

# ffmpeg-builder：安装完整 ffmpeg，再用 ldd 递归解析出 ffmpeg/ffprobe 运行时真正
# 需要的二进制 + 共享库（约 19MB），剔除整包 apk 带来的编解码器/滤镜依赖（x265、
# AV1、vpx、OpenEXR、SPIRV 等，~118MB）。/recognize 只需把音频解码为 PCM。
FROM alpine:${ALPINE_TAG} AS ffmpeg-builder
RUN apk add --no-cache ffmpeg
# 把 ffmpeg/ffprobe 及其全部（含传递）共享库依赖镜像式收集到 /deps，保持原始路径。
# 用工作队列递归遍历 ldd 输出：每个 token 以 "/" 开头的都是实际会被加载的对象
# （已解析的 .so 绝对路径，以及 musl loader /lib/ld-musl-*.so.1），逐个解析直到
# 没有新依赖，确保一个 .so 都不漏。cp -L 解引用 symlink，落地为 SONAME 同名实体。
RUN set -eu; \
    mkdir -p /deps/usr/bin; \
    cp /usr/bin/ffmpeg /usr/bin/ffprobe /deps/usr/bin/; \
    : > /tmp/libs.list; \
    queue="/usr/bin/ffmpeg /usr/bin/ffprobe"; \
    while [ -n "$queue" ]; do \
      next=""; \
      for f in $queue; do \
        for lib in $(ldd "$f" 2>/dev/null | awk '{for (i=1;i<=NF;i++) if ($i ~ /^\//) print $i}'); do \
          if [ -e "$lib" ] && ! grep -qxF "$lib" /tmp/libs.list; then \
            echo "$lib" >> /tmp/libs.list; \
            next="$next $lib"; \
          fi; \
        done; \
      done; \
      queue="$next"; \
    done; \
    while read -r lib; do \
      mkdir -p "/deps$(dirname "$lib")"; \
      cp -L "$lib" "/deps$lib"; \
    done < /tmp/libs.list; \
    echo "=== collected shared libraries ==="; cat /tmp/libs.list; \
    du -sh /deps

# 单一镜像（原 lite/full 已合并）。命名为 full 以兼容 docker-compose.yml 的
# `target: full`。精简后体积接近原 lite，但内置 ffmpeg + afp.wasm 听歌识曲。
FROM alpine:${ALPINE_TAG} AS full
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
LABEL org.opencontainers.image.description="MusicBot-Go：含 ffmpeg（精简）+ afp.wasm 听歌识曲，及 Apple Music 支持"
RUN apk add --no-cache ca-certificates tzdata
# ffmpeg/ffprobe 二进制 + 仅运行时所需的共享库（来自 ffmpeg-builder 的 ldd 精简），
# 镜像式还原到根路径（/usr/bin、/usr/lib、/lib）。/recognize 解码音频为 PCM 用。
COPY --from=ffmpeg-builder /deps/ /
WORKDIR /app
COPY --from=builder /out/MusicBot-Go /app/MusicBot-Go
COPY config_example.ini /app/config_example.ini
# afp.wasm 听歌识曲指纹编码器（由 bot 通过 wazero 在进程内执行，纯 Go，无 Node.js）。
COPY plugins/netease/recognize/wasm/afp.wasm /app/plugins/netease/recognize/wasm/afp.wasm
RUN mkdir -p /app/workdir

# Apple Music：AAC 256k 由 bot 内置原生解密（零配置）。无损 / Hi-Res / Atmos
# 需要 FairPlay wrapper，它作为独立服务运行（见 docker-compose.yml）——需要
# --privileged + 安卓 userland 以及它自己的 Apple ID 登录，因此特意不打包进本镜像。
# 在 config.ini 中用 `wrapper_host = wrapper` 让 bot 指向它。
ENTRYPOINT ["/app/MusicBot-Go"]
CMD ["-c", "/app/config.ini"]

