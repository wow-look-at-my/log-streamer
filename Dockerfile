FROM golang:1.24-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /bin/log-streamer-server ./cmd/log-streamer-server

FROM alpine:3.20
RUN apk add --no-cache ca-certificates
COPY --from=builder /bin/log-streamer-server /usr/local/bin/
RUN mkdir -p /data/logs
ENV LOG_STREAMER_DATA_DIR=/data/logs
ENV LOG_STREAMER_ADDR=:8080
EXPOSE 8080
VOLUME /data/logs
ENTRYPOINT ["log-streamer-server"]
