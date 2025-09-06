package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// PanelConfig represents configuration for a single panel
type PanelConfig struct {
	Name     string `json:"name"`
	PanelURL string `json:"panel_url"`
	APIKey   string `json:"api_key"`
	ServerID string `json:"server_id,omitempty"`
}

// MultiConfig represents the multi-panel configuration
type MultiConfig struct {
	Panels      []PanelConfig `json:"panels"`
	ActivePanel string        `json:"active_panel"`
	// Legacy fields for backward compatibility
	LegacyPanelURL string `json:"panel_url,omitempty"`
	LegacyAPIKey   string `json:"api_key,omitempty"`
	LegacyServerID string `json:"server_id,omitempty"`
}

// MultiConfigManager manages multi-panel configuration
type MultiConfigManager struct {
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
			mcm.Save()
		} else {
			return fmt.Errorf("failed to parse config file: %w", err)
		}
	}

	return nil
}

// Save saves configuration to file
func (mcm *MultiConfigManager) Save() error {
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
	if mcm.config == nil {
		mcm.config = &MultiConfig{
			Panels: []PanelConfig{},
		}
	}
	
	// Check if panel with same name exists
	for i, p := range mcm.config.Panels {
		if p.Name == panel.Name {
			// Update existing panel
			mcm.config.Panels[i] = panel
			return mcm.Save()
		}
	}
	
	// Add new panel
	mcm.config.Panels = append(mcm.config.Panels, panel)
	
	// If this is the first panel, make it active
	if len(mcm.config.Panels) == 1 {
		mcm.config.ActivePanel = panel.Name
	}
	
	return mcm.Save()
}

// RemovePanel removes a panel configuration
func (mcm *MultiConfigManager) RemovePanel(name string) error {
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
	
	return mcm.Save()
}

// SetActivePanel sets the active panel
func (mcm *MultiConfigManager) SetActivePanel(name string) error {
	if mcm.config == nil {
		return fmt.Errorf("config not initialized")
	}
	
	// Check if panel exists
	for _, p := range mcm.config.Panels {
		if p.Name == name {
			mcm.config.ActivePanel = name
			return mcm.Save()
		}
	}
	
	return fmt.Errorf("panel not found: %s", name)
}

// GetPanels returns all panel configurations
func (mcm *MultiConfigManager) GetPanels() []PanelConfig {
	if mcm.config == nil {
		return []PanelConfig{}
	}
	return mcm.config.Panels
}

// GetActivePanelName returns the name of the active panel
func (mcm *MultiConfigManager) GetActivePanelName() string {
	if mcm.config == nil {
		return ""
	}
	return mcm.config.ActivePanel
}

// IsConfigured checks if at least one panel is configured
func (mcm *MultiConfigManager) IsConfigured() bool {
	if mcm.config == nil || len(mcm.config.Panels) == 0 {
		return false
	}
	
	panel := mcm.GetActivePanel()
	return panel != nil && panel.PanelURL != "" && panel.APIKey != ""
}

// UpdateActivePanelServer updates the server ID for the active panel
func (mcm *MultiConfigManager) UpdateActivePanelServer(serverID string) error {
	panel := mcm.GetActivePanel()
	if panel == nil {
		return fmt.Errorf("no active panel")
	}
	
	// Update the panel in the list
	for i := range mcm.config.Panels {
		if mcm.config.Panels[i].Name == panel.Name {
			mcm.config.Panels[i].ServerID = serverID
			return mcm.Save()
		}
	}
	
	return fmt.Errorf("active panel not found in list")
}

// Backward compatibility wrapper
func (mcm *MultiConfigManager) GetConfig() *Config {
	panel := mcm.GetActivePanel()
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
