package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"pteroclient-wails/pkg/mcp"
)

// recorded is one request the fake panel saw.
type recorded struct {
	Method string
	Path   string
	Query  string
	Body   string
}

// fakePanel answers the handful of routes these tests exercise and records
// everything it was asked.
func fakePanel(t *testing.T) (*httptest.Server, *[]recorded) {
	t.Helper()
	var seen []recorded

	panel := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := make([]byte, r.ContentLength)
		if r.ContentLength > 0 {
			r.Body.Read(body)
		}
		seen = append(seen, recorded{
			Method: r.Method,
			Path:   r.URL.Path,
			Query:  r.URL.RawQuery,
			Body:   string(body),
		})

		switch {
		case strings.HasSuffix(r.URL.Path, "/files/list"):
			json.NewEncoder(w).Encode(map[string]interface{}{
				"object": "list",
				"data": []map[string]interface{}{
					{"object": "file_object", "attributes": map[string]interface{}{
						"name": "server.properties", "is_file": true, "size": 1240,
						"mode": "rw-r--r--", "modified_at": "2026-09-01T12:00:00+00:00"}},
					{"object": "file_object", "attributes": map[string]interface{}{
						"name": "plugins", "is_file": false,
						"mode": "rwxr-xr-x", "modified_at": "2026-09-01T12:00:00+00:00"}},
				},
			})

		case strings.HasSuffix(r.URL.Path, "/files/contents"):
			w.Write([]byte("motd=A Minecraft Server\nserver-port=25565\nmax-players=20\n"))

		case strings.HasSuffix(r.URL.Path, "/resources"):
			json.NewEncoder(w).Encode(map[string]interface{}{
				"object": "stats",
				"attributes": map[string]interface{}{
					"current_state": "running",
					"resources":     map[string]interface{}{"memory_bytes": 1048576},
				},
			})

		default:
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	t.Cleanup(panel.Close)
	return panel, &seen
}

// build wires a toolset and an initialized MCP server against a fake panel.
func build(t *testing.T, allow permissions, panelURL string) *mcp.Server {
	t.Helper()

	registry, err := NewRegistry([]PanelSpec{{
		Name:          "main",
		URL:           panelURL,
		APIKey:        "ptlc_test",
		DefaultServer: "1a7ce997",
	}})
	if err != nil {
		t.Fatal(err)
	}

	set := &toolset{registry: registry, allow: allow}
	server := mcp.NewServer(serverName, version)
	set.registerServerTools(server)
	set.registerFileTools(server)
	set.registerManagementTools(server)
	set.registerRawTool(server)

	reply := server.Handle(context.Background(),
		[]byte(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`))
	if reply == nil {
		t.Fatal("initialize produced no reply")
	}
	return server
}

// invoke calls a tool and returns its text plus whether it reported an error.
func invoke(t *testing.T, server *mcp.Server, name string, args map[string]interface{}) (string, bool) {
	t.Helper()

	params := map[string]interface{}{"name": name}
	if args != nil {
		params["arguments"] = args
	}
	request, err := json.Marshal(map[string]interface{}{
		"jsonrpc": "2.0", "id": 2, "method": "tools/call", "params": params,
	})
	if err != nil {
		t.Fatal(err)
	}

	raw := server.Handle(context.Background(), request)
	var reply struct {
		Result struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
			IsError bool `json:"isError"`
		} `json:"result"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &reply); err != nil {
		t.Fatalf("could not read the reply: %v (%s)", err, raw)
	}
	if reply.Error != nil {
		return reply.Error.Message, true
	}
	if len(reply.Result.Content) == 0 {
		return "", reply.Result.IsError
	}
	return reply.Result.Content[0].Text, reply.Result.IsError
}

func writable() permissions { return permissions{write: true} }

// --------------------------------------------------------------------- gating

func TestDestructiveToolsAreWithheldByDefault(t *testing.T) {
	panel, _ := fakePanel(t)
	server := build(t, writable(), panel.URL)

	registered := map[string]bool{}
	for _, name := range server.ToolNames() {
		registered[name] = true
	}

	// A tool that is not registered cannot be called and cannot be talked
	// into running by anything a panel or a config file says.
	for _, name := range []string{
		"ptero_files_delete", "ptero_backup_restore", "ptero_backup_delete",
		"ptero_server_reinstall", "ptero_database_delete", "ptero_files_decompress",
	} {
		if registered[name] {
			t.Errorf("%s should be withheld without -allow-destructive", name)
		}
	}
	for _, name := range []string{"ptero_account_api_key_create", "ptero_account_ssh_key_delete"} {
		if registered[name] {
			t.Errorf("%s should be withheld without -allow-account", name)
		}
	}
	if registered["ptero_request"] {
		t.Error("ptero_request should be withheld without -allow-raw")
	}

	// The ordinary ones have to be there, or the server is useless.
	for _, name := range []string{
		"ptero_files_list", "ptero_files_read", "ptero_files_write", "ptero_files_search",
		"ptero_power", "ptero_console_tail", "ptero_servers_list", "ptero_backups_list",
	} {
		if !registered[name] {
			t.Errorf("%s should be registered", name)
		}
	}
}

func TestReadOnlyWithholdsEveryWrite(t *testing.T) {
	panel, requests := fakePanel(t)
	server := build(t, permissions{write: true, destroy: true, account: true, raw: true, readOnly: true}, panel.URL)

	for _, name := range server.ToolNames() {
		switch name {
		case "ptero_files_write", "ptero_power", "ptero_command", "ptero_files_delete",
			"ptero_backup_create", "ptero_console_exec":
			t.Errorf("%s was registered in read-only mode", name)
		}
	}

	if _, isError := invoke(t, server, "ptero_files_list", map[string]interface{}{}); isError {
		t.Error("reads should still work in read-only mode")
	}
	for _, request := range *requests {
		if request.Method != http.MethodGet {
			t.Errorf("read-only mode sent a %s to %s", request.Method, request.Path)
		}
	}
}

func TestDestructiveToolsNeedConfirm(t *testing.T) {
	panel, requests := fakePanel(t)
	server := build(t, permissions{write: true, destroy: true}, panel.URL)

	// The flag says the process may destroy things; confirm says this call
	// meant to.
	text, isError := invoke(t, server, "ptero_files_delete", map[string]interface{}{
		"files": []string{"latest.log"},
	})
	if !isError {
		t.Fatalf("a delete without confirm should be refused, got %q", text)
	}
	if len(*requests) != 0 {
		t.Fatalf("the panel was called anyway: %+v", *requests)
	}

	if _, isError := invoke(t, server, "ptero_files_delete", map[string]interface{}{
		"files":   []string{"latest.log"},
		"confirm": true,
	}); isError {
		t.Fatal("a confirmed delete should go through")
	}
	if len(*requests) != 1 || !strings.HasSuffix((*requests)[0].Path, "/files/delete") {
		t.Fatalf("unexpected requests: %+v", *requests)
	}
}

func TestDeletingTheWholeServerDirectoryIsRefused(t *testing.T) {
	panel, requests := fakePanel(t)
	server := build(t, permissions{write: true, destroy: true}, panel.URL)

	for _, name := range []string{"/", ".", "", "  "} {
		text, isError := invoke(t, server, "ptero_files_delete", map[string]interface{}{
			"files":   []string{name},
			"confirm": true,
		})
		if !isError {
			t.Errorf("deleting %q should be refused, got %q", name, text)
		}
	}
	if len(*requests) != 0 {
		t.Errorf("a root delete reached the panel: %+v", *requests)
	}
}

func TestRawToolHonoursTheSameGates(t *testing.T) {
	panel, _ := fakePanel(t)

	// Otherwise the passthrough would be a way around every other decision.
	readOnlyRaw := build(t, permissions{raw: true, readOnly: true}, panel.URL)
	if _, isError := invoke(t, readOnlyRaw, "ptero_request", map[string]interface{}{
		"method": "POST", "path": "/servers/1a7ce997/power",
	}); !isError {
		t.Error("a POST through the raw tool should be refused in read-only mode")
	}

	noDestroy := build(t, permissions{write: true, raw: true}, panel.URL)
	if _, isError := invoke(t, noDestroy, "ptero_request", map[string]interface{}{
		"method": "DELETE", "path": "/servers/1a7ce997/backups/x",
	}); !isError {
		t.Error("a DELETE through the raw tool should need -allow-destructive")
	}
	if _, isError := invoke(t, noDestroy, "ptero_request", map[string]interface{}{
		"method": "GET", "path": "/servers/1a7ce997/resources",
	}); isError {
		t.Error("a GET through the raw tool should be allowed")
	}
}

func TestToolFilterTrimsTheRegistry(t *testing.T) {
	panel, _ := fakePanel(t)
	server := build(t, permissions{write: true, nameGlobs: []string{"ptero_files"}}, panel.URL)

	for _, name := range server.ToolNames() {
		if !strings.HasPrefix(name, "ptero_files") {
			t.Errorf("%s slipped past the filter", name)
		}
	}
	if len(server.ToolNames()) == 0 {
		t.Error("the filter withheld everything")
	}
}

// ---------------------------------------------------------------- tool calls

func TestFileListingIsRenderedAsATable(t *testing.T) {
	panel, requests := fakePanel(t)
	server := build(t, writable(), panel.URL)

	text, isError := invoke(t, server, "ptero_files_list", map[string]interface{}{"directory": "/plugins"})
	if isError {
		t.Fatal(text)
	}

	for _, want := range []string{"server.properties", "plugins", "1.2 KiB", "dir", "file"} {
		if !strings.Contains(text, want) {
			t.Errorf("listing did not mention %q:\n%s", want, text)
		}
	}
	// The panel's own field names would be most of the reply for a large
	// directory, so the table is the default.
	if strings.Contains(text, "attributes") {
		t.Errorf("the envelope leaked into the table:\n%s", text)
	}
	if (*requests)[0].Query != "directory=%2Fplugins" {
		t.Errorf("directory was not passed through: %q", (*requests)[0].Query)
	}
}

func TestFileListingAsJSONOnRequest(t *testing.T) {
	panel, _ := fakePanel(t)
	server := build(t, writable(), panel.URL)

	text, isError := invoke(t, server, "ptero_files_list", map[string]interface{}{"as_json": true})
	if isError {
		t.Fatal(text)
	}
	if !strings.Contains(text, "\"items\"") {
		t.Errorf("expected the flattened records:\n%s", text)
	}
}

func TestFileReadAppliesTheLineWindow(t *testing.T) {
	panel, _ := fakePanel(t)
	server := build(t, writable(), panel.URL)

	text, isError := invoke(t, server, "ptero_files_read", map[string]interface{}{
		"file":       "/server.properties",
		"start_line": 2,
		"line_count": 1,
	})
	if isError {
		t.Fatal(text)
	}
	if !strings.Contains(text, "server-port=25565") {
		t.Errorf("the requested line is missing:\n%s", text)
	}
	if strings.Contains(text, "max-players") {
		t.Errorf("the window was not applied:\n%s", text)
	}
	if !strings.Contains(text, "lines 2-2 of") {
		t.Errorf("the window was not reported:\n%s", text)
	}
}

func TestFileReadTruncatesAndSaysHowToContinue(t *testing.T) {
	panel, _ := fakePanel(t)
	server := build(t, writable(), panel.URL)

	text, isError := invoke(t, server, "ptero_files_read", map[string]interface{}{
		"file":      "/server.properties",
		"max_bytes": 10,
	})
	if isError {
		t.Fatal(text)
	}
	if !strings.Contains(text, "truncated at 10 bytes") {
		t.Errorf("truncation was silent:\n%s", text)
	}
	if !strings.Contains(text, "start_line") {
		t.Errorf("the way to read further was not offered:\n%s", text)
	}
}

func TestFileWriteSendsTheBytesAsTheBody(t *testing.T) {
	panel, requests := fakePanel(t)
	server := build(t, writable(), panel.URL)

	text, isError := invoke(t, server, "ptero_files_write", map[string]interface{}{
		"file":    "/server.properties",
		"content": "server-port=25566\n",
	})
	if isError {
		t.Fatal(text)
	}

	request := (*requests)[0]
	if request.Method != http.MethodPost || !strings.HasSuffix(request.Path, "/files/write") {
		t.Fatalf("unexpected request: %+v", request)
	}
	if request.Body != "server-port=25566\n" {
		t.Errorf("body was %q", request.Body)
	}
	if !strings.Contains(text, "18 bytes") {
		t.Errorf("the reply did not confirm the write: %q", text)
	}
}

func TestPowerValidatesTheSignal(t *testing.T) {
	panel, requests := fakePanel(t)
	server := build(t, writable(), panel.URL)

	if text, isError := invoke(t, server, "ptero_power",
		map[string]interface{}{"signal": "obliterate"}); !isError {
		t.Fatalf("a bogus signal should be refused, got %q", text)
	}
	if len(*requests) != 0 {
		t.Fatalf("a bogus signal reached the panel: %+v", *requests)
	}

	if text, isError := invoke(t, server, "ptero_power",
		map[string]interface{}{"signal": "restart"}); isError {
		t.Fatal(text)
	}
	if !strings.Contains((*requests)[0].Body, `"signal":"restart"`) {
		t.Errorf("body was %q", (*requests)[0].Body)
	}
}

func TestDefaultServerIsUsedWhenNoneIsGiven(t *testing.T) {
	panel, requests := fakePanel(t)
	server := build(t, writable(), panel.URL)

	if _, isError := invoke(t, server, "ptero_server_resources", map[string]interface{}{}); isError {
		t.Fatal("the configured default server should have been used")
	}
	if !strings.Contains((*requests)[0].Path, "/servers/1a7ce997/resources") {
		t.Errorf("path was %q", (*requests)[0].Path)
	}
}

func TestAPastedServerURLIsReducedToItsIdentifier(t *testing.T) {
	panel, requests := fakePanel(t)
	server := build(t, writable(), panel.URL)

	if _, isError := invoke(t, server, "ptero_server_resources", map[string]interface{}{
		"server": "https://panel.example.com/server/abcd1234",
	}); isError {
		t.Fatal("a pasted panel URL should be recovered from")
	}
	if !strings.Contains((*requests)[0].Path, "/servers/abcd1234/resources") {
		t.Errorf("path was %q", (*requests)[0].Path)
	}
}

func TestAnUnknownPanelNameIsRefusedRatherThanDefaulted(t *testing.T) {
	panel, requests := fakePanel(t)
	server := build(t, writable(), panel.URL)

	// Quietly acting on the default panel could restart the wrong
	// company's game server.
	text, isError := invoke(t, server, "ptero_server_resources", map[string]interface{}{"panel": "staging"})
	if !isError {
		t.Fatalf("an unknown panel should be an error, got %q", text)
	}
	if !strings.Contains(text, "main") {
		t.Errorf("the error should list what is configured: %q", text)
	}
	if len(*requests) != 0 {
		t.Errorf("a request went out anyway: %+v", *requests)
	}
}

func TestPanelNameIsMatchedCaseInsensitively(t *testing.T) {
	panel, _ := fakePanel(t)
	server := build(t, writable(), panel.URL)

	if text, isError := invoke(t, server, "ptero_server_resources",
		map[string]interface{}{"panel": "MAIN"}); isError {
		t.Errorf("case should not matter for a panel name: %q", text)
	}
}

func TestPanelsListNeverReturnsTheAPIKey(t *testing.T) {
	panel, _ := fakePanel(t)
	server := build(t, writable(), panel.URL)

	text, isError := invoke(t, server, "ptero_panels_list", map[string]interface{}{})
	if isError {
		t.Fatal(text)
	}
	if strings.Contains(text, "ptlc_test") {
		t.Fatalf("the API key leaked into a tool result:\n%s", text)
	}
	if !strings.Contains(text, "main") || !strings.Contains(text, "1a7ce997") {
		t.Errorf("the listing is missing what it is for:\n%s", text)
	}
}

func TestScheduleUpdateKeepsTheFieldsItWasNotGiven(t *testing.T) {
	var seen []recorded
	panel := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := make([]byte, r.ContentLength)
		if r.ContentLength > 0 {
			r.Body.Read(body)
		}
		seen = append(seen, recorded{Method: r.Method, Path: r.URL.Path, Body: string(body)})

		if r.Method == http.MethodGet {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"object": "server_schedule",
				"attributes": map[string]interface{}{
					"id": 4, "name": "nightly restart", "is_active": true, "only_when_online": false,
					"cron": map[string]interface{}{
						"minute": "0", "hour": "4", "day_of_month": "*", "month": "*", "day_of_week": "*",
					},
				},
			})
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer panel.Close()

	server := build(t, writable(), panel.URL)

	// The panel's own route replaces the whole schedule, so pausing one
	// without retyping its cron has to read it first.
	if text, isError := invoke(t, server, "ptero_schedule_update", map[string]interface{}{
		"schedule":  "4",
		"is_active": false,
	}); isError {
		t.Fatal(text)
	}

	if len(seen) != 2 {
		t.Fatalf("expected a read then a write, got %+v", seen)
	}
	body := seen[1].Body
	for _, want := range []string{
		`"name":"nightly restart"`, `"hour":"4"`, `"minute":"0"`, `"is_active":false`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("update body missing %s:\n%s", want, body)
		}
	}
}

func TestSubuserUpdateMergesAgainstCurrentPermissions(t *testing.T) {
	var writes []string
	panel := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"object": "server_subuser",
				"attributes": map[string]interface{}{
					"uuid":        "u-1",
					"permissions": []string{"control.console", "file.read"},
				},
			})
			return
		}
		body := make([]byte, r.ContentLength)
		r.Body.Read(body)
		writes = append(writes, string(body))
		w.WriteHeader(http.StatusNoContent)
	}))
	defer panel.Close()

	server := build(t, writable(), panel.URL)

	if text, isError := invoke(t, server, "ptero_subuser_update", map[string]interface{}{
		"user":   "u-1",
		"grant":  []string{"file.update"},
		"revoke": []string{"control.console"},
	}); isError {
		t.Fatal(text)
	}

	if len(writes) != 1 {
		t.Fatalf("expected one write, got %v", writes)
	}
	if !strings.Contains(writes[0], "file.read") || !strings.Contains(writes[0], "file.update") {
		t.Errorf("the merged set is wrong: %s", writes[0])
	}
	if strings.Contains(writes[0], "control.console") {
		t.Errorf("the revoked permission survived: %s", writes[0])
	}
}

func TestSubuserUpdateRefusesToStripEveryPermission(t *testing.T) {
	panel := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"object":     "server_subuser",
			"attributes": map[string]interface{}{"permissions": []string{"file.read"}},
		})
	}))
	defer panel.Close()

	server := build(t, writable(), panel.URL)
	text, isError := invoke(t, server, "ptero_subuser_update", map[string]interface{}{
		"user":   "u-1",
		"revoke": []string{"file.read"},
	})
	if !isError {
		t.Fatalf("expected a refusal, got %q", text)
	}
	if !strings.Contains(text, "ptero_subuser_delete") {
		t.Errorf("the refusal should point at the right tool: %q", text)
	}
}

func TestPanelErrorReachesTheModel(t *testing.T) {
	panel := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		w.Write([]byte(`{"errors":[{"code":"Conflict","detail":"Server is not running."}]}`))
	}))
	defer panel.Close()

	server := build(t, writable(), panel.URL)
	text, isError := invoke(t, server, "ptero_command", map[string]interface{}{"command": "list"})
	if !isError {
		t.Fatal("expected the conflict to be reported")
	}
	if !strings.Contains(text, "Server is not running.") {
		t.Errorf("the panel's reason was lost: %q", text)
	}
}

// ---------------------------------------------------------------- config load

// isolateHome points the optional config locations at an empty directory.
//
// loadPanels deliberately reads ~/.pteroclient/config.json so an existing
// desktop install needs no setup. That makes any test of it read the real
// user's panels and API keys, and print them on failure, so every config test
// has to be given a home of its own.
func isolateHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)        // unix
	t.Setenv("USERPROFILE", home) // windows
	t.Setenv("PTERO_MCP_CONFIG", "")
	t.Setenv("PTERO_PANELS", "")
	t.Setenv("PTERO_PANEL_URL", "")
	t.Setenv("PTERO_API_KEY", "")
	return home
}

func TestRegistryNeedsAURLAndAKey(t *testing.T) {
	if _, err := NewRegistry([]PanelSpec{{Name: "half", URL: "https://panel.example.com"}}); err == nil {
		t.Error("a panel with no key should not produce a usable registry")
	}
	if _, err := NewRegistry(nil); err == nil {
		t.Error("an empty registry should be refused")
	}
}

func TestRegistryRejectsDuplicateNames(t *testing.T) {
	_, err := NewRegistry([]PanelSpec{
		{Name: "main", URL: "https://a.example.com", APIKey: "k"},
		{Name: "main", URL: "https://b.example.com", APIKey: "k"},
	})
	if err == nil {
		t.Error("two panels with one name would make the panel argument ambiguous")
	}
}

func TestConfigAcceptsTheDesktopAppsFileUnchanged(t *testing.T) {
	dir := isolateHome(t)
	path := filepath.Join(dir, "config.json")
	// This is ~/.pteroclient/config.json as the Wails app writes it.
	os.WriteFile(path, []byte(`{
      "panels": [
        {"name": "Bloom", "panel_url": "https://mc.bloom.host", "api_key": "ptlc_a", "server_id": "aaa"},
        {"name": "Home",  "panel_url": "https://panel.lan",      "api_key": "ptlc_b"}
      ],
      "active_panel": "Home"
    }`), 0o600)

	specs, sources, err := loadPanels(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(specs) != 2 {
		t.Fatalf("expected both panels, got %+v", specs)
	}
	if len(sources) == 0 {
		t.Error("the source file was not reported")
	}

	// The panel the user last had active is the one they mean.
	if specs[0].Name != "Home" {
		t.Errorf("active_panel should come first, got %s", specs[0].Name)
	}

	registry, err := NewRegistry(specs)
	if err != nil {
		t.Fatal(err)
	}
	if registry.Default() != "Home" {
		t.Errorf("default was %s", registry.Default())
	}

	_, spec, err := registry.Resolve("Bloom")
	if err != nil {
		t.Fatal(err)
	}
	if spec.URL != "https://mc.bloom.host" || spec.DefaultServer != "aaa" {
		t.Errorf("the app's field names were not read: %+v", spec)
	}
}

func TestConfigReadsTheLegacySinglePanelShape(t *testing.T) {
	dir := isolateHome(t)
	path := filepath.Join(dir, "old.json")
	os.WriteFile(path, []byte(`{
      "panel_url": "https://panel.example.com", "api_key": "ptlc_old", "server_id": "old1"
    }`), 0o600)

	specs, _, err := loadPanels(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(specs) != 1 || specs[0].URL != "https://panel.example.com" || specs[0].DefaultServer != "old1" {
		t.Fatalf("got %+v", specs)
	}
}

func TestConfigFromTheEnvironment(t *testing.T) {
	isolateHome(t)
	t.Setenv("PTERO_PANEL_URL", "https://env.example.com")
	t.Setenv("PTERO_API_KEY", "ptlc_env")
	t.Setenv("PTERO_SERVER_ID", "envsrv")
	t.Setenv("PTERO_PANEL_NAME", "fromenv")

	specs, sources, err := loadPanels("")
	if err != nil {
		t.Fatal(err)
	}

	var found *PanelSpec
	for i := range specs {
		if specs[i].Name == "fromenv" {
			found = &specs[i]
		}
	}
	if found == nil {
		t.Fatalf("the environment panel was not picked up: %+v", specs)
	}
	if found.URL != "https://env.example.com" || found.DefaultServer != "envsrv" {
		t.Errorf("got %+v", *found)
	}
	if !strings.Contains(strings.Join(sources, ","), "environment") {
		t.Errorf("sources were %v", sources)
	}
}

func TestInlinePanelsJSONForContainers(t *testing.T) {
	isolateHome(t)
	t.Setenv("PTERO_PANELS", `[{"name":"inline","url":"https://inline.example.com","api_key":"ptlc_i"}]`)

	specs, _, err := loadPanels("")
	if err != nil {
		t.Fatal(err)
	}
	for _, spec := range specs {
		if spec.Name == "inline" && spec.URL == "https://inline.example.com" {
			return
		}
	}
	t.Fatalf("PTERO_PANELS was not read: %+v", specs)
}

func TestBadInlinePanelsJSONIsReported(t *testing.T) {
	isolateHome(t)
	t.Setenv("PTERO_PANELS", `not json`)
	if _, _, err := loadPanels(""); err == nil {
		t.Error("malformed PTERO_PANELS should stop startup rather than be ignored")
	}
}

func TestAnExplicitlyNamedConfigMustExist(t *testing.T) {
	isolateHome(t)
	// The optional locations are allowed to be absent; a path the operator
	// typed is a mistake worth stopping for.
	if _, _, err := loadPanels(filepath.Join(t.TempDir(), "nope.json")); err == nil {
		t.Error("a missing -config file should be an error")
	}
}

func TestMergePermissionsKeepsOrderAndDropsDuplicates(t *testing.T) {
	got := mergePermissions(
		[]string{"file.read", "control.console"},
		[]string{"control.console", "file.update"},
		[]string{"file.read"},
	)
	want := []string{"control.console", "file.update"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestFormatSize(t *testing.T) {
	cases := map[int64]string{0: "0 B", 512: "512 B", 1024: "1.0 KiB", 1240: "1.2 KiB", 1048576: "1.0 MiB"}
	for input, want := range cases {
		if got := formatSize(input); got != want {
			t.Errorf("formatSize(%d) = %q, want %q", input, got, want)
		}
	}
}
