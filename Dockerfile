FROM golang:1.26-bookworm AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 go build -trimpath -o /out/MusicBot-Go .

FROM node:20-bookworm AS npm-builder

RUN apt-get update \
    && apt-get install -y --no-install-recommends python3 make g++ git \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /build
COPY plugins/netease/recognize/service/package*.json ./
RUN npm install --omit=dev

FROM node:20-bookworm-slim AS runtime

ENV DEBIAN_FRONTEND=noninteractive

RUN apt-get update \
    && apt-get install -y --no-install-recommends ffmpeg ca-certificates tzdata \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app

COPY --from=builder /out/MusicBot-Go /app/MusicBot-Go
COPY config_example.ini /app/config_example.ini
COPY plugins /app/plugins
COPY --from=npm-builder /build/node_modules /app/plugins/netease/recognize/service/node_modules

RUN mkdir -p /app/workdir

ENTRYPOINT ["/app/MusicBot-Go"]
CMD ["-c", "/app/config.ini"]
