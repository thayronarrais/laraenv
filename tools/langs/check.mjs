// Validates the translation catalogues against en.json. Runs in two places:
// GitHub Actions in the public repo on any PR touching langs/**, and here as a
// Vitest case, so en.json is never published already divergent.
//
// No dependencies, by design — a translator's PR should not need an install
// step to be checked.
import { readdirSync, readFileSync } from "node:fs";
import { join } from "node:path";

const FALLBACK = "en";
const PLURAL_CATEGORIES = new Set(["zero", "one", "two", "few", "many", "other"]);

// Keys whose value the app itself substring-matches at runtime (SFTP
// large-file handling, agent-restore detection, the VC++ redistributable
// prompt) live in langs/.do-not-translate.json, not here — see
// readDoNotTranslate below. That file is intentionally excluded from
// readDir's locale listing.
const MANIFEST_FILE = ".do-not-translate.json";

// A namespace whose every child is named after a plural category is
// indistinguishable from a plural entry. Rather than guess, name the case.
function isPluralEntry(node) {
  if (typeof node !== "object" || node === null || Array.isArray(node)) return false;
  const keys = Object.keys(node);
  return keys.length > 0 && keys.every((k) => PLURAL_CATEGORIES.has(k));
}

// Flattens to "a.b.c" → string, plus a separate map of plural entries so the
// two get checked by different rules.
function flatten(doc, prefix = "", out = { strings: new Map(), plurals: new Map() }) {
  for (const [key, value] of Object.entries(doc)) {
    if (key === "$meta") continue;
    const path = prefix ? `${prefix}.${key}` : key;
    if (isPluralEntry(value)) {
      out.plurals.set(path, value);
    } else if (typeof value === "object" && value !== null && !Array.isArray(value)) {
      flatten(value, path, out);
    } else {
      out.strings.set(path, String(value));
    }
  }
  return out;
}

const placeholders = (s) => new Set([...String(s).matchAll(/\{\{(\w+)\}\}/g)].map((m) => m[1]));

// CLDR says which categories a language actually uses. Asking Intl means no
// table to maintain and no chance of it drifting from the runtime that renders.
// Falls back to the universal ["one", "other"] shape only for the plural-forms
// question; canonicalLocaleError (below) is what actually gates an
// unrecognised or misspelled tag like "ru_RU" — this fallback existing on its
// own is exactly what let ru_RU.json pass with only {one, other} before.
function requiredCategories(code) {
  try {
    return new Set(new Intl.PluralRules(code).resolvedOptions().pluralCategories);
  } catch {
    return new Set(["one", "other"]);
  }
}

// A locale filename has to be a real, canonical BCP-47 tag — not merely a
// string Intl.PluralRules tolerates. "ru_RU" (underscore, POSIX-style) is
// invalid BCP-47 and both Intl.PluralRules and Intl.getCanonicalLocales
// reject it, but the old code only asked Intl.PluralRules and then silently
// fell back to a generic ["one", "other"] shape — so ru_RU.json shipped
// broken Russian plurals and nothing ever caught it. Returns null when code
// is fine, else a message naming the canonical form to rename the file to.
function canonicalLocaleError(code) {
  let canonical;
  try {
    canonical = Intl.getCanonicalLocales(code);
  } catch {
    canonical = null;
  }
  if (canonical && canonical[0] === code) return null;
  const suggestion = canonical ? canonical[0] : code.split(/[-_]/)[0];
  return `"${code}" is not a canonical BCP-47 locale tag; rename the file to "${suggestion}.json"`;
}

// Reads langs/.do-not-translate.json: for each protected key, the exact
// substring(s) the app's own frontend code matches on at runtime (copied
// verbatim from the call sites — see the manifest's own "reason" fields).
// The value only needs to CONTAIN these substrings, not equal English —
// everything else in the string translates normally. Missing or malformed
// manifest is treated as "no protected keys" rather than a hard failure —
// the manifest is optional infrastructure, not itself a translation file.
export function readDoNotTranslate(dir) {
  try {
    const raw = readFileSync(join(dir, MANIFEST_FILE), "utf8");
    const parsed = JSON.parse(raw);
    return parsed.protected && typeof parsed.protected === "object" ? parsed.protected : {};
  } catch {
    return {};
  }
}

export function check(files, doNotTranslate = {}) {
  const errors = [];
  const warnings = [];

  const source = files[FALLBACK];
  if (!source) return { errors: [`no ${FALLBACK}.json`], warnings };
  const src = flatten(source);

  // Manifest typo guard: a protected key that doesn't exist in en.json is
  // almost certainly a stale or misspelled entry in the manifest itself.
  for (const key of Object.keys(doNotTranslate)) {
    if (!src.strings.has(key)) {
      errors.push(`${MANIFEST_FILE}: protected key "${key}" does not exist in ${FALLBACK}.json`);
    }
  }

  for (const [code, doc] of Object.entries(files)) {
    if (code === FALLBACK) continue;

    const tagError = canonicalLocaleError(code);
    if (tagError) errors.push(`${code}.json: ${tagError}`);

    const meta = doc.$meta;
    if (!meta || typeof meta !== "object") {
      errors.push(`${code}.json: missing $meta`);
    } else {
      if (meta.code !== code) {
        errors.push(`${code}.json: $meta.code is "${meta.code}", expected "${code}"`);
      }
      for (const field of ["name", "nativeName"]) {
        if (!meta[field]) errors.push(`${code}.json: $meta.${field} is missing`);
      }
    }

    const loc = flatten(doc);
    const required = requiredCategories(code);

    // Missing: a UI key blocks, an errors.* key warns. The error strings are
    // translatable but should not gate a locale's first release.
    for (const path of [...src.strings.keys(), ...src.plurals.keys()]) {
      if (loc.strings.has(path) || loc.plurals.has(path)) continue;
      const line = `${code}.json: missing key ${path}`;
      if (path.startsWith("errors.")) warnings.push(line);
      else errors.push(line);
    }

    // Extra keys always block — an unmatched path is nearly always a typo.
    for (const path of [...loc.strings.keys(), ...loc.plurals.keys()]) {
      if (src.strings.has(path) || src.plurals.has(path)) continue;
      errors.push(`${code}.json: key ${path} does not exist in ${FALLBACK}.json`);
    }

    // A key that is plural in English must be plural here, and complete.
    for (const [path, forms] of src.plurals) {
      const mine = loc.plurals.get(path);
      if (!mine) {
        if (loc.strings.has(path)) {
          errors.push(`${code}.json: ${path} must be a plural object, not a string`);
        }
        continue;
      }
      for (const category of Object.keys(mine)) {
        if (!PLURAL_CATEGORIES.has(category)) {
          errors.push(`${code}.json: ${path} has "${category}", which is not a CLDR category`);
        }
      }
      if (!("other" in mine)) {
        errors.push(`${code}.json: ${path} is missing "other", which every locale needs`);
      }
      for (const category of required) {
        if (!(category in mine)) {
          errors.push(`${code}.json: ${path} is missing "${category}", which ${code} requires`);
        }
      }
      // "count" is always implicitly available in a plural category even
      // when the English form for that category doesn't spell out {{count}}
      // literally (category selection already conveys the number) — so
      // unlike the flat-string check below, plural forms are not held to a
      // reverse "must reuse every English placeholder" rule.
      for (const [category, text] of Object.entries(mine)) {
        for (const name of placeholders(text)) {
          const known = new Set([...placeholders(forms.other ?? ""), "count"]);
          if (!known.has(name)) {
            errors.push(`${code}.json: ${path}.${category} uses {{${name}}}, absent from ${FALLBACK}.json`);
          }
        }
      }
    }

    // Placeholder names are code, not prose. A translated one renders literally.
    for (const [path, text] of loc.strings) {
      const enText = src.strings.get(path) ?? "";
      const known = placeholders(enText);
      for (const name of placeholders(text)) {
        if (!known.has(name)) {
          errors.push(`${code}.json: ${path} uses {{${name}}}, absent from ${FALLBACK}.json`);
        }
      }
      // Reverse: a translation that silently drops a {{placeholder}} the
      // English value carries loses real data (a size, a path, an id) —
      // this direction was previously unchecked entirely.
      for (const name of known) {
        if (!placeholders(text).has(name)) {
          errors.push(`${code}.json: ${path} is missing {{${name}}}, present in ${FALLBACK}.json`);
        }
      }
    }

    // Protected keys: translate freely, but the app's own frontend code
    // detects a specific failure by testing an exact substring at runtime
    // (see langs/.do-not-translate.json for the call site behind each one),
    // so a value missing that substring silently breaks the feature.
    for (const [key, spec] of Object.entries(doNotTranslate)) {
      if (src.strings.get(key) === undefined) continue; // already reported above
      const locValue = loc.strings.get(key);
      if (locValue === undefined) continue; // missing key already reported above
      const substrings = Array.isArray(spec?.substrings) ? spec.substrings : [];
      for (const substring of substrings) {
        if (!locValue.includes(substring)) {
          errors.push(
            `${code}.json: ${key} is missing required text "${substring}" — translate the rest of this ` +
              `value freely, but the application matches on this exact fragment at runtime and will break ` +
              `if it is translated or reworded` +
              (spec?.reason ? ` (${spec.reason})` : ""),
          );
        }
      }
    }
  }

  return { errors, warnings };
}

export function readDir(dir) {
  const files = {};
  for (const name of readdirSync(dir)) {
    // The do-not-translate manifest sits alongside the locale files but is
    // not itself a locale — it has no $meta, isn't a translation, and must
    // never be checked (or pulled) as one.
    if (!name.endsWith(".json") || name === MANIFEST_FILE) continue;
    const code = name.replace(/\.json$/, "");
    try {
      files[code] = JSON.parse(readFileSync(join(dir, name), "utf8"));
    } catch (e) {
      console.error(`${name}: invalid JSON — ${e.message}`);
      process.exit(1);
    }
  }
  return files;
}

// CLI. `node tools/langs/check.mjs langs`
if (process.argv[1] && process.argv[1].endsWith("check.mjs")) {
  const dir = process.argv[2] ?? "langs";
  const files = readDir(dir);
  const { errors, warnings } = check(files, readDoNotTranslate(dir));
  for (const w of warnings) console.warn(`warning: ${w}`);
  for (const e of errors) console.error(`error: ${e}`);
  if (warnings.length) console.warn(`\n${warnings.length} warning(s) — untranslated error strings, not blocking.`);
  if (errors.length) {
    console.error(`\n${errors.length} error(s).`);
    process.exit(1);
  }
  console.log(`${Object.keys(files).length} locale(s) checked, no errors.`);
}
