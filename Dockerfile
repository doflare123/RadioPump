# Сборка Go остаётся внутри Docker; приложение использует pure-Go SQLite.
FROM golang:1.26-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 go build -trimpath -o /radiopump ./cmd/server

# Отдельная цель проверяет Linux, реальные codecs и гонки; в runtime не попадает.
FROM build AS test
RUN apt-get update && apt-get install -y --no-install-recommends ffmpeg \
    && rm -rf /var/lib/apt/lists/*
RUN go test -race ./... -count=1 && go vet ./...

# FFmpeg и wget нужны для эфира и проверки HTTP внутри контейнера.
FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ffmpeg ca-certificates wget \
    && rm -rf /var/lib/apt/lists/* \
    && useradd --uid 10001 --create-home radiopump
WORKDIR /app
COPY --from=build /radiopump /usr/local/bin/radiopump
COPY web ./web
COPY config.example.yaml ./config.yaml
RUN mkdir music data && chown -R 10001:10001 music data
USER 10001:10001
EXPOSE 8080
ENTRYPOINT ["radiopump"]
