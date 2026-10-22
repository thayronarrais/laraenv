# Project runtime detection

LaraEnv resolves PHP and Node from the checkout used by each project operation. Detection never writes a manual preference or runs the runtime to discover its version.

| Runtime | Priority, highest first |
| --- | --- |
| PHP | Manual project choice → `.php-version` → `composer.json` `require.php` → global PHP |
| Node | Manual project choice → `.nvmrc` → `.node-version` → `package.json` `engines.node` → global Node |
| Package manager | `package.json` `packageManager` → lockfile → npm |

Choosing **Automatic** clears the manual override. With no project requirement, the global managed runtime is used; if no global choice exists, the highest verified stable installation is selected. With no managed installation and no requirement, existing system PATH behavior remains available.

Declared requirements select the highest compatible installed stable version. Installed executable metadata supplies the actual patch version; installation folders and catalog markers cannot prove compatibility. An unavailable or unreadable version cannot satisfy a requirement. PHP and Node catalog installation badges also use executable metadata.

## Requirements

Version files accept partial versions such as `8.3` and `v22`, full versions, and supported range expressions. Node also accepts `node`, `stable`, and `lts/*`; LTS candidates use the catalog's `lts` and `ltsFrom` metadata, so older releases from before a line entered LTS are excluded.

Composer and Node constraints support exact versions, wildcards, comparisons, caret, tilde, hyphen ranges, AND and OR. Their semantics are kept separate: Composer `8.3` means exactly `8.3.0`, while `.php-version` `8.3` selects the series; Composer `~8.2` permits PHP 8.x, while npm `~22.11` stays within 22.11.x. Prerelease selection requires an explicit matching prerelease constraint or manual choice.

Unsupported expressions or aliases are reported as invalid. Empty version files, malformed JSON, unreadable files and oversized manifests also produce diagnostics. There is no silent global fallback for a declared requirement. A manual installed choice remains effective even when incompatible or when the declaration is malformed, with a visible warning.

## Changes and execution

Project creation, clones and file edits refresh detection. The watcher handles atomic editor saves, relevant lockfiles, and registered worktree directories. Worktree commands read that worktree's declarations while retaining the parent project's manual preferences.

New terminals, agent launches/resumes, dev servers, queue workers, MCP commands and scheduled jobs use the same resolution. A cron's explicit PHP choice wins; its actual working directory determines declarations. Background Task Scheduler jobs use this path without starting the GUI or services. Missing required runtimes stop new launches and record a diagnostic.

Existing terminals, agents, queues and dev servers retain their runtime until explicitly restarted. Only changes to PHP or document roots of served checkouts regenerate web configuration. A new PHP pool must start successfully before a running site is redirected. Missing/invalid requirements preserve the last serving configuration; failed application stays pending for retry. Old pools are retained during manifest updates. Web servers reload through the existing restart mechanism after a successful configuration update.

Select a row on Projects to open its details panel. The PHP and Node selectors display effective version, origin and requirement. Missing requirements offer a Downloads link filtered to the runtime and, when available, a compatible catalog build. Installation requires the user's click. See the [Projects guide](projects-workspace.md).

PHP pools are shared by version, not created as a separate application instance for each project. The Projects header shows the shared web-server state; project Dev servers and queue workers have their own controls.

## Site Doctor contract

`Project.phpRuntime` and `Project.nodeRuntime` expose:

- `version`: managed installation identity; empty when unresolved.
- `fullVersion`: verified executable version, or empty when unknown.
- `source`: `manual`, `global`, or the declaration filename.
- `constraint`: original declared requirement.
- `status`: `resolved`, `missing`, `invalid`, `incompatible`, or `unmanaged`.
- `problem`: stable diagnostic code, when applicable.
- `installVersion`: optional compatible catalog build suggestion.

The existing `phpVersion`/`nodeVersion` fields continue to mean manual overrides. The MCP Doctor uses the effective resolution, exposes the same structured fields, and reports requirement problems without running binaries or repairing project configuration. The Projects page also uses these fields in [Site Doctor](site-doctor.md), which checks project setup, offers individual repairs, and routes missing runtimes to Downloads.
