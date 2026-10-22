import assert from "node:assert/strict";
import { readFileSync, mkdtempSync, mkdirSync, writeFileSync, copyFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { spawnSync } from "node:child_process";
import test from "node:test";
import { renderInstallerLicense } from "./installer-rtf.mjs";

test("the desktop license preserves the canonical Apache trademark qualification", () => {
  const license = readFileSync(new URL("../../LICENSE", import.meta.url), "utf8");
  assert.ok(license.replace(/\s+/g, " ").includes("except as required for reasonable and customary use in describing the"), "Apache trademark qualification must remain intact");
});

test("installer rendering preserves the full license including the Appendix", () => {
  const text = "Apache License\n\n6. Trademarks.\nAPPENDIX: How to apply the Apache License to your work.\nCopyright 2026 Thayron Arrais\n";
  const rtf = renderInstallerLicense(text);
  for (const line of text.trimEnd().split("\n")) assert.ok(rtf.includes(line + "\\par"), line);
  assert.ok(rtf.includes("THIRD_PARTY_NOTICES.txt"));
  assert.ok(!rtf.includes("By installing or using"));
});

test("license text cannot inject RTF control words and Unicode is retained", () => {
  const rtf = renderInstallerLicense("{vendor} \\test café 😀\n");
  assert.ok(rtf.includes("\\{vendor\\} \\\\test caf\\u233? \\u-10179?\\u-8704?\\par"));
});

test("installer CLI checks the nested desktop RTF against the repository license", () => {
  const root = mkdtempSync(join(tmpdir(), "laraenv-rtf-layout-"));
  mkdirSync(join(root, "tools/licenses"), { recursive: true });
  mkdirSync(join(root, "desktop/build/windows/installer"), { recursive: true });
  writeFileSync(join(root, "package.json"), '{"type":"module"}');
  const license = "Apache License\nAPPENDIX: complete fixture\nCopyright Example\n";
  writeFileSync(join(root, "LICENSE"), license);
  const target = join(root, "desktop/build/windows/installer/LaraEnv-License.rtf");
  writeFileSync(target, renderInstallerLicense(license));
  const script = join(root, "tools/licenses/installer-rtf.mjs");
  copyFileSync(new URL("./installer-rtf.mjs", import.meta.url), script);
  const checked = spawnSync(process.execPath, [script, "--check"], { encoding: "utf8" });
  assert.equal(checked.status, 0, checked.stderr);
  writeFileSync(target, "stale fixture");
  const stale = spawnSync(process.execPath, [script, "--check"], { encoding: "utf8" });
  assert.notEqual(stale.status, 0);
  assert.match(stale.stderr, /Installer license is stale/);
});
