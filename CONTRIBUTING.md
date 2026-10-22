# Contributing to LaraEnv

LaraEnv's desktop source is licensed under [Apache-2.0](LICENSE). Contributions should be focused, explain the problem they solve, and include the evidence needed to review the result.

## Report a bug or suggest a change

Search the [existing issues](https://github.com/thayronarrais/laraenv/issues) first. For a bug, include:

- LaraEnv version or source commit, Windows version and architecture, and relevant runtime versions.
- Minimal reproduction steps, expected behavior and actual behavior.
- Relevant error output or screenshots after removing secrets and personal data.

Use fictional paths, hosts and projects in examples. `example.invalid` is suitable for a nonworking example hostname. Do not attach real configuration files, tokens, private keys, database dumps, customer files or unreviewed logs. For security vulnerabilities, follow [SECURITY.md](SECURITY.md) instead of opening a public issue.

For a larger change, explain the proposed behavior in an issue before investing in a broad implementation. There is no guaranteed response or acceptance schedule.

## Develop a change

Follow the [development guide](docs/development.md) to provision dependencies, build from a clean checkout and run relevant checks. The hosted backend is not part of this distribution and is not required for local development. Keep local features usable with cloud integration disabled.

Keep a pull request small enough to review. Describe the concrete behavior before and after the change, how it was verified, and any remaining limits. Add a regression test when it can catch the failure being fixed. For UI changes, include a screenshot or a short recording made with fictional data when it helps reviewers assess the result.

Use the committed Go and npm dependency graphs. Do not replace the project-pinned Wails CLI with a separately resolved installation, remove tool-only Go requirements, or refresh lockfiles as an incidental part of an unrelated change. Explain intentional dependency updates and regenerate the affected notices using the [license tooling guide](tools/licenses/README.md).

Wails bindings under `frontend/wailsjs`, frontend build output, vendored frontend assets, dependency caches and compiled binaries are generated locally. Do not add them to a source change. Reviewed license outputs, including `THIRD_PARTY_NOTICES.txt` and `third_party/dependencies.json`, are tracked and must be updated when their inputs change.

## Before opening a pull request

- Run the tests and build checks relevant to the change, and report exact commands and results.
- Run the notice checks if dependencies, embedded resources, licensing inputs or packaging change.
- Review the diff for accidental data, generated files and unrelated changes.
- Identify platform behavior that remains unverified. Compilation does not establish a successful Windows install, upgrade or uninstall.

Changes to downloads, archives, SSH, deployment, local MCP, filesystem access and privilege handling need particular care with trust boundaries and failure recovery. Use isolated fixtures for checks that could change a system or contact a remote service.

By submitting code, you represent that you have the right to contribute it under the project's license. Preserve applicable third-party attribution and include the source and license of any new external material. The desktop license does not grant general rights to third-party trademarks or to separately downloaded runtimes.
