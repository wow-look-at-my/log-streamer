FROM golang:1.24-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /bin/log-streamer-server ./cmd/log-streamer-server

FROM alpine:3.20
RUN apk add --no-cache ca-certificates \
 && adduser -D -H -u 10001 streamer \
 && mkdir -p /data/logs \
 && chown streamer:streamer /data/logs
COPY --from=builder /bin/log-streamer-server /usr/local/bin/
ENV LOG_STREAMER_DATA_DIR=/data/logs
ENV LOG_STREAMER_ADDR=:8080
# Run as an unprivileged user. If you bind-mount a host directory over
# /data/logs, ensure it is writable by uid 10001.
USER streamer
EXPOSE 8080
VOLUME /data/logs
ENTRYPOINT ["log-streamer-server"]
