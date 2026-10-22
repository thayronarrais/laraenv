import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
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
