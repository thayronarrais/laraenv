import { describe, expect, it } from "vitest";
import { ALL_CATEGORIES, filterSources, matchesQuery } from "../downloadsFilter";
import { GROUP_LABELS, groupForKind } from "../installKinds";

const KIND_LABELS: Record<string, string> = {
  php: "PHP",
  nginx: "Nginx",
  postgres: "PostgreSQL",
};

const sources = [
  { kind: "php", version: "8.6.0beta3", notes: "Prerelease build — see release notes before installing." },
  { kind: "php", version: "8.5.10" },
  { kind: "php", version: "7.2.34" },
  { kind: "nginx", version: "1.27.3" },
  { kind: "postgres", version: "18.3", notes: "Download manually and save it to the cache folder." },
];

describe("matchesQuery", () => {
  it("matches everything when the query is empty or whitespace", () => {
    expect(matchesQuery(sources[0], "PHP", "")).toBe(true);
    expect(matchesQuery(sources[0], "PHP", "   ")).toBe(true);
  });

  it("matches on the kind's display label, case-insensitively", () => {
    expect(matchesQuery(sources[0], "PHP", "php")).toBe(true);
    expect(matchesQuery(sources[0], "PHP", "PHP")).toBe(true);
  });

  it("matches on the raw kind id even when the label differs", () => {
    expect(matchesQuery(sources[4], "PostgreSQL", "postgres")).toBe(true);
  });

  it("matches on the version string", () => {
    expect(matchesQuery(sources[1], "PHP", "8.5")).toBe(true);
    expect(matchesQuery(sources[2], "PHP", "7.2")).toBe(true);
    expect(matchesQuery(sources[1], "PHP", "9.9")).toBe(false);
  });

  it("matches on notes text", () => {
    expect(matchesQuery(sources[4], "PostgreSQL", "manually")).toBe(true);
    expect(matchesQuery(sources[3], "Nginx", "manually")).toBe(false);
  });
});

describe("filterSources", () => {
  it("returns everything for ALL_CATEGORIES and an empty query", () => {
    expect(filterSources(sources, KIND_LABELS, ALL_CATEGORIES, "", groupForKind, GROUP_LABELS)).toHaveLength(5);
  });

  it("narrows to a single group", () => {
    const out = filterSources(sources, KIND_LABELS, "language", "", groupForKind, GROUP_LABELS);
    expect(out).toHaveLength(3);
    expect(out.every((s) => s.kind === "php")).toBe(true);
  });

  it("combines category and search", () => {
    const out = filterSources(sources, KIND_LABELS, "language", "8.5", groupForKind, GROUP_LABELS);
    expect(out.map((s) => s.version)).toEqual(["8.5.10"]);
  });

  it("finds an installed-beta entry by searching its notes", () => {
    const out = filterSources(sources, KIND_LABELS, ALL_CATEGORIES, "release notes", groupForKind, GROUP_LABELS);
    expect(out.map((s) => s.version)).toEqual(["8.6.0beta3"]);
  });

  it("preserves input order", () => {
    const out = filterSources(sources, KIND_LABELS, "language", "", groupForKind, GROUP_LABELS);
    expect(out.map((s) => s.version)).toEqual(["8.6.0beta3", "8.5.10", "7.2.34"]);
  });

  it("returns an empty list when nothing matches", () => {
    expect(filterSources(sources, KIND_LABELS, ALL_CATEGORIES, "no-such-thing", groupForKind, GROUP_LABELS)).toHaveLength(0);
  });
});

describe("grouping", () => {
  // The chips exist so someone can ask "what serves my site" without
  // knowing whether this project uses Nginx, Apache or Caddy.
  it("puts every web server in one group", () => {
    expect(groupForKind("nginx")).toBe("server");
    expect(groupForKind("apache")).toBe("server");
    expect(groupForKind("caddy")).toBe("server");
  });

  it("groups the data stores together, Redis included", () => {
    for (const k of ["mysql", "postgres", "pgvector", "redis"]) {
      expect(groupForKind(k)).toBe("database");
    }
  });

  it("groups the runtimes as languages", () => {
    for (const k of ["php", "python", "nodejs"]) {
      expect(groupForKind(k)).toBe("language");
    }
  });

  // A kind nobody assigned still has to appear somewhere. Caddy was missing
  // from a map like this one and vanished from the page for a day.
  it("falls back to extras rather than dropping an unknown kind", () => {
    expect(groupForKind("something-new")).toBe("extras");
  });

  it("finds a whole group by its label", () => {
    const out = filterSources(sources, KIND_LABELS, ALL_CATEGORIES, "server", groupForKind, GROUP_LABELS);
    expect(out.map((s) => s.kind)).toEqual(["nginx"]);
  });
});
