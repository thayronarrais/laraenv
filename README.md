# LaraEnv

**The ultimate development environment for Windows.**

LaraEnv is a modern, open-source replacement for Laragon and Laravel Herd. Spin up Nginx, MySQL, PostgreSQL, Redis, Mailpit and multi-version PHP/Node with one click — no Electron, no containers, no WSL. Just a native Windows binary (~13 MB) built with Wails + Go + React.

🌐 **Website:** https://thayronarrais.github.io/laraenv/

---

## Download

[**⬇ Download LaraEnv (MSI)**](https://github.com/thayronarrais/laraenv/releases/latest/download/LaraEnv-Setup.msi)

Other options on the [Releases page](https://github.com/thayronarrais/laraenv/releases).

| Asset | Notes |
|---|---|
| `LaraEnv-Setup.msi` | Recommended. Installs to `%ProgramFiles%\LaraEnv\`, Start Menu + Desktop shortcuts, clean uninstall via Control Panel. |
| `LaraEnv-x.y.z.msi` | Versioned copy of the same MSI (kept for archival). |
| `LaraEnv.exe` | Portable binary. No installer, just run. |

**Requirements:** Windows 10/11 x64 · WebView2 Runtime (preinstalled on Win 11) · Administrator rights on first launch.

---

## What it does

- **One-click stack**: Nginx (or Apache) + MySQL (or Postgres) + PHP-FPM + Redis + Mailpit.
- **Multi-version runtimes**: keep PHP 5.6 → 8.4 side-by-side, switch globally or per project.
- **Project scaffolder**: Laravel, WordPress, Git clone, blank — with vhost + `hosts` entry + self-signed SSL generated for you.
- **Built-in terminal**: PowerShell, CMD, Git Bash, Cmder — multi-tab, project-aware cwd and PATH.
- **SSH manager**: connect, generate ED25519/RSA/ECDSA keys, test connectivity, all from the app.
- **System tray**: hide the window, services keep running.

---

## Status

`v0.1.0 — Beta`. Used daily by the author. Bugs and feedback welcome on the [issue tracker](https://github.com/thayronarrais/laraenv/issues).

## License

TBD.

---

This repository hosts the **public landing page and binary releases**. The application source lives in a separate repository.

Made with ❤
