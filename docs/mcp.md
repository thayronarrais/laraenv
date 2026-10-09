# Operate LaraEnv from your AI assistant

LaraEnv exposes seven local MCP tools so Claude Code, Codex, Cursor and other MCP clients can inspect and operate the environment already open in LaraEnv. This release also improves terminal clipboard handling and makes the in-app command palette switch between open terminals.

## Connect

1. Open the main LaraEnv window with your usual Windows account.
2. Open **Settings > Experimental > Connect assistants to LaraEnv** and enable **Local MCP access**.
3. Click **Register** for Claude Code, Codex or Cursor, then reopen that client to apply its configuration.
4. Keep LaraEnv open (the tray is sufficient). Disable local MCP access to stop new connections and cancel active MCP command executions.

Registration updates only the `laraenv` entry and saves a dated `.laraenv-*.bak` copy before changing an existing file. Malformed files are left intact. Other settings and servers are preserved; TOML formatting/comments may be normalized, with original bytes retained in the backup. Status queries do not create configurations. Registration itself is an explicit action; enabling access alone never registers a client.

| Client | User configuration |
| --- | --- |
| Claude Code | `~/.claude.json`, top-level `mcpServers.laraenv` |
| Codex | `$CODEX_HOME/config.toml`, default `~/.codex/config.toml`, table `mcp_servers.laraenv` |
| Cursor | `~/.cursor/mcp.json`, top-level `mcpServers.laraenv` |

These are the client formats documented by [Claude Code](https://code.claude.com/docs/en/mcp#user-scope), [Codex](https://learn.chatgpt.com/docs/extend/mcp?surface=cli) and [Cursor](https://cursor.com/docs/mcp#configuration-locations). They differ from provider lifecycle hook files.

For another client, use the absolute installed executable as the stdio command, with `args: ["mcp"]`. The Settings registration includes an explicit `--connection` descriptor path to select the correct installation. The descriptor is private to the current Windows account; the GUI and client must use that same account. Normal admin consent works; using credentials for a different administrator account creates a different profile and is not supported by this connection.

The stdio process does not start another GUI or another set of services. It forwards calls to a random IPv4 loopback port authenticated by a private token. The token is never written to client configuration or exposed in Settings. Remote machines and SSH connections are outside this MCP server's scope.

## Tools

Each tool has an `action` field. List projects/services before changing them; use exact names returned by LaraEnv.

| Tool | Actions and important arguments |
| --- | --- |
| `laraenv_project` | `list`; `detail` with `project`; `set_php`/`set_node` with installed `version` (empty resets override); `set_ssl` with `enabled`; `set_proxy` with `port` (0 disables); `pause`/`resume` |
| `laraenv_service` | `status` (optional `service`), `start`, `stop`, `reload` with `service`: nginx, apache, caddy, mysql, postgres, redis or mailpit |
| `laraenv_worker` | `status`, `start`, `stop`, `restart`, `logs`; `kind`: devserver/queue with `project`, or cron with `id`; cron `run` starts an existing job immediately |
| `laraenv_exec` | `artisan`, `composer`, `npm` with `project`, literal `args` array, optional `confirm` and `timeoutSeconds` |
| `laraenv_logs` | `service`, `app` (Laravel `storage/logs/laravel.log`), `worker`, `activity`; `lines` defaults to 100, maximum 500 |
| `laraenv_db` | `list`, `create`, `snapshot`, `drop`; `engine`: mysql/postgres; mutations require `database`; drop requires `confirm: true` |
| `laraenv_diag` | `doctor` (optional `project`), `request_timing` with `project` |

Project pause stops the project's dev server/queue and removes its web serving configuration. It retains project files, saved routes and certificates. Resume restores serving; start background workers explicitly afterward. Existing cron scheduling is managed separately. Cron stop disables future scheduling; it does not interrupt a run that has already started.

Commands run in the selected project's directory with its PHP/Node runtime at the beginning of PATH. Artisan and Composer invoke the selected PHP; npm invokes the selected Node directly, without constructing a shell command. Inspection commands can run without confirmation. Other commands and scripts, including indirect destructive work, require `confirm: true`. Starting/restarting project dev servers or queue workers, and enabling/running saved cron commands, also requires confirmation. Workers reuse the arguments saved in LaraEnv project settings. Arbitrary shell command strings, executable names and arguments that redirect the selected project scope are rejected.

Command output streams as MCP progress notifications when the client requests progress, and is also returned with an exit code, duration and truncation indicator. Clients decide how to display these notifications. Returned output is capped at 256 KiB. Cancellation/timeouts terminate the command's Windows process tree. Calls default to 60 seconds, up to 300 seconds; request timing additionally limits its loopback request to 15 seconds and reads at most 64 KiB.

Database operations use LaraEnv's local engines and bundled client utilities (MySQL on 127.0.0.1:3306; PostgreSQL on 127.0.0.1:5432). Start the engine first. Database names must be ASCII identifiers of at most 63 characters; system databases cannot be dropped. Snapshots are SQL dumps under LaraEnv's `state/mcp-snapshots`, limited to 512 MiB, and failed partial files are removed. No restore operation is exposed.

Doctor reports findings without changing configuration. Request timing connects only to loopback for a catalog project, validates HTTPS certificates and never follows redirects. The activity list records every tool call's action, target, time, outcome and duration; it excludes command arguments, output, error content and credentials.

## Example prompts

- “List my LaraEnv projects and tell me which PHP version serves `my-app`.”
- “Switch `my-app` to the installed PHP 8.3 version, then restart its queue worker.”
- “Run `php artisan route:list` in `my-app` and summarize the API routes.”
- “Read the last 100 Laravel log lines for `my-app` and diagnose the failure.”
- “List the local PostgreSQL databases and create a SQL snapshot of `my_app`.”
- “Run doctor for `my-app`, then measure its local response time.”
- “Pause `my-app` while I work on another project, and resume it later.”

Example tool inputs:

```json
{"action":"artisan","project":"my-app","args":["route:list"]}
```

```json
{"action":"snapshot","engine":"postgres","database":"my_app"}
```

```json
{"action":"artisan","project":"my-app","args":["migrate:fresh"],"confirm":true}
```

The last example deletes existing tables. An assistant should obtain your explicit approval before submitting a destructive command with confirmation. The MCP server uses the [official Go SDK](https://github.com/modelcontextprotocol/go-sdk).
