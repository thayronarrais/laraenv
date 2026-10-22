# LaraEnv

LaraEnv is a Windows desktop development environment for PHP/Laravel, Node.js and Python, with local services and a workspace for terminals, files, Git and optional coding agents.

It is built with Go, Wails v2, React and Tailwind. The original desktop source is licensed under [Apache-2.0](LICENSE). Third-party components keep their own licenses; see [NOTICE](NOTICE), [THIRD_PARTY_NOTICES.txt](THIRD_PARTY_NOTICES.txt) and the [licensing guide](docs/licensing.md).

The desktop works with local data without a LaraEnv account or subscription. The optional cloud integration starts disabled, including for existing profiles that have not opted in. This source tree includes the cloud client; the separately maintained hosted backend remains private and is not needed to build or use local desktop features.

Desktop source, contributions and [binary releases](https://github.com/thayronarrais/laraenv/releases) share this [LaraEnv repository](https://github.com/thayronarrais/laraenv). Published packages can predate the changes in this source tree; consult each release's notes for its behavior and validation.

## Features

- Manage web servers, databases, Mailpit and Redis from one dashboard.
- Install and select multiple PHP, Node.js and Python versions. Detect project requirements and apply per-project PHP/Node overrides.
- Create projects, manage virtual hosts and local HTTPS, run package scripts and Laravel workers, and inspect configuration with Site Doctor.
- Use a multi-tab terminal with PowerShell, CMD, Git Bash or Cmder; browse and edit files, preview Markdown, and review Git changes alongside it.
- Manage SSH hosts and keys, and run deployments over SSH using local configuration and history.
- Enable experimental coding-agent sessions and local MCP access explicitly in Settings.

Installed runtimes and local features can operate without the LaraEnv API. Initial dependency provisioning, runtime downloads, updates, remote Git, SSH, certificate issuance and external providers still need their respective networks and credentials. Downloaded runtimes are separate products with their own licenses.

## Run a published package

The desktop targets **Windows 10/11 x64** and requires a separately installed **Microsoft WebView2 Runtime with Chromium 111 or newer**. The standard build uses the installed runtime and does not bundle Microsoft's bootstrapper. The GUI requests administrator access for system integration such as editing the Windows hosts file.

Use the [GitHub releases page](https://github.com/thayronarrais/laraenv/releases) and read the notes for the selected release:

- **MSI:** install the versioned `LaraEnv-<version>.msi` package.
- **Portable:** extract `LaraEnv-<version>-portable-win64.zip` and keep its `.laraenv-portable` marker beside `LaraEnv.exe`.

The bare `LaraEnv.exe` release asset is an updater payload; the executable alone does not select portable mode. Packages have SHA-256 sidecars. Installed environments normally use `C:\laraenv`; portable environments use the extracted folder. Keep portable copies in a separate directory from an installed environment.

## Build from source

Clone the desktop repository:

```powershell
git clone https://github.com/thayronarrais/laraenv.git
cd laraenv
```

Use **Go 1.26.10**, **Node.js 24 or newer** with npm, and **Wails v2.12.0 from this checkout's Go module graph**. Build and development instructions, including the clean-checkout bootstrap and tests, are in the [development guide](docs/development.md).

The module and npm lockfiles are part of the build inputs. Preserve the project graph when building Wails, use both `-m` and `-nosyncgomod`, and use `-webview2 browser` for production builds. First-time provisioning requires network access; the notice checks themselves use cached dependencies and do not download missing inputs.

MSI packaging additionally needs .NET SDK 8, WiX CLI 5.0.2 and its matching UI/Util extensions. See [RELEASING.md](RELEASING.md) for packaging and release validation. A successful compile or package inspection does not replace clean-machine installation and upgrade testing.

## Guides

- [Projects workspace](docs/projects-workspace.md), [runtime detection](docs/project-runtime-detection.md) and [Site Doctor](docs/site-doctor.md)
- [Terminal, files and Git workspace](docs/workspace.md)
- [Offline desktop and optional cloud](docs/desktop-offline.md)
- [Local MCP access](docs/mcp.md)
- [Standalone SSH deployment TUI](cmd/deploy-tui/README.md)
- [License and dependency policy](docs/licensing.md)
- [Source snapshot creation and verification](docs/source-snapshot.md)

## Contribute and report issues

Read [CONTRIBUTING.md](CONTRIBUTING.md) for development and pull-request guidance. Use [GitHub issues](https://github.com/thayronarrais/laraenv/issues) for reproducible bugs and feature proposals, with sensitive data removed.

Report security vulnerabilities privately using [SECURITY.md](SECURITY.md).
