FROM golang:1.24 AS build
WORKDIR /app

COPY go.mod ./
COPY go.sum ./
RUN go mod download


COPY ./cmd ./cmd
COPY ./assets ./assets
COPY ./internal ./internal
COPY ./ui ./ui

RUN CGO_ENABLED=0 go build -o /easy-transcoder ./cmd/easy-transcoder


FROM linuxserver/ffmpeg:7.1.1

WORKDIR /app

COPY --from=build /easy-transcoder /app/easy-transcoder

EXPOSE 8080
VOLUME /app/media

ENTRYPOINT ["/app/easy-transcoder"]
