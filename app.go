package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	"pteroclient-wails/pkg/config"
	"pteroclient-wails/pkg/pterodactyl"
)

// App struct
type App struct {
	ctx          context.Context
	config       *config.ConfigManager
	client       *pterodactyl.Client
	consoleWS    *pterodactyl.ConsoleWebSocket
}

// NewApp creates a new App application struct
func NewApp() *App {
	return &App{}
}

// startup is called when the app starts
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	
	// Initialize config
	var err error
	a.config, err = config.NewConfigManager()
	if err != nil {
		runtime.LogError(a.ctx, "Failed to initialize config: "+err.Error())
		return
	}
	
	// Connect if configured
	if a.config.IsConfigured() {
		a.Connect()
	}
}

// Connect to Pterodactyl server
func (a *App) Connect() error {
	cfg := a.config.GetConfig()
	
	a.client = pterodactyl.NewClient(cfg.PanelURL, cfg.APIKey, cfg.ServerID)
	
	// Test connection
	_, err := a.client.GetServerState()
	if err != nil {
		return fmt.Errorf("connection failed: %v", err)
	}
	
	runtime.EventsEmit(a.ctx, "connected", true)
	return nil
}

// ListServers lists all available servers
func (a *App) ListServers() ([]map[string]interface{}, error) {
	if a.client == nil {
		return nil, fmt.Errorf("not connected")
	}
	
	servers, err := a.client.ListServers()
	if err != nil {
		return nil, err
	}
	
	result := make([]map[string]interface{}, len(servers))
	for i, s := range servers {
		result[i] = map[string]interface{}{
			"id":          s.ID,
			"name":        s.Name,
			"description": s.Description,
			"isOwner":     s.IsOwner,
			"status":      s.Status,
		}
	}
	
	return result, nil
}

// SwitchServer switches to a different server
func (a *App) SwitchServer(serverID string) error {
	if a.client == nil {
		return fmt.Errorf("not connected")
	}
	
	// Disconnect console if connected
	if a.consoleWS != nil && a.consoleWS.IsConnected() {
		a.consoleWS.Close()
		runtime.EventsEmit(a.ctx, "console-connected", false)
	}
	
	// Update client server ID
	a.client.SetServerID(serverID)
	
	// Update config
	cfg := a.config.GetConfig()
	cfg.ServerID = serverID
	a.config.SetConfig(cfg)
	a.config.Save()
	
	// Test connection to new server
	_, err := a.client.GetServerState()
	if err != nil {
		return fmt.Errorf("failed to connect to server: %v", err)
	}
	
	// Emit server changed event
	runtime.EventsEmit(a.ctx, "server-changed", serverID)
	
	return nil
}

// GetConfig returns current config
func (a *App) GetConfig() (map[string]string, error) {
	cfg := a.config.GetConfig()
	return map[string]string{
		"panelURL": cfg.PanelURL,
		"serverID": cfg.ServerID,
		"apiKey":   cfg.APIKey,
	}, nil
}

// SaveConfig saves configuration
func (a *App) SaveConfig(panelURL, apiKey, serverID string) error {
	cfg := &config.Config{
		PanelURL: panelURL,
		APIKey:   apiKey,
		ServerID: serverID,
	}
	
	a.config.SetConfig(cfg)
	if err := a.config.Save(); err != nil {
		return err
	}
	
	// Reconnect with new config
	return a.Connect()
}

// GetServerState returns server state
func (a *App) GetServerState() (string, error) {
	if a.client == nil {
		return "disconnected", nil
	}
	
	state, err := a.client.GetServerState()
	if err != nil {
		return "error", err
	}
	
	return state, nil
}

// SetPowerState sets server power state
func (a *App) SetPowerState(signal string) error {
	if a.client == nil {
		return fmt.Errorf("not connected")
	}
	
	return a.client.SetPowerState(signal)
}

// SendCommand sends a console command
func (a *App) SendCommand(command string) error {
	if a.consoleWS == nil || !a.consoleWS.IsConnected() {
		return fmt.Errorf("console not connected")
	}
	
	return a.consoleWS.SendCommand(command)
}

// ConnectConsole connects to console WebSocket
func (a *App) ConnectConsole() error {
	if a.client == nil {
		return fmt.Errorf("not connected to server")
	}
	
	// Get WebSocket credentials
	creds, err := a.client.GetWebSocketCredentials()
	if err != nil {
		return fmt.Errorf("failed to get WebSocket credentials: %v", err)
	}
	
// Create WebSocket with origin
	cfg := a.config.GetConfig()
	panelOrigin := strings.TrimSuffix(cfg.PanelURL, "/")
	
	a.consoleWS = pterodactyl.NewConsoleWebSocketWithOrigin(
		creds.Socket, creds.Token, cfg.ServerID, panelOrigin,
	)
	
	// Set up message handler
	a.consoleWS.OnOutput = func(message string) {
		// Send raw ANSI text; frontend will render colors
		runtime.EventsEmit(a.ctx, "console-output", message)
	}
	
	a.consoleWS.OnError = func(err error) {
		runtime.EventsEmit(a.ctx, "console-error", err.Error())
	}
	
	// Connect
	err = a.consoleWS.Connect()
	if err != nil {
		return fmt.Errorf("failed to connect: %v", err)
	}
	
	// Request initial logs
	if err := a.consoleWS.RequestLogs(); err != nil {
		runtime.EventsEmit(a.ctx, "console-error", "failed to request logs: "+err.Error())
	}
	
	runtime.EventsEmit(a.ctx, "console-connected", true)
	return nil
}

// DisconnectConsole disconnects console
func (a *App) DisconnectConsole() error {
	if a.consoleWS != nil {
		return a.consoleWS.Close()
	}
	return nil
}

// ListFiles lists files in a directory
func (a *App) ListFiles(path string) ([]map[string]interface{}, error) {
	if a.client == nil {
		return nil, fmt.Errorf("not connected")
	}
	
	files, err := a.client.ListFiles(path)
	if err != nil {
		return nil, err
	}
	
	result := make([]map[string]interface{}, len(files))
	for i, f := range files {
		result[i] = map[string]interface{}{
			"name":      f.Name,
			"size":      f.Size,
			"mode":      f.Mode,
			"modTime":   f.ModifiedAt,
			"isDir":     !f.IsFile && !f.IsSymlink,
			"isFile":    f.IsFile,
			"isSymlink": f.IsSymlink,
		}
	}
	
	return result, nil
}

// GetFileContent gets file content
func (a *App) GetFileContent(path string) (string, error) {
	if a.client == nil {
		return "", fmt.Errorf("not connected")
	}
	
	return a.client.GetFileContent(path)
}

// SaveFileContent saves file content
func (a *App) SaveFileContent(path, content string) error {
	if a.client == nil {
		return fmt.Errorf("not connected")
	}
	
	return a.client.SaveFileContent(path, content)
}

// CreateFolder creates a new folder
func (a *App) CreateFolder(path string) error {
	if a.client == nil {
		return fmt.Errorf("not connected")
	}
	
	// Split path into directory and name
	var dir, name string
	lastSlash := strings.LastIndex(path, "/")
	if lastSlash == -1 || lastSlash == 0 {
		dir = "/"
		name = strings.TrimPrefix(path, "/")
	} else {
		dir = path[:lastSlash]
		name = path[lastSlash+1:]
	}
	
	return a.client.CreateDirectory(dir, name)
}

// DeleteFiles deletes files or folders
func (a *App) DeleteFiles(paths []string) error {
	if a.client == nil {
		return fmt.Errorf("not connected")
	}
	
	// Group files by directory
	filesByDir := make(map[string][]string)
	for _, path := range paths {
		var dir, name string
		lastSlash := strings.LastIndex(path, "/")
		if lastSlash == -1 || lastSlash == 0 {
			dir = "/"
			name = strings.TrimPrefix(path, "/")
		} else {
			dir = path[:lastSlash]
			name = path[lastSlash+1:]
		}
		filesByDir[dir] = append(filesByDir[dir], name)
	}
	
	// Delete files in each directory
	for dir, files := range filesByDir {
		if err := a.client.DeleteFiles(dir, files); err != nil {
			return err
		}
	}
	
	return nil
}

// RenameFile renames a file or folder
func (a *App) RenameFile(oldPath, newPath string) error {
	if a.client == nil {
		return fmt.Errorf("not connected")
	}
	
	// Split paths to get directory and names
	var dir, oldName, newName string
	
	// Get directory from old path
	lastSlash := strings.LastIndex(oldPath, "/")
	if lastSlash == -1 || lastSlash == 0 {
		dir = "/"
		oldName = strings.TrimPrefix(oldPath, "/")
	} else {
		dir = oldPath[:lastSlash]
		oldName = oldPath[lastSlash+1:]
	}
	
	// Get new name
	lastSlash = strings.LastIndex(newPath, "/")
	if lastSlash == -1 || lastSlash == 0 {
		newName = strings.TrimPrefix(newPath, "/")
	} else {
		newName = newPath[lastSlash+1:]
	}
	
	return a.client.RenameFile(dir, oldName, newName)
}

// UploadFile handles file upload
func (a *App) UploadFile(path string, content []byte) error {
	if a.client == nil {
		return fmt.Errorf("not connected")
	}
	
	// Split path into directory and filename
	var dir, filename string
	lastSlash := strings.LastIndex(path, "/")
	if lastSlash == -1 || lastSlash == 0 {
		dir = "/"
		filename = strings.TrimPrefix(path, "/")
	} else {
		dir = path[:lastSlash]
		filename = path[lastSlash+1:]
	}
	
	// Convert byte array to reader
	reader := strings.NewReader(string(content))
	
	return a.client.UploadFile(dir, filename, reader)
}

// cleanANSI removes ANSI escape codes
func cleanANSI(text string) string {
	// Remove ANSI codes
	for strings.Contains(text, "\x1b[") {
		start := strings.Index(text, "\x1b[")
		if start >= 0 {
			end := strings.IndexByte(text[start:], 'm')
			if end > 0 {
				text = text[:start] + text[start+end+1:]
			} else {
				break
			}
		}
	}
	text = strings.ReplaceAll(text, "[m", "")
	return text
}
