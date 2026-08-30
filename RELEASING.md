# Releasing

Manual steps for the maintainer, once per release.

---

## 1. Pick the version

Take it from the **published releases**, not from git tags. The tags in the code
repo lag: in August 2026 the highest tag was `v0.4.24` while the live release was
already `v0.4.29`. Building from a tag produces an installer numbered *behind*
what users have, which the in-app updater immediately flags as outdated.

```bash
gh release view --repo thayronarrais/laraenv --json tagName -q .tagName
```

The next release is that version plus one. Keep it fixed while you iterate — if
you rebuild five times before publishing, all five overwrite the same file.

---

## 2. Build

```powershell
# in C:\Users\thayr\Desktop\lara4ever\lara4ever
powershell build\windows\installer\build-msi.ps1 -Version 0.4.30
```

The script runs `wails build` itself (pass `-SkipBuild` to reuse the existing
executable) and injects the version through ldflags into
`internal/version.Version`, which is what the in-app updater compares against
the latest GitHub release.

Outputs land in `build\bin\`:

- `LaraEnv-0.4.30.msi`
- `LaraEnv-0.4.30.msi.sha256` — always emitted, always published
- `LaraEnv.exe`
- `LaraEnv-0.4.30.wixpdb` — build artefact, do not publish

### Check the build before publishing

The version has to be right in two places, and they come from different
mechanisms — the MSI property from WiX, the binary string from ldflags:

```powershell
$msi  = "build\bin\LaraEnv-0.4.30.msi"
$inst = New-Object -ComObject WindowsInstaller.Installer
$db   = $inst.GetType().InvokeMember("OpenDatabase","InvokeMethod",$null,$inst,@($msi,0))
$v    = $db.GetType().InvokeMember("OpenView","InvokeMethod",$null,$db,
        @("SELECT ``Value`` FROM ``Property`` WHERE ``Property``='ProductVersion'"))
$v.GetType().InvokeMember("Execute","InvokeMethod",$null,$v,$null)
$r = $v.GetType().InvokeMember("Fetch","InvokeMethod",$null,$v,$null)
$r.GetType().InvokeMember("StringData","GetProperty",$null,$r,@(1))
```

```bash
grep -c "0\.4\.30" build/bin/LaraEnv.exe   # non-zero: ldflags took effect
```

---

## 3. Publish

```bash
gh release create v0.4.30 \
  build/bin/LaraEnv-0.4.30.msi \
  build/bin/LaraEnv-0.4.30.msi.sha256 \
  --repo thayronarrais/laraenv \
  --title "LaraEnv 0.4.30 — short summary" \
  --notes-file notes.md \
  --latest
```

Both assets are required: the `.sha256` sidecar ships with every release so the
installer can be verified without re-hashing it by hand.

Release notes are written in **English**. Say what changed for the user, not
what changed in the code, and say plainly when an upgrade is optional.

### How the site's Download button finds it

`DownloadController` redirects to
`https://github.com/thayronarrais/laraenv/releases/latest` — the releases page,
not a specific file. So the new release is live the moment it is published, with
no site redeploy, and **no fixed asset filename is required**.

> An earlier version of this document required an extra copy named
> `LaraEnv-Setup.msi`, because a previous GitHub Pages site linked straight to
> that file. That site no longer exists and the download route no longer works
> that way. Releases from v0.4.29 onward ship only the versioned MSI and its
> sidecar.

---

## 4. Verify

1. Open <https://laraenv.com> in a private window and click **Download**. It
   should land on the new release.
2. Confirm the release shows as **Latest** and is not a draft.
3. Install the MSI on a test machine and confirm:
   - installs to `%ProgramFiles%\LaraEnv\`
   - Start Menu and Desktop shortcuts are created
   - first launch prompts UAC and opens the app
   - the entry in **Uninstall or change a program** shows the LaraEnv icon
   - legacy `C:\lara4ever\*` config migrates to `C:\laraenv\*` (when upgrading)
