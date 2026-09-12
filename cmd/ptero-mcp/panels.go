package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"pteroclient-wails/pkg/pteroapi"
)

// PanelSpec is one configured panel.
type PanelSpec struct {
	Name string `json:"name"`
	URL  string `json:"url"`
	// APIKey is a client key (ptlc_...). An application key works for the
	// client routes it has been granted, but the client key is what these
	// tools are built around.
	APIKey string `json:"api_key"`
	// DefaultServer lets the server argument be omitted on every tool call.
	// Most self-hosters point this at the one server they care about.
	DefaultServer string `json:"default_server,omitempty"`
}

// Registry holds the configured panels and the clients built for them.
type Registry struct {
	mu      sync.Mutex
	order   []string
	panels  map[string]PanelSpec
	clients map[string]*pteroapi.Client
}

// NewRegistry builds a registry from panel specs. The first panel becomes the
// default for tool calls that name none.
func NewRegistry(specs []PanelSpec) (*Registry, error) {
	r := &Registry{
		panels:  make(map[string]PanelSpec, len(specs)),
		clients: make(map[string]*pteroapi.Client, len(specs)),
	}

	for _, spec := range specs {
		if spec.URL == "" || spec.APIKey == "" {
			continue // a half-filled entry is a typo, not a panel
		}
		name := spec.Name
		if name == "" {
			name = hostOf(spec.URL)
		}
		if _, clash := r.panels[name]; clash {
			return nil, fmt.Errorf("two panels are both named %q; names have to be unique", name)
		}
		spec.Name = name
		r.panels[name] = spec
		r.order = append(r.order, name)
	}

	if len(r.order) == 0 {
		return nil, fmt.Errorf("no usable panel found: every panel needs a url and an api_key")
	}
	return r, nil
}

// Names returns the configured panel names, in the order they were given.
func (r *Registry) Names() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.order...)
}

// Default returns the name used when a tool call names no panel.
func (r *Registry) Default() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.order[0]
}

// Resolve looks up a panel by name, falling back to the default.
//
// A name that does not match is an error listing what is configured, because
// the alternative — quietly acting on the default panel — could restart the
// wrong company's game server.
func (r *Registry) Resolve(name string) (*pteroapi.Client, PanelSpec, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if name == "" {
		name = r.order[0]
	}

	spec, found := r.panels[name]
	if !found {
		// Case-insensitive second pass: panel names are typed by a model
		// from a listing, and case is the one thing it gets wrong.
		for _, candidate := range r.order {
			if strings.EqualFold(candidate, name) {
				spec, found = r.panels[candidate], true
				name = candidate
				break
			}
		}
	}
	if !found {
		return nil, PanelSpec{}, fmt.Errorf("no panel named %q; configured panels are: %s",
			name, strings.Join(r.order, ", "))
	}

	client, built := r.clients[name]
	if !built {
		client = pteroapi.New(spec.URL, spec.APIKey)
		client.UserAgent = userAgent
		r.clients[name] = client
	}
	return client, spec, nil
}

// hostOf names a panel after its hostname when the config did not name it.
func hostOf(rawURL string) string {
	trimmed := strings.TrimPrefix(strings.TrimPrefix(rawURL, "https://"), "http://")
	if slash := strings.IndexByte(trimmed, '/'); slash > 0 {
		trimmed = trimmed[:slash]
	}
	if trimmed == "" {
		return "panel"
	}
	return trimmed
}

// configFile is the on-disk shape, written to accept both this server's own
// config and the desktop app's ~/.pteroclient/config.json unchanged — the
// panels are the same panels, and asking the user to retype their keys into a
// second file would be the only thing standing between them and a working
// setup.
type configFile struct {
	Panels []struct {
		Name string `json:"name"`
		// This server's spelling and the desktop app's, both accepted.
		URL      string `json:"url"`
		PanelURL string `json:"panel_url"`
		APIKey   string `json:"api_key"`
		// DefaultServer is ours; ServerID is the app's.
		DefaultServer string `json:"default_server"`
		ServerID      string `json:"server_id"`
	} `json:"panels"`

	DefaultPanel string `json:"default_panel"`
	ActivePanel  string `json:"active_panel"`

	// The app's pre-multi-panel layout, still written by old installs.
	LegacyPanelURL string `json:"panel_url"`
	LegacyAPIKey   string `json:"api_key"`
	LegacyServerID string `json:"server_id"`
}

func (f configFile) specs() []PanelSpec {
	out := make([]PanelSpec, 0, len(f.Panels)+1)
	for _, entry := range f.Panels {
		spec := PanelSpec{
			Name:          entry.Name,
			URL:           firstNonEmpty(entry.URL, entry.PanelURL),
			APIKey:        entry.APIKey,
			DefaultServer: firstNonEmpty(entry.DefaultServer, entry.ServerID),
		}
		out = append(out, spec)
	}
	if len(out) == 0 && f.LegacyPanelURL != "" {
		out = append(out, PanelSpec{
			Name:          "default",
			URL:           f.LegacyPanelURL,
			APIKey:        f.LegacyAPIKey,
			DefaultServer: f.LegacyServerID,
		})
	}

	// The panel the user last had active is the one they mean, so it goes
	// first and becomes the default.
	active := firstNonEmpty(f.DefaultPanel, f.ActivePanel)
	if active != "" {
		sort.SliceStable(out, func(i, j int) bool {
			return out[i].Name == active && out[j].Name != active
		})
	}
	return out
}

// loadPanels gathers panels from, in order of precedence: an explicit config
// path, PTERO_PANELS as inline JSON, the single-panel environment variables,
// this server's own config file, and finally the desktop app's config.
//
// Every source is read rather than the first that works, so a container can
// hold the keys in the environment while a workstation keeps them in a file,
// and a name given twice takes the higher-precedence copy.
func loadPanels(explicitPath string) ([]PanelSpec, []string, error) {
	var (
		collected []PanelSpec
		sources   []string
		seen      = map[string]bool{}
	)

	add := func(specs []PanelSpec, source string) {
		added := false
		for _, spec := range specs {
			if spec.URL == "" || spec.APIKey == "" {
				continue
			}
			name := spec.Name
			if name == "" {
				name = hostOf(spec.URL)
				spec.Name = name
			}
			if seen[strings.ToLower(name)] {
				continue // an earlier, higher-precedence source won
			}
			seen[strings.ToLower(name)] = true
			collected = append(collected, spec)
			added = true
		}
		if added {
			sources = append(sources, source)
		}
	}

	if explicitPath != "" {
		parsed, err := readConfigFile(explicitPath)
		if err != nil {
			// An explicitly named file that cannot be read is a mistake
			// worth stopping for, unlike the optional locations below.
			return nil, nil, err
		}
		add(parsed.specs(), explicitPath)
	}

	if inline := strings.TrimSpace(os.Getenv("PTERO_PANELS")); inline != "" {
		var parsed configFile
		// Accept both a whole config object and a bare array of panels.
		if strings.HasPrefix(inline, "[") {
			inline = "{\"panels\":" + inline + "}"
		}
		if err := json.Unmarshal([]byte(inline), &parsed); err != nil {
			return nil, nil, fmt.Errorf("PTERO_PANELS is not valid JSON: %w", err)
		}
		add(parsed.specs(), "PTERO_PANELS")
	}

	if url, key := os.Getenv("PTERO_PANEL_URL"), os.Getenv("PTERO_API_KEY"); url != "" && key != "" {
		add([]PanelSpec{{
			Name:          firstNonEmpty(os.Getenv("PTERO_PANEL_NAME"), hostOf(url)),
			URL:           url,
			APIKey:        key,
			DefaultServer: os.Getenv("PTERO_SERVER_ID"),
		}}, "environment")
	}

	if envPath := os.Getenv("PTERO_MCP_CONFIG"); envPath != "" && envPath != explicitPath {
		if parsed, err := readConfigFile(envPath); err == nil {
			add(parsed.specs(), envPath)
		} else {
			return nil, nil, err
		}
	}

	for _, candidate := range defaultConfigPaths() {
		if candidate == explicitPath {
			continue
		}
		parsed, err := readConfigFile(candidate)
		if err != nil {
			continue // optional location
		}
		add(parsed.specs(), candidate)
	}

	return collected, sources, nil
}

func defaultConfigPaths() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	return []string{
		filepath.Join(home, ".ptero-mcp", "panels.json"),
		// The desktop app's own config. Reading it means an existing
		// install needs no setup at all.
		filepath.Join(home, ".pteroclient", "config.json"),
	}
}

func readConfigFile(path string) (configFile, error) {
	var parsed configFile
	data, err := os.ReadFile(path)
	if err != nil {
		return parsed, err
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		return parsed, fmt.Errorf("%s is not valid JSON: %w", path, err)
	}
	return parsed, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
