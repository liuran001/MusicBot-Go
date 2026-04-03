# syntax=docker/dockerfile:1.7

ARG GO_VERSION=1.26
ARG NODE_VERSION=20
ARG BOOKWORM_TAG=bookworm

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
RUN apt-get update \
    && apt-get install -y --no-install-recommends ffmpeg \
    && rm -rf /var/lib/apt/lists/*
COPY --from=node-runtime /usr/local/bin/node /usr/local/bin/node
COPY --from=node-runtime /usr/local/lib/node_modules /usr/local/lib/node_modules
COPY --from=node-runtime /usr/local/include/node /usr/local/include/node
COPY --from=node-runtime /usr/local/share /usr/local/share
COPY plugins/netease/recognize /app/plugins/netease/recognize
COPY --from=npm-builder /build/node_modules /app/plugins/netease/recognize/service/node_modules
