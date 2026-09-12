package main

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"pteroclient-wails/pkg/mcp"
	"pteroclient-wails/pkg/pteroapi"
)

// registerManagementTools covers databases, schedules, network allocations,
// subusers and backups.
func (t *toolset) registerManagementTools(s *mcp.Server) {
	t.registerDatabaseTools(s)
	t.registerScheduleTools(s)
	t.registerNetworkTools(s)
	t.registerSubuserTools(s)
	t.registerBackupTools(s)
}

// ------------------------------------------------------------------ databases

func (t *toolset) registerDatabaseTools(s *mcp.Server) {
	t.add(s, tierRead, mcp.Tool{
		Name: "ptero_databases_list",
		Description: "List a server's databases with their hosts, usernames and connection strings. " +
			"Set include_password to have the panel return the passwords too, which it withholds " +
			"unless asked.",
		Input: mcp.In(
			panelField, serverField,
			mcp.Bool("include_password", "Also return each database's password.").Def(false),
		),
		Handler: func(ctx context.Context, args mcp.Args) (string, error) {
			query := url.Values{}
			if args.Bool("include_password", false) {
				query.Set("include", "password")
			}
			return t.call(ctx, args, http.MethodGet, "/databases", query, nil)
		},
	})

	t.add(s, tierWrite, mcp.Tool{
		Name: "ptero_database_create",
		Description: "Create a database on a server. The panel prefixes the name with the server's " +
			"id, and counts it against the server's database limit.",
		Input: mcp.In(
			panelField, serverField,
			mcp.Str("name", "Database name, without the panel's prefix.").Req(),
			mcp.Str("remote", "Connection mask the database may be reached from. '%' allows any host.").Def("%"),
		),
		Handler: func(ctx context.Context, args mcp.Args) (string, error) {
			return t.call(ctx, args, http.MethodPost, "/databases", nil, map[string]string{
				"database": args.String("name", ""),
				"remote":   args.String("remote", "%"),
			})
		},
	})

	t.add(s, tierWrite, mcp.Tool{
		Name: "ptero_database_rotate_password",
		Description: "Generate a new password for a database and return it. Anything still using " +
			"the old password stops connecting, so update the server's config too.",
		Input: mcp.In(
			panelField, serverField,
			mcp.Str("database", "Database id from ptero_databases_list.").Req(),
		),
		Handler: func(ctx context.Context, args mcp.Args) (string, error) {
			id := url.PathEscape(args.String("database", ""))
			return t.call(ctx, args, http.MethodPost, "/databases/"+id+"/rotate-password", nil, nil)
		},
	})

	t.add(s, tierDestroy, mcp.Tool{
		Name:        "ptero_database_delete",
		Description: "Delete a database and everything in it.",
		Input: mcp.In(
			panelField, serverField,
			mcp.Str("database", "Database id from ptero_databases_list.").Req(),
		),
		Handler: func(ctx context.Context, args mcp.Args) (string, error) {
			id := url.PathEscape(args.String("database", ""))
			return t.call(ctx, args, http.MethodDelete, "/databases/"+id, nil, nil)
		},
	})
}

// ------------------------------------------------------------------ schedules

var cronFields = []mcp.Field{
	mcp.Str("minute", "Cron minute field, e.g. '0' or '*/15'."),
	mcp.Str("hour", "Cron hour field, e.g. '4' or '*'."),
	mcp.Str("day_of_month", "Cron day-of-month field."),
	mcp.Str("month", "Cron month field."),
	mcp.Str("day_of_week", "Cron day-of-week field."),
}

func (t *toolset) registerScheduleTools(s *mcp.Server) {
	t.add(s, tierRead, mcp.Tool{
		Name:        "ptero_schedules_list",
		Description: "List a server's schedules with their cron expressions, next run times and tasks.",
		Input:       mcp.In(panelField, serverField),
		Handler: func(ctx context.Context, args mcp.Args) (string, error) {
			return t.call(ctx, args, http.MethodGet, "/schedules", nil, nil)
		},
	})

	t.add(s, tierRead, mcp.Tool{
		Name:        "ptero_schedule_get",
		Description: "Read one schedule and its task list.",
		Input: mcp.In(
			panelField, serverField,
			mcp.Str("schedule", "Schedule id.").Req(),
		),
		Handler: func(ctx context.Context, args mcp.Args) (string, error) {
			id := url.PathEscape(args.String("schedule", ""))
			return t.call(ctx, args, http.MethodGet, "/schedules/"+id, nil, nil)
		},
	})

	t.add(s, tierWrite, mcp.Tool{
		Name: "ptero_schedule_create",
		Description: "Create a schedule. All five cron fields are required — the panel rejects an " +
			"empty one rather than widening it, which is the safer behaviour. Add tasks to it with " +
			"ptero_schedule_task_create; a schedule with no tasks does nothing.",
		Input: mcp.In(append([]mcp.Field{
			panelField, serverField,
			mcp.Str("name", "Schedule name.").Req(),
			mcp.Bool("is_active", "Whether the schedule runs.").Def(true),
			mcp.Bool("only_when_online", "Skip runs while the server is offline.").Def(false),
		}, cronFields...)...),
		Handler: func(ctx context.Context, args mcp.Args) (string, error) {
			body := map[string]interface{}{
				"name":             args.String("name", ""),
				"is_active":        args.Bool("is_active", true),
				"only_when_online": args.Bool("only_when_online", false),
			}
			for _, field := range cronFields {
				value := args.String(field.Name, "")
				if value == "" {
					return "", fmt.Errorf("%s is required; the panel rejects a schedule with an empty cron field", field.Name)
				}
				body[field.Name] = value
			}
			return t.call(ctx, args, http.MethodPost, "/schedules", nil, body)
		},
	})

	t.add(s, tierWrite, mcp.Tool{
		Name: "ptero_schedule_update",
		Description: "Change a schedule. Only the fields given are changed: the panel's own route " +
			"replaces the whole schedule, so this reads the current one first and sends the rest " +
			"back unchanged. Use it to pause a schedule without retyping its cron.",
		Input: mcp.In(append([]mcp.Field{
			panelField, serverField,
			mcp.Str("schedule", "Schedule id.").Req(),
			mcp.Str("name", "New name. Omit to keep the current one."),
			mcp.Bool("is_active", "Whether the schedule runs. Omit to keep the current setting."),
			mcp.Bool("only_when_online", "Skip runs while offline. Omit to keep the current setting."),
		}, cronFields...)...),
		Handler: func(ctx context.Context, args mcp.Args) (string, error) {
			client, server, err := t.target(args)
			if err != nil {
				return "", err
			}
			id := url.PathEscape(args.String("schedule", ""))

			current, err := client.JSON(ctx, pteroapi.Request{
				Method: http.MethodGet,
				Path:   "/servers/" + server + "/schedules/" + id,
			})
			if err != nil {
				return "", fmt.Errorf("could not read the schedule before changing it: %w", err)
			}

			existing, ok := current.(map[string]interface{})
			if !ok {
				return "", fmt.Errorf("the panel returned a schedule this tool could not read")
			}

			body := map[string]interface{}{
				"name":             pickString(args, "name", existing, "name"),
				"is_active":        pickBool(args, "is_active", existing, "is_active", true),
				"only_when_online": pickBool(args, "only_when_online", existing, "only_when_online", false),
			}

			cron, _ := existing["cron"].(map[string]interface{})
			for _, field := range cronFields {
				body[field.Name] = pickString(args, field.Name, cron, field.Name)
			}

			value, err := client.JSON(ctx, pteroapi.Request{
				Method: http.MethodPost,
				Path:   "/servers/" + server + "/schedules/" + id,
				JSON:   body,
			})
			if err != nil {
				return "", err
			}
			return pteroapi.Pretty(value), nil
		},
	})

	t.add(s, tierWrite, mcp.Tool{
		Name: "ptero_schedule_execute",
		Description: "Run a schedule now, ignoring its cron. Its tasks run in their usual order, " +
			"including any power or backup task.",
		Input: mcp.In(
			panelField, serverField,
			mcp.Str("schedule", "Schedule id.").Req(),
		),
		Handler: func(ctx context.Context, args mcp.Args) (string, error) {
			id := url.PathEscape(args.String("schedule", ""))
			return t.call(ctx, args, http.MethodPost, "/schedules/"+id+"/execute", nil, nil)
		},
	})

	t.add(s, tierDestroy, mcp.Tool{
		Name:        "ptero_schedule_delete",
		Description: "Delete a schedule and all of its tasks.",
		Input: mcp.In(
			panelField, serverField,
			mcp.Str("schedule", "Schedule id.").Req(),
		),
		Handler: func(ctx context.Context, args mcp.Args) (string, error) {
			id := url.PathEscape(args.String("schedule", ""))
			return t.call(ctx, args, http.MethodDelete, "/schedules/"+id, nil, nil)
		},
	})

	taskFields := []mcp.Field{
		panelField, serverField,
		mcp.Str("schedule", "Schedule id the task belongs to.").Req(),
		mcp.Str("action", "What the task does.").Of("command", "power", "backup").Req(),
		mcp.Str("payload", "For 'command', the command line. For 'power', the signal. For 'backup', "+
			"the newline-separated ignore list, which may be empty."),
		mcp.Int("time_offset", "Seconds to wait after the previous task.").Def(0),
		mcp.Bool("continue_on_failure", "Run later tasks even if this one fails.").Def(false),
	}

	t.add(s, tierWrite, mcp.Tool{
		Name:        "ptero_schedule_task_create",
		Description: "Add a task to a schedule.",
		Input:       mcp.In(taskFields...),
		Handler: func(ctx context.Context, args mcp.Args) (string, error) {
			id := url.PathEscape(args.String("schedule", ""))
			return t.call(ctx, args, http.MethodPost, "/schedules/"+id+"/tasks", nil, taskBody(args))
		},
	})

	t.add(s, tierWrite, mcp.Tool{
		Name: "ptero_schedule_task_update",
		Description: "Replace one task on a schedule. Every field is sent, so give them all — the " +
			"panel's route is a full replace.",
		Input: mcp.In(append([]mcp.Field{
			mcp.Str("task", "Task id.").Req(),
		}, taskFields...)...),
		Handler: func(ctx context.Context, args mcp.Args) (string, error) {
			schedule := url.PathEscape(args.String("schedule", ""))
			task := url.PathEscape(args.String("task", ""))
			return t.call(ctx, args, http.MethodPost,
				"/schedules/"+schedule+"/tasks/"+task, nil, taskBody(args))
		},
	})

	t.add(s, tierDestroy, mcp.Tool{
		Name:        "ptero_schedule_task_delete",
		Description: "Remove one task from a schedule.",
		Input: mcp.In(
			panelField, serverField,
			mcp.Str("schedule", "Schedule id.").Req(),
			mcp.Str("task", "Task id.").Req(),
		),
		Handler: func(ctx context.Context, args mcp.Args) (string, error) {
			schedule := url.PathEscape(args.String("schedule", ""))
			task := url.PathEscape(args.String("task", ""))
			return t.call(ctx, args, http.MethodDelete, "/schedules/"+schedule+"/tasks/"+task, nil, nil)
		},
	})
}

func taskBody(args mcp.Args) map[string]interface{} {
	return map[string]interface{}{
		"action":              args.String("action", ""),
		"payload":             args.String("payload", ""),
		"time_offset":         args.Int("time_offset", 0),
		"continue_on_failure": args.Bool("continue_on_failure", false),
	}
}

// ---------------------------------------------------------------- allocations

func (t *toolset) registerNetworkTools(s *mcp.Server) {
	t.add(s, tierRead, mcp.Tool{
		Name:        "ptero_allocations_list",
		Description: "List a server's network allocations: IP, port, alias, notes, and which is primary.",
		Input:       mcp.In(panelField, serverField),
		Handler: func(ctx context.Context, args mcp.Args) (string, error) {
			return t.call(ctx, args, http.MethodGet, "/network/allocations", nil, nil)
		},
	})

	t.add(s, tierWrite, mcp.Tool{
		Name: "ptero_allocation_assign",
		Description: "Assign another allocation to the server from its node's free ports. The panel " +
			"picks the port and refuses once the server's allocation limit is reached.",
		Input: mcp.In(panelField, serverField),
		Handler: func(ctx context.Context, args mcp.Args) (string, error) {
			return t.call(ctx, args, http.MethodPost, "/network/allocations", nil, nil)
		},
	})

	t.add(s, tierWrite, mcp.Tool{
		Name:        "ptero_allocation_set_notes",
		Description: "Set the note on an allocation, which is how a port gets labelled in the panel.",
		Input: mcp.In(
			panelField, serverField,
			mcp.Str("allocation", "Allocation id from ptero_allocations_list.").Req(),
			mcp.Str("notes", "Note text. An empty string clears it.").Req(),
		),
		Handler: func(ctx context.Context, args mcp.Args) (string, error) {
			id := url.PathEscape(args.String("allocation", ""))
			return t.call(ctx, args, http.MethodPost, "/network/allocations/"+id, nil,
				map[string]string{"notes": args.String("notes", "")})
		},
	})

	t.add(s, tierWrite, mcp.Tool{
		Name: "ptero_allocation_set_primary",
		Description: "Make an allocation the server's primary one. The primary port is what the " +
			"startup command binds to, so this usually needs a restart to take effect.",
		Input: mcp.In(
			panelField, serverField,
			mcp.Str("allocation", "Allocation id.").Req(),
		),
		Handler: func(ctx context.Context, args mcp.Args) (string, error) {
			id := url.PathEscape(args.String("allocation", ""))
			return t.call(ctx, args, http.MethodPost, "/network/allocations/"+id+"/primary", nil, nil)
		},
	})

	t.add(s, tierDestroy, mcp.Tool{
		Name: "ptero_allocation_unassign",
		Description: "Remove an allocation from the server, returning the port to the node. Anything " +
			"connecting to that port stops working, and the panel refuses to remove the primary one.",
		Input: mcp.In(
			panelField, serverField,
			mcp.Str("allocation", "Allocation id.").Req(),
		),
		Handler: func(ctx context.Context, args mcp.Args) (string, error) {
			id := url.PathEscape(args.String("allocation", ""))
			return t.call(ctx, args, http.MethodDelete, "/network/allocations/"+id, nil, nil)
		},
	})
}

// ------------------------------------------------------------------- subusers

func (t *toolset) registerSubuserTools(s *mcp.Server) {
	t.add(s, tierRead, mcp.Tool{
		Name:        "ptero_subusers_list",
		Description: "List the users with access to a server and the permissions each one holds.",
		Input:       mcp.In(panelField, serverField),
		Handler: func(ctx context.Context, args mcp.Args) (string, error) {
			return t.call(ctx, args, http.MethodGet, "/users", nil, nil)
		},
	})

	t.add(s, tierRead, mcp.Tool{
		Name:        "ptero_subuser_get",
		Description: "Read one subuser's record and permission list.",
		Input: mcp.In(
			panelField, serverField,
			mcp.Str("user", "Subuser UUID from ptero_subusers_list.").Req(),
		),
		Handler: func(ctx context.Context, args mcp.Args) (string, error) {
			id := url.PathEscape(args.String("user", ""))
			return t.call(ctx, args, http.MethodGet, "/users/"+id, nil, nil)
		},
	})

	t.add(s, tierWrite, mcp.Tool{
		Name: "ptero_subuser_create",
		Description: "Invite a user to a server by email with a set of permissions. Permission keys " +
			"come from ptero_permissions; the panel rejects any it does not recognise. If the email " +
			"has no panel account, the panel creates one and emails an invitation.",
		Input: mcp.In(
			panelField, serverField,
			mcp.Str("email", "Email address of the user to invite.").Req(),
			mcp.StrList("permissions", "Permission keys to grant, e.g. 'control.console', 'file.read'.").Req(),
		),
		Handler: func(ctx context.Context, args mcp.Args) (string, error) {
			permissions := args.StringList("permissions")
			if len(permissions) == 0 {
				return "", fmt.Errorf("give at least one permission; a subuser with none cannot do anything")
			}
			return t.call(ctx, args, http.MethodPost, "/users", nil, map[string]interface{}{
				"email":       args.String("email", ""),
				"permissions": permissions,
			})
		},
	})

	t.add(s, tierWrite, mcp.Tool{
		Name: "ptero_subuser_update",
		Description: "Change a subuser's permissions. Give permissions to replace the set outright, " +
			"or grant and revoke to adjust the set the user already has — the panel's own route " +
			"only replaces, so this reads the current permissions first.",
		Input: mcp.In(
			panelField, serverField,
			mcp.Str("user", "Subuser UUID.").Req(),
			mcp.StrList("permissions", "Complete new permission set, replacing the current one."),
			mcp.StrList("grant", "Permission keys to add to the current set."),
			mcp.StrList("revoke", "Permission keys to remove from the current set."),
		),
		Handler: func(ctx context.Context, args mcp.Args) (string, error) {
			client, server, err := t.target(args)
			if err != nil {
				return "", err
			}
			id := url.PathEscape(args.String("user", ""))

			replacement := args.StringList("permissions")
			grant := args.StringList("grant")
			revoke := args.StringList("revoke")

			if len(replacement) == 0 && len(grant) == 0 && len(revoke) == 0 {
				return "", fmt.Errorf("give permissions, or grant, or revoke")
			}

			final := replacement
			if len(final) == 0 {
				current, err := client.JSON(ctx, pteroapi.Request{
					Method: http.MethodGet,
					Path:   "/servers/" + server + "/users/" + id,
				})
				if err != nil {
					return "", fmt.Errorf("could not read the subuser before changing it: %w", err)
				}
				existing, _ := current.(map[string]interface{})
				held, _ := existing["permissions"].([]interface{})
				for _, entry := range held {
					if key, ok := entry.(string); ok {
						final = append(final, key)
					}
				}
			}

			final = mergePermissions(final, grant, revoke)
			if len(final) == 0 {
				return "", fmt.Errorf("that would leave the subuser with no permissions; " +
					"use ptero_subuser_delete to remove their access instead")
			}

			value, err := client.JSON(ctx, pteroapi.Request{
				Method: http.MethodPost,
				Path:   "/servers/" + server + "/users/" + id,
				JSON:   map[string]interface{}{"permissions": final},
			})
			if err != nil {
				return "", err
			}
			return pteroapi.Pretty(value), nil
		},
	})

	t.add(s, tierDestroy, mcp.Tool{
		Name:        "ptero_subuser_delete",
		Description: "Revoke a user's access to a server. Their panel account is untouched.",
		Input: mcp.In(
			panelField, serverField,
			mcp.Str("user", "Subuser UUID.").Req(),
		),
		Handler: func(ctx context.Context, args mcp.Args) (string, error) {
			id := url.PathEscape(args.String("user", ""))
			return t.call(ctx, args, http.MethodDelete, "/users/"+id, nil, nil)
		},
	})
}

// mergePermissions applies grants and revocations to a permission set,
// keeping it free of duplicates.
func mergePermissions(current, grant, revoke []string) []string {
	held := make(map[string]bool, len(current)+len(grant))
	order := make([]string, 0, len(current)+len(grant))

	keep := func(key string) {
		if key == "" || held[key] {
			return
		}
		held[key] = true
		order = append(order, key)
	}
	for _, key := range current {
		keep(key)
	}
	for _, key := range grant {
		keep(key)
	}
	for _, key := range revoke {
		if held[key] {
			delete(held, key)
		}
	}

	out := make([]string, 0, len(held))
	for _, key := range order {
		if held[key] {
			out = append(out, key)
		}
	}
	return out
}

// -------------------------------------------------------------------- backups

func (t *toolset) registerBackupTools(s *mcp.Server) {
	t.add(s, tierRead, mcp.Tool{
		Name: "ptero_backups_list",
		Description: "List a server's backups with their sizes, lock state and whether each one " +
			"completed. The meta section carries how many of the server's backup slots are used.",
		Input: mcp.In(panelField, serverField, pageField, perPageField),
		Handler: func(ctx context.Context, args mcp.Args) (string, error) {
			return t.call(ctx, args, http.MethodGet, "/backups", pageQuery(args), nil)
		},
	})

	t.add(s, tierRead, mcp.Tool{
		Name: "ptero_backup_get",
		Description: "Read one backup's record, including whether it finished and whether it " +
			"succeeded. A backup that is still running reports no completion time.",
		Input: mcp.In(
			panelField, serverField,
			mcp.Str("backup", "Backup UUID from ptero_backups_list.").Req(),
		),
		Handler: func(ctx context.Context, args mcp.Args) (string, error) {
			id := url.PathEscape(args.String("backup", ""))
			return t.call(ctx, args, http.MethodGet, "/backups/"+id, nil, nil)
		},
	})

	t.add(s, tierWrite, mcp.Tool{
		Name: "ptero_backup_create",
		Description: "Start a backup. It runs in the background, so the reply is a record with no " +
			"completion time yet; poll ptero_backup_get to see it finish. Counts against the " +
			"server's backup limit, and the panel refuses once that limit is reached.",
		Input: mcp.In(
			panelField, serverField,
			mcp.Str("name", "Backup name. Omit to let the panel name it after the current time."),
			mcp.Str("ignored", "Paths to leave out, one per line, in .gitignore syntax."),
			mcp.Bool("locked", "Create it locked, so it cannot be deleted or rotated away.").Def(false),
		),
		Handler: func(ctx context.Context, args mcp.Args) (string, error) {
			body := map[string]interface{}{"is_locked": args.Bool("locked", false)}
			if name := args.String("name", ""); name != "" {
				body["name"] = name
			}
			if ignored := args.String("ignored", ""); ignored != "" {
				body["ignored"] = ignored
			}
			return t.call(ctx, args, http.MethodPost, "/backups", nil, body)
		},
	})

	t.add(s, tierWrite, mcp.Tool{
		Name: "ptero_backup_toggle_lock",
		Description: "Toggle a backup's lock. A locked backup cannot be deleted and is not rotated " +
			"away when the server reaches its backup limit. This flips the current state rather " +
			"than setting it, so read the backup first if you need to be sure.",
		Input: mcp.In(
			panelField, serverField,
			mcp.Str("backup", "Backup UUID.").Req(),
		),
		Handler: func(ctx context.Context, args mcp.Args) (string, error) {
			id := url.PathEscape(args.String("backup", ""))
			return t.call(ctx, args, http.MethodPost, "/backups/"+id+"/lock", nil, nil)
		},
	})

	t.add(s, tierRead, mcp.Tool{
		Name: "ptero_backup_download_url",
		Description: "Get a short-lived signed URL for downloading a backup archive. The URL carries " +
			"its own token, so anyone holding it can fetch the backup until it expires.",
		Input: mcp.In(
			panelField, serverField,
			mcp.Str("backup", "Backup UUID.").Req(),
		),
		Handler: func(ctx context.Context, args mcp.Args) (string, error) {
			id := url.PathEscape(args.String("backup", ""))
			return t.call(ctx, args, http.MethodGet, "/backups/"+id+"/download", nil, nil)
		},
	})

	t.add(s, tierDestroy, mcp.Tool{
		Name: "ptero_backup_restore",
		Description: "Restore a backup over the server's files. The server is stopped for the " +
			"restore. With truncate the whole server directory is emptied first, so anything created " +
			"since the backup is gone; without it the backup's files are unpacked over what is there.",
		Input: mcp.In(
			panelField, serverField,
			mcp.Str("backup", "Backup UUID.").Req(),
			mcp.Bool("truncate", "Empty the server directory before restoring.").Def(false),
		),
		Handler: func(ctx context.Context, args mcp.Args) (string, error) {
			id := url.PathEscape(args.String("backup", ""))
			return t.call(ctx, args, http.MethodPost, "/backups/"+id+"/restore", nil,
				map[string]interface{}{"truncate": args.Bool("truncate", false)})
		},
	})

	t.add(s, tierDestroy, mcp.Tool{
		Name: "ptero_backup_delete",
		Description: "Delete a backup. The archive is removed from its storage and cannot be " +
			"recovered. A locked backup is refused until it is unlocked.",
		Input: mcp.In(
			panelField, serverField,
			mcp.Str("backup", "Backup UUID.").Req(),
		),
		Handler: func(ctx context.Context, args mcp.Args) (string, error) {
			id := url.PathEscape(args.String("backup", ""))
			return t.call(ctx, args, http.MethodDelete, "/backups/"+id, nil, nil)
		},
	})
}

// ---------------------------------------------------------------- passthrough

// registerRawTool exposes the whole client API by path, for routes a panel
// fork added and routes a future release will add.
//
// It is off unless -allow-raw, and it still honours the write and destroy
// gates: a read-only process cannot use it to POST, and a process without
// -allow-destructive cannot use it to DELETE. Otherwise it would be a way
// around every other decision made here.
func (t *toolset) registerRawTool(s *mcp.Server) {
	t.add(s, tierRaw, mcp.Tool{
		Name: "ptero_request",
		Description: "Call any Pterodactyl client API route directly. Use it only for routes the " +
			"other tools do not cover — a fork's own endpoint, or one added after this server was " +
			"built. Paths are relative to /api/client, so '/servers/abc123/resources' reaches the " +
			"resources route.",
		Input: mcp.In(
			panelField,
			mcp.Str("method", "HTTP method.").Of("GET", "POST", "PUT", "PATCH", "DELETE").Def("GET"),
			mcp.Str("path", "Route path relative to /api/client, with a leading slash.").Req(),
			mcp.Obj("query", "Query parameters as an object."),
			mcp.Obj("body", "JSON request body as an object."),
		),
		Handler: func(ctx context.Context, args mcp.Args) (string, error) {
			method := strings.ToUpper(args.String("method", "GET"))
			switch method {
			case http.MethodGet:
			case http.MethodPost, http.MethodPut, http.MethodPatch:
				if !t.permitted(tierWrite) {
					return "", fmt.Errorf("this server is running read-only; %s is not allowed", method)
				}
			case http.MethodDelete:
				if !t.permitted(tierDestroy) {
					return "", fmt.Errorf("DELETE needs -allow-destructive, which this server was not started with")
				}
			default:
				return "", fmt.Errorf("unsupported method %q", method)
			}

			client, err := t.panelOnly(args)
			if err != nil {
				return "", err
			}

			query := url.Values{}
			raw, err := args.Object("query")
			if err != nil {
				return "", err
			}
			for key, value := range raw {
				query.Set(key, fmt.Sprintf("%v", value))
			}

			body, err := args.Object("body")
			if err != nil {
				return "", err
			}

			var payload interface{}
			if len(body) > 0 {
				payload = body
			}

			value, err := client.JSON(ctx, pteroapi.Request{
				Method: method,
				Path:   args.String("path", ""),
				Query:  query,
				JSON:   payload,
			})
			if err != nil {
				return "", err
			}
			return pteroapi.Pretty(value), nil
		},
	})
}

// pickString takes the caller's value when they sent one, and the panel's
// current value otherwise. This is what makes a partial update possible over
// a route that only replaces.
func pickString(args mcp.Args, argName string, current map[string]interface{}, key string) string {
	if args.Has(argName) {
		return args.String(argName, "")
	}
	if current != nil {
		if value, ok := current[key].(string); ok {
			return value
		}
	}
	return ""
}

func pickBool(args mcp.Args, argName string, current map[string]interface{}, key string, fallback bool) bool {
	if args.Has(argName) {
		return args.Bool(argName, fallback)
	}
	if current != nil {
		if value, ok := current[key].(bool); ok {
			return value
		}
	}
	return fallback
}
