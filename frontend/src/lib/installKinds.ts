// Human-readable labels for installer.Kind values (Go: internal/installer/catalog.go).
// Catalog kinds are internal identifiers ("nodejs", "php", "cmder", …), not
// display names — anywhere a kind is shown to the user, map it through this
// table first. Shared by Downloads.tsx (source cards, install/enable toasts)
// and Projects.tsx (the generic install:installed toast).
export const KIND_LABELS: Record<string, string> = {
  nginx: "Nginx",
  apache: "Apache",
  caddy: "Caddy",
  mysql: "MySQL",
  postgres: "PostgreSQL",
  pgvector: "pgvector",
  mailpit: "Mailpit",
  redis: "Redis",
  php: "PHP",
  python: "Python",
  nodejs: "Node.js",
  cmder: "Cmder",
  composer: "Composer",
};

// Groups are how the Libs / Tools page lets the user narrow the list. They
// answer "what is this for", which is the question someone browsing actually
// has — filtering by kind would just repeat the headings they are already
// looking at.
//
// Redis sits under Database rather than a cache of its own: from this page it
// is one more data store you install and start, and a group holding a single
// entry is a filter that never helps.
export const GROUP_LABELS: Record<string, string> = {
  server: "Server",
  database: "Database",
  mail: "Mail",
  language: "Language",
  extras: "Extras",
};

// Catalogue keys for the same groups, for call sites that render to a user
// (Downloads.tsx) rather than compare/test against a fixed English fixture
// (downloadsFilter.test.ts, which imports GROUP_LABELS directly).
export const GROUP_LABEL_KEYS: Record<string, string> = {
  server: "downloads.groups.server",
  database: "downloads.groups.database",
  mail: "downloads.groups.mail",
  language: "downloads.groups.language",
  extras: "downloads.groups.extras",
};

// Every kind belongs to exactly one group. A kind missing here falls into
// "extras" rather than vanishing — the Libs page renders only what it can
// place, and Caddy was invisible for a day because it was absent from a map
// like this one.
export const KIND_GROUP: Record<string, string> = {
  nginx: "server",
  apache: "server",
  caddy: "server",
  mysql: "database",
  postgres: "database",
  pgvector: "database",
  redis: "database",
  mailpit: "mail",
  php: "language",
  python: "language",
  nodejs: "language",
  composer: "extras",
  cmder: "extras",
};

export function groupForKind(kind: string): string {
  return KIND_GROUP[kind] ?? "extras";
}
