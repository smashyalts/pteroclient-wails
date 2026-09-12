# ptero-mcp

A self-hostable [Model Context Protocol](https://modelcontextprotocol.io) server for the
Pterodactyl panel's client API. It gives an assistant the same reach over your game servers
that the panel's own web UI has: the console, the file manager, databases, schedules,
backups, network allocations, subusers, startup variables and power control.

One static binary, no runtime dependencies, and nothing phones home. The only outbound
connections are to the panels you configure.

```
go build -o ptero-mcp ./cmd/ptero-mcp
```

> ### ⚠️ Read this before pointing it at anything you would mind losing
>
> **No warranty. No liability. You run this at your own risk.** Sections 15 and 16 of the
> [GPL-3.0 licence](../../LICENSE) are the binding terms; [DISCLAIMER.md](../../DISCLAIMER.md)
> explains in plain language what they mean here.
>
> This is driven by an AI assistant, not by a person clicking a button, and an assistant
> can act on the wrong panel, act on the wrong server, misread a file and write back
> something that will not start, or run a command nobody asked for. The safeguards below
> are real and they reduce that risk. **They cannot remove it** — nothing here can tell
> that a panel name which exists is not the one you meant.
>
> Before you connect it: take a backup, and give it a client API key on an account that is
> a subuser with access to only the servers it should reach. The key is the real boundary.
> The flags are a convenience on top of it. Run one process per panel if the panels belong
> to different people.

## Configure a panel

Create a **client** API key in the panel under *Account → API Credentials*. A client key
starts with `ptlc_`. Then pick whichever of these suits you — all of them are read, so a
container can hold the keys in its environment while a workstation keeps them in a file.

**Environment**, which is usually what a container wants:

```bash
export PTERO_PANEL_URL=https://panel.example.com
export PTERO_API_KEY=ptlc_...
export PTERO_SERVER_ID=1a7ce997   # optional: the default server for every tool call
```

**A config file** at `~/.ptero-mcp/panels.json`, for several panels:

```json
{
  "default_panel": "main",
  "panels": [
    { "name": "main",    "url": "https://panel.example.com", "api_key": "ptlc_...", "default_server": "1a7ce997" },
    { "name": "staging", "url": "https://staging.example.com", "api_key": "ptlc_..." }
  ]
}
```

**Inline JSON** in `PTERO_PANELS`, for a container with no mounted volume:

```bash
export PTERO_PANELS='[{"name":"main","url":"https://panel.example.com","api_key":"ptlc_..."}]'
```

**Nothing at all**, if you already run the desktop client in this repo. Its
`~/.pteroclient/config.json` is read as-is, panel names and all, so an existing install
needs no second copy of your keys.

Every tool takes an optional `panel` argument. Omit it and the default panel is used; name
one that does not exist and the call is refused with the list of configured names rather
than quietly acting on the default, because the wrong panel can mean the wrong company's
game server. A name that *does* exist is accepted, so the list of configured panels is the
list of panels one mistaken argument can reach.

Every source above is read and merged, not just the first one that works. Set
`PTERO_PANEL_URL` on a machine that already runs the desktop client and you have two panels
reachable, not one. The startup summary prints the panels it found and where they came
from, and warns when more than one is reachable. Read it once after any change, and run one
process per panel when the panels belong to different people.

## Connect an assistant

**Claude Code**, over stdio:

```bash
claude mcp add ptero -- /path/to/ptero-mcp
```

**Claude Desktop**, in `claude_desktop_config.json`:

```json
{
  "mcpServers": {
    "ptero": {
      "command": "/path/to/ptero-mcp",
      "env": {
        "PTERO_PANEL_URL": "https://panel.example.com",
        "PTERO_API_KEY": "ptlc_..."
      }
    }
  }
}
```

In stdio mode the protocol owns stdout, so every diagnostic goes to stderr. `-quiet`
silences the startup summary.

## Self-host over HTTP

```bash
ptero-mcp -http :8472 -token "$(openssl rand -hex 32)"
```

The endpoint is `POST /mcp`, with `GET /healthz` for a health check. A bearer token is
**required** unless the listener is loopback-only: the tools behind this port can run
commands on your servers and read every file on them, so an open port doing that is refused
at startup rather than honoured silently. Browser origins are rejected unless you list them
with `-allow-origin`, which is the DNS-rebinding guard the MCP spec asks for; clients that
send no `Origin` header, which is all of them except a browser, are unaffected.

Put it behind a reverse proxy for TLS. There is no TLS in the binary, deliberately —
your proxy already does certificates better than a flag would.

### Docker

```bash
docker build -f cmd/ptero-mcp/Dockerfile -t ptero-mcp .
docker run -p 8472:8472 \
  -e PTERO_PANEL_URL=https://panel.example.com \
  -e PTERO_API_KEY=ptlc_... \
  -e PTERO_MCP_TOKEN=... \
  ptero-mcp
```

### systemd

```ini
[Unit]
Description=Pterodactyl MCP server
After=network-online.target

[Service]
ExecStart=/usr/local/bin/ptero-mcp -http 127.0.0.1:8472
EnvironmentFile=/etc/ptero-mcp.env
DynamicUser=yes
NoNewPrivileges=yes
ProtectSystem=strict
ProtectHome=yes
PrivateTmp=yes
Restart=on-failure

[Install]
WantedBy=multi-user.target
```

With `ProtectHome=yes` the config files are unreachable, so put the panels in
`/etc/ptero-mcp.env` as `PTERO_PANELS` or `PTERO_PANEL_URL`/`PTERO_API_KEY`.

## What it is willing to do

Decided at startup, not per call. A tool that is not enabled is never registered, so it
cannot be called and does not appear in `tools/list` — nothing a panel, a file or a config
says can talk the server into running it.

| Flag | Adds | Default |
| --- | --- | --- |
| — | reads, and writes that can be undone: file writes, power signals, commands, creating databases, schedules and backups | on |
| `-read-only` | nothing; withholds every write | off |
| `-allow-destructive` | deleting files, extracting archives, restoring and deleting backups, reinstalling a server, deleting databases, schedules, subusers and allocations | off |
| `-allow-account` | the panel account's own API keys and SSH keys | off |
| `-allow-raw` | `ptero_request`, which calls any client API route by path | off |

Destructive tools need a second gate on top of the flag: `confirm: true` on the call
itself. One mistaken tool call should not be able to empty a server's world folder.
`ptero_request` honours the same gates — read-only refuses a POST, and no
`-allow-destructive` refuses a DELETE — otherwise it would be a way around every other
decision here.

Every start prints what it settled on to stderr: the panels it found, where the config came
from, how many tools were registered, which gates are open, and the liability notice. It
also prints a caution when more than one panel is reachable, when destructive tools are
enabled, and when the passthrough is enabled. `-quiet` silences all of it once you have
read it, which is what a service unit wants.

`-tools` trims the registry by name, prefix or glob, which is how to hand an assistant the
file tools and nothing else:

```bash
ptero-mcp -tools ptero_files,ptero_console_tail,ptero_server_resources
ptero-mcp -list-tools   # print what the current flags would register, then exit
```

## Tools

Every route of the client API is covered. 54 tools by default, 69 with every gate open.

**Panel and account** — `ptero_panels_list` (never returns your API keys),
`ptero_permissions`, `ptero_account_get`, `ptero_account_activity`,
`ptero_account_api_keys_list`, `ptero_account_ssh_keys_list`, and under `-allow-account`
the create and delete tools for API keys and SSH keys.

**Servers** — `ptero_servers_list`, `ptero_server_get`, `ptero_server_resources`,
`ptero_server_activity`, `ptero_server_rename`, `ptero_server_set_docker_image`,
`ptero_startup_get`, `ptero_startup_set_variable`, `ptero_server_reinstall`.

**Power and console** — `ptero_power`, `ptero_command`, and three that read the console
websocket, which is the only place a crash trace or a plugin error exists at all:

- `ptero_console_tail` reads the live console and returns the lines. Reads only.
- `ptero_console_exec` sends a command and returns the output that follows it, so an
  assistant can ask a server something and see the answer in one call.
- `ptero_console_power_watch` sends a power signal and returns the output it produces, so
  starting a server and finding out whether it actually came up is one call rather than a
  poll loop.

All three take `wait_seconds`, `max_lines` and `until`, which stops the read at the first
line containing some text — `until: "Done ("` for a Minecraft server that has finished
starting. ANSI colour escapes are stripped by default; they are a third of the bytes of a
log line and mean nothing here.

**Files** — `ptero_files_list`, `ptero_files_read`, `ptero_files_write`,
`ptero_files_rename`, `ptero_files_copy`, `ptero_files_create_folder`, `ptero_files_chmod`,
`ptero_files_compress`, `ptero_files_decompress`, `ptero_files_delete`, `ptero_files_pull`,
`ptero_files_download_url`, `ptero_files_upload_url`, and `ptero_files_search`.

`ptero_files_search` has no counterpart in the panel's API — the panel's own file manager
searches one directory at a time in the browser. It walks the tree instead, matching names
by glob, contents by substring, or both:

```
ptero_files_search  name="*.yml"  contains="server-port"
→ /plugins/Essentials/config.yml:14  server-port: 25565
```

Giving both a name and a contains is much cheaper than contains alone, because only the
files whose names match are fetched. Caches, libraries and world region folders are skipped
by default; `skip_dirs` overrides that.

Directory listings come back as a compact table rather than the panel's records, which for
a folder of two hundred plugins would spend most of the reply on repeated field names.
`as_json: true` returns the full records. File reads take `max_bytes`, `start_line` and
`line_count`, and say so when they truncate.

**Databases** — list, create, rotate password, delete.

**Schedules** — list, get, create, update, execute, delete, and create/update/delete for
tasks. The panel's update route replaces a whole schedule, so `ptero_schedule_update` reads
the current one first and sends back whatever you did not change. Pausing a schedule does
not mean retyping its cron.

**Network** — list allocations, assign one, set notes, set primary, unassign.

**Subusers** — list, get, create, update, delete. `ptero_subuser_update` takes `grant` and
`revoke` as well as a whole `permissions` set, merging them against what the user already
has, and refuses to leave a subuser with no permissions at all.

**Backups** — list, get, create, toggle lock, download URL, restore, delete.

## Deliberately absent

Every route in `routes/api-client.php` has a tool except six.

Five are the account's own credentials: `PUT /account/email`, `PUT /account/password`, and
the three two-factor routes. Each one needs your plaintext panel password as a tool
argument, or hands back your TOTP secret, and neither belongs in a model's context or in a
transcript. Do those in the panel.

The sixth is `GET /servers/{server}/websocket`, which mints a console token. The three
console tools use it internally, so there is no reason to hand the token itself to a model.

If you need any of the six anyway, `-allow-raw` reaches them by path and you supply the
password yourself.

## Notes

Rate limiting is handled: the panel limits the client API per key, and a recursive search
meets 429 as a matter of course. Only 429 is retried — the request was refused rather than
carried out, so repeating it cannot apply a write twice — and the panel's `Retry-After` is
obeyed up to a ten-second cap. A 500 is never retried, because a write that failed that way
may still have been applied.

Panel errors are passed through with their detail intact. A 422 arrives as *"The name field
is required. (field "name")"* rather than as a bare status code, because a caller told only
`422` has to guess.
