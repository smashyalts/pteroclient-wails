package pterodactyl

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
)

// This file covers the parts of the Pterodactyl client API that the file
// manager and console do not touch: databases, schedules, subusers, network
// allocations, startup variables, server settings and the activity log. Routes
// follow panel/routes/api-client.php.
//
// Two route groups are deliberately absent. The backup routes are not
// implemented, so nothing here — however buggy — can restore a backup over the
// server's files or destroy one. Neither is the reinstall route, which re-runs
// the egg's install script and wipes the files on some eggs. Both are one
// mis-fired request away from losing a server, and both are still available in
// the panel's own UI for the rare times they are wanted.
//
// Listing endpoints are returned as decoded attribute maps rather than typed
// structs: the panel adds fields between releases, and the frontend renders
// them directly, so an unknown field should reach the UI instead of being
// dropped by a struct that predates it.

// Attributes is a single decoded object from a Pterodactyl API response.
type Attributes map[string]interface{}

// listResponse is the envelope every paginated client endpoint returns.
type listResponse struct {
	Object string `json:"object"`
	Data   []struct {
		Object     string     `json:"object"`
		Attributes Attributes `json:"attributes"`
	} `json:"data"`
	Meta json.RawMessage `json:"meta,omitempty"`
}

func (c *Client) serverPath(format string, args ...interface{}) string {
	suffix := fmt.Sprintf(format, args...)
	return fmt.Sprintf("%s/api/client/servers/%s%s", c.baseURL, c.serverID, suffix)
}

// getList performs a GET and flattens the data[].attributes envelope.
func (c *Client) getList(endpoint string) ([]Attributes, error) {
	resp, err := c.client.R().Get(endpoint)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, fmt.Errorf("API returned status %d: %s", resp.StatusCode(), resp.String())
	}

	var parsed listResponse
	if err := json.Unmarshal(resp.Body(), &parsed); err != nil {
		return nil, err
	}

	items := make([]Attributes, 0, len(parsed.Data))
	for _, entry := range parsed.Data {
		items = append(items, entry.Attributes)
	}
	return items, nil
}

// getObject performs a GET on an endpoint returning a single object.
func (c *Client) getObject(endpoint string) (Attributes, error) {
	resp, err := c.client.R().Get(endpoint)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, fmt.Errorf("API returned status %d: %s", resp.StatusCode(), resp.String())
	}

	var parsed struct {
		Attributes Attributes `json:"attributes"`
	}
	if err := json.Unmarshal(resp.Body(), &parsed); err != nil {
		return nil, err
	}
	return parsed.Attributes, nil
}

// mutate performs a write and returns the response body when the panel sends
// one back (creates return the new object; most actions return 204).
func (c *Client) mutate(method, endpoint string, body interface{}) (Attributes, error) {
	req := c.client.R()
	if body != nil {
		req = req.SetBody(body)
	}

	resp, err := req.Execute(method, endpoint)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode() < 200 || resp.StatusCode() > 299 {
		return nil, fmt.Errorf("API returned status %d: %s", resp.StatusCode(), resp.String())
	}
	if len(resp.Body()) == 0 {
		return nil, nil
	}

	var parsed struct {
		Attributes Attributes `json:"attributes"`
	}
	if err := json.Unmarshal(resp.Body(), &parsed); err != nil {
		// A 2xx with a body we cannot parse is not a failure of the action.
		return nil, nil
	}
	return parsed.Attributes, nil
}

// ---------------------------------------------------------------- resources

// ServerResources is the full /resources payload. GetServerState reads only
// current_state from the same endpoint; this returns the usage figures too.
type ServerResources struct {
	CurrentState string `json:"current_state"`
	IsSuspended  bool   `json:"is_suspended"`
	Resources    struct {
		MemoryBytes    int64   `json:"memory_bytes"`
		CPUAbsolute    float64 `json:"cpu_absolute"`
		DiskBytes      int64   `json:"disk_bytes"`
		NetworkRxBytes int64   `json:"network_rx_bytes"`
		NetworkTxBytes int64   `json:"network_tx_bytes"`
		Uptime         int64   `json:"uptime"`
	} `json:"resources"`
}

// GetResources returns the server's current state and resource usage.
func (c *Client) GetResources() (*ServerResources, error) {
	resp, err := c.client.R().Get(c.serverPath("/resources"))
	if err != nil {
		return nil, fmt.Errorf("failed to get resources: %w", err)
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, fmt.Errorf("API returned status %d", resp.StatusCode())
	}

	var parsed struct {
		Attributes ServerResources `json:"attributes"`
	}
	if err := json.Unmarshal(resp.Body(), &parsed); err != nil {
		return nil, err
	}
	return &parsed.Attributes, nil
}

// GetServerDetails returns the server object itself (name, description,
// limits, feature limits, allocation relationships).
func (c *Client) GetServerDetails() (Attributes, error) {
	return c.getObject(c.serverPath(""))
}

// ---------------------------------------------------------------- databases

// ListDatabases returns the server's databases. Passing includePassword adds
// the relationship the panel only exposes on request.
func (c *Client) ListDatabases(includePassword bool) ([]Attributes, error) {
	endpoint := c.serverPath("/databases")
	if includePassword {
		endpoint += "?include=password"
	}
	return c.getList(endpoint)
}

// CreateDatabase creates a database. remote is the connection mask the
// database may be reached from, e.g. "%".
func (c *Client) CreateDatabase(name, remote string) (Attributes, error) {
	if remote == "" {
		remote = "%"
	}
	return c.mutate(http.MethodPost, c.serverPath("/databases"), map[string]string{
		"database": name,
		"remote":   remote,
	})
}

// RotateDatabasePassword generates a new password for a database.
func (c *Client) RotateDatabasePassword(databaseID string) (Attributes, error) {
	return c.mutate(http.MethodPost, c.serverPath("/databases/%s/rotate-password", databaseID), nil)
}

// DeleteDatabase removes a database and its contents.
func (c *Client) DeleteDatabase(databaseID string) error {
	_, err := c.mutate(http.MethodDelete, c.serverPath("/databases/%s", databaseID), nil)
	return err
}

// ---------------------------------------------------------------- schedules

// ListSchedules returns the server's schedules with their tasks included.
func (c *Client) ListSchedules() ([]Attributes, error) {
	return c.getList(c.serverPath("/schedules"))
}

// ExecuteSchedule runs a schedule immediately, ignoring its cron.
func (c *Client) ExecuteSchedule(scheduleID string) error {
	_, err := c.mutate(http.MethodPost, c.serverPath("/schedules/%s/execute", scheduleID), nil)
	return err
}

// SetScheduleActive enables or disables a schedule. The panel's update route
// is a full replace, so the caller passes the fields it wants preserved.
func (c *Client) SetScheduleActive(scheduleID, name string, cron map[string]string, isActive, onlyWhenOnline bool) (Attributes, error) {
	payload := map[string]interface{}{
		"name":             name,
		"minute":           cron["minute"],
		"hour":             cron["hour"],
		"day_of_month":     cron["day_of_month"],
		"month":            cron["month"],
		"day_of_week":      cron["day_of_week"],
		"is_active":        isActive,
		"only_when_online": onlyWhenOnline,
	}
	return c.mutate(http.MethodPost, c.serverPath("/schedules/%s", scheduleID), payload)
}

// DeleteSchedule removes a schedule and its tasks.
func (c *Client) DeleteSchedule(scheduleID string) error {
	_, err := c.mutate(http.MethodDelete, c.serverPath("/schedules/%s", scheduleID), nil)
	return err
}

// ----------------------------------------------------------------- subusers

// ListSubusers returns the users with access to this server.
func (c *Client) ListSubusers() ([]Attributes, error) {
	return c.getList(c.serverPath("/users"))
}

// CreateSubuser invites a user by email with the given permission keys.
func (c *Client) CreateSubuser(email string, permissions []string) (Attributes, error) {
	return c.mutate(http.MethodPost, c.serverPath("/users"), map[string]interface{}{
		"email":       email,
		"permissions": permissions,
	})
}

// UpdateSubuser replaces a subuser's permission set.
func (c *Client) UpdateSubuser(userUUID string, permissions []string) (Attributes, error) {
	return c.mutate(http.MethodPost, c.serverPath("/users/%s", userUUID), map[string]interface{}{
		"permissions": permissions,
	})
}

// DeleteSubuser revokes a user's access to the server.
func (c *Client) DeleteSubuser(userUUID string) error {
	_, err := c.mutate(http.MethodDelete, c.serverPath("/users/%s", userUUID), nil)
	return err
}

// -------------------------------------------------------------- allocations

// ListAllocations returns the network allocations assigned to the server.
func (c *Client) ListAllocations() ([]Attributes, error) {
	return c.getList(c.serverPath("/network/allocations"))
}

// SetAllocationNotes sets the free-text note on an allocation.
func (c *Client) SetAllocationNotes(allocationID, notes string) (Attributes, error) {
	return c.mutate(http.MethodPost, c.serverPath("/network/allocations/%s", allocationID), map[string]string{
		"notes": notes,
	})
}

// SetPrimaryAllocation makes an allocation the server's primary one.
func (c *Client) SetPrimaryAllocation(allocationID string) (Attributes, error) {
	return c.mutate(http.MethodPost, c.serverPath("/network/allocations/%s/primary", allocationID), nil)
}

// ------------------------------------------------------------------ startup

// StartupInfo is the startup tab: the egg variables plus the resolved and raw
// startup commands and the docker images the egg allows.
type StartupInfo struct {
	Variables         []Attributes      `json:"variables"`
	StartupCommand    string            `json:"startup_command"`
	RawStartupCommand string            `json:"raw_startup_command"`
	DockerImages      map[string]string `json:"docker_images"`
}

// GetStartup returns the server's startup variables and commands.
func (c *Client) GetStartup() (*StartupInfo, error) {
	resp, err := c.client.R().Get(c.serverPath("/startup"))
	if err != nil {
		return nil, fmt.Errorf("failed to get startup: %w", err)
	}
	if resp.StatusCode() != http.StatusOK {
		return nil, fmt.Errorf("API returned status %d: %s", resp.StatusCode(), resp.String())
	}

	var parsed struct {
		Data []struct {
			Attributes Attributes `json:"attributes"`
		} `json:"data"`
		Meta struct {
			StartupCommand    string            `json:"startup_command"`
			RawStartupCommand string            `json:"raw_startup_command"`
			DockerImages      map[string]string `json:"docker_images"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(resp.Body(), &parsed); err != nil {
		return nil, err
	}

	info := &StartupInfo{
		Variables:         make([]Attributes, 0, len(parsed.Data)),
		StartupCommand:    parsed.Meta.StartupCommand,
		RawStartupCommand: parsed.Meta.RawStartupCommand,
		DockerImages:      parsed.Meta.DockerImages,
	}
	for _, entry := range parsed.Data {
		info.Variables = append(info.Variables, entry.Attributes)
	}
	return info, nil
}

// SetStartupVariable updates one egg variable by its environment key.
func (c *Client) SetStartupVariable(key, value string) (Attributes, error) {
	return c.mutate(http.MethodPut, c.serverPath("/startup/variable"), map[string]string{
		"key":   key,
		"value": value,
	})
}

// SetDockerImage switches the server to another image the egg allows.
func (c *Client) SetDockerImage(image string) error {
	_, err := c.mutate(http.MethodPut, c.serverPath("/settings/docker-image"), map[string]string{
		"docker_image": image,
	})
	return err
}

// ----------------------------------------------------------------- settings

// RenameServer changes the server's display name and description.
func (c *Client) RenameServer(name, description string) error {
	_, err := c.mutate(http.MethodPost, c.serverPath("/settings/rename"), map[string]string{
		"name":        name,
		"description": description,
	})
	return err
}

// ----------------------------------------------------------------- activity

// ListActivity returns the server's activity log, newest first. The actor is
// requested as an include so the UI can name who did what.
func (c *Client) ListActivity(page int) ([]Attributes, error) {
	if page < 1 {
		page = 1
	}
	query := url.Values{}
	query.Set("page", fmt.Sprintf("%d", page))
	query.Set("sort", "-timestamp")
	return c.getList(c.serverPath("/activity?%s", query.Encode()))
}
