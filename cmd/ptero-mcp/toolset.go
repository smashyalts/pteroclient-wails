package main

import (
	"context"
	"fmt"
	"net/url"
	"path"
	"strings"

	"pteroclient-wails/pkg/mcp"
	"pteroclient-wails/pkg/pteroapi"
)

// tier is how dangerous a tool is, which decides whether it is registered at
// all.
//
// Gating at registration rather than inside the handler is deliberate: a tool
// that is not registered cannot be called, cannot be described in tools/list,
// and cannot be talked into running by anything a panel or a config file says.
type tier int

const (
	// tierRead only reads. Always available.
	tierRead tier = iota

	// tierWrite changes something that can be changed back — a file
	// written over a file the caller just read, a server restarted, a
	// schedule created.
	tierWrite

	// tierDestroy can lose data that is not coming back: deleting files,
	// restoring or deleting a backup, reinstalling a server. Off unless
	// -allow-destructive, and each call also has to pass confirm: true.
	tierDestroy

	// tierAccount changes the panel account itself — its password, its
	// email, its two-factor secret, its API keys. Off unless
	// -allow-account. Separate from tierDestroy because wanting an agent
	// to clear a crashed server's logs is not wanting it to rotate the
	// account's credentials.
	tierAccount

	// tierRaw is the passthrough tool, which can reach any route including
	// ones added by a panel fork. Off unless -allow-raw, and still bound by
	// the write and destroy gates for the method it is given.
	tierRaw
)

// permissions is what this process was started willing to do.
type permissions struct {
	write     bool
	destroy   bool
	account   bool
	raw       bool
	readOnly  bool
	nameGlobs []string
}

// toolset builds and registers the tools against a panel registry.
type toolset struct {
	registry *Registry
	allow    permissions

	// registered and skipped are reported at startup so an operator can see
	// what the flags actually produced.
	registered int
	skipped    int
}

var (
	panelField = mcp.Str("panel",
		"Configured panel to act on. Omit to use the default panel. Use ptero_panels_list to see the names.")

	serverField = mcp.Str("server",
		"Server identifier: the short id from the panel URL, e.g. 1a7ce997. Omit to use the panel's configured default_server.")
)

// permitted reports whether a tier is enabled.
func (t *toolset) permitted(level tier) bool {
	switch level {
	case tierRead:
		return true
	case tierWrite:
		return t.allow.write && !t.allow.readOnly
	case tierDestroy:
		return t.allow.destroy && !t.allow.readOnly
	case tierAccount:
		return t.allow.account && !t.allow.readOnly
	case tierRaw:
		return t.allow.raw
	}
	return false
}

// matchesFilter applies the -tools name filter.
func (t *toolset) matchesFilter(name string) bool {
	if len(t.allow.nameGlobs) == 0 {
		return true
	}
	for _, pattern := range t.allow.nameGlobs {
		if pattern == "" {
			continue
		}
		if matched, err := path.Match(pattern, name); err == nil && matched {
			return true
		}
		// A bare word is treated as a prefix, so -tools=ptero_files gets
		// the whole file group without the caller writing a glob.
		if strings.HasPrefix(name, pattern) {
			return true
		}
	}
	return false
}

// add registers one tool if its tier and name allow it.
func (t *toolset) add(server *mcp.Server, level tier, tool mcp.Tool) {
	if !t.permitted(level) || !t.matchesFilter(tool.Name) {
		t.skipped++
		return
	}

	tool.ReadOnly = level == tierRead
	tool.Destructive = level == tierDestroy

	if level == tierDestroy {
		// The flag says this process may destroy things; confirm says this
		// call meant to. Two gates, because one mistaken tool call should
		// not be able to empty a server's world folder.
		tool.Input.Fields = append(tool.Input.Fields, mcp.Bool("confirm",
			"Must be true. This action cannot be undone, so it is refused without an explicit confirmation.").Req())

		inner := tool.Handler
		tool.Handler = func(ctx context.Context, args mcp.Args) (string, error) {
			if !args.Bool("confirm", false) {
				return "", fmt.Errorf("refused: %s destroys data and needs confirm: true", tool.Name)
			}
			return inner(ctx, args)
		}
		tool.Description += " Irreversible; requires confirm: true."
	}

	server.Register(tool)
	t.registered++
}

// target resolves the panel and server a call names.
func (t *toolset) target(args mcp.Args) (*pteroapi.Client, string, error) {
	client, spec, err := t.registry.Resolve(args.String("panel", ""))
	if err != nil {
		return nil, "", err
	}

	server := strings.TrimSpace(args.String("server", spec.DefaultServer))
	if server == "" {
		return nil, "", fmt.Errorf("no server given, and panel %q has no default_server configured", spec.Name)
	}
	// A pasted panel URL instead of an id is a common slip and cheap to
	// recover from: the id is the last path segment.
	if strings.Contains(server, "/") {
		parts := strings.Split(strings.Trim(server, "/"), "/")
		server = parts[len(parts)-1]
	}
	return client, server, nil
}

// panelOnly resolves just the panel, for the account and listing routes that
// are not about one server.
func (t *toolset) panelOnly(args mcp.Args) (*pteroapi.Client, error) {
	client, _, err := t.registry.Resolve(args.String("panel", ""))
	return client, err
}

// call performs a request against a server route and renders the reply.
func (t *toolset) call(ctx context.Context, args mcp.Args, method, suffix string, query url.Values, body interface{}) (string, error) {
	client, server, err := t.target(args)
	if err != nil {
		return "", err
	}
	value, err := client.JSON(ctx, pteroapi.Request{
		Method: method,
		Path:   "/servers/" + server + suffix,
		Query:  query,
		JSON:   body,
	})
	if err != nil {
		return "", err
	}
	return pteroapi.Pretty(value), nil
}

// callPanel performs a request against a non-server route.
func (t *toolset) callPanel(ctx context.Context, args mcp.Args, method, routePath string, query url.Values, body interface{}) (string, error) {
	client, err := t.panelOnly(args)
	if err != nil {
		return "", err
	}
	value, err := client.JSON(ctx, pteroapi.Request{
		Method: method,
		Path:   routePath,
		Query:  query,
		JSON:   body,
	})
	if err != nil {
		return "", err
	}
	return pteroapi.Pretty(value), nil
}

// pageQuery builds the pagination and filter parameters the list routes share.
func pageQuery(args mcp.Args) url.Values {
	query := url.Values{}
	if page := args.Int("page", 0); page > 0 {
		query.Set("page", fmt.Sprint(page))
	}
	if perPage := args.Int("per_page", 0); perPage > 0 {
		query.Set("per_page", fmt.Sprint(perPage))
	}
	return query
}

var (
	pageField    = mcp.Int("page", "Page number, 1-based. Omit for the first page.")
	perPageField = mcp.Int("per_page", "Rows per page. The panel's own cap applies.")
)
