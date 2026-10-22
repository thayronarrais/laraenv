import { describe, expect, it } from "vitest";
// @ts-ignore — plain ESM, no types
import { check } from "../../../../../tools/langs/check.mjs";

const en = {
  $meta: { code: "en", name: "English", nativeName: "English" },
  projects: {
    title: "Projects",
    created: "Project {{name}} created",
    count: { one: "{{count}} project", other: "{{count}} projects" },
  },
  errors: { openFailed: "open {{path}} failed" },
};

const ok = (files: Record<string, unknown>) => check(files);

describe("key parity", () => {
  it("passes an identical locale", () => {
    const ru = structuredClone(en);
    (ru as any).$meta = { code: "ru", name: "Russian", nativeName: "Русский" };
    (ru as any).projects.count = { one: "1", few: "2", many: "5", other: "2" };
    expect(ok({ en, ru }).errors).toEqual([]);
  });

  it("fails on a missing UI key", () => {
    const ru: any = structuredClone(en);
    ru.$meta = { code: "ru", name: "R", nativeName: "R" };
    ru.projects.count = { one: "1", few: "2", many: "5", other: "2" };
    delete ru.projects.title;
    expect(ok({ en, ru }).errors.join("\n")).toContain("projects.title");
  });

  it("only warns on a missing errors.* key", () => {
    const ru: any = structuredClone(en);
    ru.$meta = { code: "ru", name: "R", nativeName: "R" };
    ru.projects.count = { one: "1", few: "2", many: "5", other: "2" };
    delete ru.errors.openFailed;
    const res = ok({ en, ru });
    expect(res.errors).toEqual([]);
    expect(res.warnings.join("\n")).toContain("errors.openFailed");
  });

  it("fails on an extra key, which is almost always a typo", () => {
    const ru: any = structuredClone(en);
    ru.$meta = { code: "ru", name: "R", nativeName: "R" };
    ru.projects.count = { one: "1", few: "2", many: "5", other: "2" };
    ru.projects.titel = "Проекты";
    expect(ok({ en, ru }).errors.join("\n")).toContain("projects.titel");
  });
});

describe("plural completeness", () => {
  it("fails Russian missing few and many", () => {
    const ru: any = structuredClone(en);
    ru.$meta = { code: "ru", name: "R", nativeName: "R" };
    ru.projects.count = { one: "1 проект", other: "2 проекта" };
    const msg = ok({ en, ru }).errors.join("\n");
    expect(msg).toContain("few");
    expect(msg).toContain("many");
  });

  it("fails a plural entry with no other", () => {
    const bad: any = structuredClone(en);
    bad.$meta = { code: "pt-BR", name: "P", nativeName: "P" };
    bad.projects.count = { one: "1 projeto" };
    expect(ok({ en, "pt-BR": bad }).errors.join("\n")).toContain("other");
  });
});

describe("interpolation", () => {
  it("fails a translated placeholder name", () => {
    const bad: any = structuredClone(en);
    bad.$meta = { code: "pt-BR", name: "P", nativeName: "P" };
    bad.projects.created = "Projeto {{nome}} criado";
    bad.projects.count = { one: "1", other: "2" };
    expect(ok({ en, "pt-BR": bad }).errors.join("\n")).toContain("{{nome}}");
  });
});

describe("$meta", () => {
  it("fails when code does not match the filename", () => {
    const bad: any = structuredClone(en);
    bad.$meta = { code: "pt", name: "P", nativeName: "P" };
    bad.projects.count = { one: "1", other: "2" };
    expect(ok({ en, "pt-BR": bad }).errors.join("\n")).toContain("$meta.code");
  });
});

describe("$meta.needsNativeReview", () => {
  it("passes when absent", () => {
    const ru: any = structuredClone(en);
    ru.$meta = { code: "ru", name: "R", nativeName: "R" };
    ru.projects.count = { one: "1", few: "2", many: "5", other: "2" };
    expect(ok({ en, ru }).errors).toEqual([]);
  });

  it("passes a real boolean", () => {
    const ru: any = structuredClone(en);
    ru.$meta = { code: "ru", name: "R", nativeName: "R", needsNativeReview: true };
    ru.projects.count = { one: "1", few: "2", many: "5", other: "2" };
    expect(ok({ en, ru }).errors).toEqual([]);
  });

  it("fails a non-boolean value", () => {
    const ru: any = structuredClone(en);
    ru.$meta = { code: "ru", name: "R", nativeName: "R", needsNativeReview: "true" };
    ru.projects.count = { one: "1", few: "2", many: "5", other: "2" };
    expect(ok({ en, ru }).errors.join("\n")).toContain("$meta.needsNativeReview must be a boolean");
  });
});

describe("canonical locale tags", () => {
  it("hard-fails a POSIX-style tag instead of silently falling back to {one, other}", () => {
    const bad: any = structuredClone(en);
    bad.$meta = { code: "ru_RU", name: "R", nativeName: "R" };
    // Only {one, other} — exactly the broken shape that previously slipped
    // through because Intl.PluralRules("ru_RU") threw and the fallback
    // Set(["one", "other"]) made this look complete.
    bad.projects.count = { one: "1", other: "2" };
    const msg = ok({ en, ru_RU: bad }).errors.join("\n");
    expect(msg).toContain("ru_RU");
    expect(msg).toContain('rename the file to "ru.json"');
  });

  it("passes a real canonical tag", () => {
    const ru: any = structuredClone(en);
    ru.$meta = { code: "ru", name: "R", nativeName: "R" };
    ru.projects.count = { one: "1", few: "2", many: "5", other: "2" };
    expect(ok({ en, ru }).errors).toEqual([]);
  });
});

// pt-BR needs "one", "many" and "other" (CLDR), so every fixture below gives
// a complete set purely to keep plural-completeness noise out of assertions
// unrelated to what each test actually exercises.
const ptBRCount = { one: "1", many: "1000", other: "2" };

describe("reverse placeholder parity", () => {
  it("fails a translation that drops an English placeholder", () => {
    const bad: any = structuredClone(en);
    bad.$meta = { code: "pt-BR", name: "P", nativeName: "P" };
    bad.projects.created = "Projeto criado"; // dropped {{name}}
    bad.projects.count = ptBRCount;
    const msg = ok({ en, "pt-BR": bad }).errors.join("\n");
    expect(msg).toContain("projects.created");
    expect(msg).toContain("{{name}}");
  });

  it("does not require a plural category to repeat {{count}}", () => {
    // Plural category selection already conveys the number, so a form that
    // doesn't spell out {{count}} literally is not a parity violation —
    // unlike a flat string, which has no other way to carry the value.
    const good: any = structuredClone(en);
    good.$meta = { code: "pt-BR", name: "P", nativeName: "P" };
    good.projects.count = { one: "um projeto", many: "mil projetos", other: "vários projetos" };
    expect(ok({ en, "pt-BR": good }).errors).toEqual([]);
  });
});

describe("do-not-translate manifest", () => {
  const enProtected = {
    ...en,
    errors: { ...en.errors, sentinel: "file too large: {{size}} bytes (limit {{limit}})" },
  };
  const manifest = { "errors.sentinel": { substrings: ["file too large"], reason: "test fixture" } };

  it("rejects an English source edit that drops a runtime fragment", () => {
    expect(check({ en: enProtected }, manifest).errors).toEqual([]);
    const bad = structuredClone(enProtected);
    bad.errors.sentinel = "huge file: {{size}} bytes (limit {{limit}})";
    const errors = check({ en: bad }, manifest).errors.join("\n");
    expect(errors).toContain("en.json: errors.sentinel");
    expect(errors).toContain('missing required text "file too large"');
  });

  it("fails a locale whose value is missing the required substring", () => {
    const bad: any = structuredClone(enProtected);
    bad.$meta = { code: "pt-BR", name: "P", nativeName: "P" };
    bad.projects.count = ptBRCount;
    bad.errors.sentinel = "arquivo muito grande: {{size}} bytes (limite {{limit}})";
    const msg = check({ en: enProtected, "pt-BR": bad }, manifest).errors.join("\n");
    expect(msg).toContain("errors.sentinel");
    expect(msg).toContain("file too large");
    expect(msg).toContain("test fixture");
  });

  it("passes a locale that leaves the protected key untranslated", () => {
    const good: any = structuredClone(enProtected);
    good.$meta = { code: "pt-BR", name: "P", nativeName: "P" };
    good.projects.count = ptBRCount;
    expect(check({ en: enProtected, "pt-BR": good }, manifest).errors).toEqual([]);
  });

  it("passes a locale that keeps the substring inside an otherwise-translated sentence", () => {
    const good: any = structuredClone(enProtected);
    good.$meta = { code: "pt-BR", name: "P", nativeName: "P" };
    good.projects.count = ptBRCount;
    // Coherent Portuguese around the frozen English fragment — the whole
    // point of a substring rule instead of a whole-value-equals-English one.
    good.errors.sentinel = "file too large: o arquivo tem {{size}} bytes, mas o limite é {{limit}}";
    expect(check({ en: enProtected, "pt-BR": good }, manifest).errors).toEqual([]);
  });

  it("flags a manifest key that does not exist in en.json", () => {
    const good: any = structuredClone(en);
    good.$meta = { code: "pt-BR", name: "P", nativeName: "P" };
    good.projects.count = { one: "1", other: "2" };
    const msg = check(
      { en, "pt-BR": good },
      { "errors.doesNotExist": { substrings: ["x"] } },
    ).errors.join("\n");
    expect(msg).toContain("errors.doesNotExist");
  });
});
