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

// ------------------------------------------------- schedules: full editing

func (a *App) GetSchedule(scheduleID string) (map[string]interface{}, error) {
	if err := a.requireClient(); err != nil {
		return nil, err
	}
	attrs, err := a.client.GetSchedule(scheduleID)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}(attrs), nil
}

func (a *App) CreateSchedule(name, minute, hour, dayOfMonth, month, dayOfWeek string, isActive, onlyWhenOnline bool) (map[string]interface{}, error) {
	if err := a.requireClient(); err != nil {
		return nil, err
	}
	attrs, err := a.client.CreateSchedule(name, cronFields(minute, hour, dayOfMonth, month, dayOfWeek), isActive, onlyWhenOnline)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}(attrs), nil
}

func (a *App) UpdateSchedule(scheduleID, name, minute, hour, dayOfMonth, month, dayOfWeek string, isActive, onlyWhenOnline bool) (map[string]interface{}, error) {
	if err := a.requireClient(); err != nil {
		return nil, err
	}
	attrs, err := a.client.UpdateSchedule(scheduleID, name, cronFields(minute, hour, dayOfMonth, month, dayOfWeek), isActive, onlyWhenOnline)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}(attrs), nil
}

// ScheduleTaskActions is what the panel accepts, handed to the UI so the
// picker and the validation here cannot drift apart.
func (a *App) ScheduleTaskActions() []map[string]string {
	return []map[string]string{
		{"id": "command", "label": "Send a console command", "hint": "The command, without a leading slash."},
		{"id": "power", "label": "Power action", "hint": "One of start, stop, restart or kill."},
		{"id": "backup", "label": "Create a backup", "hint": "Optional ignore list, one glob per line."},
	}
}

func (a *App) CreateScheduleTask(scheduleID, action, payload string, timeOffset int, continueOnFailure bool) (map[string]interface{}, error) {
	if err := a.requireClient(); err != nil {
		return nil, err
	}
	if err := validateTask(action, payload); err != nil {
		return nil, err
	}
	attrs, err := a.client.CreateScheduleTask(scheduleID, action, payload, timeOffset, continueOnFailure)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}(attrs), nil
}

func (a *App) UpdateScheduleTask(scheduleID, taskID, action, payload string, timeOffset int, continueOnFailure bool) (map[string]interface{}, error) {
	if err := a.requireClient(); err != nil {
		return nil, err
	}
	if err := validateTask(action, payload); err != nil {
		return nil, err
	}
	attrs, err := a.client.UpdateScheduleTask(scheduleID, taskID, action, payload, timeOffset, continueOnFailure)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}(attrs), nil
}

func (a *App) DeleteScheduleTask(scheduleID, taskID string) error {
	if err := a.requireClient(); err != nil {
		return err
	}
	return a.client.DeleteScheduleTask(scheduleID, taskID)
}

func cronFields(minute, hour, dayOfMonth, month, dayOfWeek string) map[string]string {
	return map[string]string{
		"minute":       minute,
		"hour":         hour,
		"day_of_month": dayOfMonth,
		"month":        month,
		"day_of_week":  dayOfWeek,
	}
}

// validateTask catches the two mistakes the panel reports as an opaque 422.
func validateTask(action, payload string) error {
	switch action {
	case "command":
		if strings.TrimSpace(payload) == "" {
			return fmt.Errorf("a command task needs a command")
		}
	case "power":
		switch strings.TrimSpace(payload) {
		case "start", "stop", "restart", "kill":
		default:
			return fmt.Errorf("a power task takes start, stop, restart or kill, not %q", payload)
		}
	case "backup":
		// An empty ignore list is normal.
	default:
		return fmt.Errorf("unknown task action %q", action)
	}
	return nil
}

// ----------------------------------------------- subusers: full permissions

// SubuserPermissions is the panel's permission set, grouped the way its own
// UI groups them. Sent to the frontend so the picker cannot fall out of step
// with what the API accepts.
func (a *App) SubuserPermissions() []map[string]interface{} {
	group := func(name, describe string, keys ...[2]string) map[string]interface{} {
		items := make([]map[string]string, 0, len(keys))
		for _, k := range keys {
			items = append(items, map[string]string{"key": k[0], "label": k[1]})
		}
		return map[string]interface{}{"group": name, "hint": describe, "permissions": items}
	}

	return []map[string]interface{}{
		group("Console", "Reading output and sending commands.",
			[2]string{"control.console", "See the console"},
			[2]string{"control.start", "Start the server"},
			[2]string{"control.stop", "Stop the server"},
			[2]string{"control.restart", "Restart the server"},
		),
		group("Files", "Everything in the file manager.",
			[2]string{"file.read", "List files"},
			[2]string{"file.read-content", "Read file contents"},
			[2]string{"file.create", "Create files and folders"},
			[2]string{"file.update", "Edit and rename"},
			[2]string{"file.delete", "Delete"},
			[2]string{"file.archive", "Archive and extract"},
			[2]string{"file.sftp", "Connect over SFTP"},
		),
		group("Backups", "",
			[2]string{"backup.read", "See backups"},
			[2]string{"backup.create", "Create backups"},
			[2]string{"backup.delete", "Delete backups"},
			[2]string{"backup.download", "Download backups"},
			[2]string{"backup.restore", "Restore backups"},
		),
		group("Databases", "",
			[2]string{"database.read", "See databases"},
			[2]string{"database.create", "Create databases"},
			[2]string{"database.update", "Rotate passwords"},
			[2]string{"database.delete", "Delete databases"},
			[2]string{"database.view_password", "See passwords"},
		),
		group("Schedules", "",
			[2]string{"schedule.read", "See schedules"},
			[2]string{"schedule.create", "Create schedules"},
			[2]string{"schedule.update", "Edit schedules"},
			[2]string{"schedule.delete", "Delete schedules"},
		),
		group("Subusers", "Managing who else has access.",
			[2]string{"user.read", "See subusers"},
			[2]string{"user.create", "Invite subusers"},
			[2]string{"user.update", "Change permissions"},
			[2]string{"user.delete", "Remove subusers"},
		),
		group("Network", "",
			[2]string{"allocation.read", "See allocations"},
			[2]string{"allocation.create", "Add allocations"},
			[2]string{"allocation.update", "Edit allocations"},
			[2]string{"allocation.delete", "Remove allocations"},
		),
		group("Startup", "",
			[2]string{"startup.read", "See startup variables"},
			[2]string{"startup.update", "Change startup variables"},
			[2]string{"startup.docker-image", "Change the Docker image"},
		),
		group("Settings", "",
			[2]string{"settings.rename", "Rename the server"},
			[2]string{"settings.reinstall", "Reinstall the server"},
			[2]string{"activity.read", "See the activity log"},
		),
	}
}
