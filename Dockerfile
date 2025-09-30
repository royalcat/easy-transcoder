FROM golang:1.25 AS build
WORKDIR /app

COPY go.mod go.sum ./
RUN --mount=type=cache,mode=0777,target=/go/pkg/mod \
    go mod download


COPY ./cmd ./cmd
COPY ./assets ./assets
COPY ./internal ./internal
COPY ./ui ./ui
COPY ./templui ./templui

# Build with cache
RUN --mount=type=cache,mode=0777,target=/go/pkg/mod \
    CGO_ENABLED=0 go build -o /easy-transcoder ./cmd/easy-transcoder

FROM linuxserver/ffmpeg:7.1.1

WORKDIR /app

COPY --from=build /easy-transcoder /app/easy-transcoder

EXPOSE 8080
VOLUME /app/media

ENTRYPOINT ["/app/easy-transcoder"]
