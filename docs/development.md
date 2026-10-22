# Desktop development

Run commands from the project root in PowerShell unless stated otherwise. The desktop is a Windows application; some packages and tests are platform-specific. The separately maintained cloud backend is not needed to build the desktop or develop local features.

## Prerequisites

| Tool | Required input |
| --- | --- |
| Windows | Windows 10/11 x64 desktop target |
| Go | 1.26.10, matching `go.mod` and the notice policy |
| Node.js/npm | Node.js 24 or newer; install with the committed npm lockfiles |
| Wails | v2.12.0, built from this project's module graph as below |
| WebView2 | Separately installed Microsoft runtime with Chromium 111 or newer, for running the GUI |
| Git | Required for Git integration and relevant tests |

Packaging an MSI additionally requires PowerShell 7, .NET SDK 8, WiX CLI 5.0.2, and `WixToolset.UI.wixext/5.0.2` and `WixToolset.Util.wixext/5.0.2`. These are not prerequisites for ordinary frontend tests. See [RELEASING.md](../RELEASING.md) before packaging.

## Provision dependencies

The first provisioning step requires network access to Go and npm dependency sources. Install the required toolchains first, then use the locked graphs:

```powershell
go version
node --version
npm --version
go mod download
go list -mod=readonly -m -json all | Out-Null
npm ci --prefix frontend --ignore-scripts
npm ci --prefix tools/screenshots --ignore-scripts
New-Item -ItemType Directory -Force build/bin | Out-Null
go build -mod=readonly -o build/bin/wails.exe github.com/wailsapp/wails/v2/cmd/wails
node tools/licenses/installer-rtf.mjs --check
.\build\bin\wails.exe doctor
```

The explicit `go list` call caches expanded graph metadata needed by offline notice verification; `go mod download` alone does not populate all of it in a fresh cache. The screenshots dependency tree is also an input to the reviewed dependency inventory. Installing it does not require running the screenshot capture harness or downloading a browser.

Build Wails from the checkout rather than using `go install ...@version`, which resolves the tool's upstream graph separately. This project carries explicit tool dependencies and security version floors in `go.mod`; preserve them when updating dependencies. Do not run `go mod tidy` as routine setup.

Notice checks need the installed dependencies, cached Go sources and generated embed assets. Run the community checker after the first build below. Its Go metadata commands disable automatic downloads with `GOTOOLCHAIN=local`, `GOPROXY=off`, `GOSUMDB=off` and read-only module resolution. Missing sources or stale required evidence cause a failure; resolve missing inputs during provisioning rather than silently changing the inventory. If Go 1.26.10 is not the default `go`, pass its executable as the optional positional argument to `tools/ci/check-notices.mjs`. The strict generator accepts `--go-bin`, as described in the [tooling guide](../tools/licenses/README.md).

## First build and generated files

A source checkout does not include `frontend/dist`, `frontend/wailsjs`, `frontend/public/vs` or `frontend/public/legal`. Wails creates the embed placeholder, generates Go-to-JavaScript bindings, then builds the frontend. Run a Wails build before attempting a standalone frontend build or the full Go suite:

```powershell
node tools/ci/build-desktop.mjs .\build\bin\wails.exe
node tools/ci/check-notices.mjs
node frontend/scripts/vendor-legal.mjs --check
```

The output is `build/bin/LaraEnv.exe`. This command builds the application; it does not install or launch it. After the locked `npm ci` provisioning above, the helper temporarily disables Wails' redundant frontend installation and restores `wails.json` byte-for-byte even if the build fails. Bindings and the complete frontend/Go build still run. Frontend hooks copy Monaco and the reviewed legal texts into local assets so the editors and notices do not require a CDN at runtime.

Use both `-m` and `-nosyncgomod` to skip module tidying and Wails module synchronization, preserving the project graph. Production builds must use `-webview2 browser`, matching the dependency inventory's build tags and the separate WebView2 prerequisite. Do not change that strategy without reviewing the resulting distribution and license inputs.

The executable alone is not a portable package. Portable mode needs its marker and accompanying notice payloads; MSI/portable packaging and version injection are covered in [RELEASING.md](../RELEASING.md).

## Development mode

```powershell
.\build\bin\wails.exe dev -m -nosyncgomod
```

This starts Vite and the desktop with hot reload. Running the GUI uses the application's normal local environment and may request elevation for system integration. Use a test Windows profile or isolated environment when exercising actions that change hosts, certificates, services, databases, scheduled tasks or files.

Cloud integration starts disabled. A saved token or an older synchronization preference does not enable it. Keep it disabled for local-only development; no hosted backend credentials are needed. Dependencies and runtimes must already be available for offline use. Downloads, remote Git/SSH, external providers and other network operations have their own connectivity requirements. See [desktop offline behavior](desktop-offline.md).

## Checks

After the first Wails build, run the appropriate checks for a change:

```powershell
go test -mod=readonly ./...
npm --prefix frontend test
npm --prefix frontend run lint
npm --prefix frontend run build
node --test tools/licenses/*.test.mjs tools/ci/*.test.mjs tools/source-snapshot/*.test.mjs
node tools/ci/check-notices.mjs
node tools/licenses/installer-rtf.mjs --check
node frontend/scripts/vendor-legal.mjs --check
```

Use focused tests while iterating, then run the broader affected suite before submitting. Go tests can depend on Windows features, Git and filesystem permissions. The license tooling has a directory-junction fixture that requires permission to create junctions. Report an environment-related failure with the exact output instead of treating a skipped or blocked check as passing. The frontend lint focuses on React hook correctness; it is not a general security audit.

The community notice checker verifies every committed license text, aggregate and WiX source archive, then compares regenerated inventory metadata. Only installed evidence and scope for optional, nonrequired npm build tools may vary between hosts because npm omits platform-specific binaries. Runtime evidence, Go dependencies, nonoptional npm components, lockfile identities and policy inputs must still match. The checker does not rewrite tracked files.

For release preparation and intentional dependency or legal-input updates, use the reviewed release dependency tree and the strict check:

```powershell
node tools/licenses/generate.mjs --check
```

Investigate a mismatch before regenerating notices. A platform difference in optional npm build-tool evidence alone does not require changing the committed inventory. For intentional input changes, regenerate and review the outputs using the [licensing guide](licensing.md) and [tooling guide](../tools/licenses/README.md), then rerun the strict check. Do not edit generated Wails bindings manually or commit dependency caches and compiled output.

## Source layout

| Path | Purpose |
| --- | --- |
| `main.go`, `app*.go` | Wails lifecycle and desktop methods exposed to the frontend |
| `internal/` | Local configuration, projects, services, installers, terminals, SSH, deployment and integration packages |
| `frontend/src/` | React UI and frontend tests |
| `frontend/wailsjs/` | Generated Wails bindings and runtime bridge |
| `langs/` | Translation catalogs |
| `cmd/deploy-tui/` | Standalone SSH deployment client |
| `build/windows/` | Windows metadata and packaging sources |
| `tools/licenses/`, `third_party/` | Notice generation, preserved texts and dependency inventory |

Public methods on `App` are exposed through generated bindings under `frontend/wailsjs/go/main/`. Changes to exposed signatures need a Wails regeneration/build before frontend type checking. Backend events use Wails runtime events; keep producer and consumer names and payloads aligned.

## Release limits

Successful unit tests, compilation, notice checks and MSI inspection establish only their respective results. Clean Windows installation and upgrade verification remain release gates. Test those behaviors on an isolated clean Windows machine before publishing a new release.

The runtime catalog downloads software separately from suppliers or accepts user-provided archives. Desktop source licensing and retained notices do not clear those runtime binaries for a new project mirror or bundle. Review exact archives, licenses and source-delivery obligations before redistributing them; see the [licensing guide](licensing.md).
