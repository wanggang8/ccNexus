# AGENTS.md

## Cursor Cloud specific instructions

**ccNexus** is a smart API endpoint rotation proxy for Claude Code and Codex CLI. It has two build targets sharing the same `internal/` packages:

- **Headless server** (`cmd/server/`) — HTTP API on port 3000 with an embedded Web UI at `/ui/`
- **Desktop app** (`cmd/desktop/`) — Wails v2 GUI (requires GTK3 + WebKit2GTK on Linux)

### Building and running

| Task | Command |
|---|---|
| Run all tests | `go test ./...` |
| Lint (built-in) | `go vet ./...` |
| Build headless server | `go build -o build/bin/ccnexus-server ./cmd/server` |
| Run headless server | `CCNEXUS_PORT=3000 CCNEXUS_DATA_DIR=/tmp/ccnexus-dev ./build/bin/ccnexus-server` |
| Build desktop frontend | `cd cmd/desktop/frontend && npm run build` |
| Health check | `curl http://localhost:3000/health` |
| Web UI | `http://localhost:3000/ui/` |

See `docs/development.md` for the full development guide.

### Non-obvious caveats

- **Desktop `go:embed` requirement**: `cmd/desktop/` embeds `frontend/dist` via `go:embed`. You must run `cd cmd/desktop/frontend && npm install && npm run build` before `go test ./...` or `go build ./cmd/desktop` will fail. The headless server (`cmd/server/`) does not depend on this.
- **No `go.sum` in git**: The repo does not check in `go.sum`. Running `go mod tidy` generates it. This is handled automatically by the update script.
- **SQLite is embedded**: No external database server is needed. The DB file is created automatically at `$CCNEXUS_DB_PATH` or `~/.ccNexus/ccnexus.db`.
- **`go vet` warnings**: There are pre-existing `go vet` warnings about lock value copies in `internal/service/`. These are not blocking and exist in the upstream code.
- **Environment variables**: `CCNEXUS_PORT` (default 3000), `CCNEXUS_DATA_DIR`, `CCNEXUS_DB_PATH`, `CCNEXUS_UI_TOKEN` (optional auth for Web UI), `CCNEXUS_LOG_LEVEL`.
