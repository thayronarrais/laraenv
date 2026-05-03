# Releasing & Pages setup

Manual steps for the maintainer. Do this after the first `git push` and once per new release.

---

## One-time setup — enable GitHub Pages

1. Go to https://github.com/thayronarrais/laraenv/settings/pages
2. Under **Build and deployment → Source**, select **GitHub Actions**.
3. Save.
4. Trigger the first deploy:
   - Go to **Actions** tab → **Deploy to GitHub Pages** → **Run workflow** → branch `main`.
   - Or push any commit to `main` (the workflow runs automatically).
5. Wait ~1 min. Site goes live at **https://thayronarrais.github.io/laraenv/**

If a custom domain is added later, drop a `CNAME` file in `public/` with the domain name and configure DNS.

---

## Per-release — publish a new version

After running the build pipeline in `laraenv-builder`:

```powershell
# in C:\Users\thayr\Desktop\lara4ever\lara4ever
wails build -clean -platform windows/amd64
pwsh build\windows\installer\build-msi.ps1 -Version 0.1.0 -SkipBuild
```

Outputs are in `C:\Users\thayr\Desktop\lara4ever\lara4ever\build\bin\`:
- `LaraEnv.exe`
- `LaraEnv-0.1.0.msi`

### Upload to Releases

1. Make a copy of `LaraEnv-0.1.0.msi` named `LaraEnv-Setup.msi` (the site's Download button hardcodes this filename so the link stays stable across versions).

2. Go to https://github.com/thayronarrais/laraenv/releases/new

3. Fill in:
   - **Tag**: `v0.1.0` (create new tag, target `main`)
   - **Title**: `LaraEnv 0.1.0`
   - **Description**: short changelog (what's new, what's fixed)
   - **Assets** (drag and drop):
     - `LaraEnv-Setup.msi` ← **required** (powers the stable Download link)
     - `LaraEnv-0.1.0.msi` ← versioned archive copy
     - `LaraEnv.exe` ← portable, optional
   - Check **Set as the latest release**.

4. Publish.

The Download button on the site (`/releases/latest/download/LaraEnv-Setup.msi`) now serves the new MSI immediately — no site redeploy needed.

---

## Stable filename rule

**Always upload `LaraEnv-Setup.msi`** in every release. It is the public, version-agnostic name referenced by:

- The hero CTA in `src/App.tsx`
- The README.md download link

If the file is missing from a release, the Download button on the live site will 404.

---

## Versioning

- Bump `ProductVersion` in `build-msi.ps1` invocation (`-Version 0.2.0`).
- Update the matching version in any UI references (e.g. README "Status: vX.Y.Z").
- Tag matches the version: `v0.2.0`.

---

## Verifying a release

1. Open https://thayronarrais.github.io/laraenv/ in a private window.
2. Click **Download**. The browser should start downloading `LaraEnv-Setup.msi` directly.
3. Run the MSI on a test machine. Confirm:
   - Installs to `%ProgramFiles%\LaraEnv\`
   - Start Menu and Desktop shortcuts created
   - First launch prompts UAC, opens the app
   - Migrates legacy `C:\lara4ever\*` config to `C:\laraenv\*` automatically (if upgrading)
