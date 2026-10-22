# Desktop source snapshots

The desktop source candidate is exported from reviewed Git-index files using `tools/source-snapshot/policy.json`. It contains desktop code/tests, the optional cloud client, deployment presets, translations, owned assets, packaging inputs and complete collected dependency notices. The hosted backend is separately maintained and is not needed to build local features.

The canonical community source repository is [LaraEnv](https://github.com/thayronarrais/laraenv), alongside its [binary releases](https://github.com/thayronarrais/laraenv/releases). The reviewed desktop source was added on top of the existing public history; the former private builder history was not imported. The export policy governs the new source payload and does not rewrite earlier public commits.

The policy intentionally excludes private Git history, local configuration, dependency/build caches, downloaded runtimes, old internal audits/plans, historical release drafts and unused concept assets. New tracked paths outside its reviewed rules stop export until classified. Generated bindings, frontend assets and installed dependency trees are rebuilt following [development.md](development.md).

The Wails application is grouped under `desktop/`; the root Go module, shared `internal/` and `cmd/` packages, docs, tooling and legal inventory remain separate. The policy applies generated/private exclusions inside the nested project as well as at the root. The repository's current layout is listed in [README.md](../README.md#repository-layout).

From a maintainer checkout with the intended changes reviewed and staged:

```powershell
node --test tools/source-snapshot/*.test.mjs
node tools/source-snapshot/export.mjs export --output build/source/desktop-candidate-1
node tools/source-snapshot/export.mjs verify build/source/desktop-candidate-1
```

The output directory must not exist. Each attempt gets a new directory, preserving previous candidates. The exporter reads indexed blobs, so unstaged changes and untracked files are not copied. It rejects unsafe Windows paths, case aliases, tracked links/submodules, selected high-confidence credential patterns and output-link redirection before writing the candidate. These checks support a human content/security review; they do not prove arbitrary source is secret-free or authentic.

Outputs are `LaraEnv-desktop-source.zip`, its SHA-256 sidecar, and an identical `source/` tree. `SOURCE_SNAPSHOT_MANIFEST.json` inside both forms lists every indexed payload's path, mode, size and SHA-256. The generated manifest itself is outside its own file list. When importing an extracted snapshot into Git, leave this generated integrity artifact untracked: the root `.gitignore` ignores it, and the export policy excludes any force-staged old copy before regenerating it from the selected payloads. It contains no private Git history, excluded-file list or operator checkout path. The ZIP uses uncompressed entries, fixed metadata and preserved file modes so identical selected index bytes and policy produce identical archives. It is an integrity artifact, not a signed publisher attestation.

Verify the archive with an independent ZIP reader, then extract a separate copy for build/testing. Keep the pristine `source/` tree intact: building creates generated files that correctly make a later pristine-tree verification fail. Tests run in a new temporary Git repository and remain usable from an extracted source ZIP. Creating a new snapshot requires a Git index containing the intended public source.

Source preparation does not publish a release or change repository visibility. Before announcing a release, run CI and the checks in [RELEASING.md](../RELEASING.md), including clean Windows installation/upgrade tests. The optional runtime downloads retain separate redistribution obligations in [licensing.md](licensing.md).
