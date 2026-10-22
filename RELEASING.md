# Releasing

Manual steps for the maintainer, once per release.

Build the desktop from the reviewed public source candidate described in
[source snapshots](docs/source-snapshot.md). Complete the first-build setup in
[development](docs/development.md) before running the strict release checks.
The version `0.4.30` below is an illustrative historical example; replace every
occurrence with the reviewed version selected in step 1. It is not a current
release recommendation. Source preparation and CI do not authorize publication
or establish successful clean-machine installation/upgrade testing.

---

## 1. Pick the version

Take it from the **published releases**, not from git tags. The tags in the code
repo lag: in August 2026 the highest tag was `v0.4.24` while the live release was
already `v0.4.29`. Building from a tag produces an installer numbered *behind*
what users have, which the in-app updater immediately flags as outdated.

```bash
gh release view --repo thayronarrais/laraenv --json tagName -q .tagName
```

The next release is that version plus one. Keep it fixed while you iterate.
Each packaging run requires an empty or nonexistent output directory, so a
retry uses a new `-OutputDir`; previous artifacts remain available for comparison.

---

## 2. Build

```powershell
# From the project root, after the first source build in docs/development.md
node tools/licenses/generate.mjs --check
node tools/licenses/installer-rtf.mjs --check
pwsh build\windows\installer\build-release.ps1 -Version 0.4.30
```

The script runs the Wails build from this checkout's module graph using
`go run -mod=readonly github.com/wailsapp/wails/v2/cmd/wails` (pass `-SkipBuild` to reuse the existing
executable) and injects the version through ldflags into
`internal/version.Version`, which is what the in-app updater compares against
the latest GitHub release. The helper passes `-webview2 browser`, matching the
notice inventory's `desktop,wv2runtime.browser,production` build tags. Automatic
module synchronization is disabled so the tool security floors remain pinned. WebView2
is a separate platform prerequisite. New builds use a unique executable name in
`build\bin\` and do not run `wails -clean`. `-SkipBuild` reuses
`build\bin\LaraEnv.exe`; verify that binary's provenance and version yourself.

The notice generator and installer RTF checks are mandatory at the start of
release preparation. If dependency locks or license inputs change, regenerate
and review `THIRD_PARTY_NOTICES.txt`, `third_party/dependencies.json` and the
installer RTF before retrying. The text aggregate includes full dependency
license texts; the JSON records versions, scopes and evidence hashes. These checks do not clear the separate
runtime-download, build-tool, historical binary or remaining security issues.

WiX CLI **5.0.2**, `WixToolset.UI.wixext/5.0.2` and
`WixToolset.Util.wixext/5.0.2` must already be provisioned. The release script
checks the CLI and global extension cache and reports missing prerequisites;
it does not install or update global tools. Both extension references are
version-pinned when compiling the MSI. The cached extension DLL hashes must
match the official-package hashes recorded in the notice policy.

Everything this release publishes lands in `build\release\0.4.30\` by
default, and the directory must be empty or nonexistent. A separate attempt
can use `-OutputDir build\release\0.4.30-review-2`. Each directory contains:

- `LaraEnv-0.4.30.msi` + `.sha256` — the installed edition
- `LaraEnv-0.4.30-portable-win64.zip` + `.sha256` — the portable edition: the
  executable, the `.laraenv-portable` marker that activates portable mode, and
  a README, root `LICENSE`, `NOTICE`, `THIRD_PARTY_NOTICES.txt`, and
  `third_party/dependencies.json`, plus the WiX corresponding-source archive at
  `third_party/sources/wix-5.0.2-source.zip`
- `LaraEnv.exe` + `.sha256` — **not a user download.** With no marker beside it
  this executable is not portable; it resolves the normal installed root. It
  ships because the in-app updater fetches it to replace a portable copy's
  executable in place, and that path refuses to run without the sidecar.

The MSI installs the same license/notice payloads beside the executable,
with the aggregate in `third_party\` and the pinned WiX source archive in
`third_party\sources\`. The source archive accompanies the compiled WiX
installer components and is checked by notice generation before packaging.
The bare executable is the updater payload and embeds the complete notice
text for offline viewing in Settings. Sidecar notices in older installations
are not rewritten by an executable-only update; use the embedded current
notices, or install the current complete package to refresh those files.

Intermediate packaging assets live in a unique temporary directory outside
the release output. Its path is printed and retained for inspection. Installer
BMPs and the ICO are generated there from the owned `build/appicon.png` and
`frontend/logo_laraenv.png`; legacy installer artwork is not reused. Previous
release directories are never recursively cleaned or overwritten.

For a packaging fixture, supply a known executable and an isolated empty path:

```powershell
pwsh build\windows\installer\build-release.ps1 -Version 0.4.30 `
  -SourceExe C:\fixtures\LaraEnv.exe -OutputDir build\verification\packaging-fixture
```

`-SourceExe` implies `-SkipBuild` and `-SkipLangSync`; it only packages the file.
It does not execute it or establish release provenance. Fixture outputs must
stay out of the directory used for publishing.

The optional NSIS authoring runs both notice checks at compile time, shows the
root license, and includes the same payloads and WiX source archive. Its default output is the
version-specific release directory, which must already exist. To compile
separately, pass `-DARG_OUTPUT_FILE=<new-file-path>` to `makensis`; existing
output files are refused. `-DARG_INSTALLER_ICON=<generated-ico>` can reuse the
owned icon printed in the MSI helper's staging directory. The default icon is
the one Wails generates from `build/appicon.png`. NSIS does not embed or run
the Microsoft WebView2 bootstrapper; provision WebView2 separately, as for MSI.

Future pgvector ZIPs built by `build/windows/pgvector/build-pgvector.ps1`
include the upstream `LICENSE` at `share/doc/pgvector/LICENSE` and refuse to
package when it is missing. They use unique temporary work trees and require
an empty/nonexistent output directory (default `dist/<version>-pg<major>` next
to that script, or `-OutputDir`; the old `-OutDir` spelling remains an alias).
Existing published runtime ZIPs are not retroactively changed by this script.

The four pgvector 0.8.6 ZIPs and matching checksum sidecars were separately
repaired and replaced on 2026-10-21 after owner authorization. The existing
release URLs remain unchanged; the archive hashes changed because the exact
upstream LICENSE was added. All 43 original members per ZIP were preserved.
The version/hash-bound publication record is in `tools/licenses/runtime-policy.json`;
see [public readback validation](docs/opensource-audit/2026-10-21/runtime-publication-verification.md).
Download corrected ZIPs together with their current checksum sidecars.

### Check the build before publishing

The version has to be right in two places, and they come from different
mechanisms — the MSI property from WiX, the binary string from ldflags:

```powershell
$msi  = "build\release\0.4.30\LaraEnv-0.4.30.msi"
$inst = New-Object -ComObject WindowsInstaller.Installer
$db   = $inst.GetType().InvokeMember("OpenDatabase","InvokeMethod",$null,$inst,@($msi,0))
$v    = $db.GetType().InvokeMember("OpenView","InvokeMethod",$null,$db,
        @("SELECT ``Value`` FROM ``Property`` WHERE ``Property``='ProductVersion'"))
$v.GetType().InvokeMember("Execute","InvokeMethod",$null,$v,$null)
$r = $v.GetType().InvokeMember("Fetch","InvokeMethod",$null,$v,$null)
$r.GetType().InvokeMember("StringData","GetProperty",$null,$r,@(1))
```

```bash
rg -a -c "0\.4\.30" build/release/0.4.30/LaraEnv.exe   # non-zero: ldflags took effect
```

Also inspect the MSI file table and portable ZIP entries for all license
payloads, including `third_party/sources/wix-5.0.2-source.zip`, compare their
extracted hashes with the checked source files, and
verify the artifact SHA256 sidecars. Inspecting an MSI does not require
installing it. Check the remaining security/provenance findings separately
before deciding to publish.

---

## 3. Publish

```bash
gh release create v0.4.30 build/release/0.4.30/* \
  --repo thayronarrais/laraenv \
  --title "LaraEnv 0.4.30 — short summary" \
  --notes-file notes.md \
  --latest
```

Glob only the reviewed version directory. Every sidecar ships with every
release so artefacts can be verified without re-hashing them by hand — and a
portable copy cannot update at all without `LaraEnv.exe.sha256`, since it
refuses to replace its own executable against an unverified download.

Release notes are written in **English**. Say what changed for the user, not
what changed in the code, and say plainly when an upgrade is optional.

### How the site's Download button finds it

`DownloadController` redirects to
`https://github.com/thayronarrais/laraenv/releases/latest` — the releases page,
not a specific file. So the new release is live the moment it is published, with
no site redeploy, and **no fixed asset filename is required**.

The README's **Download MSI** button points at the same URL, for the same
reason.

> Earlier releases were expected to carry an extra copy named
> `LaraEnv-Setup.msi`, so that a fixed URL
> (`releases/latest/download/LaraEnv-Setup.msi`) always resolved. That rule is
> gone. It relied on remembering the copy in every single release — and it was
> already forgotten in v0.4.29, which left the README's download button
> returning 404 until v0.4.30. Both consumers now point at the releases page,
> which cannot go stale.

---

## 4. Verify

1. Open <https://laraenv.com> in a private window and click **Download**. It
   should land on the new release.
2. Confirm the release shows as **Latest** and is not a draft.
3. Extract the portable ZIP somewhere that cannot collide with the installed
   root (Windows is case-insensitive, so `C:\LaraEnv` *is* `C:\laraenv` and the
   two modes would resolve to the same folder). Run it and confirm the whole
   environment is created inside the extracted folder and `C:\laraenv` is not.
4. Install the MSI on a test machine and confirm:
   - installs to `%ProgramFiles%\LaraEnv\`
   - Start Menu and Desktop shortcuts are created
   - first launch prompts UAC and opens the app
   - the entry in **Uninstall or change a program** shows the LaraEnv icon
   - legacy `C:\lara4ever\*` config migrates to `C:\laraenv\*` (when upgrading)
