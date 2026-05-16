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
- `internal/protocol/` - Shared message types
- `internal/token/` - Token generation and validation
- `internal/storage/` - File-based JSONL storage
- `internal/server/` - HTTP/WebSocket server

## Testing

All tests run via `go-toolchain`. Integration tests use `httptest` + real WebSocket connections.

## Docker

Server runs in Docker. Data persists in `/data/logs` volume.
