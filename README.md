# log-streamer

Stream command output to a server and retrieve or delete it later using a token.

The server is intentionally **public / unauthenticated**: anyone who can reach it
may open a stream. The token returned for each stream is a 256-bit random value
and is the only credential needed to fetch or delete that stream's logs, so treat
it as a secret. The server only assigns where data is stored (one file per
server-generated token); a client can never choose a path or touch another
stream's data.

## Usage

### Client

```bash
# Run a command, stream its output (stdout + stderr) to the server
log-streamer-client run make build
# prints to stderr: log-streamer token: <64-char-hex>

# Pipe output to the server
./long-running-job.sh | log-streamer-client send
# prints to stderr: log-streamer token: <64-char-hex>

# Retrieve logs
log-streamer-client fetch <token>

# Retrieve logs verbatim (do not escape terminal control sequences)
log-streamer-client fetch --raw <token>

# Delete logs
log-streamer-client delete <token>
```

`run` tees stdout/stderr locally while streaming to the server; `send` does the
same for piped stdin. Lines of any length are supported — output is streamed in
fixed-size chunks, so a single multi-megabyte line costs no more memory than many
small ones, and line boundaries are reconstructed on retrieval.

`fetch` escapes terminal control sequences when writing to a terminal, so a
stored log line cannot manipulate your terminal (cursor, title, etc.). When the
output is piped or redirected it is emitted byte-for-byte; `--raw` forces raw
output everywhere.

### Server

```bash
# Run directly
LOG_STREAMER_ADDR=:8080 LOG_STREAMER_DATA_DIR=/var/log/streams log-streamer-server

# Run via Docker
docker build -t log-streamer-server .
docker run -p 8080:8080 -v /data/logs:/data/logs log-streamer-server
```

The server speaks plain HTTP and has no built-in TLS. For any non-trusted network,
terminate TLS in front of it (e.g. a reverse proxy) so tokens are not sent in the
clear. The Docker image runs as an unprivileged user (uid 10001); if you bind-mount
a host directory over `/data/logs`, make it writable by that uid.

## Configuration

### Server (environment variables)

| Variable | Default | Description |
|----------|---------|-------------|
| `LOG_STREAMER_ADDR` | `:8080` | Listen address |
| `LOG_STREAMER_DATA_DIR` | `./data` | Storage directory for log files |
| `LOG_STREAMER_MAX_STREAM_BYTES` | `1073741824` (1 GiB) | Max bytes per stream; `0` = unlimited. Writes past the cap stop the stream. |
| `LOG_STREAMER_MAX_TOTAL_BYTES` | `0` (unlimited) | Max total bytes on disk; `0` = unlimited. New writes are rejected past the cap. |
| `LOG_STREAMER_TTL` | `168h` (7 days) | Delete streams older than this (by file mtime); `0` = never expire. |
| `LOG_STREAMER_SWEEP_INTERVAL` | `10m` | How often the expiry sweep runs. |

Byte limits are plain integers (bytes). Durations use Go syntax (`30m`, `24h`, ...).

### Client

| Variable | Default | Description |
|----------|---------|-------------|
| `LOG_STREAMER_SERVER` | `ws://localhost:8080` | Server WebSocket URL |

The `--server` flag overrides the environment variable.

## Protocol

- **Stream**: WebSocket at `/api/stream`. The server sends a JSON `hello` with the
  token, the client streams log data as **binary frames**, and the server sends a
  JSON `ack` (bytes received) at the end. Each binary frame is
  `[stream:1 byte][timestamp:8 bytes big-endian unix-nanos][payload...]`; the
  payload is raw bytes (any line length, any bytes).
- **Fetch**: `GET /api/logs/{token}` returns all reassembled log lines as JSON.
- **Delete**: `DELETE /api/logs/{token}` removes the log.

A single inbound frame is capped (1 MiB) and the server enforces the byte/total
limits above, so an unauthenticated client cannot exhaust memory or disk without
bound.

## Storage

Each stream is stored as a binary file (`<token>.bin`) in the data directory: a
sequence of length-prefixed records, each `[uvarint length][frame]` where the
frame is the wire frame described above. The format stores bytes verbatim (binary
safe) and is read back via `fetch`; it is not meant to be inspected by hand.

## Building

```bash
go-toolchain
```

Binaries are output to `build/`.
