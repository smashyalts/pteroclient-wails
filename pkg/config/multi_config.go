package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// PanelConfig represents configuration for a single panel
type PanelConfig struct {
	Name     string `json:"name"`
	PanelURL string `json:"panel_url"`
	APIKey   string `json:"api_key"`
	AdminKey string `json:"admin_key,omitempty"` // Optional admin API key for listing all servers
	ServerID string `json:"server_id,omitempty"`
}

// MultiConfig represents the multi-panel configuration
type MultiConfig struct {
	Panels      []PanelConfig `json:"panels"`
	ActivePanel string        `json:"active_panel"`
	// RecycleBin is per-server: true keeps a local copy of everything a delete
	// takes away, false deletes straight through. Servers with no entry follow
	// RecycleBinDefault.
	RecycleBin map[string]bool `json:"recycle_bin,omitempty"`
	// RecycleBinDefault applies to a server nobody has set. Nil means on,
	// which is what it was before this existed.
	RecycleBinDefault *bool `json:"recycle_bin_default,omitempty"`
	// App holds the settings that are about how the app behaves rather than
	// about any one panel. Strings so that adding one does not need a
	// migration; the app decides what each means and what a missing one
	// defaults to.
	App map[string]string `json:"app,omitempty"`
	// Legacy fields for backward compatibility
	LegacyPanelURL string `json:"panel_url,omitempty"`
	LegacyAPIKey   string `json:"api_key,omitempty"`
	LegacyServerID string `json:"server_id,omitempty"`
}

// MultiConfigManager manages multi-panel configuration.
//
// Guarded: every exported method here is reachable from a Wails binding, and
// Wails runs each of those on its own goroutine. The panel slice was already
// being read and rewritten concurrently, and the per-server map added below
// would have turned that into a process abort rather than a stale read.
type MultiConfigManager struct {
	mu         sync.RWMutex
	configPath string
	config     *MultiConfig
}

// NewMultiConfigManager creates a new multi-panel configuration manager
func NewMultiConfigManager() (*MultiConfigManager, error) {
	// Get user's config directory
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get home directory: %w", err)
	}

	configDir := filepath.Join(homeDir, ".pteroclient")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create config directory: %w", err)
	}

	configPath := filepath.Join(configDir, "config.json")

	mcm := &MultiConfigManager{
		configPath: configPath,
		config:     &MultiConfig{Panels: []PanelConfig{}},
	}

	// Try to load existing configuration
	_ = mcm.Load()

	return mcm, nil
}

// Load loads configuration from file
func (mcm *MultiConfigManager) Load() error {
	mcm.mu.Lock()
	defer mcm.mu.Unlock()
	data, err := os.ReadFile(mcm.configPath)
	if err != nil {
		if os.IsNotExist(err) {
			// Config file doesn't exist yet, that's okay
			return nil
		}
		return fmt.Errorf("failed to read config file: %w", err)
	}

	// First try to unmarshal as MultiConfig
	var multiConfig MultiConfig
	if err := json.Unmarshal(data, &multiConfig); err == nil {
		// Check if this is a legacy config
		if len(multiConfig.Panels) == 0 && multiConfig.LegacyPanelURL != "" {
			// Migrate legacy config
			multiConfig.Panels = []PanelConfig{
				{
					Name:     "Default",
					PanelURL: multiConfig.LegacyPanelURL,
					APIKey:   multiConfig.LegacyAPIKey,
					ServerID: multiConfig.LegacyServerID,
				},
			}
			multiConfig.ActivePanel = "Default"
			multiConfig.LegacyPanelURL = ""
			multiConfig.LegacyAPIKey = ""
			multiConfig.LegacyServerID = ""
		}
		mcm.config = &multiConfig
	} else {
		// Try to unmarshal as legacy Config
		var legacyConfig Config
		if err := json.Unmarshal(data, &legacyConfig); err == nil {
			// Migrate from legacy format
			mcm.config = &MultiConfig{
				Panels: []PanelConfig{
					{
						Name:     "Default",
						PanelURL: legacyConfig.PanelURL,
						APIKey:   legacyConfig.APIKey,
						ServerID: legacyConfig.ServerID,
					},
				},
				ActivePanel: "Default",
			}
			// Save in new format
			_ = mcm.saveLocked()
		} else {
			return fmt.Errorf("failed to parse config file: %w", err)
		}
	}

	return nil
}

// Save saves configuration to file
func (mcm *MultiConfigManager) Save() error {
	mcm.mu.Lock()
	defer mcm.mu.Unlock()
	return mcm.saveLocked()
}

// saveLocked is Save for a caller that already holds the write lock.
func (mcm *MultiConfigManager) saveLocked() error {
	data, err := json.MarshalIndent(mcm.config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.WriteFile(mcm.configPath, data, 0600); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}

// GetActivePanel returns the active panel configuration
func (mcm *MultiConfigManager) GetActivePanel() *PanelConfig {
	mcm.mu.RLock()
	defer mcm.mu.RUnlock()
	return mcm.activePanelLocked()
}

// activePanelLocked is GetActivePanel for a caller already holding the lock.
func (mcm *MultiConfigManager) activePanelLocked() *PanelConfig {
	if mcm.config == nil || mcm.config.ActivePanel == "" {
		return nil
	}

	for i := range mcm.config.Panels {
		if mcm.config.Panels[i].Name == mcm.config.ActivePanel {
			return &mcm.config.Panels[i]
		}
	}

	// If active panel not found but panels exist, use first one
	if len(mcm.config.Panels) > 0 {
		mcm.config.ActivePanel = mcm.config.Panels[0].Name
		return &mcm.config.Panels[0]
	}

	return nil
}

// AddOrUpdatePanel adds or updates a panel configuration
func (mcm *MultiConfigManager) AddOrUpdatePanel(panel PanelConfig) error {
	mcm.mu.Lock()
	defer mcm.mu.Unlock()
	if mcm.config == nil {
		mcm.config = &MultiConfig{
			Panels: []PanelConfig{},
		}
	}

	// Check if panel with same name exists
	for i, p := range mcm.config.Panels {
		if p.Name == panel.Name {
			// Update, not replace. Assigning the incoming struct wholesale
			// dropped whatever the caller left blank — which meant re-saving a
			// panel to correct its URL also forgot which server was selected.
			// Blank means "keep"; ClearAdminKey is how one is removed.
			if panel.APIKey == "" {
				panel.APIKey = p.APIKey
			}
			if panel.AdminKey == "" {
				panel.AdminKey = p.AdminKey
			}
			if panel.ServerID == "" {
				panel.ServerID = p.ServerID
			}
			mcm.config.Panels[i] = panel
			return mcm.saveLocked()
		}
	}

	// Add new panel
	mcm.config.Panels = append(mcm.config.Panels, panel)

	// If this is the first panel, make it active
	if len(mcm.config.Panels) == 1 {
		mcm.config.ActivePanel = panel.Name
	}

	return mcm.saveLocked()
}

// FindPanel returns the stored panel with this name, or nil.
func (mcm *MultiConfigManager) FindPanel(name string) *PanelConfig {
	mcm.mu.RLock()
	defer mcm.mu.RUnlock()
	return mcm.findPanelLocked(name)
}

// findPanelLocked is FindPanel for a caller that already holds the lock.
// RWMutex is not reentrant, so taking the read lock while holding the write
// lock deadlocks rather than merely being slow.
func (mcm *MultiConfigManager) findPanelLocked(name string) *PanelConfig {
	if mcm.config == nil {
		return nil
	}
	for i := range mcm.config.Panels {
		if mcm.config.Panels[i].Name == name {
			return &mcm.config.Panels[i]
		}
	}
	return nil
}

// ClearAdminKey drops a panel's admin key. AddOrUpdatePanel treats a blank
// admin key as "keep the stored one", so this is the only way to remove it.
func (mcm *MultiConfigManager) ClearAdminKey(name string) error {
	mcm.mu.Lock()
	defer mcm.mu.Unlock()

	panel := mcm.findPanelLocked(name)
	if panel == nil {
		return fmt.Errorf("panel not found: %s", name)
	}
	panel.AdminKey = ""
	return mcm.saveLocked()
}

// RemovePanel removes a panel configuration
func (mcm *MultiConfigManager) RemovePanel(name string) error {
	mcm.mu.Lock()
	defer mcm.mu.Unlock()
	if mcm.config == nil {
		return nil
	}

	var newPanels []PanelConfig
	for _, p := range mcm.config.Panels {
		if p.Name != name {
			newPanels = append(newPanels, p)
		}
	}

	mcm.config.Panels = newPanels

	// If we removed the active panel, select another
	if mcm.config.ActivePanel == name {
		mcm.config.ActivePanel = ""
		if len(mcm.config.Panels) > 0 {
			mcm.config.ActivePanel = mcm.config.Panels[0].Name
		}
	}

	return mcm.saveLocked()
}

// SetActivePanel sets the active panel
func (mcm *MultiConfigManager) SetActivePanel(name string) error {
	mcm.mu.Lock()
	defer mcm.mu.Unlock()
	if mcm.config == nil {
		return fmt.Errorf("config not initialized")
	}

	// Check if panel exists
	for _, p := range mcm.config.Panels {
		if p.Name == name {
			mcm.config.ActivePanel = name
			return mcm.saveLocked()
		}
	}

	return fmt.Errorf("panel not found: %s", name)
}

// GetPanels returns all panel configurations
func (mcm *MultiConfigManager) GetPanels() []PanelConfig {
	mcm.mu.RLock()
	defer mcm.mu.RUnlock()
	if mcm.config == nil {
		return []PanelConfig{}
	}
	return mcm.config.Panels
}

// GetActivePanelName returns the name of the active panel
func (mcm *MultiConfigManager) GetActivePanelName() string {
	mcm.mu.RLock()
	defer mcm.mu.RUnlock()
	if mcm.config == nil {
		return ""
	}
	return mcm.config.ActivePanel
}

// IsConfigured checks if at least one panel is configured
func (mcm *MultiConfigManager) IsConfigured() bool {
	mcm.mu.RLock()
	defer mcm.mu.RUnlock()

	if mcm.config == nil || len(mcm.config.Panels) == 0 {
		return false
	}

	panel := mcm.activePanelLocked()
	return panel != nil && panel.PanelURL != "" && panel.APIKey != ""
}

// UpdateActivePanelServer updates the server ID for the active panel
func (mcm *MultiConfigManager) UpdateActivePanelServer(serverID string) error {
	mcm.mu.Lock()
	defer mcm.mu.Unlock()

	panel := mcm.activePanelLocked()
	if panel == nil {
		return fmt.Errorf("no active panel")
	}

	// Update the panel in the list
	for i := range mcm.config.Panels {
		if mcm.config.Panels[i].Name == panel.Name {
			mcm.config.Panels[i].ServerID = serverID
			return mcm.saveLocked()
		}
	}

	return fmt.Errorf("active panel not found in list")
}

// Backward compatibility wrapper
func (mcm *MultiConfigManager) GetConfig() *Config {
	mcm.mu.RLock()
	defer mcm.mu.RUnlock()

	panel := mcm.activePanelLocked()
	if panel == nil {
		return &Config{}
	}
	return &Config{
		PanelURL: panel.PanelURL,
		APIKey:   panel.APIKey,
		ServerID: panel.ServerID,
	}
}

// Backward compatibility wrapper
func (mcm *MultiConfigManager) SetConfig(config *Config) {
	mcm.mu.Lock()
	defer mcm.mu.Unlock()
	if config == nil {
		return
	}

	// Update or add a "Default" panel
	panel := PanelConfig{
		Name:     "Default",
		PanelURL: config.PanelURL,
		APIKey:   config.APIKey,
		ServerID: config.ServerID,
	}

	mcm.AddOrUpdatePanel(panel)
	mcm.SetActivePanel("Default")
}

// ---------------------------------------------------- per-server recycle bin

// RecycleBinEnabled reports whether deletes on this server keep a local copy.
// Unset servers follow the default, which is on.
func (mcm *MultiConfigManager) RecycleBinEnabled(serverID string) bool {
	mcm.mu.RLock()
	defer mcm.mu.RUnlock()

	if mcm.config == nil {
		return true
	}
	if on, set := mcm.config.RecycleBin[serverID]; set {
		return on
	}
	if mcm.config.RecycleBinDefault != nil {
		return *mcm.config.RecycleBinDefault
	}
	return true
}

// RecycleBinDefaultEnabled reports the setting for servers nobody has set.
func (mcm *MultiConfigManager) RecycleBinDefaultEnabled() bool {
	mcm.mu.RLock()
	defer mcm.mu.RUnlock()

	if mcm.config == nil || mcm.config.RecycleBinDefault == nil {
		return true
	}
	return *mcm.config.RecycleBinDefault
}

// RecycleBinOverrides returns the servers that have been set explicitly.
func (mcm *MultiConfigManager) RecycleBinOverrides() map[string]bool {
	mcm.mu.RLock()
	defer mcm.mu.RUnlock()

	out := map[string]bool{}
	for id, on := range mcm.config.RecycleBin {
		out[id] = on
	}
	return out
}

// SetRecycleBinEnabled sets one server's policy.
func (mcm *MultiConfigManager) SetRecycleBinEnabled(serverID string, on bool) error {
	if serverID == "" {
		return fmt.Errorf("no server given")
	}

	mcm.mu.Lock()
	defer mcm.mu.Unlock()

	if mcm.config == nil {
		return fmt.Errorf("config not initialized")
	}
	if mcm.config.RecycleBin == nil {
		mcm.config.RecycleBin = map[string]bool{}
	}
	mcm.config.RecycleBin[serverID] = on
	return mcm.saveLocked()
}

// ---------------------------------------------------------- app settings

// AppSetting reads one setting, or fallback when it has never been set.
func (mcm *MultiConfigManager) AppSetting(key, fallback string) string {
	mcm.mu.RLock()
	defer mcm.mu.RUnlock()

	if mcm.config == nil || mcm.config.App == nil {
		return fallback
	}
	if value, ok := mcm.config.App[key]; ok && value != "" {
		return value
	}
	return fallback
}

// AppSettings returns everything that has been set.
func (mcm *MultiConfigManager) AppSettings() map[string]string {
	mcm.mu.RLock()
	defer mcm.mu.RUnlock()

	out := map[string]string{}
	if mcm.config == nil {
		return out
	}
	for key, value := range mcm.config.App {
		out[key] = value
	}
	return out
}

// SetAppSetting stores one. An empty value removes it, so a setting can be put
// back to its default rather than only to another value.
func (mcm *MultiConfigManager) SetAppSetting(key, value string) error {
	if key == "" {
		return fmt.Errorf("no setting named")
	}

	mcm.mu.Lock()
	defer mcm.mu.Unlock()

	if mcm.config == nil {
		return fmt.Errorf("config not initialized")
	}
	if mcm.config.App == nil {
		mcm.config.App = map[string]string{}
	}
	if value == "" {
		delete(mcm.config.App, key)
	} else {
		mcm.config.App[key] = value
	}
	return mcm.saveLocked()
}

// ClearRecycleBinOverride puts one server back on the default.
func (mcm *MultiConfigManager) ClearRecycleBinOverride(serverID string) error {
	mcm.mu.Lock()
	defer mcm.mu.Unlock()

	if mcm.config == nil {
		return fmt.Errorf("config not initialized")
	}
	delete(mcm.config.RecycleBin, serverID)
	return mcm.saveLocked()
}

// SetRecycleBinDefault sets the policy for servers nobody has set.
func (mcm *MultiConfigManager) SetRecycleBinDefault(on bool) error {
	mcm.mu.Lock()
	defer mcm.mu.Unlock()

	if mcm.config == nil {
		return fmt.Errorf("config not initialized")
	}
	mcm.config.RecycleBinDefault = &on
	return mcm.saveLocked()
}
