# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

ccNexus is a smart API endpoint rotation proxy for Claude Code and Codex CLI. It provides automatic failover between multiple API endpoints with format conversion support (Claude ↔ OpenAI ↔ Gemini).

**Two deployment modes:**
- **Desktop app** (`cmd/desktop/`): Wails-based GUI with system tray support
- **Server mode** (`cmd/server/`): Pure HTTP backend service with optional Docker deployment

Both modes share the same core logic in `internal/`.

## Development Commands

### Desktop App Development

```bash
# Install Wails CLI (first time only)
go install github.com/wailsapp/wails/v2/cmd/wails@latest

# Check environment dependencies
wails doctor

# Install frontend dependencies
cd cmd/desktop/frontend && npm install && cd ../../..

# Start development mode with hot reload (run from cmd/desktop/)
cd cmd/desktop && wails dev

# Build for current platform (run from cmd/desktop/)
cd cmd/desktop && wails build
# Output: cmd/desktop/build/bin/
```

### Server Mode Development

```bash
# Run server directly
cd cmd/server && go run main.go webui_plugin.go

# Build server binary
cd cmd/server && go build -o ccnexus-server

# Run with Docker
cd cmd/server && docker-compose up
```

### Testing

```bash
# Run all tests
go test ./...

# Run tests for specific package
go test ./internal/transformer/cc/
go test ./internal/proxy/

# Run tests with verbose output
go test -v ./internal/transformer/...

# Run specific test
go test -v -run TestClaudeToOpenAI ./internal/transformer/convert/
```

## Architecture

### Core Components

**`internal/proxy/`** - HTTP proxy server core
- Handles incoming requests from Claude Code/Codex CLI
- Manages endpoint rotation and failover logic
- Tracks statistics (tokens, requests, errors)
- Records traffic logs for debugging

**`internal/transformer/`** - API format conversion engine
- Registry-based transformer system (`registry.go`)
- Three transformer categories:
  - `cc/` - Claude Code format transformers (claude, openai, openai2, gemini, cli)
  - `cx/` - Codex CLI format transformers (chat/ and responses/ subdirs)
  - `convert/` - Cross-format converters (claude→openai, openai→gemini, etc.)
- Each transformer implements the `Transformer` interface
- Supports bidirectional conversion with streaming SSE support

**`internal/storage/`** - SQLite persistence layer
- Stores endpoints, configuration, statistics
- WAL mode enabled for better concurrency
- Device ID management for multi-device sync
- Safe config keys for cross-platform backup/restore

**`internal/config/`** - Configuration management
- Endpoint definitions (API URL, key, transformer type, model)
- Application settings (port, log level, language, theme)
- Validation logic

**`internal/service/`** - Business logic layer
- Endpoint CRUD operations
- Statistics aggregation
- Backup/restore (local, S3, WebDAV)
- Terminal integration
- Update checking

### Desktop App Structure

**`cmd/desktop/app.go`** - Wails application bridge
- Exposes Go methods to frontend via Wails runtime
- Handles system tray integration
- Manages application lifecycle

**`cmd/desktop/frontend/`** - Vanilla JS frontend (Vite, no framework)
- `frontend/src/modules/` - Feature modules:
  - `endpoints.js` - Endpoint list, CRUD, toolbar with filters
  - `stats.js` - Real-time statistics dashboard
  - `settings.js` - Application settings
  - `history.js` - Request history viewer
  - `session.js` - Session management
  - `modal.js` - Reusable modal dialogs
  - `filters.js` - Unified filter panel for endpoints
  - `broadcast.js` - Event broadcasting system
  - `terminal.js`, `traffic.js`, `sync.js`, etc.
- `frontend/src/i18n/` - Internationalization (zh-CN, en-US)
- `frontend/src/themes/` - 12 built-in themes
- `frontend/src/styles/` - Modular CSS (toolbar, filters, etc.)
- `frontend/src/style.css` - Global styles and CSS variables

### Server Mode Structure

**`cmd/server/main.go`** - HTTP server entry point
- REST API for configuration and statistics
- Shares proxy logic with desktop app
- WebUI served from `cmd/server/webui/`

## Key Patterns

### Transformer Registration

Transformers self-register on package init:

```go
func init() {
    transformer.Register(&ClaudeTransformer{})
}
```

Lookup by name:
```go
t, err := transformer.Get("claude")
```

### Endpoint Configuration

Each endpoint specifies:
- `name` - Display name
- `apiUrl` - Base URL (e.g., `https://api.anthropic.com`)
- `apiKey` - Authentication key
- `transformer` - Type: `claude`, `openai`, `openai2`, `gemini`, `cli`
- `model` - Optional model override
- `enabled` - Active/inactive flag

### Statistics Tracking

Event-driven zero-latency updates:
- Request/response intercepted in `internal/proxy/`
- Token usage extracted from API responses
- Stats updated in real-time via `Stats` struct
- Aggregated by period (today, yesterday, week, month)

### Frontend-Backend Communication

Desktop app uses Wails runtime:
```javascript
// Call Go method from JS
const result = await window.go.main.App.GetEndpoints();

// Listen to Go events
runtime.EventsOn('stats:updated', (data) => { ... });
```

Server mode uses REST API:
```javascript
const response = await fetch('/api/endpoints');
```

## Development Workflow

Follow the workflow defined in `.cursor/rules/project-norms.mdc`:

1. **Research** - Understand requirements, constraints, dependencies
2. **Ideation** - Propose 1-2 solutions with tradeoffs
3. **Planning** - List files/modules to change, key logic, I/O
4. **Execution** - Implement according to approved plan
5. **Review** - Verify consistency, error handling, naming

For small changes, user may request "quick path" (Execution → Review only).

## Frontend Guidelines

- **No frameworks** - Vanilla JS with Vite bundler
- **Internationalization required** - All user-facing text must use i18n system (`cmd/desktop/frontend/src/i18n/`)
- **Theme compatibility** - Use existing CSS variables, support all 12 themes in `frontend/src/themes/`
- **Responsive design** - Primary target: 1024px+, graceful degradation for smaller screens
- **Module organization** - New features go in `cmd/desktop/frontend/src/modules/`, follow existing patterns
- **Modular CSS** - Component-specific styles go in `frontend/src/styles/`, use CSS variables for theming

## Data Storage

- **Database**: `~/.ccNexus/ccnexus.db` (SQLite with WAL mode)
- **Safe config keys**: Defined in `internal/storage/sqlite.go` - only these sync across devices
- **Device-specific settings**: Terminal paths, local backup dirs, proxy URLs - NOT synced

## Testing Notes

- Most tests are in `internal/transformer/` subdirectories
- Test files follow `*_test.go` naming convention
- Transformer tests verify bidirectional conversion accuracy
- No tests currently for `internal/proxy/` or `internal/service/` (opportunity for contribution)

## Recent Optimizations

### Endpoint Toolbar Optimization (March 2024)
Reduced button count from 8 to 6 (25% reduction) with improved UX:
- **Unified filter panel** (`filters.js`) - Consolidated 3 separate filter buttons into one
- **"More" dropdown menu** (`toolbar.js`) - Groups less-frequent actions (Terminal, Sync)
- **Responsive layout** - Better mobile/small window support with 768px and 480px breakpoints
- **Filter badges** - Visual indicators showing active filter count
- **Modular CSS** - Separated toolbar and filter styles into `frontend/src/styles/`

Key files modified:
- `cmd/desktop/frontend/src/modules/endpoints.js` - Toolbar HTML structure
- `cmd/desktop/frontend/src/modules/filters.js` - Filter panel logic
- `cmd/desktop/frontend/src/modules/toolbar.js` - "More" menu implementation
- `cmd/desktop/frontend/src/styles/toolbar.css` - Toolbar styles
- `cmd/desktop/frontend/src/styles/filters.css` - Filter panel styles

## Important Constraints

- **Go version**: 1.22+ required (uses toolchain go1.24.3)
- **Node.js**: 18+ for frontend build
- **Wails**: v2 for desktop app
- **No React/Vue**: Frontend is intentionally framework-free
- **SQLite only**: No other database backends supported
