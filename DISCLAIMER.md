# Disclaimer and release of liability

This applies to everything in this repository: the desktop client and the
`ptero-mcp` server.

## No warranty

This software is provided **as is**, without warranty of any kind, express or
implied, including but not limited to the warranties of merchantability,
fitness for a particular purpose and non-infringement.

## No liability

In no event shall the authors or contributors be liable for any claim, damage
or other liability, whether in an action of contract, tort or otherwise,
arising from, out of or in connection with this software or its use. This
includes, without limitation, loss of data, loss of game worlds or databases,
server downtime, corrupted files, unintended configuration changes, costs
billed by a hosting provider, and any consequence to third parties such as
the players on a server.

By running this software you accept that you do so at your own risk, and that
responsibility for its effect on your panels, your servers, your data and
your users rests with you.

## Why this is spelled out

Both programs hold a live Pterodactyl API key and act on real servers. They
can stop a running server, overwrite a config file, delete a directory and
send console commands. Those actions take effect immediately on a machine you
may not own.

`ptero-mcp` adds a specific hazard worth naming plainly. It is driven by an AI
assistant rather than by a person clicking a button. An assistant can:

- act on the **wrong panel**, when several are configured in one process;
- act on the **wrong server**, when a panel has many and the identifiers look
  alike;
- **misread a file** and write back something that does not start;
- run a command **nobody asked for**, from a misunderstood instruction;
- be **talked into an action** by text it read from a file, a console line or
  a panel response, rather than from you.

The safeguards in `ptero-mcp` are real and they are described in
[cmd/ptero-mcp/README.md](cmd/ptero-mcp/README.md): tools are gated by tier at
startup, destructive ones are withheld unless explicitly enabled and then
still need a per-call confirmation, an unknown panel name is refused rather
than defaulted, and a delete that would empty a server's root is refused
outright. They reduce the chance of a costly mistake. **They cannot prevent
one.** No safeguard can tell that a panel name which exists is not the one you
meant.

## Reducing your exposure

If you run this against anything you would mind losing:

- **Take backups first**, and verify at least one restores.
- **Scope the API key.** Create a client key on an account that is a subuser
  with access to only the servers the assistant should reach, holding only the
  permissions it needs. The key is the real boundary; the flags are a
  convenience on top of it.
- **One process per panel.** Do not put a customer's panel and your own behind
  the same server.
- **Leave `-allow-destructive` off** unless you are watching the session.
- **Use `-read-only` or `-tools`** when you only need it to look at things.
- **Do not expose the HTTP transport publicly.** Keep it on loopback or behind
  a reverse proxy with authentication, and always set a bearer token.

## Relationship to the licence

This repository is licensed under the GNU General Public License, version 3.
See [LICENSE](LICENSE).

Sections 15 and 16 of that licence are the operative parts here. Section 15
disclaims warranty; section 16 limits liability. They are the terms that carry
legal weight, they have been tested over decades, and they govern if anything
in this file reads differently.

This file exists because those sections are written in general terms, and the
specific hazard of handing a panel API key to an AI assistant is worth stating
in words a reader will actually absorb before connecting it to a live server.
It is a plain statement of intent, written by the project rather than by a
lawyer, and it is not legal advice.
