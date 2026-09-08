# log-streamer

Go project with two binaries: `ls-client` and `ls-server`.

## Build

```bash
go-toolchain
```

Binaries are output to `build/`.

## Project structure

- `cmd/ls-client/` - CLI client (cobra, self-registering subcommands)
- `cmd/ls-server/` - Server binary
- `internal/protocol/` - Shared JSON control types + binary wire frame codec (`wire.go`)
- `internal/token/` - Token generation, validation, and HMAC derivation for CI
- `.github/actions/stream/` - Composite action running a command through the client
- `.github/actions/setup/` - Composite action wiring a whole job's steps to one stream
- `internal/storage/` - File-based binary record storage (`<token>.bin`), per-stream/total byte caps, TTL sweep
- `internal/server/` - HTTP/WebSocket server (binary stream ingest, limits, graceful shutdown)

## Design notes

- The server is public/unauthenticated by design. The per-stream 256-bit token is the only credential to fetch, append or delete. A client may name its own stream via `?token=`, which live CI watching depends on. Every token is still validated as 64 hex before it names a file, so no caller can pick a storage path.
- A whole job streams by naming `ls-client shell {0}` as the job's `defaults.run.shell`. Actions hands a custom shell the step's script path as `{0}`, and resolves the command on `PATH`. A job default never applies inside a composite action. Each step is its own process, so it wraps itself in `marker` frames built from `GITHUB_ACTION`/`GITHUB_JOB`/`GITHUB_WORKFLOW`. Actions exports no step name, hence the command-line label and `LOG_STREAMER_STEP_NAME`. `fetch --steps` and `fetch --step` read the log back per step.
- A stream may register under a group token (`?group=`), which the server indexes as `<group>.grp` and `GET /api/groups/{group}` lists. The group derives from the run alone (`repository/run-id/run-attempt`), namespaced apart from stream tokens. A watcher therefore lists a run's matrix legs without knowing the names they derive from. `--key`, `--context` and `--name` are global flags: any command derives the token or the group it needs.
- Live CI watching is why `--token`, `token derive` and `fetch --follow` exist. A CI provider withholds the job log, so a server-minted token can never get out of the job. Both sides derive it instead, from a shared key plus the run's identity. See the README's "Watching a CI build live".
- Log data flows as WebSocket **binary** frames (`[stream:1][ts:8 BE unix-nanos][payload]`); `hello`/`ack`/`error` are JSON text frames. Payloads are raw bytes, chunked, so arbitrarily long lines stream with bounded memory; lines are reconstructed on fetch by splitting on `\n`.
- Abuse is bounded by `LOG_STREAMER_MAX_STREAM_BYTES`, `LOG_STREAMER_MAX_TOTAL_BYTES`, `LOG_STREAMER_TTL`, plus a 1 MiB inbound frame cap. See README for all env vars.

## Testing

All tests run via `go-toolchain`. Integration tests use `httptest` + real WebSocket connections.

## Docker

Server runs in Docker. Data persists in `/data/logs` volume.
