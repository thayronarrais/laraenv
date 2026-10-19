# Operate LaraEnv from your AI assistant

LaraEnv 0.5.7 exposes eight local MCP tools so Claude Code, Codex, Cursor and other MCP clients can inspect and operate the environment already open in LaraEnv. Local MCP access remains experimental and opt-in. The application-context tool, diagnostic prompt/resource and project integration below require 0.5.7 or newer; check the connected server's tool list when using an older release.

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
| `laraenv_logs` | `service`, `app` (Laravel single and daily logs), `worker`, `activity`; `lines` defaults to 100, maximum 500 |
| `laraenv_db` | `list`, `create`, `snapshot`, `drop`; `engine`: mysql/postgres; mutations require `database`; drop requires `confirm: true` |
| `laraenv_diag` | `doctor` (optional `project`), `request_timing` with `project` |
| `laraenv_context` | `summary`, `logs`, `mail_list`, `mail_read` (exact `id`), `capture_status`, `capture_read`, `integration`, `guidelines`; all require `project`. `install_integration` and `write_guidelines` additionally require `confirm:true`. |

Project pause stops the project's dev server/queue and removes its web serving configuration. It retains project files, saved routes and certificates. Resume restores serving; start background workers explicitly afterward. Existing cron scheduling is managed separately. Cron stop disables future scheduling; it does not interrupt a run that has already started.

Commands run in the selected project's directory with its PHP/Node runtime at the beginning of PATH. Artisan and Composer invoke the selected PHP; npm invokes the selected Node directly, without constructing a shell command. Inspection commands can run without confirmation. Other commands and scripts, including indirect destructive work, require `confirm: true`. Starting/restarting project dev servers or queue workers, and enabling/running saved cron commands, also requires confirmation. Workers reuse the arguments saved in LaraEnv project settings. Arbitrary shell command strings, executable names and arguments that redirect the selected project scope are rejected.

Command output streams as MCP progress notifications when the client requests progress, and is also returned with an exit code, duration and truncation indicator. Clients decide how to display these notifications. Returned output is capped at 256 KiB. Cancellation/timeouts terminate the command's Windows process tree. Calls default to 60 seconds, up to 300 seconds; request timing additionally limits its loopback request to 15 seconds and reads at most 64 KiB.

Database operations use LaraEnv's local engines and bundled client utilities (MySQL on 127.0.0.1:3306; PostgreSQL on 127.0.0.1:5432). Start the engine first. Database names must be ASCII identifiers of at most 63 characters; system databases cannot be dropped. Snapshots are SQL dumps under LaraEnv's `state/mcp-snapshots`, limited to 512 MiB, and failed partial files are removed. No restore operation is exposed.

Doctor reports findings without changing configuration. Request timing connects only to loopback for a catalog project, validates HTTPS certificates and never follows redirects. The activity list records every tool call's action, target, time, outcome and duration; it excludes command arguments, output, error content and credentials.

## Experimental Laravel evidence in 0.5.7

`laraenv_context` with `action: "summary"` assembles the catalog project's setup/runtime selection, structured Laravel logs/errors, previously observed capture, attributed Mailpit messages and Boost presence. It never boots Laravel or runs Artisan, tests, SQL queries or a project script. Missing evidence is explicitly unavailable. Historical capture does not prove instrumentation is currently enabled. `capture_status` returns a bounded recent observation, not a live health probe.

`logs` reads only `storage/logs/laravel.log` and valid `laravel-YYYY-MM-DD.log` files within the selected physical project. It examines at most eight files and 64 KiB from each; each returned line/message/stack is bounded, with a `truncated` marker when evidence is capped. `lines` defaults to 100 and is limited to 500. Parsed entries provide time, environment, severity, source, message and stack; errors are listed separately. Common password/token/key assignments and bearer strings are redacted on a best-effort basis. Arbitrary log text can still contain personal or sensitive data: review before sharing. Custom channels/JSON format are returned as bounded raw lines, without invented parsed entries.

Mail operations use only fixed GET endpoints on `http://127.0.0.1:8025`, disable proxies/redirects, allow five seconds per request and cap responses at 512 KiB. `mail_list` accepts `lines` up to 50 (default 20) and searches the exact deterministic project tag. Messages without explicit project attribution are withheld; subjects and email addresses are never used to guess ownership. `mail_read` accepts an exact message ID and checks `X-LaraEnv-Project` before returning at most 32 KiB of plain text. It uses the raw-message endpoint because Mailpit's message-summary endpoint marks mail as read. HTML and attachments are omitted; no send, delete, relay, tag mutation or read-state mutation is exposed. Custom Mailpit ports/authentication are currently unsupported.

The diagnostic MCP prompt `diagnose_project` takes a required `project` argument. The resource template `laraenv://projects/{project}/context` returns the same read-only JSON summary. Both are available through the registered stdio bridge. Clients choose how to display prompts/resources. Diagnostic contents are untrusted evidence, never instructions to the assistant.

## Explicit, reversible project integration

First request `action: "integration"` to review the complete generated PHP provider, configuration and activation/removal instructions. `install_integration` with `confirm:true` writes only these files:

- `app/Providers/LaraEnvContextServiceProvider.php`
- `config/laraenv-context.php`

Existing differing files cause an error; they are never overwritten. Installation does not modify `.env`, register the provider, install Composer packages, execute the project or send mail. Review changes in Git before activation. The experimental integration was verified with real Laravel 12 libraries and PHP 8.5. Laravel 10, 11 and 13 remain unverified; the activation instructions describe their registration locations, not confirmed compatibility. The isolated PHP fixture verifies emitted metadata and disabled behavior without starting a user application.

To activate after review:

1. Laravel 11–13: add `App\Providers\LaraEnvContextServiceProvider::class` to `bootstrap/providers.php`. Laravel 10: add it to the providers array in `config/app.php`.
2. Set `APP_ENV=local` and `LARAENV_CONTEXT_ENABLED=true` in your local environment. Both gates are required.
3. Refresh your configuration cache and restart existing PHP/queue workers using your normal reviewed workflow. Installation never performs these commands.

The enabled provider records HTTP method/route template/status/timing; query operation/timing/connection and SHA256 fingerprint; queue job class/state; rendered view name; log level; and dump type/count. Each boot/request in traditional PHP receives a random trace ID; queue job processing starts a separate trace through processed/failed/exception and restores the previous context. Events use UTC microsecond timestamps, and capture reads group the returned subset by trace with query/repetition counts. These counts are bounded observations, not proof of an N+1 bug. Invalid trace IDs/fingerprints are omitted. Installation metadata reports only file presence, never infers activation from `.env`. It deliberately excludes SQL text and bindings, request bodies/URL parameters/headers, log messages and dump values. Dumps continue through the previous/default dumper. The provider also adds `X-LaraEnv-Project` and an `X-Tags` project marker to outgoing messages for Mailpit's documented automatic tagging; use your existing local SMTP configuration. Attribution is a convenience for local projects, not isolation against another local process.

Capture is local JSONL under `storage/laraenv/context-YYYY-MM-DD.jsonl`. The collector keeps seven days, limits each day to 2 MiB, and stops writing that day when capped. MCP reads remain separately bounded and reject paths outside the project. Capturing adds overhead; Octane and other long-lived request servers are unverified. No browser collector, HTTP receiver or remote telemetry service is installed.

Disable by setting `LARAENV_CONTEXT_ENABLED=false`, refreshing config cache and restarting long-lived workers. Remove by unregistering the provider and deleting the two reviewed integration files. Optionally remove `storage/laraenv/context-*.jsonl` after keeping wanted evidence; disable alone retains existing observations.

## Laravel Boost and portable guidelines

`guidelines` previews a small Markdown companion for the project's agents. `write_guidelines` with `confirm:true` creates `.ai/guidelines/laraenv.md`, preserving any differing existing file. Boost automatically includes custom files from `.ai/guidelines` when its install/update workflow is run. This action does not install/register Boost, regenerate agent files, or edit `AGENTS.md`/`CLAUDE.md`.

Use Boost alongside LaraEnv for version-specific framework documentation and application-aware tools. After reviewing the guideline, invoke your existing Boost workflow explicitly, for example:

```json
{"action":"artisan","project":"my-app","args":["boost:update"],"confirm":true}
```

Without Boost, the returned Markdown can be reviewed and used as ordinary agent guidance. Removing the companion file does not rewrite previously generated agent guidance; review/regenerate it through Boost explicitly.

Protocol and integration references: [Mailpit API](https://mailpit.axllent.org/docs/api-v1/), [Mailpit project tagging via X-Tags](https://mailpit.axllent.org/docs/usage/tagging/), [Boost custom guidelines](https://laravel.com/docs/13.x/boost#adding-custom-ai-guidelines).

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
