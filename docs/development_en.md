# Development Guide

## Prerequisites

- Go 1.22+
- Node.js 18+
- Wails CLI v2

```bash
# Install Wails CLI
go install github.com/wailsapp/wails/v2/cmd/wails@latest

# Check environment dependencies
wails doctor
```

## Development Mode

```bash
# Install frontend dependencies
cd cmd/desktop/frontend && npm install && cd ../../..

# Start development mode (with hot reload)
cd cmd/desktop && wails dev
```

## Build for Release

```bash
cd cmd/desktop && wails build
```

Build output is in `cmd/desktop/build/bin/` directory.

## Project Structure

```
ccNexus/
├── cmd/
│   ├── desktop/             # Desktop app (Wails)
│   │   ├── app.go
│   │   ├── main.go
│   │   └── frontend/         # Frontend code
│   │       ├── src/modules/  # Feature modules
│   │       ├── src/i18n/     # Internationalization
│   │       └── src/themes/   # Theme styles
│   └── server/               # Headless server
├── internal/
│   ├── proxy/                # HTTP proxy core
│   ├── transformer/          # API format transformers
│   ├── storage/              # SQLite data storage
│   ├── config/               # Configuration management
│   ├── webdav/               # WebDAV sync
│   ├── logger/               # Logging system
│   └── tray/                 # System tray
└── docs/
```
