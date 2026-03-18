# AGENTS.md

## Cursor Cloud specific instructions

### Project overview

ccNexus is a Go-based API endpoint rotation proxy with two deployment modes:
- **Server mode** (`cmd/server/`): headless HTTP service (port 3000 by default). This is the primary mode for Cloud Agent development.
- **Desktop mode** (`cmd/desktop/`): Wails v2 GUI app. Requires a `augment-private-key.pem` file embedded at build time and Wails CLI; will not compile without it.

### Running tests

```bash
# All tests (desktop package will fail with "pattern augment-private-key.pem: no matching files found" — this is expected)
go test ./...

# Skip the desktop package (recommended for CI/cloud)
go test $(go list ./... | grep -v cmd/desktop)

# Augment-specific tests
go test -v ./internal/transformer/augment/...
```

### Linting

```bash
go vet $(go list ./... | grep -v cmd/desktop)
```

The `cmd/desktop` package always fails `go vet` because it uses `//go:embed` on a file (`augment-private-key.pem`) that only exists in release builds. Filter it out.

### Building and running the server

```bash
cd cmd/server && go build -o ccnexus-server .
./ccnexus-server
# Health check: curl http://localhost:3000/health
```

Data directory: `~/.ccNexus/ccnexus.db` (auto-created on first run).

### Frontend dependencies

The frontend lives at `cmd/desktop/frontend/` and uses npm + Vite (vanilla JS, no framework). Install with:

```bash
cd cmd/desktop/frontend && npm install
```

This is only needed for desktop mode development, not for server mode.

### Key caveats

- The Go module requires **Go 1.24+** (toolchain `go1.24.3`). The `go.mod` declares `go 1.24.0`.
- `cmd/desktop/app.go` embeds `augment-private-key.pem` via `//go:embed`. This file is not in the repo. Desktop compilation will fail without it. Server mode is unaffected.
- SQLite is used via pure-Go `modernc.org/sqlite` — no CGO or system SQLite required.
- The proxy listens on port 3000. The Augment server (when enabled) listens on port 8888.
- See `CLAUDE.md` for architecture details, transformer registration patterns, and development workflow.
