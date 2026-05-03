# LaraEnv

**The ultimate development environment for Windows.**

LaraEnv is a modern, open-source replacement for Laragon and Laravel Herd. Spin up Nginx, MySQL, PostgreSQL, Redis, Mailpit and multi-version PHP/Node with one click — no Electron, no containers, no WSL. Just a native Windows binary (~13 MB) built with Wails + Go + React.

🌐 **Website & account:** https://laraenv.com

---

## Download

[**⬇ Download LaraEnv (MSI)**](https://github.com/thayronarrais/laraenv/releases/latest/download/LaraEnv-Setup.msi)

Other options on the [Releases page](https://github.com/thayronarrais/laraenv/releases).

| Asset | Notes |
|---|---|
| `LaraEnv-Setup.msi` | Recommended. Installs to `%ProgramFiles%\LaraEnv\`, Start Menu + Desktop shortcuts, clean uninstall via Control Panel. |
| `LaraEnv-x.y.z.msi` | Versioned copy of the same MSI (kept for archival). |
| `LaraEnv.exe` | Portable binary. No installer, just run. |
| `LaraEnv-x.y.z.msi.sha256` | SHA-256 sidecar. The in-app updater uses it to verify the download. |

**Requirements:** Windows 10/11 x64 · WebView2 Runtime (preinstalled on Win 11) · Administrator rights on first launch.

---

## What it does

- **One-click stack**: Nginx (or Apache) + MySQL (or Postgres) + PHP-FPM + Redis + Mailpit.
- **Multi-version runtimes**: keep PHP 5.6 → 8.4 side-by-side, switch globally or per project.
- **Project scaffolder**: Laravel, WordPress, Git clone, blank — with vhost + `hosts` entry + self-signed SSL generated for you.
- **Built-in terminal**: PowerShell, CMD, Git Bash, Cmder — multi-tab, multi-pane (split view), project-aware cwd and PATH.
- **SSH manager**: connect, generate ED25519/RSA/ECDSA keys, test connectivity, all from the app.
- **Cron scheduler**: per-project or global, in-process. Laravel scheduler one-click preset.
- **Auto-updates**: in-app check, MSI download, verify, install — see Settings → Updates.
- **System tray**: hide the window, services keep running.

---

## LaraEnv Pro — $3/month

Pro adds cloud features that work seamlessly with the free desktop app:

- **Sync** of SSH hosts, crons and preferences across machines (zero-knowledge AES-256-GCM, server can't read your data).
- **Mini-Forge deploy**: bootstrap a remote VPS from scratch — install nginx + PHP + composer + git, configure vhosts, set up Let's Encrypt HTTPS, schedule auto-pull on git push.
- Web dashboard at [laraenv.com/dashboard](https://laraenv.com/dashboard).

Subscribe via Stripe or PayPal at https://laraenv.com/pricing. Cancel anytime.

---

## Status

Used daily by the author. Bugs and feedback welcome on the [issue tracker](https://github.com/thayronarrais/laraenv/issues).

## License

Source-available. Releases are free to use; the desktop client is open. The cloud server is private.

---

This repository hosts the **binary releases** of the LaraEnv desktop app. Marketing, accounts, billing and the Pro API live at [laraenv.com](https://laraenv.com).

Made with ❤️ and hate for ads.
