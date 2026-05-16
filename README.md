# log-streamer

Stream command output to a private server. Retrieve or delete logs later using a secure token.

## Usage

### Client

```bash
# Run a command, stream its output to the server
log-streamer-client run make build
# prints: log-streamer token: <64-char-hex>

# Pipe output to the server
./long-running-job.sh | log-streamer-client send
# prints: log-streamer token: <64-char-hex>

# Retrieve logs
log-streamer-client fetch <token>

# Delete logs
log-streamer-client delete <token>
```

The `run` command tees stdout/stderr locally while streaming to the server. The `send` command does the same for piped stdin.

### Server

```bash
# Run directly
LOG_STREAMER_ADDR=:8080 LOG_STREAMER_DATA_DIR=/var/log/streams log-streamer-server

# Run via Docker
docker build -t log-streamer-server .
docker run -p 8080:8080 -v /data/logs:/data/logs log-streamer-server
```

## Configuration

### Server (environment variables)

| Variable | Default | Description |
|----------|---------|-------------|
| `LOG_STREAMER_ADDR` | `:8080` | Listen address |
| `LOG_STREAMER_DATA_DIR` | `./data` | Storage directory for log files |

### Client

| Variable | Default | Description |
|----------|---------|-------------|
| `LOG_STREAMER_SERVER` | `ws://localhost:8080` | Server WebSocket URL |

The `--server` flag overrides the environment variable.

## Protocol

- **Stream**: WebSocket at `/api/stream`. Server sends token as first message, client streams JSON log lines.
- **Fetch**: `GET /api/logs/{token}` returns all log lines as JSON.
- **Delete**: `DELETE /api/logs/{token}` removes the log.

## Storage

Each stream is stored as a JSONL file (`<token>.jsonl`) in the data directory. Each line:

```json
{"ts":"2026-05-16T12:00:00Z","line":"output text","stream":"stdout"}
```

## Building

```bash
go-toolchain
```
