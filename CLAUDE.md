# log-streamer

Go project with two binaries: `log-streamer-client` and `log-streamer-server`.

## Build

```bash
go-toolchain
```

Binaries are output to `build/`.

## Project structure

- `cmd/log-streamer-client/` - CLI client (cobra, self-registering subcommands)
- `cmd/log-streamer-server/` - Server binary
- `internal/protocol/` - Shared JSON control types + binary wire frame codec (`wire.go`)
- `internal/token/` - Token generation and validation
- `internal/storage/` - File-based binary record storage (`<token>.bin`), per-stream/total byte caps, TTL sweep
- `internal/server/` - HTTP/WebSocket server (binary stream ingest, limits, graceful shutdown)

## Design notes

- The server is public/unauthenticated by design. The per-stream 256-bit token is the only credential to fetch/delete; tokens are server-generated, so clients cannot choose a storage path or touch other streams.
- Log data flows as WebSocket **binary** frames (`[stream:1][ts:8 BE unix-nanos][payload]`); `hello`/`ack`/`error` are JSON text frames. Payloads are raw bytes, chunked, so arbitrarily long lines stream with bounded memory; lines are reconstructed on fetch by splitting on `\n`.
- Abuse is bounded by `LOG_STREAMER_MAX_STREAM_BYTES`, `LOG_STREAMER_MAX_TOTAL_BYTES`, `LOG_STREAMER_TTL`, plus a 1 MiB inbound frame cap. See README for all env vars.

## Testing

All tests run via `go-toolchain`. Integration tests use `httptest` + real WebSocket connections.

## Docker

Server runs in Docker. Data persists in `/data/logs` volume.
