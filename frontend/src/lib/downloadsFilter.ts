// Filtering logic for the Libs / Tools page (Downloads.tsx). Kept separate
// from the component so it's testable without mounting React or mocking the
// Wails bindings.

export const ALL_CATEGORIES = "__all__";

// The minimal shape filtering needs — a subset of Downloads.tsx's Source.
export type FilterableSource = {
  kind: string;
  version: string;
  notes?: string;
};

// matchesQuery reports whether a source matches a free-text search: the
// kind's display label, its raw kind id, its group's label, its version
// string, or its notes. The group is searchable so typing "server" finds
// Nginx, Apache and Caddy without knowing their names — the same reason the
// chips exist. An empty/whitespace query always matches.
export function matchesQuery(
  s: FilterableSource,
  kindLabel: string,
  query: string,
  groupLabel = "",
): boolean {
  const q = query.trim().toLowerCase();
  if (!q) return true;
  return (
    kindLabel.toLowerCase().includes(q) ||
    s.kind.toLowerCase().includes(q) ||
    groupLabel.toLowerCase().includes(q) ||
    s.version.toLowerCase().includes(q) ||
    (s.notes ?? "").toLowerCase().includes(q)
  );
}

// filterSources narrows `sources` down to the ones matching both the selected
// group (a group id, or ALL_CATEGORIES for no filter) and the free-text
// query. Order is preserved.
//
// The filter is by group rather than by kind on purpose: a chip per kind
// would just restate the section headings the user is already scrolling
// past. "Server", "Database", "Language" answer what someone is actually
// looking for.
export function filterSources<T extends FilterableSource>(
  sources: T[],
  kindLabels: Record<string, string>,
  group: string,
  query: string,
  groupForKind: (kind: string) => string = () => "",
  groupLabels: Record<string, string> = {},
): T[] {
  return sources.filter((s) => {
    const g = groupForKind(s.kind);
    if (group !== ALL_CATEGORIES && g !== group) return false;
    return matchesQuery(s, kindLabels[s.kind] ?? s.kind, query, groupLabels[g] ?? "");
  });
}
