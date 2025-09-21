# Pterodactyl Client - Wails Desktop Application

A powerful desktop client for managing multiple Pterodactyl panels and servers with an integrated file editor featuring Monaco Editor and split view capabilities.

## Features

- **Multi-Panel Management**: Connect to and manage multiple Pterodactyl panels from a single application
- **Server Management**: Switch between servers across different panels seamlessly
- **File Manager**: Browse, edit, upload, and manage server files with an intuitive tree view
- **Integrated Editor**: 
  - Monaco Editor integration for syntax highlighting and advanced code editing
  - Simple fallback editor with line numbers
  - Split view for comparing files side-by-side
  - Multi-tab editing support
  - Auto-save functionality
- **Console Access**: Real-time server console with command execution
- **Power Controls**: Start, stop, restart, and kill server processes
- **Cross-Platform**: Built with Wails for Windows, macOS, and Linux support

## Prerequisites

- Go 1.21 or later
- Node.js 18+ and npm (for building frontend)
- Wails CLI v2

## Installation

### Install Wails CLI

```bash
go install github.com/wailsapp/wails/v2/cmd/wails@latest
```

### Clone and Build

```bash
git clone https://github.com/yourusername/pteroclient-wails.git
cd pteroclient-wails

# Install frontend dependencies
cd frontend
npm install
cd ..

# Build the application
wails build
```

The executable will be generated in the `build/bin` directory.

## Development

To run in development mode with live reload:

```bash
wails dev
```

## Configuration

On first launch, the application will prompt you to add a Pterodactyl panel. You'll need:

- Panel URL (e.g., `https://panel.example.com`)
- API Key (either admin or client API key from your Pterodactyl panel)

### API Key Types

- **Admin API Key**: Provides full access to all servers on the panel (found in Admin Area → Application API)
- **Client API Key**: Provides access to servers you have permissions for (found in Account Settings → API Credentials)

## Usage

### Panel Management
- Click the gear icon next to the panel dropdown to manage panels
- Add multiple panels with different API keys
- Switch between panels using the dropdown

### File Editing
- Navigate the file tree on the left
- Click files to open in the editor
- Use Ctrl+S to save, Ctrl+W to close tabs
- Toggle split view for side-by-side editing

### Console
- Switch to the Console tab to access server console
- Send commands directly to the server
- Use power controls to manage server state

## Architecture

- **Backend**: Go with Wails framework
- **Frontend**: Vanilla JavaScript with Monaco Editor
- **API Integration**: Pterodactyl REST API and WebSocket for console

## Project Structure

```
pteroclient-wails/
├── app.go              # Wails app configuration
├── main.go             # Application entry point
├── pkg/
│   ├── config/         # Configuration management
│   └── pterodactyl/    # Pterodactyl API client
├── frontend/
│   ├── index.html      # Main UI
│   ├── main-editor.js  # Main application logic
│   ├── monaco-integrated.js  # Monaco editor integration
│   ├── integrated-split.js   # Split view functionality
│   └── src/
│       ├── style.css   # Application styles
│       └── app.css     # Additional styles
└── wails.json          # Wails configuration
```

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

## License

[Add your license here]

## Acknowledgments

- Built with [Wails](https://wails.io/)
- Uses [Monaco Editor](https://microsoft.github.io/monaco-editor/)
- Integrates with [Pterodactyl Panel](https://pterodactyl.io/)
