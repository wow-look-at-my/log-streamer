# log-streamer

Stream command output to a server and retrieve or delete it later using a token.

The client talks to `wss://logs.pazer.io` unless you point it somewhere else with `--server` or `LOG_STREAMER_SERVER`.

The server is intentionally **public / unauthenticated**: anyone who can reach it may open a stream. The 256-bit token is the only credential needed to fetch or delete a stream's logs. Treat it as a secret.

A client may name its own stream with `--token`. That is what makes a live CI build watchable, as described under [Watching a CI build live](#watching-a-ci-build-live). A caller holding a token can therefore append to that stream, as well as read and delete it. The token was always the whole credential. What a caller still cannot do is choose a storage path: every token is validated as 64 hex characters before it names a file.

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

# Trail a stream that is still being written, printing new lines as they land
log-streamer-client fetch --follow <token>

# Retrieve logs verbatim (do not escape terminal control sequences)
log-streamer-client fetch --raw <token>

# Delete logs
log-streamer-client delete <token>
```

`fetch --follow` polls on a fixed interval (`--interval`, default `1s`). It prints only the lines it has not printed yet. A stream that does not exist yet makes it wait instead of fail. You can therefore start watching before the writer connects. Stop it with Ctrl-C.

`run` tees stdout and stderr locally while streaming to the server. `send` does the same for piped stdin. Lines of any length are supported. Output is streamed in fixed-size chunks, so a multi-megabyte line costs no more memory than many small ones. Line boundaries are reconstructed on retrieval.

`fetch` escapes terminal control sequences when it writes to a terminal, so a stored log line cannot drive your cursor or title. Piped or redirected output is emitted byte-for-byte. `--raw` forces raw output everywhere.

### Server

```bash
# Run directly
LOG_STREAMER_ADDR=:8080 LOG_STREAMER_DATA_DIR=/var/log/streams log-streamer-server

# Run via Docker
docker build -t log-streamer-server .
docker run -p 8080:8080 -v /data/logs:/data/logs log-streamer-server
```

Point the client at your own server with `--server ws://localhost:8080`, or set `LOG_STREAMER_SERVER`.

The server speaks plain HTTP and has no built-in TLS. Terminate TLS in front of it on any untrusted network, behind a reverse proxy, so tokens never travel in the clear. The Docker image runs as an unprivileged user (uid 10001). Make `/data/logs` writable by that uid when you bind-mount a host directory over it.

## Watching a CI build live

GitHub Actions withholds a job's log until the run finishes. Streaming the output elsewhere gets around that. It works only if you can name the stream in advance. A server-minted token reaches the job over its own socket. The job's only channel back is the log you cannot read yet.

Both sides instead derive the same token from a key they already share:

```bash
# In the job, and again wherever you watch from
export LOG_STREAMER_STREAM_KEY='...'      # a repository or org secret
log-streamer-client token derive          # prints the token for this run
```

Inside Actions the context defaults to `$GITHUB_REPOSITORY/$GITHUB_RUN_ID/$GITHUB_RUN_ATTEMPT/$GITHUB_JOB`. A watcher reads every one of those from the REST API. That API serves run metadata immediately while the log is still withheld. Pass `--context` to derive from something else.

Matrix legs of a job share `GITHUB_JOB`. Give each leg its own `--name`. Without it, every leg writes into the same stream.

### In a workflow

```yaml
- uses: wow-look-at-my/log-streamer/.github/actions/stream@master
  with:
    stream-key: ${{ secrets.LOG_STREAMER_STREAM_KEY }}
    name: ${{ matrix.os }}      # required only for a matrix job
    run: |
      make build
      make test
```

Set `server:` only to reach a different server. The action installs the client and derives the token. It then runs your command through the client. Output still reaches the job's own log. The command's exit status is still the step's status. A failing command still fails the build. The action masks the derived token, so the log never shows it.

### Watching from your machine

```bash
export LOG_STREAMER_STREAM_KEY='...'

run_id="$(gh run list --branch my-branch --limit 1 --json databaseId --jq '.[0].databaseId')"
token="$(GITHUB_REPOSITORY=owner/repo GITHUB_RUN_ID=$run_id \
         GITHUB_RUN_ATTEMPT=1 GITHUB_JOB=test log-streamer-client token derive)"

log-streamer-client fetch --follow "$token"
```

Start this before or during the run. It waits for the stream to appear. Then it trails the stream.

### Buffering, the thing that will bite you

A program that writes to a pipe rather than a terminal usually switches to block buffering. Its output can then sit in that program's own buffer before log-streamer sees any of it. This is the program's behaviour, not the stream's. Use `stdbuf -oL` when a build goes quiet and then emits everything at once. Many tools also have an unbuffered flag of their own.

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
| `LOG_STREAMER_SERVER` | `wss://logs.pazer.io` | Server WebSocket URL |
| `LOG_STREAMER_TOKEN` | (none) | Stream into this token instead of a server-generated one. Used by `run` and `send`. |
| `LOG_STREAMER_STREAM_KEY` | (none) | Key that `token derive` derives from. |

The `--server`, `--token` and `--key` flags override the matching environment variables.

A server URL may be written as `wss://`, `ws://`, `https://`, `http://`, or a bare host. The client converts it to the scheme each request needs. A bare host becomes `wss://`.

## Protocol

- **Stream**: WebSocket at `/api/stream`. The server sends a JSON `hello` carrying the token. The client then streams log data as **binary frames**. The server sends a JSON `ack` with the byte count at the end. Each binary frame is `[stream:1 byte][timestamp:8 bytes big-endian unix-nanos][payload...]`. The payload is raw bytes, so any line length and any byte value survive. Pass `?token=<64 hex>` to name the stream yourself. The `hello` echoes back whichever token applies.
- **Fetch**: `GET /api/logs/{token}` returns all reassembled log lines as JSON. `?since=<n>` returns only the lines from index `n`. The `count` field stays the total. `fetch --follow` uses that to trail a growing log.
- **Delete**: `DELETE /api/logs/{token}` removes the log.

A fetch reads a stream that is still open, and returns everything written so far. That is what makes a live CI build readable mid-run.

A single inbound frame is capped at 1 MiB, and the server enforces the byte and total limits above. An unauthenticated client therefore cannot exhaust memory or disk without bound.

## Storage

Each stream is stored as a binary file (`<token>.bin`) in the data directory. It holds a sequence of length-prefixed records, each `[uvarint length][frame]`, where the frame is the wire frame described above. The format stores bytes verbatim, so it is binary safe. Read it back with `fetch`, rather than by hand.

## Building

```bash
go-toolchain
```

Binaries are output to `build/`.
