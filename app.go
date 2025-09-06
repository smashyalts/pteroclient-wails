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
	config       *config.MultiConfigManager
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
	
	// Initialize multi-panel config
	var err error
	a.config, err = config.NewMultiConfigManager()
	if err != nil {
		runtime.LogError(a.ctx, "Failed to initialize config: "+err.Error())
		return
	}
	
	// Connect if we have an active configured panel
	if a.config.IsConfigured() {
		a.Connect()
	}
}

// Connect to Pterodactyl server
func (a *App) Connect() error {
	cfg := a.config.GetConfig()
	
	// Create client with empty server ID initially if not set
	serverID := cfg.ServerID
	if serverID == "" {
		// Use empty string, will need to select server from dropdown
		serverID = ""
	}
	
	a.client = pterodactyl.NewClient(cfg.PanelURL, cfg.APIKey, serverID)
	
	// If no server ID, just test API connection without server-specific call
	if serverID != "" {
		// Test connection to specific server
		_, err := a.client.GetServerState()
		if err != nil {
			return fmt.Errorf("connection failed: %v", err)
		}
	} else {
		// Just test that we can list servers (API key is valid)
		_, err := a.client.ListServers()
		if err != nil {
			return fmt.Errorf("API connection failed: %v", err)
		}
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
	
	// Update config for active panel
	a.config.UpdateActivePanelServer(serverID)
	
	// Test connection to new server
	_, err := a.client.GetServerState()
	if err != nil {
		return fmt.Errorf("failed to connect to server: %v", err)
	}
	
	// Emit server changed event
	runtime.EventsEmit(a.ctx, "server-changed", serverID)
	
	return nil
}

// Panel Management Methods

// ListPanels returns the names of all configured panels
func (a *App) ListPanels() []string {
	panels := a.config.GetPanels()
	names := make([]string, len(panels))
	for i, p := range panels {
		names[i] = p.Name
	}
	return names
}

// SetActivePanel sets the active panel by name
func (a *App) SetActivePanel(panelName string) error {
	return a.config.SetActivePanel(panelName)
}

// GetPanels returns all configured panels
func (a *App) GetPanels() []map[string]interface{} {
	panels := a.config.GetPanels()
	result := make([]map[string]interface{}, len(panels))
	
	for i, p := range panels {
		result[i] = map[string]interface{}{
			"name":     p.Name,
			"panelURL": p.PanelURL,
			"serverID": p.ServerID,
		}
	}
	
	return result
}

// GetActivePanel returns the name of the active panel
func (a *App) GetActivePanel() string {
	return a.config.GetActivePanelName()
}

// SwitchPanel switches to a different panel
func (a *App) SwitchPanel(panelName string) error {
	// Disconnect console if connected
	if a.consoleWS != nil && a.consoleWS.IsConnected() {
		a.consoleWS.Close()
		runtime.EventsEmit(a.ctx, "console-connected", false)
	}
	
	// Set the active panel
	if err := a.config.SetActivePanel(panelName); err != nil {
		return err
	}
	
	// Reconnect with new panel
	if err := a.Connect(); err != nil {
		return err
	}
	
	// Emit panel changed event
	runtime.EventsEmit(a.ctx, "panel-changed", panelName)
	
	return nil
}

// AddPanel adds a new panel configuration
func (a *App) AddPanel(name, panelURL, apiKey string) error {
	if name == "" || panelURL == "" || apiKey == "" {
		return fmt.Errorf("name, panel URL, and API key are required")
	}
	
	panel := config.PanelConfig{
		Name:     name,
		PanelURL: panelURL,
		APIKey:   apiKey,
	}
	
	return a.config.AddOrUpdatePanel(panel)
}

// RemovePanel removes a panel configuration
func (a *App) RemovePanel(name string) error {
	// Can't remove the active panel if it's the only one
	panels := a.config.GetPanels()
	if len(panels) <= 1 {
		return fmt.Errorf("cannot remove the last panel")
	}
	
	// If removing active panel, switch to another first
	if a.config.GetActivePanelName() == name {
		for _, p := range panels {
			if p.Name != name {
				a.SwitchPanel(p.Name)
				break
			}
		}
	}
	
	return a.config.RemovePanel(name)
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
