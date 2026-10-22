// Optional maintainer helper for copying the English catalogue, runtime
// fragment manifest and checker between local LaraEnv checkouts. The canonical
// public repository contains their source and accepts coordinated changes.
// Review the destination diff before committing; this helper overwrites only
// the three explicitly listed files, not translated catalogues.
//
// desktop/langs/README.md is maintained with the source and is deliberately not copied.
import { copyFileSync, existsSync, mkdirSync } from "node:fs";
import { join } from "node:path";

const target = process.argv[2];
if (!target) {
  console.error("usage: node tools/langs/push-en.mjs <path-to-laraenv-checkout>");
  process.exit(1);
}
if (!existsSync(join(target, "desktop", "langs"))) {
  console.error(`no desktop/langs/ directory in ${target}`);
  process.exit(1);
}
mkdirSync(join(target, "tools", "langs"), { recursive: true });

const files = [
  ["desktop/langs", "en.json"],
  ["desktop/langs", ".do-not-translate.json"],
  ["tools/langs", "check.mjs"],
];
for (const [dir, name] of files) {
  const dest = join(target, dir, name);
  copyFileSync(join(dir, name), dest);
  console.log(`copied ${dir}/${name} → ${dest}`);
}
console.log("review the destination diff and commit the intended catalogue changes.");
