package main

import (
	"fmt"
	"sort"
	"strings"
)

// destructivePolicy says where destructive tools may run: everywhere, on whole
// panels, or on named servers.
//
// The original gate was one process-wide boolean, which meant enabling file
// deletion for a disposable test server also enabled it for every customer
// panel the same process could reach. Scoping it is the difference between "I
// may delete things here" and "I may delete things".
//
// Nothing here is a substitute for an API key scoped to the servers it should
// reach. This is the second lock, not the first.
type destructivePolicy struct {
	// all is the bare -allow-destructive: every panel, every server.
	all bool

	// panels are whole panels, keyed by folded name.
	panels map[string]bool

	// servers are single servers, keyed by folded panel name then folded
	// server id.
	servers map[string]map[string]bool
}

func fold(value string) string { return strings.ToLower(strings.TrimSpace(value)) }

// allowPanel puts a whole panel in scope.
func (p *destructivePolicy) allowPanel(panel string) {
	if p.panels == nil {
		p.panels = map[string]bool{}
	}
	p.panels[fold(panel)] = true
}

// allowServer puts one server on one panel in scope.
func (p *destructivePolicy) allowServer(panel, server string) {
	if p.servers == nil {
		p.servers = map[string]map[string]bool{}
	}
	key := fold(panel)
	if p.servers[key] == nil {
		p.servers[key] = map[string]bool{}
	}
	p.servers[key][fold(server)] = true
}

// any reports whether destructive tools are allowed anywhere at all.
//
// Registration still hangs on this: when nothing is in scope the tools are
// never registered, so they cannot be called, do not appear in tools/list, and
// cannot be talked into running. Scoping adds a second gate at call time
// rather than replacing the first one.
func (p destructivePolicy) any() bool {
	return p.all || len(p.panels) > 0 || len(p.servers) > 0
}

// allows reports whether a specific panel and server are in scope.
func (p destructivePolicy) allows(panel, server string) bool {
	if p.all {
		return true
	}
	key := fold(panel)
	if p.panels[key] {
		return true
	}
	return p.servers[key][fold(server)]
}

// describe renders the scope for the startup summary and for error messages.
func (p destructivePolicy) describe() string {
	if p.all {
		return "every panel and server"
	}
	if !p.any() {
		return "nothing"
	}

	targets := make([]string, 0, len(p.panels)+len(p.servers))
	for panel := range p.panels {
		targets = append(targets, panel+"/* (whole panel)")
	}
	for panel, servers := range p.servers {
		for server := range servers {
			if p.panels[panel] {
				continue // already covered by the whole-panel entry
			}
			targets = append(targets, panel+"/"+server)
		}
	}
	sort.Strings(targets)
	return strings.Join(targets, ", ")
}

// parseDestructiveTargets reads the -allow-destructive value.
//
// Accepted: an empty string or "true" for everything, which is what the bare
// flag produces and keeps the original meaning; "false", "none" or "off" for
// nothing; otherwise a comma-separated list of "panel" for a whole panel and
// "panel/server" for one server.
func parseDestructiveTargets(spec string) (destructivePolicy, error) {
	var policy destructivePolicy

	trimmed := strings.TrimSpace(spec)
	switch fold(trimmed) {
	case "", "true", "all", "yes", "on", "1":
		policy.all = true
		return policy, nil
	case "false", "none", "off", "0":
		return policy, nil
	}

	for _, entry := range strings.Split(trimmed, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}

		panel, server, hasServer := strings.Cut(entry, "/")
		panel = strings.TrimSpace(panel)
		server = strings.TrimSpace(server)

		if panel == "" {
			return destructivePolicy{}, fmt.Errorf(
				"%q names no panel; write panel or panel/server", entry)
		}
		// A trailing slash with nothing after it is a typo, not a wildcard;
		// "panel/*" is the explicit way to say the whole panel.
		if hasServer && server == "" {
			return destructivePolicy{}, fmt.Errorf(
				"%q ends in a slash with no server; write %q for the whole panel", entry, panel)
		}

		switch {
		case !hasServer, server == "*":
			policy.allowPanel(panel)
		default:
			policy.allowServer(panel, server)
		}
	}

	if !policy.any() {
		return destructivePolicy{}, fmt.Errorf("%q named no targets", spec)
	}
	return policy, nil
}

// destructiveFlag adapts the policy to the flag package.
//
// IsBoolFlag lets the same flag work bare (-allow-destructive, meaning
// everything, as it always did) and with a value (-allow-destructive=main/abc).
type destructiveFlag struct {
	policy *destructivePolicy
	set    bool
	raw    string
}

func (f *destructiveFlag) String() string {
	if f == nil || f.policy == nil {
		return ""
	}
	return f.policy.describe()
}

func (f *destructiveFlag) Set(value string) error {
	parsed, err := parseDestructiveTargets(value)
	if err != nil {
		return err
	}
	*f.policy = parsed
	f.set = true
	f.raw = value
	return nil
}

// IsBoolFlag makes "-allow-destructive" on its own legal. It also means the
// flag never consumes the following argument, so "-allow-destructive main/abc"
// leaves main/abc as a positional argument; run() reports that rather than
// silently granting everything.
func (f *destructiveFlag) IsBoolFlag() bool { return true }
