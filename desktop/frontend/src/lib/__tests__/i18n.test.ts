import { describe, expect, it } from "vitest";
import { translate, formatRelative, mergeOverrides, localesFrom, type Catalogue } from "../i18n";

const en: Catalogue = {
  $meta: { code: "en", name: "English", nativeName: "English" },
  projects: {
    title: "Projects",
    created: "Project {{name}} created",
    count: { one: "{{count}} project", other: "{{count}} projects" },
  },
  only: { inEnglish: "Fallback me" },
  server: { port: "Serving on {{port}}" },
};

const ru: Catalogue = {
  $meta: { code: "ru", name: "Russian", nativeName: "Русский" },
  projects: {
    title: "Проекты",
    created: "Проект {{name}} создан",
    count: {
      one: "{{count}} проект",
      few: "{{count}} проекта",
      many: "{{count}} проектов",
      other: "{{count}} проекта",
    },
  },
};

const all = { en, ru };
const t = (lang: string, key: string, vars?: Record<string, string | number>) =>
  translate(all, lang, key, vars);

describe("lookup and fallback", () => {
  it("returns the value for the active locale", () => {
    expect(t("ru", "projects.title")).toBe("Проекты");
  });
  it("falls back to English for a key the locale lacks", () => {
    expect(t("ru", "only.inEnglish")).toBe("Fallback me");
  });
  it("falls back to English for an unknown locale", () => {
    expect(t("de", "projects.title")).toBe("Projects");
  });
  it("returns the key path when nothing has it, never a blank", () => {
    expect(t("en", "nope.missing")).toBe("nope.missing");
  });
});

describe("interpolation", () => {
  it("substitutes named variables", () => {
    expect(t("en", "projects.created", { name: "shop" })).toBe("Project shop created");
  });
  it("leaves an unsupplied placeholder visible rather than blank", () => {
    expect(t("en", "projects.created")).toBe("Project {{name}} created");
  });
});

describe("plurals", () => {
  it("picks English one/other", () => {
    expect(t("en", "projects.count", { count: 1 })).toBe("1 project");
    expect(t("en", "projects.count", { count: 0 })).toBe("0 projects");
  });
  it("picks all three Russian forms", () => {
    expect(t("ru", "projects.count", { count: 1 })).toBe("1 проект");
    expect(t("ru", "projects.count", { count: 2 })).toBe("2 проекта");
    expect(t("ru", "projects.count", { count: 5 })).toBe("5 проектов");
    expect(t("ru", "projects.count", { count: 21 })).toBe("21 проект");
    expect(t("ru", "projects.count", { count: 11 })).toBe("11 проектов");
  });
  it("formats count with locale grouping in English", () => {
    expect(t("en", "projects.count", { count: 1234 })).toBe("1,234 projects");
  });
  it("formats count with locale grouping in Russian", () => {
    // Russian uses a non-breaking space (U+00A0) as thousands separator
    expect(t("ru", "projects.count", { count: 1234 })).toBe("1 234 проекта");
  });
});

describe("number formatting", () => {
  it("formats {{count}} with locale grouping", () => {
    expect(t("en", "projects.count", { count: 1234 })).toBe("1,234 projects");
  });
  it("does not format non-count numeric variables", () => {
    expect(t("en", "server.port", { port: 3000 })).toBe("Serving on 3000");
  });
});

describe("formatRelative", () => {
  it("says 'now' rather than false precision under 60 seconds", () => {
    expect(formatRelative("en", Date.now() - 45_000)).toBe("now");
    expect(formatRelative("pt-BR", Date.now() - 45_000)).toBe("agora");
  });
  it("uses relative units for a few days ago", () => {
    const threeDaysAgo = Date.now() - 3 * 24 * 60 * 60 * 1000;
    expect(formatRelative("en", threeDaysAgo)).toBe("3 days ago");
  });
  it("falls back to an absolute date beyond 7 days, not an ever-growing relative unit", () => {
    const d = new Date(Date.now() - 30 * 24 * 60 * 60 * 1000);
    expect(formatRelative("en", d)).toBe(new Intl.DateTimeFormat("en").format(d));
    expect(formatRelative("en", d)).not.toMatch(/ago/);
  });
});

// The frontend's half of the disk-override overlay: internal/i18n.
// MergeOverrides already checked these fragments against the do-not-translate
// manifest and stripped anything that failed, so mergeOverrides itself only
// needs to prove the overlay is key-level and additive, matching the Go side.
describe("mergeOverrides", () => {
  it("a partial fragment changes only its own keys", () => {
    const merged = mergeOverrides(all, { ru: { projects: { title: "Проекты (правка)" } } });
    expect(translate(merged, "ru", "projects.title")).toBe("Проекты (правка)");
    // The sibling key, and the plural entry, are untouched.
    expect(translate(merged, "ru", "projects.created", { name: "loja" })).toBe("Проект loja создан");
    expect(translate(merged, "ru", "projects.count", { count: 2 })).toBe("2 проекта");
    // The base object passed in is never mutated.
    expect(translate(all, "ru", "projects.title")).toBe("Проекты");
  });

  it("a locale code absent from base becomes a new selectable locale", () => {
    const merged = mergeOverrides(all, {
      de: {
        $meta: { code: "de", name: "German", nativeName: "Deutsch" },
        projects: { title: "Projekte" },
      },
    });
    expect(localesFrom(merged).map((l) => l.code)).toContain("de");
    expect(translate(merged, "de", "projects.title")).toBe("Projekte");
    // A key the disk-only fragment never supplied still falls back to
    // English, exactly like an incomplete shipped locale would.
    expect(translate(merged, "de", "projects.created", { name: "shop" })).toBe("Project shop created");
  });

  it("no overrides at all is a no-op, not an error", () => {
    expect(mergeOverrides(all, {})).toBe(all);
  });
});
