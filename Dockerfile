# syntax=docker/dockerfile:1.7

ARG GO_VERSION=1.26
ARG NODE_VERSION=20
ARG BOOKWORM_TAG=bookworm
ARG FFMPEG_AMD64_ASSET=ffmpeg-master-latest-linux64-gpl-shared.tar.xz
ARG FFMPEG_ARM64_ASSET=ffmpeg-master-latest-linuxarm64-gpl-shared.tar.xz
ARG FFMPEG_AMD64_SHA256=204d05ca9bf655dec5970a1176f17413b532d2aa3294bf14b4fc70ab0981008d
ARG FFMPEG_ARM64_SHA256=375520f90433e9bef2ac2958b2c9afddd1ff65e25ce905f729d9dfdde7e20eb7

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

FROM --platform=$BUILDPLATFORM node:${NODE_VERSION}-${BOOKWORM_TAG} AS npm-builder
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
RUN apt-get update \
    && apt-get install -y --no-install-recommends python3 make g++ git \
    && rm -rf /var/lib/apt/lists/*
WORKDIR /build
COPY plugins/netease/recognize/service/package*.json ./
RUN npm install --omit=dev \
    && npm cache clean --force

FROM node:${NODE_VERSION}-${BOOKWORM_TAG}-slim AS node-runtime

FROM --platform=$TARGETPLATFORM debian:${BOOKWORM_TAG}-slim AS ffmpeg-runtime
ARG FFMPEG_AMD64_ASSET
ARG FFMPEG_ARM64_ASSET
ARG FFMPEG_AMD64_SHA256
ARG FFMPEG_ARM64_SHA256
ARG TARGETARCH
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates curl xz-utils \
    && rm -rf /var/lib/apt/lists/*
WORKDIR /tmp
RUN case "$TARGETARCH" in \
      amd64) asset="$FFMPEG_AMD64_ASSET"; sha="$FFMPEG_AMD64_SHA256" ;; \
      arm64) asset="$FFMPEG_ARM64_ASSET"; sha="$FFMPEG_ARM64_SHA256" ;; \
      *) echo "unsupported arch: $TARGETARCH"; exit 1 ;; \
    esac \
    && curl -L --fail --retry 3 -o ffmpeg.tar.xz "https://github.com/BtbN/FFmpeg-Builds/releases/latest/download/${asset}" \
    && echo "${sha}  ffmpeg.tar.xz" | sha256sum -c - \
    && mkdir out \
    && tar -xJf ffmpeg.tar.xz -C out --strip-components=1 \
    && test -x out/bin/ffmpeg \
    && test -x out/bin/ffprobe

FROM debian:${BOOKWORM_TAG}-slim AS runtime-base
ARG HTTP_PROXY
ARG HTTPS_PROXY
ARG ALL_PROXY
ARG NO_PROXY
ARG http_proxy
ARG https_proxy
ARG all_proxy
ARG no_proxy
ENV DEBIAN_FRONTEND=noninteractive \
    HTTP_PROXY=${HTTP_PROXY} \
    HTTPS_PROXY=${HTTPS_PROXY} \
    ALL_PROXY=${ALL_PROXY} \
    NO_PROXY=${NO_PROXY} \
    http_proxy=${http_proxy} \
    https_proxy=${https_proxy} \
    all_proxy=${all_proxy} \
    no_proxy=${no_proxy}
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates tzdata \
    && rm -rf /var/lib/apt/lists/*
WORKDIR /app
COPY --from=builder /out/MusicBot-Go /app/MusicBot-Go
COPY config_example.ini /app/config_example.ini
RUN mkdir -p /app/workdir
ENTRYPOINT ["/app/MusicBot-Go"]
CMD ["-c", "/app/config.ini"]

FROM runtime-base AS lite
LABEL org.opencontainers.image.description="MusicBot-Go lightweight image without /recognize dependencies"

FROM runtime-base AS full
LABEL org.opencontainers.image.description="MusicBot-Go full image with /recognize dependencies"
COPY --from=node-runtime /usr/local/bin/node /usr/local/bin/node
COPY --from=node-runtime /usr/local/lib/node_modules /usr/local/lib/node_modules
COPY --from=node-runtime /usr/local/include/node /usr/local/include/node
COPY --from=node-runtime /usr/local/share /usr/local/share
COPY --from=ffmpeg-runtime /tmp/out/bin/ffmpeg /usr/local/bin/ffmpeg
COPY --from=ffmpeg-runtime /tmp/out/bin/ffprobe /usr/local/bin/ffprobe
COPY --from=ffmpeg-runtime /tmp/out/lib /usr/local/lib
COPY plugins/netease/recognize /app/plugins/netease/recognize
COPY --from=npm-builder /build/node_modules /app/plugins/netease/recognize/service/node_modules
