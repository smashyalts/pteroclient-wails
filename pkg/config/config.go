package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Config represents the application configuration
type Config struct {
	PanelURL string `json:"panel_url"`
	APIKey   string `json:"api_key"`
	ServerID string `json:"server_id"`
}

// ConfigManager manages application configuration
type ConfigManager struct {
	configPath string
	config     *Config
}

// NewConfigManager creates a new configuration manager
func NewConfigManager() (*ConfigManager, error) {
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
	
	cm := &ConfigManager{
		configPath: configPath,
		config:     &Config{},
	}

	// Try to load existing configuration
	_ = cm.Load()

	return cm, nil
}

// Load loads configuration from file
func (cm *ConfigManager) Load() error {
	data, err := os.ReadFile(cm.configPath)
	if err != nil {
		if os.IsNotExist(err) {
			// Config file doesn't exist yet, that's okay
			return nil
		}
		return fmt.Errorf("failed to read config file: %w", err)
	}

	if err := json.Unmarshal(data, cm.config); err != nil {
		return fmt.Errorf("failed to parse config file: %w", err)
	}

	return nil
}

// Save saves configuration to file
func (cm *ConfigManager) Save() error {
	data, err := json.MarshalIndent(cm.config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	if err := os.WriteFile(cm.configPath, data, 0600); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}

// GetConfig returns the current configuration
func (cm *ConfigManager) GetConfig() *Config {
	return cm.config
}

// SetConfig sets the configuration
func (cm *ConfigManager) SetConfig(config *Config) {
	cm.config = config
}

// IsConfigured checks if the configuration is valid
func (cm *ConfigManager) IsConfigured() bool {
	return cm.config != nil &&
		cm.config.PanelURL != "" &&
		cm.config.APIKey != "" &&
		cm.config.ServerID != ""
}
