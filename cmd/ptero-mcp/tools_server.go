package main

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"pteroclient-wails/pkg/mcp"
	"pteroclient-wails/pkg/pteroapi"
)

// registerServerTools covers the panel itself, the account, and everything
// about a server that is not a file: its record, its state, its console, its
// startup configuration and its activity log.
func (t *toolset) registerServerTools(s *mcp.Server) {
	// ----------------------------------------------------------- discovery

	t.add(s, tierRead, mcp.Tool{
		Name: "ptero_panels_list",
		Description: "List the Pterodactyl panels this server is configured for, and which one is " +
			"used when a tool call names none. Call this first when more than one panel may exist. " +
			"API keys are never returned.",
		Input: mcp.In(),
		Handler: func(ctx context.Context, args mcp.Args) (string, error) {
			names := t.registry.Names()
			rows := make([]map[string]interface{}, 0, len(names))
			for _, name := range names {
				_, spec, err := t.registry.Resolve(name)
				if err != nil {
					continue
				}
				row := map[string]interface{}{
					"name":       spec.Name,
					"url":        spec.URL,
					"is_default": name == t.registry.Default(),
				}
				if spec.DefaultServer != "" {
					row["default_server"] = spec.DefaultServer
				}
				rows = append(rows, row)
			}
			return pteroapi.Pretty(map[string]interface{}{"panels": rows}), nil
		},
	})

	t.add(s, tierRead, mcp.Tool{
		Name: "ptero_permissions",
		Description: "List every permission key the panel understands, grouped by subject. Use it " +
			"before creating or updating a subuser, since the panel rejects keys it does not know.",
		Input: mcp.In(panelField),
		Handler: func(ctx context.Context, args mcp.Args) (string, error) {
			return t.callPanel(ctx, args, http.MethodGet, "/permissions", nil, nil)
		},
	})

	// ------------------------------------------------------------- servers

	t.add(s, tierRead, mcp.Tool{
		Name: "ptero_servers_list",
		Description: "List the servers the API key can reach on a panel. Returns each server's " +
			"identifier, which every other tool takes as its server argument.",
		Input: mcp.In(
			panelField,
			mcp.Str("name_contains", "Only return servers whose name contains this text."),
			mcp.Str("type", "Scope of the listing. 'owner' is servers the account owns; the admin "+
				"scopes need an API key with admin rights.").Of("owner", "admin", "admin-all"),
			pageField,
			perPageField,
		),
		Handler: func(ctx context.Context, args mcp.Args) (string, error) {
			query := pageQuery(args)
			if name := args.String("name_contains", ""); name != "" {
				query.Set("filter[name]", name)
			}
			if scope := args.String("type", ""); scope != "" {
				query.Set("type", scope)
			}
			return t.callPanel(ctx, args, http.MethodGet, "/", query, nil)
		},
	})

	t.add(s, tierRead, mcp.Tool{
		Name: "ptero_server_get",
		Description: "Read one server's record: name, description, limits, feature limits, egg, " +
			"allocations and SFTP endpoint.",
		Input: mcp.In(
			panelField, serverField,
			mcp.Str("include", "Extra relationships to embed, comma separated, e.g. 'egg,subusers'."),
		),
		Handler: func(ctx context.Context, args mcp.Args) (string, error) {
			query := url.Values{}
			if include := args.String("include", ""); include != "" {
				query.Set("include", include)
			}
			return t.call(ctx, args, http.MethodGet, "", query, nil)
		},
	})

	t.add(s, tierRead, mcp.Tool{
		Name: "ptero_server_resources",
		Description: "Read a server's current power state and live resource usage: CPU, memory, " +
			"disk, network and uptime.",
		Input: mcp.In(panelField, serverField),
		Handler: func(ctx context.Context, args mcp.Args) (string, error) {
			return t.call(ctx, args, http.MethodGet, "/resources", nil, nil)
		},
	})

	t.add(s, tierRead, mcp.Tool{
		Name: "ptero_server_activity",
		Description: "Read a server's activity log, newest first: who did what, from which IP, and when. " +
			"This is where to look for why a server was stopped or a file changed.",
		Input: mcp.In(panelField, serverField, pageField, perPageField),
		Handler: func(ctx context.Context, args mcp.Args) (string, error) {
			query := pageQuery(args)
			query.Set("sort", "-timestamp")
			query.Set("include", "actor")
			return t.call(ctx, args, http.MethodGet, "/activity", query, nil)
		},
	})

	// --------------------------------------------------------------- power

	t.add(s, tierWrite, mcp.Tool{
		Name: "ptero_power",
		Description: "Send a power signal to a server. 'stop' asks the process to exit and 'restart' " +
			"stops then starts it; 'kill' terminates it immediately, which can corrupt a world or " +
			"database mid-write, so prefer stop unless the server is already unresponsive.",
		Input: mcp.In(
			panelField, serverField,
			mcp.Str("signal", "Power signal to send.").Of("start", "stop", "restart", "kill").Req(),
		),
		Handler: func(ctx context.Context, args mcp.Args) (string, error) {
			signal := args.String("signal", "")
			switch signal {
			case "start", "stop", "restart", "kill":
			default:
				return "", fmt.Errorf("signal must be start, stop, restart or kill, not %q", signal)
			}
			return t.call(ctx, args, http.MethodPost, "/power", nil, map[string]string{"signal": signal})
		},
	})

	t.add(s, tierWrite, mcp.Tool{
		Name: "ptero_command",
		Description: "Send one console command to a running server. The panel accepts the command " +
			"without returning its output; use ptero_console_exec when the output matters. Fails if " +
			"the server is not running.",
		Input: mcp.In(
			panelField, serverField,
			mcp.Str("command", "Command line to send, without a leading slash unless the game wants one.").Req(),
		),
		Handler: func(ctx context.Context, args mcp.Args) (string, error) {
			return t.call(ctx, args, http.MethodPost, "/command", nil,
				map[string]string{"command": args.String("command", "")})
		},
	})

	// ------------------------------------------------------------- console

	consoleFields := []mcp.Field{
		panelField, serverField,
		mcp.Int("wait_seconds", "How long to keep reading after the backlog. Raise it when waiting "+
			"for a slow startup.").Def(5),
		mcp.Int("max_lines", "Most lines to return, keeping the newest.").Def(200),
		mcp.Str("until", "Stop reading early at the first line containing this text, e.g. 'Done ('."),
		mcp.Bool("include_backlog", "Include the console history the panel has stored.").Def(true),
		mcp.Bool("keep_ansi", "Keep terminal colour escapes. Off by default; they are noise here.").Def(false),
	}

	t.add(s, tierRead, mcp.Tool{
		Name: "ptero_console_tail",
		Description: "Read a server's live console over its websocket and return the lines. This is " +
			"the only way to see crash traces, plugin errors and startup output, none of which the " +
			"REST routes expose. Reads only; it sends nothing to the server.",
		Input: mcp.In(consoleFields...),
		Handler: func(ctx context.Context, args mcp.Args) (string, error) {
			return t.tailConsole(ctx, args, "", "")
		},
	})

	t.add(s, tierWrite, mcp.Tool{
		Name: "ptero_console_exec",
		Description: "Send a console command and return the console output that follows it, in one " +
			"call. Use this rather than ptero_command whenever the reply matters — a plugin list, a " +
			"whitelist check, the result of a save. Raise wait_seconds for a command that takes time.",
		Input: mcp.In(append([]mcp.Field{
			mcp.Str("command", "Command to send once the console is connected.").Req(),
		}, consoleFields...)...),
		Handler: func(ctx context.Context, args mcp.Args) (string, error) {
			// The backlog is off by default here: the point of the call is
			// what the command produced, not the hour of log before it.
			return t.tailConsole(ctx, args, args.String("command", ""), "")
		},
	})

	t.add(s, tierWrite, mcp.Tool{
		Name: "ptero_console_power_watch",
		Description: "Send a power signal and return the console output it produces, in one call. " +
			"Use it to start a server and see whether it actually came up, instead of polling the " +
			"resources route. Pair it with until='Done' or a similar ready line.",
		Input: mcp.In(append([]mcp.Field{
			mcp.Str("signal", "Power signal to send.").Of("start", "stop", "restart", "kill").Req(),
		}, consoleFields...)...),
		Handler: func(ctx context.Context, args mcp.Args) (string, error) {
			signal := args.String("signal", "")
			switch signal {
			case "start", "stop", "restart", "kill":
			default:
				return "", fmt.Errorf("signal must be start, stop, restart or kill, not %q", signal)
			}
			return t.tailConsole(ctx, args, "", signal)
		},
	})

	// -------------------------------------------------------- startup, etc

	t.add(s, tierRead, mcp.Tool{
		Name: "ptero_startup_get",
		Description: "Read a server's startup configuration: the egg's variables with their current " +
			"values and rules, the resolved and raw startup commands, and the docker images the egg allows.",
		Input: mcp.In(panelField, serverField),
		Handler: func(ctx context.Context, args mcp.Args) (string, error) {
			return t.call(ctx, args, http.MethodGet, "/startup", nil, nil)
		},
	})

	t.add(s, tierWrite, mcp.Tool{
		Name: "ptero_startup_set_variable",
		Description: "Set one startup variable by its environment key. Read ptero_startup_get first: " +
			"the panel refuses a value that breaks the variable's rules, and refuses any variable the " +
			"egg marks as not user-editable. Takes effect on the next start.",
		Input: mcp.In(
			panelField, serverField,
			mcp.Str("key", "Environment variable name, e.g. SERVER_JARFILE.").Req(),
			mcp.Str("value", "New value.").Req(),
		),
		Handler: func(ctx context.Context, args mcp.Args) (string, error) {
			return t.call(ctx, args, http.MethodPut, "/startup/variable", nil, map[string]string{
				"key":   args.String("key", ""),
				"value": args.String("value", ""),
			})
		},
	})

	t.add(s, tierWrite, mcp.Tool{
		Name:        "ptero_server_rename",
		Description: "Change a server's display name and description. Cosmetic; nothing restarts.",
		Input: mcp.In(
			panelField, serverField,
			mcp.Str("name", "New display name.").Req(),
			mcp.Str("description", "New description. Send an empty string to clear it."),
		),
		Handler: func(ctx context.Context, args mcp.Args) (string, error) {
			return t.call(ctx, args, http.MethodPost, "/settings/rename", nil, map[string]string{
				"name":        args.String("name", ""),
				"description": args.String("description", ""),
			})
		},
	})

	t.add(s, tierWrite, mcp.Tool{
		Name: "ptero_server_set_docker_image",
		Description: "Switch a server to another docker image. Only the images the egg lists are " +
			"accepted; ptero_startup_get returns them. Takes effect on the next start.",
		Input: mcp.In(
			panelField, serverField,
			mcp.Str("docker_image", "Image reference exactly as the egg lists it.").Req(),
		),
		Handler: func(ctx context.Context, args mcp.Args) (string, error) {
			return t.call(ctx, args, http.MethodPut, "/settings/docker-image", nil, map[string]string{
				"docker_image": args.String("docker_image", ""),
			})
		},
	})

	t.add(s, tierDestroy, mcp.Tool{
		Name: "ptero_server_reinstall",
		Description: "Re-run the egg's install script on a server. On many eggs this replaces the " +
			"server files, so worlds, configs and plugins can be lost. Take a backup first.",
		Input: mcp.In(panelField, serverField),
		Handler: func(ctx context.Context, args mcp.Args) (string, error) {
			return t.call(ctx, args, http.MethodPost, "/settings/reinstall", nil, nil)
		},
	})

	// ------------------------------------------------------------- account

	t.add(s, tierRead, mcp.Tool{
		Name:        "ptero_account_get",
		Description: "Read the panel account the API key belongs to: id, username, email, admin flag.",
		Input:       mcp.In(panelField),
		Handler: func(ctx context.Context, args mcp.Args) (string, error) {
			return t.callPanel(ctx, args, http.MethodGet, "/account", nil, nil)
		},
	})

	t.add(s, tierRead, mcp.Tool{
		Name:        "ptero_account_activity",
		Description: "Read the account's own activity log, newest first: logins, key changes, and server actions.",
		Input:       mcp.In(panelField, pageField, perPageField),
		Handler: func(ctx context.Context, args mcp.Args) (string, error) {
			query := pageQuery(args)
			query.Set("sort", "-timestamp")
			return t.callPanel(ctx, args, http.MethodGet, "/account/activity", query, nil)
		},
	})

	t.add(s, tierRead, mcp.Tool{
		Name: "ptero_account_api_keys_list",
		Description: "List the account's client API keys. Only the identifiers and descriptions are " +
			"stored by the panel, so no secrets come back.",
		Input: mcp.In(panelField),
		Handler: func(ctx context.Context, args mcp.Args) (string, error) {
			return t.callPanel(ctx, args, http.MethodGet, "/account/api-keys", nil, nil)
		},
	})

	t.add(s, tierRead, mcp.Tool{
		Name:        "ptero_account_ssh_keys_list",
		Description: "List the SSH public keys the account uses for SFTP.",
		Input:       mcp.In(panelField),
		Handler: func(ctx context.Context, args mcp.Args) (string, error) {
			return t.callPanel(ctx, args, http.MethodGet, "/account/ssh-keys", nil, nil)
		},
	})

	t.add(s, tierAccount, mcp.Tool{
		Name: "ptero_account_api_key_create",
		Description: "Create a client API key on the account. The reply contains the secret, which " +
			"the panel never shows again — treat the output as a credential.",
		Input: mcp.In(
			panelField,
			mcp.Str("description", "What the key is for.").Req(),
			mcp.StrList("allowed_ips", "IP addresses or CIDR ranges allowed to use the key. Omit to allow any."),
		),
		Handler: func(ctx context.Context, args mcp.Args) (string, error) {
			body := map[string]interface{}{"description": args.String("description", "")}
			if ips := args.StringList("allowed_ips"); len(ips) > 0 {
				body["allowed_ips"] = ips
			}
			return t.callPanel(ctx, args, http.MethodPost, "/account/api-keys", nil, body)
		},
	})

	t.add(s, tierAccount, mcp.Tool{
		Name:        "ptero_account_api_key_delete",
		Description: "Revoke one client API key by its identifier. Anything using that key stops working.",
		Input: mcp.In(
			panelField,
			mcp.Str("identifier", "Key identifier from ptero_account_api_keys_list.").Req(),
		),
		Handler: func(ctx context.Context, args mcp.Args) (string, error) {
			return t.callPanel(ctx, args, http.MethodDelete,
				"/account/api-keys/"+url.PathEscape(args.String("identifier", "")), nil, nil)
		},
	})

	t.add(s, tierAccount, mcp.Tool{
		Name:        "ptero_account_ssh_key_create",
		Description: "Add an SSH public key to the account for SFTP access.",
		Input: mcp.In(
			panelField,
			mcp.Str("name", "Label for the key.").Req(),
			mcp.Str("public_key", "The public key, in OpenSSH format.").Req(),
		),
		Handler: func(ctx context.Context, args mcp.Args) (string, error) {
			return t.callPanel(ctx, args, http.MethodPost, "/account/ssh-keys", nil, map[string]string{
				"name":       args.String("name", ""),
				"public_key": args.String("public_key", ""),
			})
		},
	})

	t.add(s, tierAccount, mcp.Tool{
		Name:        "ptero_account_ssh_key_delete",
		Description: "Remove an SSH public key from the account by fingerprint.",
		Input: mcp.In(
			panelField,
			mcp.Str("fingerprint", "Fingerprint from ptero_account_ssh_keys_list.").Req(),
		),
		Handler: func(ctx context.Context, args mcp.Args) (string, error) {
			return t.callPanel(ctx, args, http.MethodPost, "/account/ssh-keys/remove", nil, map[string]string{
				"fingerprint": args.String("fingerprint", ""),
			})
		},
	})
}

// tailConsole is shared by the three console tools.
func (t *toolset) tailConsole(ctx context.Context, args mcp.Args, command, power string) (string, error) {
	client, server, err := t.target(args)
	if err != nil {
		return "", err
	}

	// A command's own output is the point when one was sent, so the stored
	// backlog is only included by default for a plain tail.
	backlogDefault := command == "" && power == ""

	opts := pteroapi.ConsoleOptions{
		Server:      server,
		Command:     command,
		Power:       power,
		Wait:        time.Duration(args.Int("wait_seconds", 5)) * time.Second,
		MaxLines:    args.Int("max_lines", 200),
		Until:       args.String("until", ""),
		SkipBacklog: !args.Bool("include_backlog", backlogDefault),
		KeepANSI:    args.Bool("keep_ansi", false),
	}

	tail, err := client.TailConsole(ctx, opts)
	if err != nil {
		return "", err
	}

	// Rendered as a header plus raw lines rather than as JSON: a console log
	// inside a JSON array is every quote escaped and every newline spelled
	// out, which is harder to read and larger than the log itself.
	var out strings.Builder
	fmt.Fprintf(&out, "server: %s", tail.Server)
	if tail.State != "" {
		fmt.Fprintf(&out, "   state: %s", tail.State)
	}
	fmt.Fprintf(&out, "   lines: %d", len(tail.Lines))
	if tail.Truncated {
		out.WriteString(" (older lines dropped)")
	}
	if tail.Matched {
		fmt.Fprintf(&out, "   stopped at: %q", args.String("until", ""))
	}
	out.WriteString("\n")
	if tail.Note != "" {
		fmt.Fprintf(&out, "note: %s\n", tail.Note)
	}
	if tail.Stats != nil {
		fmt.Fprintf(&out, "stats: %s\n", compactStats(tail.Stats))
	}

	if len(tail.Lines) == 0 {
		out.WriteString("--- no console output in the window ---\n")
		return out.String(), nil
	}

	out.WriteString("--- console ---\n")
	out.WriteString(strings.Join(tail.Lines, "\n"))
	out.WriteString("\n")
	return out.String(), nil
}

// compactStats renders the resource frame on one line.
func compactStats(stats map[string]interface{}) string {
	keys := []string{"cpu_absolute", "memory_bytes", "disk_bytes", "uptime"}
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		if value, ok := stats[key]; ok {
			parts = append(parts, fmt.Sprintf("%s=%v", key, value))
		}
	}
	if len(parts) == 0 {
		return pteroapi.Pretty(stats)
	}
	return strings.Join(parts, " ")
}
