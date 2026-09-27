package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestParseDestructiveTargets(t *testing.T) {
	cases := []struct {
		spec          string
		all           bool
		allowed       [][2]string // panel, server pairs that must be in scope
		refused       [][2]string
		expectFailure bool
	}{
		// The bare flag keeps the meaning it always had.
		{spec: "", all: true, allowed: [][2]string{{"main", "abc"}, {"other", "zzz"}}},
		{spec: "true", all: true, allowed: [][2]string{{"anything", "anything"}}},

		{spec: "false", refused: [][2]string{{"main", "abc"}}},
		{spec: "none", refused: [][2]string{{"main", "abc"}}},

		// A whole panel.
		{
			spec:    "lab",
			allowed: [][2]string{{"lab", "abc"}, {"lab", "def"}},
			refused: [][2]string{{"prod", "abc"}},
		},
		// Explicit wildcard form of the same thing.
		{
			spec:    "lab/*",
			allowed: [][2]string{{"lab", "whatever"}},
			refused: [][2]string{{"prod", "whatever"}},
		},
		// One server only. The rest of its own panel stays out.
		{
			spec:    "prod/1a7ce997",
			allowed: [][2]string{{"prod", "1a7ce997"}},
			refused: [][2]string{{"prod", "b2c3d4e5"}, {"lab", "1a7ce997"}},
		},
		// A list mixing both forms.
		{
			spec:    "lab, prod/1a7ce997",
			allowed: [][2]string{{"lab", "x"}, {"prod", "1a7ce997"}},
			refused: [][2]string{{"prod", "other"}, {"staging", "x"}},
		},
		// Panel names and ids fold, matching how the registry resolves a name.
		{
			spec:    "Prod/1A7CE997",
			allowed: [][2]string{{"prod", "1a7ce997"}, {"PROD", "1A7CE997"}},
		},

		{spec: "/abc", expectFailure: true},
		{spec: "prod/", expectFailure: true},
	}

	for _, tc := range cases {
		policy, err := parseDestructiveTargets(tc.spec)
		if tc.expectFailure {
			if err == nil {
				t.Errorf("%q should have been refused", tc.spec)
			}
			continue
		}
		if err != nil {
			t.Errorf("%q: %v", tc.spec, err)
			continue
		}
		if policy.all != tc.all {
			t.Errorf("%q: all = %v, want %v", tc.spec, policy.all, tc.all)
		}
		for _, target := range tc.allowed {
			if !policy.allows(target[0], target[1]) {
				t.Errorf("%q should allow %s/%s", tc.spec, target[0], target[1])
			}
		}
		for _, target := range tc.refused {
			if policy.allows(target[0], target[1]) {
				t.Errorf("%q must not allow %s/%s", tc.spec, target[0], target[1])
			}
		}
	}
}

func TestEmptyPolicyAllowsNothingAndRegistersNothing(t *testing.T) {
	var empty destructivePolicy
	if empty.any() {
		t.Error("a zero policy must not report any scope")
	}
	if empty.allows("main", "abc") {
		t.Error("a zero policy must allow nothing")
	}

	// The original property has to survive scoping: with nothing in scope the
	// tools are not registered at all, so they cannot be called and do not
	// appear in tools/list.
	panel, _ := fakePanel(t)
	server := build(t, permissions{write: true}, panel.URL)
	for _, name := range server.ToolNames() {
		if strings.Contains(name, "delete") || strings.Contains(name, "reinstall") {
			t.Errorf("%s was registered with no destructive scope", name)
		}
	}
}

func TestScopedPolicyRegistersButRefusesOutOfScopeServers(t *testing.T) {
	panel, requests := fakePanel(t)

	// The fake registry's panel is "main" with default server 1a7ce997.
	scoped := permissions{write: true, destroy: destructivePolicy{}}
	scoped.destroy.allowServer("main", "1a7ce997")
	server := build(t, scoped, panel.URL)

	// Registered, because something is in scope.
	found := false
	for _, name := range server.ToolNames() {
		if name == "ptero_files_delete" {
			found = true
		}
	}
	if !found {
		t.Fatal("ptero_files_delete should be registered when one server is in scope")
	}

	// In scope: goes through.
	if text, isError := invoke(t, server, "ptero_files_delete", map[string]interface{}{
		"files": []string{"latest.log"}, "confirm": true,
	}); isError {
		t.Fatalf("the in-scope server should be allowed: %s", text)
	}
	if len(*requests) != 1 {
		t.Fatalf("expected one panel call, got %+v", *requests)
	}

	// Out of scope: refused, and nothing reaches the panel.
	text, isError := invoke(t, server, "ptero_files_delete", map[string]interface{}{
		"server": "b2c3d4e5", "files": []string{"latest.log"}, "confirm": true,
	})
	if !isError {
		t.Fatalf("an out-of-scope server must be refused, got %q", text)
	}
	if !strings.Contains(text, "b2c3d4e5") || !strings.Contains(text, "main/1a7ce997") {
		t.Errorf("the refusal should name the target and the scope: %q", text)
	}
	if len(*requests) != 1 {
		t.Errorf("the out-of-scope call reached the panel: %+v", *requests)
	}
}

func TestScopeIsCheckedBeforeConfirm(t *testing.T) {
	panel, requests := fakePanel(t)
	scoped := permissions{write: true}
	scoped.destroy.allowServer("main", "1a7ce997")
	server := build(t, scoped, panel.URL)

	// Without confirm AND out of scope, the useful answer is the scope, since
	// adding confirm would not help.
	text, isError := invoke(t, server, "ptero_files_delete", map[string]interface{}{
		"server": "somewhere-else", "files": []string{"x"},
	})
	if !isError {
		t.Fatal("expected a refusal")
	}
	if strings.Contains(text, "confirm: true") {
		t.Errorf("an out-of-scope call should not be told to add confirm: %q", text)
	}
	if len(*requests) != 0 {
		t.Errorf("nothing should have reached the panel: %+v", *requests)
	}
}

func TestRawDeleteNeedsAnUnscopedGrant(t *testing.T) {
	panel, _ := fakePanel(t)

	// A free-form path cannot be checked against a per-server scope, so the
	// passthrough must not become the way around it.
	scoped := permissions{write: true, raw: true}
	scoped.destroy.allowServer("main", "1a7ce997")
	server := build(t, scoped, panel.URL)

	text, isError := invoke(t, server, "ptero_request", map[string]interface{}{
		"method": "DELETE", "path": "/servers/1a7ce997/backups/x",
	})
	if !isError {
		t.Fatalf("a scoped grant must not permit raw DELETE, got %q", text)
	}
	if !strings.Contains(text, "unscoped") {
		t.Errorf("the refusal should explain why: %q", text)
	}

	// The unscoped grant still permits it, as before.
	open := build(t, permissions{write: true, raw: true, destroy: destructivePolicy{all: true}}, panel.URL)
	if _, isError := invoke(t, open, "ptero_request", map[string]interface{}{
		"method": "DELETE", "path": "/servers/1a7ce997/backups/x",
	}); isError {
		t.Error("an unscoped grant should still allow raw DELETE")
	}
}

func TestPanelConfigCarriesItsOwnScope(t *testing.T) {
	cases := []struct {
		field   string
		allowed bool
		panel   string
		server  string
	}{
		{field: `true`, allowed: true, panel: "lab", server: "anything"},
		{field: `false`, allowed: false, panel: "lab", server: "anything"},
		{field: `null`, allowed: false, panel: "lab", server: "anything"},
		{field: `["1a7ce997"]`, allowed: true, panel: "lab", server: "1a7ce997"},
		{field: `["1a7ce997"]`, allowed: false, panel: "lab", server: "other"},
		{field: `["*"]`, allowed: true, panel: "lab", server: "anything"},
	}

	for _, tc := range cases {
		spec := PanelSpec{Name: "lab", URL: "https://lab.example.com", APIKey: "k",
			AllowDestructive: json.RawMessage(tc.field)}

		var policy destructivePolicy
		if err := spec.destructiveTargets(&policy); err != nil {
			t.Errorf("allow_destructive %s: %v", tc.field, err)
			continue
		}
		if got := policy.allows(tc.panel, tc.server); got != tc.allowed {
			t.Errorf("allow_destructive %s: allows(%s/%s) = %v, want %v",
				tc.field, tc.panel, tc.server, got, tc.allowed)
		}
	}
}

func TestPanelConfigRejectsAnUnreadableScope(t *testing.T) {
	spec := PanelSpec{Name: "lab", AllowDestructive: json.RawMessage(`{"server":"abc"}`)}
	var policy destructivePolicy
	err := spec.destructiveTargets(&policy)
	if err == nil {
		t.Fatal("an object should be refused; the field takes true, false or a list")
	}
	if !strings.Contains(err.Error(), "lab") {
		t.Errorf("the error should name the panel: %v", err)
	}
}

func TestOnePanelsScopeDoesNotLeakToAnother(t *testing.T) {
	// The whole point: marking a test panel disposable must not mark a
	// customer's panel disposable.
	var policy destructivePolicy
	for _, spec := range []PanelSpec{
		{Name: "lab", AllowDestructive: json.RawMessage(`true`)},
		{Name: "customer"},
	} {
		if err := spec.destructiveTargets(&policy); err != nil {
			t.Fatal(err)
		}
	}

	if !policy.allows("lab", "anything") {
		t.Error("lab should be in scope")
	}
	if policy.allows("customer", "anything") {
		t.Error("customer must not be in scope")
	}
	if policy.all {
		t.Error("a per-panel grant must not become a global one")
	}
}

func TestDescribeNamesTheScope(t *testing.T) {
	var all destructivePolicy
	all.all = true
	if got := all.describe(); got != "every panel and server" {
		t.Errorf("got %q", got)
	}

	var none destructivePolicy
	if got := none.describe(); got != "nothing" {
		t.Errorf("got %q", got)
	}

	var mixed destructivePolicy
	mixed.allowPanel("lab")
	mixed.allowServer("prod", "1a7ce997")
	// A whole-panel grant swallows its own per-server entries rather than
	// listing them twice.
	mixed.allowServer("lab", "ignored")
	description := mixed.describe()
	if !strings.Contains(description, "lab/*") || !strings.Contains(description, "prod/1a7ce997") {
		t.Errorf("got %q", description)
	}
	if strings.Contains(description, "lab/ignored") {
		t.Errorf("a whole-panel grant should not also list its servers: %q", description)
	}
}

func TestStartupCautionNamesTheScope(t *testing.T) {
	scoped := permissions{write: true}
	scoped.destroy.allowServer("main", "1a7ce997")

	summary := summarize(t, onePanel(), scoped, options{})
	flowing := strings.Join(strings.Fields(summary), " ")

	// "destructive tools are enabled" on its own is what made the old flag
	// dangerous: it did not say where.
	if !strings.Contains(flowing, "destructive tools are enabled on main/1a7ce997") {
		t.Errorf("the caution should name the scope:\n%s", summary)
	}
}

func TestScopedToolDescriptionSaysWhereItApplies(t *testing.T) {
	panel, _ := fakePanel(t)
	scoped := permissions{write: true}
	scoped.destroy.allowServer("main", "1a7ce997")
	server := build(t, scoped, panel.URL)

	raw := server.Handle(context.Background(), []byte(`{"jsonrpc":"2.0","id":9,"method":"tools/list"}`))
	var reply struct {
		Result struct {
			Tools []struct {
				Name        string `json:"name"`
				Description string `json:"description"`
			} `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal(raw, &reply); err != nil {
		t.Fatal(err)
	}

	for _, tool := range reply.Result.Tools {
		if tool.Name != "ptero_files_delete" {
			continue
		}
		// A model reading tools/list should learn the limit before it spends a
		// call discovering it.
		if !strings.Contains(tool.Description, "main/1a7ce997") {
			t.Errorf("the description should name the scope: %q", tool.Description)
		}
		return
	}
	t.Fatal("ptero_files_delete was not in tools/list")
}

func TestDestructiveFlagAcceptsBareAndScopedForms(t *testing.T) {
	var policy destructivePolicy
	flag := &destructiveFlag{policy: &policy}

	// Bare, which is what the flag package passes for a boolean flag.
	if err := flag.Set("true"); err != nil {
		t.Fatal(err)
	}
	if !policy.all {
		t.Error("the bare flag should still mean everything")
	}
	if !flag.IsBoolFlag() {
		t.Error("the flag has to report itself as boolean for the bare form to parse")
	}

	if err := flag.Set("lab,prod/1a7ce997"); err != nil {
		t.Fatal(err)
	}
	if policy.all {
		t.Error("a scoped value must clear the global grant")
	}
	if !policy.allows("lab", "x") || !policy.allows("prod", "1a7ce997") {
		t.Error("the scoped value was not applied")
	}
	if policy.allows("prod", "other") {
		t.Error("the scoped value granted too much")
	}

	if err := flag.Set("/nope"); err == nil {
		t.Error("a malformed target should be refused at parse time")
	}
}

func TestReadOnlyStillOverridesAnyScope(t *testing.T) {
	panel, _ := fakePanel(t)
	server := build(t, permissions{write: true, readOnly: true,
		destroy: destructivePolicy{all: true}}, panel.URL)

	for _, name := range server.ToolNames() {
		if strings.Contains(name, "delete") {
			t.Errorf("%s was registered in read-only mode", name)
		}
	}
}
