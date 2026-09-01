package main

import (
	"fmt"
	"strings"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	"pteroclient-wails/pkg/pterodactyl"
)

// Bindings for the panel tabs that sit alongside Files and Console:
// Databases, Schedules, Users, Network, Startup, Settings and Activity. They
// all act on the active server, the same way the console and power controls
// do — switching servers goes through SwitchServer first.
//
// There is no Backups binding and no ReinstallServer binding. A restore
// overwrites every file on the server and a reinstall wipes them on some eggs;
// neither is recoverable from anything this app keeps locally, so they are left
// out of the app rather than guarded by a dialog. The panel's own UI still has
// them.

func (a *App) requireClient() error {
	if a.client == nil {
		return fmt.Errorf("not connected")
	}
	if a.client.GetServerID() == "" {
		return fmt.Errorf("no server selected")
	}
	return nil
}

// toMaps converts the client's attribute maps into the plain slice Wails
// marshals to the frontend.
func toMaps(items []pterodactyl.Attributes) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(items))
	for _, item := range items {
		out = append(out, map[string]interface{}(item))
	}
	return out
}

// GetServerResources returns state plus CPU, memory, disk and network usage.
func (a *App) GetServerResources() (map[string]interface{}, error) {
	if err := a.requireClient(); err != nil {
		return nil, err
	}
	res, err := a.client.GetResources()
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"current_state":    res.CurrentState,
		"is_suspended":     res.IsSuspended,
		"memory_bytes":     res.Resources.MemoryBytes,
		"cpu_absolute":     res.Resources.CPUAbsolute,
		"disk_bytes":       res.Resources.DiskBytes,
		"network_rx_bytes": res.Resources.NetworkRxBytes,
		"network_tx_bytes": res.Resources.NetworkTxBytes,
		"uptime":           res.Resources.Uptime,
	}, nil
}

// GetServerDetails returns the server object: name, description, limits and
// feature limits. The UI uses the feature limits to hide tabs the server has
// no allowance for.
func (a *App) GetServerDetails() (map[string]interface{}, error) {
	if err := a.requireClient(); err != nil {
		return nil, err
	}
	attrs, err := a.client.GetServerDetails()
	if err != nil {
		return nil, err
	}
	return map[string]interface{}(attrs), nil
}

// ---------------------------------------------------------------- databases

func (a *App) ListDatabases() ([]map[string]interface{}, error) {
	if err := a.requireClient(); err != nil {
		return nil, err
	}
	items, err := a.client.ListDatabases(true)
	if err != nil {
		return nil, err
	}
	return toMaps(items), nil
}

func (a *App) CreateDatabase(name, remote string) (map[string]interface{}, error) {
	if err := a.requireClient(); err != nil {
		return nil, err
	}
	attrs, err := a.client.CreateDatabase(name, remote)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}(attrs), nil
}

func (a *App) RotateDatabasePassword(databaseID string) (map[string]interface{}, error) {
	if err := a.requireClient(); err != nil {
		return nil, err
	}
	attrs, err := a.client.RotateDatabasePassword(databaseID)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}(attrs), nil
}

func (a *App) DeleteDatabase(databaseID string) error {
	if err := a.requireClient(); err != nil {
		return err
	}
	return a.client.DeleteDatabase(databaseID)
}

// ---------------------------------------------------------------- schedules

func (a *App) ListSchedules() ([]map[string]interface{}, error) {
	if err := a.requireClient(); err != nil {
		return nil, err
	}
	items, err := a.client.ListSchedules()
	if err != nil {
		return nil, err
	}
	return toMaps(items), nil
}

func (a *App) ExecuteSchedule(scheduleID string) error {
	if err := a.requireClient(); err != nil {
		return err
	}
	return a.client.ExecuteSchedule(scheduleID)
}

// SetScheduleActive toggles a schedule. The panel's update route replaces the
// whole schedule, so the caller passes back the cron fields unchanged.
func (a *App) SetScheduleActive(scheduleID, name, minute, hour, dayOfMonth, month, dayOfWeek string, isActive, onlyWhenOnline bool) (map[string]interface{}, error) {
	if err := a.requireClient(); err != nil {
		return nil, err
	}
	cron := map[string]string{
		"minute":       minute,
		"hour":         hour,
		"day_of_month": dayOfMonth,
		"month":        month,
		"day_of_week":  dayOfWeek,
	}
	attrs, err := a.client.SetScheduleActive(scheduleID, name, cron, isActive, onlyWhenOnline)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}(attrs), nil
}

func (a *App) DeleteSchedule(scheduleID string) error {
	if err := a.requireClient(); err != nil {
		return err
	}
	return a.client.DeleteSchedule(scheduleID)
}

// ----------------------------------------------------------------- subusers

func (a *App) ListSubusers() ([]map[string]interface{}, error) {
	if err := a.requireClient(); err != nil {
		return nil, err
	}
	items, err := a.client.ListSubusers()
	if err != nil {
		return nil, err
	}
	return toMaps(items), nil
}

func (a *App) CreateSubuser(email string, permissions []string) (map[string]interface{}, error) {
	if err := a.requireClient(); err != nil {
		return nil, err
	}
	attrs, err := a.client.CreateSubuser(email, permissions)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}(attrs), nil
}

func (a *App) UpdateSubuser(userUUID string, permissions []string) (map[string]interface{}, error) {
	if err := a.requireClient(); err != nil {
		return nil, err
	}
	attrs, err := a.client.UpdateSubuser(userUUID, permissions)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}(attrs), nil
}

func (a *App) DeleteSubuser(userUUID string) error {
	if err := a.requireClient(); err != nil {
		return err
	}
	return a.client.DeleteSubuser(userUUID)
}

// -------------------------------------------------------------- allocations

func (a *App) ListAllocations() ([]map[string]interface{}, error) {
	if err := a.requireClient(); err != nil {
		return nil, err
	}
	items, err := a.client.ListAllocations()
	if err != nil {
		return nil, err
	}
	return toMaps(items), nil
}

func (a *App) SetAllocationNotes(allocationID, notes string) (map[string]interface{}, error) {
	if err := a.requireClient(); err != nil {
		return nil, err
	}
	attrs, err := a.client.SetAllocationNotes(allocationID, notes)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}(attrs), nil
}

func (a *App) SetPrimaryAllocation(allocationID string) (map[string]interface{}, error) {
	if err := a.requireClient(); err != nil {
		return nil, err
	}
	attrs, err := a.client.SetPrimaryAllocation(allocationID)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}(attrs), nil
}

// ------------------------------------------------------------------ startup

func (a *App) GetStartup() (map[string]interface{}, error) {
	if err := a.requireClient(); err != nil {
		return nil, err
	}
	info, err := a.client.GetStartup()
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"variables":           toMaps(info.Variables),
		"startup_command":     info.StartupCommand,
		"raw_startup_command": info.RawStartupCommand,
		"docker_images":       info.DockerImages,
	}, nil
}

func (a *App) SetStartupVariable(key, value string) (map[string]interface{}, error) {
	if err := a.requireClient(); err != nil {
		return nil, err
	}
	attrs, err := a.client.SetStartupVariable(key, value)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}(attrs), nil
}

func (a *App) SetDockerImage(image string) error {
	if err := a.requireClient(); err != nil {
		return err
	}
	return a.client.SetDockerImage(image)
}

// ----------------------------------------------------------------- settings

func (a *App) RenameServer(name, description string) error {
	if err := a.requireClient(); err != nil {
		return err
	}
	if err := a.client.RenameServer(name, description); err != nil {
		return err
	}
	runtime.EventsEmit(a.ctx, "server-renamed", name)
	return nil
}

// ----------------------------------------------------------------- activity

func (a *App) ListActivity(page int) ([]map[string]interface{}, error) {
	if err := a.requireClient(); err != nil {
		return nil, err
	}
	items, err := a.client.ListActivity(page)
	if err != nil {
		return nil, err
	}
	return toMaps(items), nil
}

// ------------------------------------------------------- cross-panel lookup

// ServerRef identifies a server together with the panel it lives on.
type ServerRef struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Panel string `json:"panel"`
}

// ListAllServers returns every server across every configured panel, so the
// split editor can open a file from one panel beside a file from another.
// ListServers by contrast only covers the active panel.
func (a *App) ListAllServers() ([]ServerRef, error) {
	if a.config == nil {
		return nil, fmt.Errorf("not configured")
	}

	refs := make([]ServerRef, 0)
	var lastErr error

	for _, panel := range a.config.GetPanels() {
		panelURL := panel.PanelURL
		if !strings.HasPrefix(panelURL, "http://") && !strings.HasPrefix(panelURL, "https://") {
			panelURL = "https://" + panelURL
		}

		tmpClient := pterodactyl.NewClient(panelURL, panel.APIKey, "")
		servers, err := tmpClient.ListServers()
		if err != nil {
			// One unreachable panel should not hide the others.
			lastErr = err
			continue
		}

		for _, s := range servers {
			a.serverPanelMap[s.ID] = panel.Name
			refs = append(refs, ServerRef{ID: s.ID, Name: s.Name, Panel: panel.Name})
		}
	}

	if len(refs) == 0 && lastErr != nil {
		return nil, lastErr
	}
	return refs, nil
}
