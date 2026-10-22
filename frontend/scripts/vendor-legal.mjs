import { existsSync, lstatSync, mkdirSync, readFileSync, readdirSync, writeFileSync } from "node:fs";
import { join, resolve, parse } from "node:path";
import { fileURLToPath } from "node:url";

const DEFAULT_ROOT = fileURLToPath(new URL("../../", import.meta.url));
const FILES = [
  ["LICENSE", "LICENSE.txt"],
  ["NOTICE", "NOTICE.txt"],
  ["THIRD_PARTY_NOTICES.txt", "THIRD_PARTY_NOTICES.txt"],
  ["third_party/dependencies.json", "dependencies.json"],
];

function rejectLinks(path) {
  const absolute = resolve(path);
  let current = parse(absolute).root;
  for (const part of absolute.slice(current.length).split(/[\\/]/).filter(Boolean)) {
    current = join(current, part);
    let stat;
    try { stat = lstatSync(current); } catch (error) {
      if (error.code === "ENOENT") continue;
      throw error;
    }
    if (stat.isSymbolicLink()) throw new Error(`Legal assets cannot traverse a symbolic link: ${current}`);
  }
}

// Fixed, bounded outputs: preserve unknown files and reject symlink redirection.
// The aggregate includes full upstream texts, making the embedded UI sufficient
// even when the updater distributes only the executable.
export function vendorLegal({ repoRoot = DEFAULT_ROOT, check = false } = {}) {
  const dest = join(repoRoot, "frontend/public/legal");
  rejectLinks(dest);
  const sources = FILES.map(([source, target]) => {
    const path = join(repoRoot, source);
    rejectLinks(path);
    return [target, readFileSync(path)];
  });
  if (existsSync(dest)) {
    const names = new Set(FILES.map(([, target]) => target));
    for (const name of readdirSync(dest)) {
      if (!names.has(name)) throw new Error(`Unexpected legal output: ${name}; preserve or relocate it before regeneration`);
      rejectLinks(join(dest, name));
    }
  }
  if (!check) mkdirSync(dest, { recursive: true });
  for (const [target, bytes] of sources) {
    const path = join(dest, target);
    if (check) {
      if (!existsSync(path) || !readFileSync(path).equals(bytes)) throw new Error(`Legal asset is stale: ${target}`);
    } else writeFileSync(path, bytes);
  }
  return dest;
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  const args = process.argv.slice(2);
  if (args.some(arg => arg !== "--check")) throw new Error("Usage: node frontend/scripts/vendor-legal.mjs [--check]");
  vendorLegal({ check: args.includes("--check") });
}
