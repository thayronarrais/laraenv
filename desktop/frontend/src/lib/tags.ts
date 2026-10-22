// addTag appends `raw` to `tags` after trimming and deduping case-insensitively.
export function addTag(tags: string[], raw: string): string[] {
  const t = raw.trim();
  if (!t) return tags;
  const lower = t.toLowerCase();
  if (tags.some((x) => x.toLowerCase() === lower)) return tags;
  return [...tags, t];
}

// commitDraft folds any uncommitted draft text into the tag list before save.
export function commitDraft(tags: string[], draft: string): string[] {
  return draft.trim() ? addTag(tags, draft) : tags;
}

// canonicalTag returns the existing spelling when `raw` only differs by case,
// so "fullwear" reuses "FullWear" instead of creating a look-alike tag.
export function canonicalTag(existing: string[], raw: string): string {
  const t = raw.trim();
  const lower = t.toLowerCase();
  return existing.find((x) => x.trim().toLowerCase() === lower)?.trim() ?? t;
}

// suggestTags proposes existing tags for the current draft: every unselected
// tag when the draft is empty; otherwise case-insensitive prefix matches first,
// then substring matches. Already-selected tags are never suggested.
export function suggestTags(existing: string[], selected: string[], draft: string, limit = 8): string[] {
  const needle = draft.trim().toLowerCase();
  const taken = new Set(selected.map((x) => x.trim().toLowerCase()));
  const pool: string[] = [];
  const seen = new Set<string>();
  for (const raw of existing) {
    const t = raw.trim();
    const lower = t.toLowerCase();
    if (!t || taken.has(lower) || seen.has(lower)) continue;
    seen.add(lower);
    pool.push(t);
  }
  const prefix = pool.filter((t) => t.toLowerCase().startsWith(needle));
  const inner = needle
    ? pool.filter((t) => !t.toLowerCase().startsWith(needle) && t.toLowerCase().includes(needle))
    : [];
  return [...prefix, ...inner].slice(0, limit);
}
