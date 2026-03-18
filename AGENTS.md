# AGENTS.md

## Cursor Cloud specific instructions

### Project overview
ccNexus is a Go 1.24 API proxy that rotates endpoints and converts between Claude, OpenAI (Chat + Responses), and Gemini API formats. Two deployment modes: Wails desktop app (`cmd/desktop/`) and pure HTTP server (`cmd/server/`).

### Build & test
- All standard commands are in `CLAUDE.md` (server: `go run cmd/server/main.go cmd/server/webui_plugin.go`, tests: `go test ./...`).
- `go vet ./...` reports pre-existing warnings in `internal/service/` (lock copy); these are not related to the transformer layer.
- The desktop app (`wails dev`) requires system GTK/WebKit libraries and a display, so it cannot be built in headless Cloud Agent VMs. The server binary builds and runs fine.

### Key caveats
- The converter package (`internal/transformer/convert/`) uses untyped `map[string]interface{}` heavily for JSON manipulation; be careful with type assertions when editing.
- `StreamContext` is created fresh per-stream — fields like `ToolIndex` and `CurrentToolID` are per-stream, not global.
- `TOOL_CODE` is used as a Gemini `finishReason` in several converters, but this is not an official Gemini API value. The code compensates by also checking for the presence of `functionCall` parts.
