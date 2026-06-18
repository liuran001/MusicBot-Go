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

# ffmpeg-builder：从源码编译最小化 ffmpeg，仅启用 bot 实际用到的编解码/复用/滤镜，
# 把 ffmpeg 从整包 apk 的 ~118MB 砍到 ~4MB（二进制静态链接 libav*/libsw*，运行时
# 仅依赖 libmp3lame.so）。启用项经逐条核对 bot 全部 ffmpeg/ffprobe 调用得出：
#   - 解码: flac/alac/aac/mp3/opus/vorbis + 各 PCM（识曲解码、soda 校验/重封装）
#   - 编码: flac（soda 无损重写）、libmp3lame（识曲转 MP3）、pcm_f32le（识曲 f32le）、
#           pcm_s16le（-f null 校验默认重编码）
#   - 复用: flac / mov+ipod（m4a 重封装）/ mp3 / pcm_f32le（注意 -f f32le 的 configure
#           组件名是 pcm_f32le 而非 f32le）/ null
#   - 滤镜: aresample/aformat/anull/abuffer/abuffersink（-ac/-ar/编码格式转换会自动插入）
#   - bsf: aac_adtstoasc（ADTS AAC 重封装进 mp4/m4a 必需）
FROM alpine:${ALPINE_TAG} AS ffmpeg-builder
ARG HTTP_PROXY
ARG HTTPS_PROXY
ARG ALL_PROXY
ARG NO_PROXY
ARG http_proxy
ARG https_proxy
ARG all_proxy
ARG no_proxy
ARG FFMPEG_VERSION=7.1.1
ENV HTTP_PROXY=${HTTP_PROXY} \
    HTTPS_PROXY=${HTTPS_PROXY} \
    ALL_PROXY=${ALL_PROXY} \
    NO_PROXY=${NO_PROXY} \
    http_proxy=${http_proxy} \
    https_proxy=${https_proxy} \
    all_proxy=${all_proxy} \
    no_proxy=${no_proxy}
RUN apk add --no-cache build-base nasm yasm pkgconf lame-dev zlib-dev \
    coreutils curl ca-certificates tar xz
WORKDIR /src
RUN curl -fsSL "https://ffmpeg.org/releases/ffmpeg-${FFMPEG_VERSION}.tar.xz" -o ffmpeg.tar.xz \
    && tar xf ffmpeg.tar.xz && mv "ffmpeg-${FFMPEG_VERSION}" ffmpeg
WORKDIR /src/ffmpeg
RUN ./configure \
      --prefix=/usr/local \
      --disable-everything \
      --disable-doc --disable-htmlpages --disable-manpages --disable-txtpages \
      --disable-network --disable-autodetect --disable-debug \
      --disable-avdevice --disable-swscale --disable-postproc \
      --enable-small \
      --disable-programs --enable-ffmpeg --enable-ffprobe \
      --enable-libmp3lame \
      --enable-decoder=flac,alac,aac,aac_latm,mp3,mp3float,opus,vorbis,pcm_s16le,pcm_s16be,pcm_f32le,pcm_s24le,pcm_u8 \
      --enable-encoder=flac,libmp3lame,pcm_s16le,pcm_f32le \
      --enable-muxer=flac,mov,ipod,mp3,pcm_f32le,null \
      --enable-demuxer=flac,mov,matroska,aac,mp3,ogg,wav,pcm_s16le \
      --enable-parser=flac,aac,opus,vorbis,mpegaudio \
      --enable-bsf=aac_adtstoasc \
      --enable-filter=aresample,aformat,anull,abuffer,abuffersink \
      --enable-protocol=file,pipe \
    && make -j"$(nproc)" && make install
RUN strip /usr/local/bin/ffmpeg /usr/local/bin/ffprobe

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
# 源码编译的最小 ffmpeg/ffprobe（静态链接 libav*/libsw*），运行时仅依赖 libmp3lame.so。
# musl libc 由 alpine base 提供。/recognize 解码音频为 PCM、soda 无损重写/重封装均用它。
COPY --from=ffmpeg-builder /usr/local/bin/ffmpeg /usr/local/bin/ffmpeg
COPY --from=ffmpeg-builder /usr/local/bin/ffprobe /usr/local/bin/ffprobe
COPY --from=ffmpeg-builder /usr/lib/libmp3lame.so.0 /usr/lib/libmp3lame.so.0
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

