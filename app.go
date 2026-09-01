package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	"pteroclient-wails/pkg/config"
	"pteroclient-wails/pkg/pterodactyl"
	"pteroclient-wails/pkg/safestore"
)

// App struct
type App struct {
	ctx          context.Context
	config       *config.MultiConfigManager
	client       *pterodactyl.Client       // Client API for file operations
	adminClient  *pterodactyl.Client       // Admin API for server listing (optional)
	consoleWS    *pterodactyl.ConsoleWebSocket
	serverPanelMap map[string]string // Maps server ID to panel name

	// Local safety net. store holds the pre-write copies and the recycle bin;
	// fileMu serialises every remote file mutation so two saves cannot race on
	// the shared client's server ID. See filesafe.go.
	store       *safestore.Store
	fileMu      sync.Mutex
	planMu      sync.Mutex
	deletePlans map[string]*DeletePlan
}

// NewApp creates a new App application struct
func NewApp() *App {
	return &App{}
}

// startup is called when the app starts
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.serverPanelMap = make(map[string]string)
	a.deletePlans = make(map[string]*DeletePlan)

	// The local copy store. Without it every write and delete is refused
	// rather than performed unprotected.
	if home, homeErr := os.UserHomeDir(); homeErr == nil {
		store, storeErr := safestore.New(filepath.Join(home, ".pteroclient"))
		if storeErr != nil {
			runtime.LogError(a.ctx, "Failed to open the local file store: "+storeErr.Error())
		} else {
			a.store = store
		}
	} else {
		runtime.LogError(a.ctx, "Failed to locate the home directory: "+homeErr.Error())
	}
	
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
		// Initialize server mappings for all panels
		a.RefreshAllServerMappings()
	}
}

// Connect to Pterodactyl server
func (a *App) Connect() error {
	panel := a.config.GetActivePanel()
	if panel == nil {
		return fmt.Errorf("no active panel")
	}
	
	// Validate panel URL
	if panel.PanelURL == "" {
		return fmt.Errorf("panel URL is empty")
	}
	
	// Ensure URL has protocol
	panelURL := panel.PanelURL
	if !strings.HasPrefix(panelURL, "http://") && !strings.HasPrefix(panelURL, "https://") {
		// Default to https if no protocol specified
		panelURL = "https://" + panelURL
	}
	
	// Log for debugging (only if context is set)
	if a.ctx != nil {
		runtime.LogInfo(a.ctx, fmt.Sprintf("[CONNECT] Connecting to panel: %s (Name: %s)", panelURL, panel.Name))
		runtime.LogInfo(a.ctx, fmt.Sprintf("[CONNECT] API Key: %s...%s", panel.APIKey[:8], panel.APIKey[len(panel.APIKey)-4:]))
	}
	
	// Create client with empty server ID initially if not set
	serverID := panel.ServerID
	if serverID == "" {
		// Use empty string, will need to select server from dropdown
		serverID = ""
	}
	
	if a.ctx != nil {
		runtime.LogInfo(a.ctx, fmt.Sprintf("[CONNECT] Server ID: %s", serverID))
	}
	
	// Close existing clients to release connections
	if a.client != nil {
		if a.ctx != nil {
			runtime.LogInfo(a.ctx, "[CONNECT] Closing existing client connection")
		}
		a.client.Close()
		a.client = nil
	}
	if a.adminClient != nil {
		if a.ctx != nil {
			runtime.LogInfo(a.ctx, "[CONNECT] Closing existing admin client connection")
		}
		a.adminClient.Close()
		a.adminClient = nil
	}
	
	// Create new client API client (for file operations). Replacing the client
	// wholesale is the most disruptive thing that can happen to an in-flight
	// file operation, so it waits for one to finish.
	if a.ctx != nil {
		runtime.LogInfo(a.ctx, "[CONNECT] Creating new client")
	}
	a.fileMu.Lock()
	a.client = pterodactyl.NewClient(panelURL, panel.APIKey, serverID)
	a.fileMu.Unlock()
	
	// Create admin API client if admin key is provided (for listing all servers)
	if panel.AdminKey != "" {
		a.adminClient = pterodactyl.NewClient(panelURL, panel.AdminKey, "")
	} else {
		a.adminClient = nil
	}
	
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
	
	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, "connected", true)
	}
	return nil
}

// RefreshAllServerMappings refreshes server mappings from all configured panels
func (a *App) RefreshAllServerMappings() {
	// Clear existing mappings to avoid stale entries
	a.serverPanelMap = make(map[string]string)
	
	for _, panel := range a.config.GetPanels() {
		// Create temporary clients for each panel
		panelURL := panel.PanelURL
		if !strings.HasPrefix(panelURL, "http://") && !strings.HasPrefix(panelURL, "https://") {
			panelURL = "https://" + panelURL
		}
		
		// Use the panel's primary API key (which auto-detects if it's admin or client)
		tmpClient := pterodactyl.NewClient(panelURL, panel.APIKey, "")
		servers, err := tmpClient.ListServers()
		if err == nil {
			// Map all servers from this panel
			for _, s := range servers {
				a.serverPanelMap[s.ID] = panel.Name
			}
		}
	}
}

// ListServers lists all available servers
func (a *App) ListServers() ([]map[string]interface{}, error) {
	if a.client == nil {
		return nil, fmt.Errorf("not connected")
	}
	
	// Use admin client if available, otherwise use regular client
	var servers []pterodactyl.ServerInfo
	var err error
	
	if a.adminClient != nil {
		// Use admin API to list all servers
		servers, err = a.adminClient.ListServers()
	} else {
		// Use client API to list user's servers
		servers, err = a.client.ListServers()
	}
	
	if err != nil {
		return nil, err
	}
	
	// Map servers to the current panel
	currentPanel := a.config.GetActivePanelName()
	for _, s := range servers {
		a.serverPanelMap[s.ID] = currentPanel
	}
	
	result := make([]map[string]interface{}, len(servers))
	for i, s := range servers {
		result[i] = map[string]interface{}{
			"id":          s.ID,
			"name":        s.Name,
			"description": s.Description,
			"isOwner":     s.IsOwner,
			"status":      s.Status,
			"isAdmin":     a.adminClient != nil && a.adminClient.IsAdmin(),
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
	
	// Check if we're switching to a server on a different panel
	if panelName, ok := a.serverPanelMap[serverID]; ok {
		if panelName != a.config.GetActivePanelName() {
			// Server is on a different panel, switch to that panel first
			if err := a.SwitchPanel(panelName); err != nil {
				return fmt.Errorf("failed to switch to panel %s: %v", panelName, err)
			}
		}
	}
	
	// Update client server ID. Under fileMu: switching servers out from under
	// an in-flight save is the same hazard as browsing during one.
	a.fileMu.Lock()
	a.client.SetServerID(serverID)
	a.fileMu.Unlock()

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

// fileInfoToMap is the shape the file tree reads.
func fileInfoToMap(files []pterodactyl.FileInfo) []map[string]interface{} {
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
	return result
}

// ListFilesFromServer lists files on a specific server without disturbing the
// active one. Held under fileMu like every other client operation: it repoints
// the shared client, and doing that beside an in-flight save used to be able to
// redirect the save.
func (a *App) ListFilesFromServer(serverID string, path string) ([]map[string]interface{}, error) {
	cleaned, err := normalizeRemotePath(path)
	if err != nil {
		return nil, err
	}

	a.fileMu.Lock()
	defer a.fileMu.Unlock()

	var out []map[string]interface{}
	err = a.withServerClient(serverID, func(c *pterodactyl.Client, _ string) error {
		files, listErr := c.ListFiles(cleaned)
		if listErr != nil {
			return listErr
		}
		out = fileInfoToMap(files)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// GetFileContentFromServer reads a file on a specific server without
// disturbing the active one.
func (a *App) GetFileContentFromServer(serverID string, path string) (string, error) {
	cleaned, err := normalizeRemotePath(path)
	if err != nil {
		return "", err
	}

	a.fileMu.Lock()
	defer a.fileMu.Unlock()

	var content string
	err = a.withServerClient(serverID, func(c *pterodactyl.Client, _ string) error {
		got, readErr := c.GetFileContent(cleaned)
		if readErr != nil {
			return friendlyFileError(readErr, cleaned, serverID)
		}
		content = got
		return nil
	})
	if err != nil {
		return "", err
	}
	return content, nil
}

// friendlyFileError turns the panel's status codes into something the UI can
// show without the reader having to know HTTP.
func friendlyFileError(err error, path, serverID string) error {
	if err == nil {
		return nil
	}
	switch {
	case strings.Contains(err.Error(), "status 500"):
		return fmt.Errorf("daemon connection error: the server daemon may be offline or experiencing issues")
	case strings.Contains(err.Error(), "status 404"):
		return fmt.Errorf("file not found: %s (server: %s)", path, serverID)
	case strings.Contains(err.Error(), "status 403"):
		return fmt.Errorf("permission denied: cannot access this file")
	}
	return err
}

// SaveFileContentToServer saves file content to a specific server without
// switching the active server.
//
// It routes through the safe path in filesafe.go, so the remote bytes are
// copied locally before they are replaced. force is implied here because this
// entry point carries no baseline to compare against; callers that have one
// should use SafeSaveFileContentToServer and get conflict detection too.
func (a *App) SaveFileContentToServer(serverID string, path string, content string) error {
	_, err := a.SafeSaveFileContentToServer(serverID, path, content, "", true)
	return err
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
	if a.ctx != nil {
		runtime.LogInfo(a.ctx, fmt.Sprintf("[SWITCH_PANEL] Switching from %s to %s", a.config.GetActivePanelName(), panelName))
	}
	
	// Disconnect console if connected
	if a.consoleWS != nil && a.consoleWS.IsConnected() {
		a.consoleWS.Close()
		if a.ctx != nil {
			runtime.EventsEmit(a.ctx, "console-connected", false)
		}
	}
	
	// Set the active panel
	if err := a.config.SetActivePanel(panelName); err != nil {
		if a.ctx != nil {
			runtime.LogError(a.ctx, fmt.Sprintf("[SWITCH_PANEL] Failed to set active panel: %v", err))
		}
		return err
	}
	
	if a.ctx != nil {
		runtime.LogInfo(a.ctx, "[SWITCH_PANEL] Active panel set, reconnecting...")
	}
	
	// Reconnect with new panel
	if err := a.Connect(); err != nil {
		if a.ctx != nil {
			runtime.LogError(a.ctx, fmt.Sprintf("[SWITCH_PANEL] Failed to connect: %v", err))
		}
		return err
	}
	
	if a.ctx != nil {
		runtime.LogInfo(a.ctx, "[SWITCH_PANEL] Connected, refreshing server mappings...")
	}
	
	// Refresh server mappings for all panels
	a.RefreshAllServerMappings()
	
	if a.ctx != nil {
		runtime.LogInfo(a.ctx, fmt.Sprintf("[SWITCH_PANEL] Switch complete. Client state - URL: %s, ServerID: %s", a.client.GetBaseURL(), a.client.GetServerID()))
	}
	
	// Emit panel changed event
	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, "panel-changed", panelName)
	}
	
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
	
	if a.ctx != nil {
		runtime.LogInfo(a.ctx, fmt.Sprintf("[LIST_FILES] Called with path: %s", path))
		runtime.LogInfo(a.ctx, fmt.Sprintf("[LIST_FILES] Current client state - URL: %s, ServerID: %s", a.client.GetBaseURL(), a.client.GetServerID()))
		runtime.LogInfo(a.ctx, fmt.Sprintf("[LIST_FILES] Active panel: %s", a.config.GetActivePanelName()))
	}
	
	// Under fileMu so the listing cannot read a client that a save has
	// temporarily aimed at another server.
	a.fileMu.Lock()
	files, err := a.client.ListFiles(path)
	a.fileMu.Unlock()

	if err != nil {
		if a.ctx != nil {
			runtime.LogError(a.ctx, fmt.Sprintf("[LIST_FILES] Error: %v", err))
		}
		return nil, err
	}

	return fileInfoToMap(files), nil
}

// GetFileContent gets file content
func (a *App) GetFileContent(path string) (string, error) {
	if a.client == nil {
		return "", fmt.Errorf("not connected")
	}
	
	if a.ctx != nil {
		runtime.LogInfo(a.ctx, fmt.Sprintf("[GET_FILE] Called with path: %s", path))
		runtime.LogInfo(a.ctx, fmt.Sprintf("[GET_FILE] Current client state - URL: %s, ServerID: %s", a.client.GetBaseURL(), a.client.GetServerID()))
		runtime.LogInfo(a.ctx, fmt.Sprintf("[GET_FILE] Active panel: %s", a.config.GetActivePanelName()))
	}
	
	a.fileMu.Lock()
	content, err := a.client.GetFileContent(path)
	a.fileMu.Unlock()

	if err != nil {
		if a.ctx != nil {
			runtime.LogError(a.ctx, fmt.Sprintf("[GET_FILE] Error: %v", err))
		}
		return "", err
	}
	
	if a.ctx != nil {
		runtime.LogInfo(a.ctx, fmt.Sprintf("[GET_FILE] Successfully retrieved file (length: %d)", len(content)))
	}
	
	return content, nil
}

// SaveFileContent saves file content to the active server.
//
// Like SaveFileContentToServer this always takes a local copy of what it is
// about to replace. Prefer SafeSaveFileContent, which also refuses to write
// over a file that changed on the panel since it was opened.
func (a *App) SaveFileContent(path, content string) error {
	_, err := a.SafeSaveFileContent(path, content, "", true)
	return err
}

// CreateFolder creates a new folder. It refuses a name that is already taken
// rather than letting the panel decide what that means.
func (a *App) CreateFolder(path string) error {
	return a.CreateFolderStrict(path)
}

// DeleteFiles is deliberately no longer a delete.
//
// Deleting is a two-step handshake now: PlanDelete lists exactly what would go
// and returns a token bound to that path set, and SafeDeleteFiles executes it
// after copying everything into the local recycle bin. This stub stays so a
// caller that missed the change fails loudly instead of removing files.
func (a *App) DeleteFiles(paths []string) error {
	return fmt.Errorf("direct deletes are disabled: call PlanDelete to review the selection, then SafeDeleteFiles with the plan token")
}

// RenameFile renames a file or folder within its directory. It refuses to
// land on an existing name, which the panel would otherwise silently replace.
func (a *App) RenameFile(oldPath, newPath string) error {
	oldDir, _, err := splitRemote(oldPath)
	if err != nil {
		return err
	}
	newDir, newName, err := splitRemote(newPath)
	if err != nil {
		return err
	}
	// Moving between directories is a different operation with a different
	// failure mode. Refuse it rather than silently renaming in place, which is
	// what this did before.
	if oldDir != newDir {
		return fmt.Errorf("cannot move %s to %s: renaming only works within one directory", oldDir, newDir)
	}
	return a.RenameFileStrict(oldPath, newName)
}

// UploadFile writes an uploaded file into a directory.
//
// An upload that lands on an existing name replaces it, so the file being
// replaced is copied locally first — the same guarantee the editor's save has.
// overwrite must be true for that to happen at all.
func (a *App) UploadFile(path string, content []byte) error {
	return a.UploadFileSafe(path, content, false)
}

// UploadFileSafe is UploadFile with an explicit answer for the "a file with
// that name is already there" case.
func (a *App) UploadFileSafe(remotePath string, content []byte, overwrite bool) error {
	dir, filename, err := splitRemote(remotePath)
	if err != nil {
		return err
	}

	a.fileMu.Lock()
	defer a.fileMu.Unlock()

	return a.withServerClient("", func(c *pterodactyl.Client, panel string) error {
		files, listErr := c.ListFiles(dir)
		if listErr != nil {
			return fmt.Errorf("cannot list %s before uploading: %w", dir, listErr)
		}

		for i := range files {
			// Exact match, not EqualFold: the panel's filesystem is case
			// sensitive, so Config.yml and config.yml are two files. Matching
			// loosely here backed up the wrong one and left the one actually
			// being replaced uncopied.
			if files[i].Name != filename {
				continue
			}
			if !overwrite {
				return fmt.Errorf("%s already exists in %s — confirm the replacement to continue", filename, dir)
			}
			if !files[i].IsFile {
				return fmt.Errorf("%s in %s is a directory; refusing to replace it with a file", filename, dir)
			}
			// The file being replaced goes to the recycle bin: an upload swaps
			// the whole thing out, which is a removal, not an edit.
			if _, captureErr := a.captureRemote(safestore.KindBin, c, panel, c.GetServerID(),
				joinRemote(dir, files[i].Name), "replaced by an upload", files[i].Size); captureErr != nil {
				return fmt.Errorf("refusing to upload: keeping a copy of the file it would replace failed: %w", captureErr)
			}
			break
		}

		return c.UploadFile(dir, filename, strings.NewReader(string(content)))
	})
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
