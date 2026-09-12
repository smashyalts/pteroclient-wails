package main

import (
	"strings"
	"testing"

	"pteroclient-wails/pkg/mcp"
)

// summarize renders the startup summary for a given set of panels and gates.
func summarize(t *testing.T, specs []PanelSpec, allow permissions, opts options) string {
	t.Helper()

	registry, err := NewRegistry(specs)
	if err != nil {
		t.Fatal(err)
	}
	set := &toolset{registry: registry, allow: allow}
	server := mcp.NewServer(serverName, version)
	set.registerServerTools(server)

	var out strings.Builder
	printSummary(&out, opts, registry, set, server, nil)
	return out.String()
}

func onePanel() []PanelSpec {
	return []PanelSpec{{Name: "main", URL: "https://panel.example.com", APIKey: "ptlc_a"}}
}

func twoPanels() []PanelSpec {
	return []PanelSpec{
		{Name: "main", URL: "https://panel.example.com", APIKey: "ptlc_a"},
		{Name: "customer", URL: "https://customer.example.com", APIKey: "ptlc_b"},
	}
}

func TestStartupPrintsTheLiabilityNotice(t *testing.T) {
	summary := summarize(t, onePanel(), writable(), options{})

	// The notice is hard-wrapped for the terminal, so a phrase that reads as
	// one sentence can span a line break. Match on the text, not the layout.
	flowing := strings.Join(strings.Fields(summary), " ")

	// The person who suffers a mistake is whoever launched the process, and
	// the launch is the last moment they are guaranteed to be looking.
	for _, want := range []string{
		"NO WARRANTY, NO LIABILITY",
		"at your own risk",
		"wrong panel",
		"Take backups",
		"They do not remove it",
		"contributors accept liability",
	} {
		if !strings.Contains(flowing, want) {
			t.Errorf("the notice is missing %q:\n%s", want, summary)
		}
	}
}

func TestStartupPrintsTheLicenceNotice(t *testing.T) {
	flowing := strings.Join(strings.Fields(summarize(t, onePanel(), writable(), options{})), " ")

	// The GPL asks a program that reads commands to announce its copyright,
	// the absence of warranty, and where the terms are. This reads MCP
	// commands, so the startup notice is where that belongs.
	for _, want := range []string{
		"Copyright (C) 2025-2026 smashyalts",
		"ABSOLUTELY NO WARRANTY",
		"GNU General Public License version 3",
		"LICENSE file",
	} {
		if !strings.Contains(flowing, want) {
			t.Errorf("the licence notice is missing %q", want)
		}
	}
}

func TestQuietSuppressesTheWholeSummary(t *testing.T) {
	// run() skips printSummary entirely under -quiet, so the notice is shown
	// once to the operator who then chooses to silence their own logs. This
	// records that the notice lives in the summary rather than beside it.
	if !strings.Contains(riskNotice, "-quiet") {
		t.Error("the notice should say how to silence it")
	}
}

func TestMultiplePanelsRaiseACaution(t *testing.T) {
	single := summarize(t, onePanel(), writable(), options{})
	if strings.Contains(single, "panels are reachable") {
		t.Errorf("one panel should not warn about several:\n%s", single)
	}

	several := summarize(t, twoPanels(), writable(), options{})
	if !strings.Contains(several, "2 panels are reachable") {
		t.Errorf("the multi-panel caution is missing:\n%s", several)
	}
	// This is the failure the user actually worries about: an existing but
	// wrong panel name is not something any gate can catch.
	if !strings.Contains(several, "wrong panel") {
		t.Errorf("the caution should say what goes wrong:\n%s", several)
	}
}

func TestDestructiveAndRawGatesRaiseCautions(t *testing.T) {
	plain := summarize(t, onePanel(), writable(), options{})
	if strings.Contains(plain, "destructive tools are enabled") {
		t.Errorf("the default run should not warn about destructive tools:\n%s", plain)
	}
	if strings.Contains(plain, "raw passthrough is enabled") {
		t.Errorf("the default run should not warn about the passthrough:\n%s", plain)
	}

	open := summarize(t, onePanel(), permissions{write: true, destroy: true, raw: true}, options{})
	if !strings.Contains(open, "destructive tools are enabled") {
		t.Errorf("the destructive caution is missing:\n%s", open)
	}
	if !strings.Contains(open, "raw passthrough is enabled") {
		t.Errorf("the passthrough caution is missing:\n%s", open)
	}
}

func TestSummaryNeverPrintsAnAPIKey(t *testing.T) {
	summary := summarize(t, twoPanels(), permissions{write: true, destroy: true, account: true, raw: true},
		options{httpAddr: ":8472", token: "super-secret-bearer"})

	for _, secret := range []string{"ptlc_a", "ptlc_b", "super-secret-bearer"} {
		if strings.Contains(summary, secret) {
			t.Errorf("%q leaked into the startup summary:\n%s", secret, summary)
		}
	}
	// The presence of auth is worth reporting; its value is not.
	if !strings.Contains(summary, "bearer token") {
		t.Errorf("the summary should say that auth is configured:\n%s", summary)
	}
}
