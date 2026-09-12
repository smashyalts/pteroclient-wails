// Command ptero-mcp is a self-hostable Model Context Protocol server for the
// Pterodactyl panel's client API.
//
// It exposes the whole client API as MCP tools so an assistant can read a
// server's console, search and edit its files, manage its databases,
// schedules, backups, allocations and subusers, and control its power state.
//
// Run it over stdio for a local assistant:
//
//	ptero-mcp
//
// or over HTTP to host it for several clients:
//
//	ptero-mcp -http :8472 -token "$SECRET"
//
// What it is willing to do is decided at startup rather than per call. Reads
// and ordinary writes are on; anything that loses data, and anything that
// touches the panel account's own credentials, needs its own flag. A tool
// that was not enabled is not registered, so it cannot be called and does not
// appear in the tool list.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"pteroclient-wails/pkg/mcp"
)

const (
	serverName = "ptero-mcp"
	version    = "1.0.0"
	userAgent  = serverName + "/" + version
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", serverName, err)
		os.Exit(1)
	}
}

type options struct {
	configPath string

	httpAddr       string
	token          string
	allowedOrigins string
	requestTimeout time.Duration

	readOnly         bool
	allowDestructive bool
	allowAccount     bool
	allowRaw         bool

	toolFilter string
	listTools  bool
	showVer    bool
	quiet      bool
}

func run() error {
	var opts options

	flag.StringVar(&opts.configPath, "config", "",
		"path to a panels config file; by default ~/.ptero-mcp/panels.json and the desktop app's ~/.pteroclient/config.json are both read")
	flag.StringVar(&opts.httpAddr, "http", envOr("PTERO_MCP_ADDR", ""),
		"listen address for the HTTP transport, e.g. :8472; omit to speak MCP over stdio")
	flag.StringVar(&opts.token, "token", os.Getenv("PTERO_MCP_TOKEN"),
		"bearer token clients must present in HTTP mode; required unless the listener is loopback only")
	flag.StringVar(&opts.allowedOrigins, "allow-origin", os.Getenv("PTERO_MCP_ALLOW_ORIGIN"),
		"comma-separated browser origins allowed to call the HTTP transport; non-browser clients are always allowed")
	flag.DurationVar(&opts.requestTimeout, "request-timeout", 5*time.Minute,
		"ceiling on one tool call in HTTP mode")

	flag.BoolVar(&opts.readOnly, "read-only", envBool("PTERO_MCP_READ_ONLY"),
		"register only tools that read; nothing can be changed")
	flag.BoolVar(&opts.allowDestructive, "allow-destructive", envBool("PTERO_MCP_ALLOW_DESTRUCTIVE"),
		"also register tools that lose data: deleting files, restoring or deleting backups, reinstalling a server. Each call additionally needs confirm: true")
	flag.BoolVar(&opts.allowAccount, "allow-account", envBool("PTERO_MCP_ALLOW_ACCOUNT"),
		"also register tools that change the panel account's API keys and SSH keys")
	flag.BoolVar(&opts.allowRaw, "allow-raw", envBool("PTERO_MCP_ALLOW_RAW"),
		"also register ptero_request, which can call any client API route by path")

	flag.StringVar(&opts.toolFilter, "tools", os.Getenv("PTERO_MCP_TOOLS"),
		"comma-separated tool names, prefixes or globs to register; omit for all of them")
	flag.BoolVar(&opts.listTools, "list-tools", false, "print the tools that would be registered, then exit")
	flag.BoolVar(&opts.showVer, "version", false, "print the version, then exit")
	flag.BoolVar(&opts.quiet, "quiet", false, "do not write the startup summary to stderr")

	flag.Parse()

	if opts.showVer {
		fmt.Println(userAgent)
		return nil
	}

	specs, sources, err := loadPanels(opts.configPath)
	if err != nil {
		return err
	}
	if len(specs) == 0 {
		return errors.New(noPanelsHelp)
	}

	registry, err := NewRegistry(specs)
	if err != nil {
		return err
	}

	set := &toolset{
		registry: registry,
		allow: permissions{
			write:     true, // reads and ordinary writes are the point of this
			destroy:   opts.allowDestructive,
			account:   opts.allowAccount,
			raw:       opts.allowRaw,
			readOnly:  opts.readOnly,
			nameGlobs: splitList(opts.toolFilter),
		},
	}

	server := mcp.NewServer(serverName, version)
	set.registerServerTools(server)
	set.registerFileTools(server)
	set.registerManagementTools(server)
	set.registerRawTool(server)

	if set.registered == 0 {
		return fmt.Errorf("no tools were registered; check the -tools filter %q", opts.toolFilter)
	}

	if opts.listTools {
		for _, name := range server.ToolNames() {
			fmt.Println(name)
		}
		return nil
	}

	if !opts.quiet {
		// stderr, always: in stdio mode stdout carries the protocol and one
		// stray line on it is a parse error at the client.
		printSummary(os.Stderr, opts, registry, set, server, sources)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if opts.httpAddr != "" {
		return serveHTTP(ctx, server, opts)
	}
	return mcp.ServeStdio(ctx, server, os.Stdin, os.Stdout)
}

func serveHTTP(ctx context.Context, server *mcp.Server, opts options) error {
	httpServer, err := mcp.NewHTTPServer(server, mcp.HTTPOptions{
		Addr:           opts.httpAddr,
		Token:          opts.token,
		AllowedOrigins: splitList(opts.allowedOrigins),
		RequestTimeout: opts.requestTimeout,
	})
	if err != nil {
		return err
	}

	done := make(chan error, 1)
	go func() {
		err := httpServer.ListenAndServe()
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		done <- err
	}()

	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return httpServer.Shutdown(shutdownCtx)
	}
}

func printSummary(out *os.File, opts options, registry *Registry, set *toolset, server *mcp.Server, sources []string) {
	fmt.Fprintf(out, "%s\n", userAgent)

	names := registry.Names()
	fmt.Fprintf(out, "  panels:    %s (default: %s)\n", strings.Join(names, ", "), registry.Default())
	if len(sources) > 0 {
		fmt.Fprintf(out, "  config:    %s\n", strings.Join(sources, ", "))
	}

	fmt.Fprintf(out, "  tools:     %d registered, %d withheld\n", set.registered, set.skipped)

	enabled := []string{"read"}
	if set.permitted(tierWrite) {
		enabled = append(enabled, "write")
	}
	if set.permitted(tierDestroy) {
		enabled = append(enabled, "destructive")
	}
	if set.permitted(tierAccount) {
		enabled = append(enabled, "account")
	}
	if set.permitted(tierRaw) {
		enabled = append(enabled, "raw")
	}
	fmt.Fprintf(out, "  allowed:   %s\n", strings.Join(enabled, ", "))

	if opts.httpAddr != "" {
		scheme := "http"
		fmt.Fprintf(out, "  transport: %s://%s/mcp\n", scheme, opts.httpAddr)
		if opts.token == "" {
			fmt.Fprintf(out, "  auth:      none (loopback only)\n")
		} else {
			fmt.Fprintf(out, "  auth:      bearer token\n")
		}
	} else {
		fmt.Fprintf(out, "  transport: stdio\n")
	}
}

const noPanelsHelp = `no panel configured.

Give one in the environment:

    PTERO_PANEL_URL=https://panel.example.com
    PTERO_API_KEY=ptlc_...
    PTERO_SERVER_ID=1a7ce997        # optional, becomes the default server

or write ~/.ptero-mcp/panels.json:

    {
      "panels": [
        {
          "name": "main",
          "url": "https://panel.example.com",
          "api_key": "ptlc_...",
          "default_server": "1a7ce997"
        }
      ]
    }

An existing install of the desktop client is picked up automatically from
~/.pteroclient/config.json, so its panels need no second copy.

Create a client API key in the panel under Account, API Credentials.`

func envOr(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func envBool(name string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(name))) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

func splitList(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}
