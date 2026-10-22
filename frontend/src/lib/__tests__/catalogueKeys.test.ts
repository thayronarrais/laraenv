import { readFileSync, readdirSync, statSync } from "node:fs";
import { join } from "node:path";
import { describe, expect, it } from "vitest";

// frontend/package.json has "type": "module", so __dirname is undefined here
// (this file runs as native ESM under Vitest, not through a CJS shim) —
// import.meta.dirname is Node 24's replacement and is what actually resolves.
const SRC = join(import.meta.dirname, "..", "..");
const EN = JSON.parse(
  readFileSync(join(import.meta.dirname, "..", "..", "..", "..", "langs", "en.json"), "utf8"),
);

function walk(dir: string, out: string[] = []): string[] {
  for (const name of readdirSync(dir)) {
    const full = join(dir, name);
    if (statSync(full).isDirectory()) {
      // __tests__ files legitimately contain t("en", ...)-shaped calls that
      // are not catalogue keys (locale-selection tests, mock harnesses); skip
      // them so they cannot pollute either direction of this guard.
      if (name === "node_modules" || name === "__tests__") continue;
      walk(full, out);
    } else if (/\.tsx?$/.test(name)) {
      out.push(full);
    }
  }
  return out;
}

function flatten(node: unknown, prefix = "", out = new Set<string>()): Set<string> {
  const PLURALS = new Set(["zero", "one", "two", "few", "many", "other"]);
  if (typeof node !== "object" || node === null) return out;
  const entries = Object.entries(node as Record<string, unknown>).filter(([k]) => k !== "$meta");
  const isPlural = entries.length > 0 && entries.every(([k]) => PLURALS.has(k));
  if (isPlural) {
    out.add(prefix);
    return out;
  }
  for (const [key, value] of entries) {
    const path = prefix ? `${prefix}.${key}` : key;
    if (typeof value === "object" && value !== null) flatten(value, path, out);
    else out.add(path);
  }
  return out;
}

const defined = flatten(EN);
const sources = walk(SRC);

// Two different matchers for two different failure modes, deliberately not
// the same regex:
//
// - "does the code ask for a key that doesn't exist" needs a NARROW matcher:
//   genuine call sites only, t("..."). A false match here — e.g. on
//   "config.json", which is dot-shaped but never a catalogue key — would
//   fail the build over a key that was never a key. Missing a real call site
//   is harmless: it just means this guard doesn't vouch for that key, not
//   that anything breaks.
// - "is a key in en.json actually used" needs a BROAD matcher: does the
//   key's exact literal text appear anywhere in the source, not only at a
//   t(...) call site. Under-matching here breaks the build for no reason —
//   the same bug class as internal/tray/tray.go's Relabel(), which passes
//   keys through a local closure (`set(item, "backend.tray.showWindow")`)
//   rather than calling t(...) directly. Over-matching only ever spares a
//   key from being flagged, which is the safe direction to err in.
const CALL_RE = /\bt\(\s*["'`]([\w.-]+)["'`]/g;
const LITERAL_RE = /["'`]([A-Za-z0-9_-]+(?:\.[A-Za-z0-9_-]+)+)["'`]/g;

const calledKeys = new Set<string>();
const literalKeys = new Set<string>();
for (const file of sources) {
  const text = readFileSync(file, "utf8");
  for (const m of text.matchAll(CALL_RE)) calledKeys.add(m[1]);
  for (const m of text.matchAll(LITERAL_RE)) literalKeys.add(m[1]);
}

describe("catalogue keys", () => {
  it("every key the UI asks for exists in en.json", () => {
    const missing = [...calledKeys].filter((k) => !defined.has(k)).sort();
    expect(missing, `t() calls with no key in langs/en.json:\n  ${missing.join("\n  ")}`).toEqual([]);
  });

  it("every UI key in en.json is actually used", () => {
    // backend.* and errors.* are rendered from Go, so the frontend never
    // references them; a Go test covers those.
    const orphans = [...defined]
      .filter((k) => !k.startsWith("backend.") && !k.startsWith("errors."))
      .filter((k) => !literalKeys.has(k))
      .sort();
    expect(orphans, `keys in langs/en.json nobody uses:\n  ${orphans.join("\n  ")}`).toEqual([]);
  });
});
