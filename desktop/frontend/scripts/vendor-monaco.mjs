// Copies Monaco's prebuilt AMD bundle out of node_modules and into
// frontend/public/vs so it ships inside the app.
//
// @monaco-editor/react otherwise loads Monaco from cdn.jsdelivr.net the first
// time an editor mounts. LaraEnv runs as a desktop app over a local
// environment, so that request is exactly the one it cannot rely on: behind a
// firewall, a proxy, or simply offline it never completes, the loader promise
// never settles, and every built-in editor hangs on "Loading..." (issue #30).
//
// Runs from the prebuild/predev/pretest hooks in package.json. public/vs is
// generated, not committed — see .gitignore.
import { cpSync, existsSync, mkdirSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { pathToFileURL } from "node:url";
import { vendorLegal } from "./vendor-legal.mjs";

const FRONTEND = dirname(import.meta.dirname);
const DEST = join(FRONTEND, "public", "vs");
// Written last, so an interrupted copy leaves no stamp and the next run redoes
// the work instead of trusting a half-populated public/vs.
const STAMP = join(DEST, ".monaco-version");

function monacoRoot() {
  // Flat install first, then a workspace-hoisted one. Resolving via
  // require.resolve("monaco-editor") would pull in the ESM entry point and
  // load ~5 MB of editor source just to learn where the package lives.
  const candidates = [
    join(FRONTEND, "node_modules", "monaco-editor"),
    join(dirname(FRONTEND), "node_modules", "monaco-editor"),
  ];
  const found = candidates.find((dir) => existsSync(join(dir, "package.json")));
  if (!found) {
    throw new Error(`monaco-editor not found in node_modules (looked in: ${candidates.join(", ")})`);
  }
  return found;
}

export function vendorMonaco() {
  const root = monacoRoot();
  const { version } = JSON.parse(readFileSync(join(root, "package.json"), "utf8"));
  const src = join(root, "min", "vs");
  if (!existsSync(join(src, "loader.js"))) {
    throw new Error(`monaco-editor ${version} has no prebuilt AMD bundle at ${src}`);
  }

  const stamped = existsSync(STAMP) ? readFileSync(STAMP, "utf8").trim() : "";
  if (stamped === version) return DEST;

  rmSync(DEST, { recursive: true, force: true });
  mkdirSync(DEST, { recursive: true });
  cpSync(src, DEST, { recursive: true });
  writeFileSync(STAMP, `${version}\n`);
  return DEST;
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  vendorLegal();
  vendorMonaco();
}
