# syntax=docker/dockerfile:1.7

ARG GO_VERSION=1.26
ARG NODE_VERSION=20
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

FROM --platform=$BUILDPLATFORM node:${NODE_VERSION}-alpine AS npm-builder
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
RUN apk add --no-cache python3 make g++ git
WORKDIR /build
COPY plugins/netease/recognize/service/package*.json ./
RUN npm install --omit=dev \
    && npm cache clean --force

FROM node:${NODE_VERSION}-alpine AS node-runtime

FROM alpine:3.20 AS runtime-base
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
# ffmpeg：音频转码；libstdc++/libgcc：从 node:alpine 拷出的 node 二进制运行时依赖。
RUN apk add --no-cache ffmpeg libstdc++ libgcc
COPY --from=node-runtime /usr/local/bin/node /usr/local/bin/node
COPY --from=node-runtime /usr/local/lib/node_modules /usr/local/lib/node_modules
COPY plugins/netease/recognize /app/plugins/netease/recognize
COPY --from=npm-builder /build/node_modules /app/plugins/netease/recognize/service/node_modules

# Apple Music：AAC 256k 由 bot 内置原生解密（零配置）。无损 / Hi-Res / Atmos
# 需要 FairPlay wrapper，它作为独立服务运行（见 docker-compose.yml）——需要
# --privileged + 安卓 userland 以及它自己的 Apple ID 登录，因此特意不打包进本镜像。
# 在 config.ini 中用 `wrapper_host = wrapper` 让 bot 指向它。
CMD ["-c", "/app/config.ini"]

