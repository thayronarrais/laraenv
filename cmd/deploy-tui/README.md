# deploy-tui

Standalone terminal app for deploying a project to a Linux host over SSH. It
uses the same plans, detection probes, and presets as the desktop app through
`internal/deploy`, with `internal/ssh` handling the connection. No LaraEnv
account or cloud API is required.

## Build and run

```powershell
go build -o deploy-tui.exe ./cmd/deploy-tui
./deploy-tui.exe
```

The form collects the SSH host, project, stack, HTTPS, and auto-deploy settings.
For example, use a deployment name such as `my-app`, a domain you control, and
your project's Git HTTPS URL. Supply a PAT in its separate field only if the
repository requires one. The CLI keeps these inputs in memory for the run.

The app probes the host, builds an installation plan, and shows each step with
a live log. Depending on the selected stack, the plan installs packages,
clones the repository under `/var/www/<name>`, runs the build script, configures
the web server, and optionally configures HTTPS and an auto-deploy cron.

The current form uses a free-text build script. Enter commands appropriate to
your own project; these commands run on the remote host with the selected SSH
identity. The detection and plan builders account for packages already present,
but custom build scripts must handle their own repeated execution.

## Execution limits

- The selected user must be able to run the plan's privileged steps with
  `sudo -n`. Review the access needed on your own host before running.
- A failed step stops the run. Previously applied changes remain on the host;
  inspect the log and remote state before retrying.
- The CLI does not use the desktop's deployment history or process-wide
  operation coordinator. Run only one deployment tool against a resource at
  a time.
- SSH, package downloads, Git, and certificate issuance require access to their
  respective network services.
