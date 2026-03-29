FROM golang:1.26-bookworm AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 go build -trimpath -o /out/MusicBot-Go .

FROM node:20-bookworm-slim AS runtime

ENV DEBIAN_FRONTEND=noninteractive

RUN apt-get update \
    && apt-get install -y --no-install-recommends ffmpeg ca-certificates tzdata \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app

COPY --from=builder /out/MusicBot-Go /app/MusicBot-Go
COPY config_example.ini /app/config_example.ini
COPY plugins /app/plugins

RUN cd /app/plugins/netease/recognize/service && npm ci --omit=dev

RUN mkdir -p /app/workdir

ENTRYPOINT ["/app/MusicBot-Go"]
CMD ["-c", "/app/config.ini"]
