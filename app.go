package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	"pteroclient-wails/pkg/config"
	"pteroclient-wails/pkg/pterodactyl"
	"pteroclient-wails/pkg/safestore"
)

// App struct
type App struct {
	ctx            context.Context
	config         *config.MultiConfigManager
	client         *pterodactyl.Client // Client API for file operations
	adminClient    *pterodactyl.Client // Admin API for server listing (optional)
	consoleWS      *pterodactyl.ConsoleWebSocket
	serverPanelMap map[string]string // Maps server ID to panel name

	// Local safety net. store holds the pre-write copies and the recycle bin;
	// fileMu serialises every remote file mutation so two saves cannot race on
	// the shared client's server ID. See filesafe.go.
	store       *safestore.Store
	fileMu      sync.Mutex
	planMu      sync.Mutex
	deletePlans map[string]*DeletePlan

	// One client per panel, so browsing another panel does not rebuild one —
	// and re-probe whether its key is an admin key — on every call. The
	// generation is bumped whenever credentials change; see invalidateClients.
	clientMu  sync.Mutex
	clients   map[string]*cachedClient
	clientGen int

	// Archives made so a folder could be dragged out of the window. They are
	// real files on somebody's server, so they are tracked and swept.
	dragMu       sync.Mutex
	dragArchives []dragArchive

	// Guards consoleWS: the read loop clears it from its own goroutine while
	// a binding call may be reading it.
	consoleMu sync.Mutex

	// One search at a time. Starting another cancels the one running, whose
	// workers would otherwise go on listing folders for a window that has
	// moved on.
	searchMu     sync.Mutex
	searchCancel func()
	searchRun    uint64

	// Set when this process was launched as a console window rather than the
	// main app. See main.go and OpenConsoleWindow.
	consoleOnly      bool
	consoleServerID  string
	consolePanelName string
}

// cachedClient is one panel's client and the config generation it was built
// against.
type cachedClient struct {
	client *pterodactyl.Client
	gen    int
}

// NewApp creates a new App application struct
func NewApp() *App {
	return &App{}
}

// startup is called when the app starts
// shutdown runs as the window closes. Anything this app made on somebody's
// server and has not cleaned up yet gets cleaned up here.
//
// Bounded: this blocks the window from closing, and a panel that has stopped
// answering must not mean an app that will not quit. Whatever is left behind
// is swept on the next run instead.
func (a *App) shutdown(ctx context.Context) {
	done := make(chan struct{})
	go func() {
		defer close(done)
		a.sweepDragArchives(true)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
	}
}

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

	// A console window was told which panel and server it is for; switching
	// before the first connect saves it connecting twice.
	if a.consoleOnly && a.consolePanelName != "" {
		_ = a.config.SetActivePanel(a.consolePanelName)
	}

	// Connect if we have an active configured panel
	if a.config.IsConfigured() {
		a.Connect()
		// Initialize server mappings for all panels
		a.RefreshAllServerMappings()

		if a.consoleOnly && a.consoleServerID != "" && a.client != nil {
			a.client.SetServerID(a.consoleServerID)
			_ = a.config.UpdateActivePanelServer(a.consoleServerID)
		}
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
// RefreshAllServerMappings rebuilds the server-to-panel map.
//
// Exported, so Wails runs it on its own goroutine — and every file operation
// reads this map under fileMu. Writing it unlocked was a concurrent map access
// that takes the whole process down, possibly part way through a delete.
func (a *App) RefreshAllServerMappings() {
	a.fileMu.Lock()
	defer a.fileMu.Unlock()
	a.refreshServerMappingsLocked()
}

// refreshServerMappingsLocked is the body of the above, for callers that
// already hold fileMu. withServerClient is one, so the exported form cannot be
// called from there without deadlocking.
func (a *App) refreshServerMappingsLocked() {
	// Clear existing mappings to avoid stale entries
	a.serverPanelMap = make(map[string]string)

	for _, panel := range a.config.GetPanels() {
		// The cached client, not a fresh one. Building one probes the panel to
		// find out whether its key is an admin key, and this paid for that on
		// every refresh and then leaked the client's connections.
		client, clientErr := a.clientFor(panel.Name)
		if clientErr != nil {
			continue
		}

		// Use the panel's primary API key (which auto-detects if it's admin or client)
		servers, err := client.ListServers()
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

	// Map servers to the current panel. Under fileMu: this map is read by
	// every file operation, and Wails calls this binding on its own goroutine.
	currentPanel := a.config.GetActivePanelName()
	a.fileMu.Lock()
	if a.serverPanelMap == nil {
		a.serverPanelMap = make(map[string]string)
	}
	for _, s := range servers {
		a.serverPanelMap[s.ID] = currentPanel
	}
	a.fileMu.Unlock()

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

	// Check if we're switching to a server on a different panel. Read under
	// the lock: RefreshAllServerMappings replaces this map wholesale.
	a.fileMu.Lock()
	panelName, ok := a.serverPanelMap[serverID]
	a.fileMu.Unlock()
	if ok && panelName != a.config.GetActivePanelName() {
		// Server is on a different panel, switch to that panel first
		if err := a.SwitchPanel(panelName); err != nil {
			return fmt.Errorf("failed to switch to panel %s: %v", panelName, err)
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
			// Whether one is set, never the key itself. The UI only needs to
			// show the badge and offer to clear it.
			"hasAdminKey": p.AdminKey != "",
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

	a.invalidateClients()

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

// ErrPanelSavedNotConnected prefixes the one failure the UI has to treat
// differently: the panel is on disk and will be there next launch, but talking
// to it did not work.
const ErrPanelSavedNotConnected = "panel saved, but connecting failed"

// AddPanel adds or updates a panel, and connects when that panel is the active
// one.
//
// It used to write the config and return. Nothing connected and nothing listed
// the servers, so the app showed a configured panel with no connection and an
// empty server picker until the next restart — while the dialog said the panel
// had been added successfully.
// adminKey is optional. A client key (ptlc_) only lists servers the account
// owns or was added to; an admin key (ptla_) lists every server on the panel,
// and Connect keeps it on a second client used solely for that listing. Both
// keys may be left blank when editing an existing panel, which keeps the ones
// already stored.
func (a *App) AddPanel(name, panelURL, apiKey, adminKey string) error {
	if name == "" || panelURL == "" {
		return fmt.Errorf("a display name and the panel URL are required")
	}

	// A panel being created has nothing to fall back on.
	if a.config.FindPanel(name) == nil && apiKey == "" {
		return fmt.Errorf("an API key is required for a new panel")
	}

	panel := config.PanelConfig{
		Name:     name,
		PanelURL: panelURL,
		APIKey:   apiKey,
		AdminKey: adminKey,
	}

	if err := a.config.AddOrUpdatePanel(panel); err != nil {
		return err
	}

	// Credentials may have changed; nothing may be served from the cache.
	a.invalidateClients()

	// Only the active panel drives the connection. Adding a second panel must
	// not drag the app off the one it is already working on.
	if a.config.GetActivePanelName() != name {
		return nil
	}

	if err := a.Connect(); err != nil {
		// The credentials stay saved so they can be corrected from the panel
		// list rather than retyped.
		return fmt.Errorf("%s: %w", ErrPanelSavedNotConnected, err)
	}

	a.RefreshAllServerMappings()
	return nil
}

// ClearPanelAdminKey drops a panel's admin key and reconnects if it is the
// active one, so the server list falls back to what the client key can see.
func (a *App) ClearPanelAdminKey(name string) error {
	if err := a.config.ClearAdminKey(name); err != nil {
		return err
	}
	a.invalidateClients()
	if a.config.GetActivePanelName() != name {
		return nil
	}
	if err := a.Connect(); err != nil {
		return fmt.Errorf("%s: %w", ErrPanelSavedNotConnected, err)
	}
	a.RefreshAllServerMappings()
	return nil
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

	if err := a.config.RemovePanel(name); err != nil {
		return err
	}
	// After the config, not before: until the panel is gone, clientFor could
	// still find it and rebuild the client this was meant to drop.
	a.invalidateClients()
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

	// The token the panel issues lasts about ten minutes. It warns a minute
	// before the end, and this hands back a fresh one so the socket is
	// re-authenticated in place rather than dropped.
	a.consoleWS.RefreshToken = func() (string, error) {
		if a.client == nil {
			return "", fmt.Errorf("not connected")
		}
		fresh, credErr := a.client.GetWebSocketCredentials()
		if credErr != nil {
			return "", credErr
		}
		return fresh.Token, nil
	}

	// However the socket ends, the UI hears about it. Without this the button
	// went on saying Disconnect over a console that had already expired.
	socket := a.consoleWS
	a.consoleWS.OnClose = func() {
		if a.ctx != nil {
			runtime.EventsEmit(a.ctx, "console-connected", false)
		}
		// Only for the socket this closure belongs to: a later connection has
		// its own, and must not be reported as dead by an older one.
		a.consoleMu.Lock()
		if a.consoleWS == socket {
			a.consoleWS = nil
		}
		a.consoleMu.Unlock()
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
	a.consoleMu.Lock()
	socket := a.consoleWS
	a.consoleWS = nil
	a.consoleMu.Unlock()

	if socket != nil {
		return socket.Close()
	}
	// Nothing open, but the UI may still think there is — say so plainly
	// rather than leaving it showing a button that does nothing.
	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, "console-connected", false)
	}
	return nil
}

// ConsoleConnected reports whether the console socket is actually live, for a
// UI that wants to check rather than remember.
func (a *App) ConsoleConnected() bool {
	a.consoleMu.Lock()
	socket := a.consoleWS
	a.consoleMu.Unlock()
	return socket != nil && socket.IsConnected()
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
