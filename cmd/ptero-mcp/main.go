// Copyright (C) 2025-2026 smashyalts
//
// This program is free software: you can redistribute it and/or modify it
// under the terms of the GNU General Public License, version 3, as published
// by the Free Software Foundation.
//
// This program is distributed in the hope that it will be useful, but WITHOUT
// ANY WARRANTY; without even the implied warranty of MERCHANTABILITY or
// FITNESS FOR A PARTICULAR PURPOSE. See the GNU General Public License for
// more details. You should have received a copy of it along with this program
// in the LICENSE file at the root of this repository; if not, see
// <https://www.gnu.org/licenses/>.

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
	"io"
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

func printSummary(out io.Writer, opts options, registry *Registry, set *toolset, server *mcp.Server, sources []string) {
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

	printCautions(out, registry, set)
	fmt.Fprint(out, riskNotice)
}

// printCautions names the two conditions that turn a mistake into someone
// else's outage: more than one panel behind one process, and destructive
// tools being available at all.
//
// The gates refuse a panel name that does not exist, but they cannot know
// that a name which does exist is the wrong one. Saying how many panels are
// reachable is the only warning that can honestly be given.
func printCautions(out io.Writer, registry *Registry, set *toolset) {
	if count := len(registry.Names()); count > 1 {
		fmt.Fprintf(out, "\n  CAUTION: %d panels are reachable from this process. A tool call that\n"+
			"           names an existing but wrong panel reaches that panel's servers.\n"+
			"           Run one process per panel if they belong to different people.\n", count)
	}
	if set.permitted(tierDestroy) {
		fmt.Fprint(out, "\n  CAUTION: destructive tools are enabled. Files, databases, schedules and\n"+
			"           backups can be deleted, and a server can be reinstalled over.\n")
	}
	if set.permitted(tierRaw) {
		fmt.Fprint(out, "\n  CAUTION: the raw passthrough is enabled. It can reach any client API\n"+
			"           route, including ones no other tool exposes.\n")
	}
}

// riskNotice is printed at every start that prints a summary.
//
// It is here rather than only in the README because the person who suffers a
// mistake is whoever launched the process, and the launch is the last moment
// they are guaranteed to be looking.
const riskNotice = `
  NO WARRANTY, NO LIABILITY. This software is provided as is. It drives live
  game servers over a real panel API, and it is driven by an AI assistant
  that can pick the wrong panel or the wrong server, misread a file, or run a
  command nobody asked for. The gates above reduce that risk. They do not
  remove it.

  You run this at your own risk and you are responsible for what it does to
  your servers, your players and your data. Take backups. Give it an API key
  scoped to only the servers it should reach. Neither the authors nor the
  contributors accept liability for any loss, downtime or damage arising from
  its use.

  ptero-mcp  Copyright (C) 2025-2026 smashyalts. This program comes with
  ABSOLUTELY NO WARRANTY. It is free software under the GNU General Public
  License version 3, and you are welcome to redistribute it under those
  terms; see the LICENSE file for the conditions, including sections 15 and
  16, which are the warranty and liability terms that govern.

  Suppress this notice with -quiet once you have read it.

`

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
