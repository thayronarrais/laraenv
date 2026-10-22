// The frontend half of the translation pair. internal/i18n implements the
// same semantics in Go over the same JSON files; both take plural categories
// from CLDR, so a string renders identically wherever it is produced.
import { createContext, useCallback, useContext, useEffect, useMemo, useState } from "react";
// @ts-ignore
import { GetLanguage, SetLanguage } from "../../wailsjs/go/main/App";
// GetLanguageOverrides is read through the namespace import, not a named
// one: a test mocking this module without it (most of them, since disk
// overrides are the rare case) must get `undefined` back, not a hard
// module-resolution failure — see the guarded call in I18nProvider below.
// @ts-ignore
import * as AppApi from "../../wailsjs/go/main/App";
// @ts-ignore
import { EventsOn } from "../../wailsjs/runtime";

export type Meta = {
  code: string;
  name: string;
  nativeName: string;
  // Mirrors internal/i18n.Meta.NeedsNativeReview: true when the catalogue
  // was machine-translated or otherwise not yet checked by a native
  // speaker. Absent or false means reviewed.
  needsNativeReview: boolean;
};
export type Vars = Record<string, string | number>;
export type Catalogue = Record<string, unknown>;

// The disk-override shapes internal/i18n.OverrideResult/RejectedKey produce
// over the bridge (see App.GetLanguageOverrides in app.go).
export type RejectedKey = { key: string; reason: string };
export type OverrideResult = {
  locale: string;
  applied: number;
  rejected: RejectedKey[];
  // Set instead of applied/rejected when the file itself couldn't be turned
  // into a catalogue at all (invalid JSON — a trailing comma is the everyday
  // case — or valid JSON that isn't an object). Names why, so a translator
  // who edits and sees nothing change can find out.
  unreadable: string;
};
type OverridesPayload = {
  dir: string;
  catalogues: Record<string, Catalogue>;
  results: OverrideResult[];
};

export const FALLBACK = "en";

// Every langs/*.json, bundled at build time. The glob reaches outside the Vite
// root, which vite.config.ts allows via server.fs.allow.
const modules = import.meta.glob("../../../langs/*.json", { eager: true }) as Record<
  string,
  { default: Catalogue }
>;

export const catalogues: Record<string, Catalogue> = Object.fromEntries(
  Object.entries(modules).map(([path, mod]) => [
    path.split("/").pop()!.replace(/\.json$/, ""),
    mod.default,
  ]),
);

// localesFrom reads every catalogue's $meta into the shape the selector
// renders, English first and the rest alphabetical — the same order
// internal/i18n's Set.Locales() uses, so the two never disagree. Exported so
// I18nProvider can recompute it once disk overrides (possibly a whole new
// locale nobody shipped) are merged in, not just from what was bundled.
export function localesFrom(cats: Record<string, Catalogue>): Meta[] {
  return Object.entries(cats)
    .map(([code, cat]) => {
      const meta = (cat.$meta ?? {}) as Partial<Meta>;
      return {
        code,
        name: meta.name ?? code,
        nativeName: meta.nativeName ?? code,
        needsNativeReview: meta.needsNativeReview === true,
      };
    })
    .sort((a, b) => (a.code === FALLBACK ? -1 : b.code === FALLBACK ? 1 : a.code.localeCompare(b.code)));
}

export const locales: Meta[] = localesFrom(catalogues);

const PLURAL_CATEGORIES = new Set(["zero", "one", "two", "few", "many", "other"]);

// isPluralEntry mirrors tools/langs/check.mjs and internal/i18n's Go
// counterpart: a non-empty object whose every key is a CLDR plural category
// is a single translatable unit, not a namespace to keep walking into.
function isPluralEntry(node: unknown): node is Record<string, string> {
  if (typeof node !== "object" || node === null || Array.isArray(node)) return false;
  const keys = Object.keys(node);
  return keys.length > 0 && keys.every((k) => PLURAL_CATEGORIES.has(k));
}

// flattenLeaves walks doc and records every leaf under its dot-joined path —
// a plain string, or a whole plural-forms object treated as one unit. Same
// granularity as internal/i18n's flattenLeaves, so "a key" means the same
// thing on both sides of the bridge.
function flattenLeaves(doc: Catalogue, prefix = "", out: Record<string, unknown> = {}): Record<string, unknown> {
  for (const [k, v] of Object.entries(doc)) {
    if (k === "$meta") continue;
    const path = prefix ? `${prefix}.${k}` : k;
    if (typeof v === "object" && v !== null && !Array.isArray(v) && !isPluralEntry(v)) {
      flattenLeaves(v as Catalogue, path, out);
    } else {
      out[path] = v;
    }
  }
  return out;
}

// setLeaf writes value at key's dot-separated path into doc, creating
// intermediate objects as needed.
function setLeaf(doc: Record<string, unknown>, key: string, value: unknown) {
  const parts = key.split(".");
  let node = doc;
  parts.forEach((part, i) => {
    if (i === parts.length - 1) {
      node[part] = value;
      return;
    }
    const next = node[part];
    if (typeof next !== "object" || next === null || Array.isArray(next)) {
      node[part] = {};
    }
    node = node[part] as Record<string, unknown>;
  });
}

// mergeOverrides overlays disk overrides (bridge-provided, already checked
// against the do-not-translate manifest on the Go side) onto the catalogues
// bundled at build time. A partial fragment changes only the keys it names;
// a locale code absent from `base` becomes a brand-new selectable locale
// built from just what's on disk (the frontend's Vite glob only sees files
// that existed when it was built, so this is the only way it ever learns
// about one). Pure and exported so the merge itself is unit-testable without
// the bridge or React.
export function mergeOverrides(
  base: Record<string, Catalogue>,
  overrides: Record<string, Catalogue>,
): Record<string, Catalogue> {
  if (!overrides || Object.keys(overrides).length === 0) return base;
  const merged: Record<string, Catalogue> = { ...base };
  for (const [code, overrideDoc] of Object.entries(overrides)) {
    const target: Record<string, unknown> = JSON.parse(JSON.stringify(merged[code] ?? {}));
    const meta = (overrideDoc as { $meta?: object }).$meta;
    if (meta) target.$meta = { ...(target.$meta as object | undefined), ...meta };
    for (const [key, value] of Object.entries(flattenLeaves(overrideDoc))) {
      setLeaf(target, key, value);
    }
    merged[code] = target as Catalogue;
  }
  return merged;
}

function lookup(cat: Catalogue | undefined, key: string): unknown {
  if (!cat) return undefined;
  let node: unknown = cat;
  for (const part of key.split(".")) {
    if (typeof node !== "object" || node === null) return undefined;
    node = (node as Record<string, unknown>)[part];
    if (node === undefined) return undefined;
  }
  return node;
}

// A plain string is itself; an object is a plural entry and needs a count to
// pick a category.
function resolve(node: unknown, lang: string, vars?: Vars): string {
  if (typeof node === "string") return node;
  if (typeof node !== "object" || node === null) return "";
  const forms = node as Record<string, string>;
  let category = "other";
  const count = vars?.count;
  if (typeof count === "number") {
    try {
      category = new Intl.PluralRules(lang).select(count);
    } catch {
      category = new Intl.PluralRules(FALLBACK).select(count);
    }
  }
  // A translator may have omitted a category; "other" always exists (CI
  // enforces it), and rendering that beats rendering nothing.
  return forms[category] ?? forms.other ?? "";
}

// {{name}} → vars.name. {{count}} is formatted for the locale — 1234 is
// "1,234" in English and "1.234" in Portuguese — because a count is prose.
// Every other variable is inserted verbatim: a port, a PID or a version number
// must never grow a thousands separator. An unsupplied placeholder is left in
// place: visible in the UI, and far easier to trace than an empty gap.
function interpolate(text: string, lang: string, vars?: Vars): string {
  if (!vars || !text.includes("{{")) return text;
  return text.replace(/\{\{(\w+)\}\}/g, (whole, name: string) => {
    const value = vars[name];
    if (value === undefined) return whole;
    if (name === "count" && typeof value === "number") {
      return new Intl.NumberFormat(lang).format(value);
    }
    return String(value);
  });
}

// The pure core: no React, no bridge. Everything below is a wrapper on this,
// and the tests drive it directly.
export function translate(
  all: Record<string, Catalogue>,
  lang: string,
  key: string,
  vars?: Vars,
): string {
  let node = lookup(all[lang], key);
  let resolved = lang;
  if (node === undefined) {
    node = lookup(all[FALLBACK], key);
    resolved = FALLBACK;
    if (node === undefined) {
      if (import.meta.env.DEV) console.warn(`[i18n] missing key: ${key}`);
      return key;
    }
  }
  return interpolate(resolve(node, resolved, vars), resolved, vars);
}

// The app's only relative-time formatter — also pure, also exported, for the
// same reason as translate(): every later batch reaches for this instead of
// hand-rolling its own, so its two thresholds need to be locked down by
// tests, not just eyeballed.
//
// - Under 60 seconds: Intl.RelativeTimeFormat would say "45 seconds ago",
//   which reads as false precision for something that just happened.
//   rtf.format(0, "second") gives "now" (English) / "agora" (Portuguese)
//   under numeric: "auto", with no catalogue key needed.
// - Beyond 7 days: counting in ever-larger relative units forever ("3 months
//   ago") is worse than just naming the date, so we hand off to the same
//   Intl.DateTimeFormat path formatDate uses.
export function formatRelative(
  lang: string,
  d: Date | number,
  opts?: Intl.DateTimeFormatOptions,
): string {
  const seconds = (new Date(d).getTime() - Date.now()) / 1000;
  const rtf = new Intl.RelativeTimeFormat(lang, { numeric: "auto" });

  if (Math.abs(seconds) < 60) return rtf.format(0, "second");

  const minutes = seconds / 60;
  if (Math.abs(minutes) < 60) return rtf.format(Math.round(minutes), "minute");

  const hours = minutes / 60;
  if (Math.abs(hours) < 24) return rtf.format(Math.round(hours), "hour");

  const days = hours / 24;
  if (Math.abs(days) <= 7) return rtf.format(Math.round(days), "day");

  return new Intl.DateTimeFormat(lang, opts).format(new Date(d));
}

type Ctx = {
  lang: string;
  locales: Meta[];
  setLang: (code: string) => Promise<void>;
  t: (key: string, vars?: Vars) => string;
  formatDate: (d: Date | number, opts?: Intl.DateTimeFormatOptions) => string;
  formatNumber: (n: number, opts?: Intl.NumberFormatOptions) => string;
  formatRelative: (d: Date | number) => string;
  // Disk-override status for the Settings screen (see LanguageSelector):
  // overrideDir is the langs/ folder's path, "" when it doesn't exist or
  // contributed nothing; overrideResults names what each contributing
  // locale applied and rejected.
  overrideDir: string;
  overrideResults: OverrideResult[];
};

const I18nContext = createContext<Ctx | null>(null);

export function I18nProvider({ children }: { children: React.ReactNode }) {
  const [lang, setLangState] = useState(FALLBACK);
  const [overrides, setOverrides] = useState<Record<string, Catalogue>>({});
  const [overrideDir, setOverrideDir] = useState("");
  const [overrideResults, setOverrideResults] = useState<OverrideResult[]>([]);

  useEffect(() => {
    GetLanguage()
      .then((code: string) => code && setLangState(code))
      .catch(() => { /* pre-startup or a non-main window: English until told otherwise */ });
    // Emitted by this process on SetLanguage, and by every other process when
    // its config watcher sees the file change. Detached terminal windows and
    // the command bar are separate processes, so the file is what unites them.
    return EventsOn("language:changed", (code: string) => setLangState(code));
  }, []);

  useEffect(() => {
    // Loaded once at startup (see internal/i18n.MergeOverrides — rule 2: no
    // watcher, restarting the app is the reload). Older bridges and most
    // test mocks don't define this call at all, and — under Vitest's module
    // mocking — even reading the property off the mocked module throws
    // synchronously rather than yielding undefined (its "did you forget to
    // return it from vi.mock?" guard), so the read itself is wrapped in
    // try/catch rather than just checked for undefined. This is exactly the
    // "bridge returns nothing" case that must not break the common
    // no-overrides path.
    let fn: (() => Promise<OverridesPayload>) | undefined;
    try {
      fn = (AppApi as { GetLanguageOverrides?: () => Promise<OverridesPayload> }).GetLanguageOverrides;
    } catch {
      fn = undefined;
    }
    if (!fn) return;
    fn()
      .then((payload) => {
        if (!payload) return;
        setOverrides(payload.catalogues ?? {});
        setOverrideDir(payload.dir ?? "");
        // Go marshals a nil slice as JSON `null`, not `[]` — normalise both
        // the top-level array and each entry's `rejected` here, once, so
        // every consumer (LanguageSelector today, anything reading
        // overrideResults tomorrow) can index and iterate without knowing
        // that an older bridge or a clean, zero-rejection override can hand
        // back null in place of an array.
        setOverrideResults(
          (payload.results ?? []).map((r) => ({ ...r, rejected: r.rejected ?? [] })),
        );
      })
      .catch(() => { /* pre-startup or a non-main window: no overrides until told otherwise */ });
  }, []);

  const setLang = useCallback(async (code: string) => {
    setLangState(code);
    await SetLanguage(code);
  }, []);

  const mergedCatalogues = useMemo(() => mergeOverrides(catalogues, overrides), [overrides]);
  const mergedLocales = useMemo(() => localesFrom(mergedCatalogues), [mergedCatalogues]);

  const value = useMemo<Ctx>(
    () => ({
      lang,
      locales: mergedLocales,
      setLang,
      t: (key, vars) => translate(mergedCatalogues, lang, key, vars),
      formatDate: (d, opts) => new Intl.DateTimeFormat(lang, opts).format(new Date(d)),
      formatNumber: (n, opts) => new Intl.NumberFormat(lang, opts).format(n),
      formatRelative: (d) => formatRelative(lang, d),
      overrideDir,
      overrideResults,
    }),
    [lang, setLang, mergedCatalogues, mergedLocales, overrideDir, overrideResults],
  );

  return <I18nContext.Provider value={value}>{children}</I18nContext.Provider>;
}

export function useI18n(): Ctx {
  const ctx = useContext(I18nContext);
  if (!ctx) throw new Error("useI18n outside I18nProvider");
  return ctx;
}

export function useT() {
  return useI18n().t;
}
