import { readFileSync, writeFileSync } from "node:fs";
import { resolve } from "node:path";
import { fileURLToPath } from "node:url";

function escapeRtf(text) {
  return text.split("").map(char => {
    if (char === "\\" || char === "{" || char === "}") return "\\" + char;
    const code = char.charCodeAt(0);
    return code > 127 ? "\\u" + (code > 32767 ? code - 65536 : code) + "?" : char;
  }).join("");
}

// One authoritative license text is used by the source and the MSI dialog.
export function renderInstallerLicense(license) {
  const body = license.replace(/\r\n/g, "\n").split("\n").map(line => escapeRtf(line) + "\\par").join("\n");
  return "{\\rtf1\\ansi\\ansicpg1252\\deff0\\nouicompat\n" +
    "{\\fonttbl{\\f0\\fnil\\fcharset0 Segoe UI;}}\n" +
    "\\viewkind4\\uc1\\pard\\sa120\\sl240\\slmult1\\fs18\n" +
    "{\\b LaraEnv - Desktop License\\par}\n" +
    "The original desktop code is licensed under Apache-2.0. Third-party components retain their own licenses; see THIRD_PARTY_NOTICES.txt and the accompanying third_party directory.\\par\n\\par\n" +
    body + "\n}\n";
}

export function generateInstallerLicense({ check = false } = {}) {
  const target = new URL("../../build/windows/installer/LaraEnv-License.rtf", import.meta.url);
  const expected = renderInstallerLicense(readFileSync(new URL("../../LICENSE", import.meta.url), "utf8"));
  if (check) {
    if (readFileSync(target, "utf8").replace(/\r\n/g, "\n") !== expected) {
      throw new Error("Installer license is stale; run node tools/licenses/installer-rtf.mjs");
    }
  } else {
    writeFileSync(target, expected);
  }
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  const args = process.argv.slice(2);
  if (args.some(arg => arg !== "--check")) throw new Error("Usage: node tools/licenses/installer-rtf.mjs [--check]");
  generateInstallerLicense({ check: args.includes("--check") });
}
