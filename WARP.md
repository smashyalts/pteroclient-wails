# WARP.md

This file provides guidance to WARP (warp.dev) when working with code in this repository.

## Project Overview

This is a Pterodactyl Panel client built with Wails v2 (Go backend + vanilla JavaScript frontend). It provides a desktop application for managing Pterodactyl game servers with features like console access, file management, and server control.

## Development Commands

### Build and Run
```bash
# Live development mode with hot reload
wails dev

# Build production binary for current platform
wails build

# Build with skip bindings flag for faster builds
wails build -s

# Build for specific platform
wails build -s -platform linux/amd64
wails build -s -platform windows/amd64
wails build -s -platform darwin/amd64

# Build for Arch Linux (using build script)
./build-arch.sh

# Build Arch Linux package
makepkg -si
```

### Frontend Development
```bash
# Install frontend dependencies
cd frontend && npm install

# Run frontend dev server (usually auto-started by wails dev)
cd frontend && npm run dev

# Build frontend for production
cd frontend && npm run build
```

### Testing and Validation
```bash
# Format Go code
go fmt ./...

# Run Go vet
go vet ./...

# Update dependencies
go mod tidy

# Download dependencies
go mod download
```

## Architecture

### Backend Structure (Go)
- **main.go**: Application entry point, Wails initialization
- **app.go**: Core application logic and API bindings
  - Panel management (multi-panel support)
  - Server connection and switching
  - Console WebSocket management
  - File operations (CRUD)
  - Configuration management

- **pkg/config/**: Configuration management
  - `config.go`: Single panel configuration
  - `multi_config.go`: Multi-panel configuration support
  - Config stored in `~/.pteroclient/config.json`

- **pkg/pterodactyl/**: Pterodactyl API client
  - `client.go`: REST API client for panel operations
  - `websocket.go`: WebSocket client for console access
  - Auto-detects admin vs client API keys
  - Handles server state, file management, power controls

### Frontend Structure (JavaScript/HTML)
- **frontend/src/**: Main frontend source
  - `main.js`: Primary UI logic and Wails bindings
  - `console.js`: Console output handling
  - `app.js`, `main2.js`: Additional UI components
  
- **frontend/**: Monaco editor integration
  - `monaco-editor.html`: Editor modal template
  - `monaco-editor.js`: Editor initialization and file handling
  - `main-editor.js`, `main-full.js`: Different UI layouts

### Key APIs and Patterns

#### Wails Bindings
All Go methods in `app.go` are exposed to frontend via Wails auto-generated bindings:
- Panel operations: `ListPanels()`, `SwitchPanel()`, `AddPanel()`, `RemovePanel()`
- Server operations: `ListServers()`, `SwitchServer()`, `GetServerState()`, `SetPowerState()`
- Console: `ConnectConsole()`, `DisconnectConsole()`, `SendCommand()`
- Files: `ListFiles()`, `GetFileContent()`, `SaveFileContent()`, `CreateFolder()`, `DeleteFiles()`, `RenameFile()`, `UploadFile()`

#### Event System
Frontend-backend communication via Wails events:
- `connected`: Server connection status
- `console-output`: Console messages from server
- `console-error`: Console connection errors
- `console-connected`: Console WebSocket status
- `server-changed`: Active server switched
- `panel-changed`: Active panel switched

#### Configuration Storage
Multi-panel configuration with active panel tracking:
```json
{
  "panels": [
    {
      "name": "Panel Name",
      "panel_url": "https://panel.example.com",
      "api_key": "ptlc_xxx",
      "server_id": "uuid"
    }
  ],
  "active_panel": "Panel Name"
}
```

## Platform-Specific Notes

### Linux (Arch)
- Dependencies: `webkit2gtk`, `gtk3`, `libappindicator-gtk3`
- Build outputs to `build/bin/pteroclient-wails`
- PKGBUILD provided for AUR-style packaging
- Config location: `~/.pteroclient/config.json`

### Windows
- Uses WebView2 (auto-downloaded if needed)
- Build with: `wails build -s -platform windows/amd64`
- Config location: `%USERPROFILE%\.pteroclient\config.json`

### Development URLs
- Wails dev server: http://localhost:34115
- Vite dev server: Auto-detected by Wails

## API Integration

### Pterodactyl Panel API
- Supports both Client API (`/api/client`) and Admin API (`/api/application`)
- Auto-detects API type based on key permissions
- WebSocket console via `/api/client/servers/{id}/websocket`
- File operations via `/api/client/servers/{id}/files/*`

### Authentication
- Bearer token authentication with API keys
- Supports multiple panel configurations
- Server ID can be empty initially (server selection dropdown)

## Common Tasks

### Add New Panel Support
1. Use `AddPanel()` method in app.go
2. Switch panels with `SwitchPanel()`
3. Config automatically saved to disk

### Implement New Server Feature
1. Add method to `pkg/pterodactyl/client.go`
2. Expose via method in `app.go`
3. Regenerate bindings: `wails generate module`
4. Call from frontend via generated bindings

### Debug WebSocket Console
- Check origin header matches panel URL (handled in `pterodactyl.NewConsoleWebSocketWithOrigin()`)
- Console auth token from `/api/client/servers/{id}/websocket`
- Monitor events: `console-output`, `console-error`, `console-connected`

### Handle ANSI Colors in Console
- Backend sends raw ANSI codes
- Frontend `ansiToHtml()` function processes colors
- Terminal emulation in `console.js`
