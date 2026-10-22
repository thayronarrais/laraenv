import { readFileSync, readdirSync, statSync } from "node:fs";
import { join } from "node:path";
import { describe, expect, it } from "vitest";

// frontend/package.json has "type": "module", so __dirname is undefined here
// (this file runs as native ESM under Vitest, not through a CJS shim) —
// import.meta.dirname is Node 24's replacement and is what actually resolves.
// Same pattern as catalogueKeys.test.ts.
const SRC = join(import.meta.dirname, "..", "..");

// Literals that are not prose: brands, technologies, units, and the symbols a
// UI shows verbatim in every language — plus, below, every specific string
// the widened scan (see PROP_RE) turns up that is genuinely data or
// unreachable rather than a missed extraction. Every addition below carries
// its own reason; an allowlist entry with no reason is how this guard rots.
const ALLOWED = new Set([
  "LaraEnv", "PHP", "MySQL", "PostgreSQL", "nginx", "Apache", "Caddy", "Redis",
  "Node.js", "npm", "pnpm", "yarn", "bun", "Composer", "Laravel", "SSH", "SFTP",
  "HTTP", "HTTPS", "TLS", "SSL", "JSON", "YAML", "Git", "GitHub", "VS Code",
  "Cursor", "MB", "GB", "KB", "ms", "%", "…", "—", "·", "/", ":", "×",

  // --- theme.ts: THEMES[].label — theme NAMES, brands the same in every
  // locale (the prose that goes with each one is a catalogue key,
  // descriptionKey, resolved with t() at the render site; see the comment
  // on ThemeDef in theme.ts). ---
  "Nebula", "Aurora", "Sunset", "Crimson", "Oceanic", "Midnight",

  // --- NewProjectModal.tsx: TYPES[].label — technology/brand names,
  // rendered verbatim for every kind except blank/git (which route through
  // t() keys instead). Never translated, same as "PHP"/"MySQL" above. ---
  "WordPress", "Vite", "Next.js", "Nuxt", "Astro",
  // TYPES[].label for "blank" and "git" — dead fallback data. The render
  // ternary always intercepts entry.id === "blank" / "git" before reaching
  // entry.label (it uses t("...blankLabel") / t("...gitLabel") instead), so
  // these two literals never reach the screen.
  "Blank", "Clone from Git",
  // TYPES[].desc for "laravel" and "git" — literal shell-command syntax,
  // rendered as-is (not prose), same category as "git clone <url>" already
  // called out below.
  "composer create-project laravel", "git clone <url>",
  // TYPES[].desc for "blank"/"wordpress"/"vite"/"next"/"nuxt"/"astro" — dead
  // fallback data. TYPE_DESC_KEYS overrides entry.desc for wordpress/vite/
  // next/nuxt/astro, and the "blank" id branch is intercepted the same way
  // as its label above, so none of these ever render.
  "Empty folder + welcome page", "Latest WP from wordpress.org",
  "create vite (react/vue/svelte/…)", "create-next-app (React + SSR)",
  "create nuxt (Vue + SSR)", "create astro (content-driven)",

  // --- commandCenter/builtins.ts: NEW_PROJECT_KINDS[].hint — deliberately
  // untranslated per the comment above that array: it mirrors
  // NewProjectModal's own `desc` for these kinds, which are themselves
  // either literal shell commands or short technical taglines left as
  // English data (see the NewProjectModal.tsx entries above). The casing
  // differs from NewProjectModal's own copy of this string (lowercase
  // "empty"/"latest" here vs. capitalized there) — both are the same data,
  // not two separate strings to reconcile. ---
  "latest WP from wordpress.org", "empty folder + welcome page",

  // --- terminal/workspaceReducer.ts: the tab-name fallback when a rename is
  // cleared to blank. Four characters, identical in Portuguese; deliberately
  // left untranslated rather than minted as a catalogue key for one word. ---
  "term",

  // --- PhpExtensionsModal.tsx: initial `message` on a freshly-created
  // progress record. Confirmed dead — the only render sites for
  // pp.message/progress.message are the "error" phase and the "extract"
  // phase, and this literal is only ever set while phase is "download". ---
  "starting…",

  // --- Sidebar.tsx / Dashboard.tsx: the wordmark is split into two styled
  // <span>s so "Env" can carry its own accent class — "Lara" is a JSX text
  // node in its own right but it's half of the brand name "LaraEnv", not
  // prose. ---
  "Lara",

  // --- Dashboard.tsx: metric-card headings naming the service they show —
  // "Node" (Node.js) and "PHP-FPM" — technology names, same category as
  // "PHP"/"MySQL" above. Settings.tsx's GlassCard title="Nginx" is the same
  // case with the capitalized spelling nginx.org itself uses for the
  // product name (vs. the lowercase "nginx" package/binary name already
  // allowed above). ---
  "Node", "PHP-FPM", "Nginx",

  // --- VhostEditorModal.tsx: the web-server picker's option id/label pairs.
  // "apache" and "caddy" are the literal Server union values ("nginx" |
  // "apache" | "caddy"), not prose — same category as the already-allowed
  // "Apache"/"Caddy" brand names, just lowercase because that's the on-disk
  // identifier being echoed back as its own option label. ---
  "apache", "caddy",

  // --- SSHKeyGenModal.tsx: KEY_TYPES[].label — key-algorithm identifiers
  // (ED25519, RSA 4096, ECDSA P-256). Algorithm ids, never translated, same
  // category as "TLS"/"SSL" above. ---
  "ED25519", "RSA 4096", "ECDSA P-256",

  // --- SFTPModal.tsx: the panel heading "SFTP · {hostLabel}" — SFTP is
  // already an allowed brand and "·" an allowed separator; the JSX text-node
  // scan just happens to capture them pre-joined with the space before the
  // dynamic {hostLabel} span. ---
  "SFTP ·",

  // --- DialogProvider.tsx: appears only inside a `//`-comment documenting
  // how to call dialogs.prompt() — a usage example in source commentary,
  // never executed, never rendered. ---
  "Rename session",

  // --- Placeholder/example text in <input placeholder="..."> and similar
  // hint props across several forms: illustrative values (a branch name, a
  // hostname, a URL, an email, a masked secret, a cron expression, a shell
  // command) shown grayed-out so the user knows the expected shape of the
  // field, not prose describing anything. This is the "example tokens in
  // hints" category — the same idea as a shell command or algorithm id,
  // just typed by the user rather than shown as an option. ---
  "feature/my-change", "main", "feature-preview",           // AgentLauncherMenu.tsx
  "php artisan migrate",                                     // CommandCenterSettings.tsx
  "Laravel — client X",                                      // DeployPresetsModal.tsx (name placeholder)
  "production | staging | development", "nginx | apache", "mysql | postgres | empty", // DeployPresetsModal.tsx (DPL_TOKENS hints)
  "mysite", "https://github.com/owner/repo.git",             // NewProjectModal.tsx
  "staging", "203.0.113.5 or staging.example.com", "deploy", // SSHHostModal.tsx
  "e.g. cloudflared access ssh --hostname %h",               // SSHHostModal.tsx
  "id_ed25519, id_rsa…",                                     // SSHImportKeyModal.tsx
  "you@example.com", "••••••••",                             // Account.tsx
  "* * * * *", "php artisan schedule:run",                   // Cron.tsx
  "prod-web", "https://github.com/you/repo.git", "ghp_••••••••••••", "*/5 * * * *", // Deployments.tsx
  "--queue=high --tries=3 --timeout=60", "npm run dev",      // Projects.tsx

  // --- CommandCenterSettings.tsx: hotkey syntax shown next to the
  // instructions that name them ("... Win+…", "... Ctrl+Alt+…", etc). Key
  // combos, not prose — same idea as "Ctrl+K" would be. ---
  "Win+…", "Ctrl+Alt+…", "Ctrl+T", "Ctrl+W",
]);

// A literal shaped like a catalogue key ("settings.phpExtensions.flash.foo")
// rather than prose — e.g. `label: t("commandCenter.groups.custom")` reads a
// quoted string right after the property's colon, but that string is the
// t() call's key argument, not a hardcoded label. Filtering by shape (rather
// than trying to detect the enclosing t(...) call) is deliberately the same
// kind of broad, error-on-the-safe-side heuristic catalogueKeys.test.ts uses.
const DOT_PATH = /^[A-Za-z0-9_]+(?:\.[A-Za-z0-9_]+)+$/;

function walk(dir: string, out: string[] = []): string[] {
  for (const name of readdirSync(dir)) {
    const full = join(dir, name);
    if (statSync(full).isDirectory()) {
      // __tests__ files legitimately contain English fixture text.
      if (name === "node_modules" || name === "__tests__") continue;
      walk(full, out);
    } else if (/\.tsx?$/.test(name)) {
      out.push(full);
    }
  }
  return out;
}

// A JSX text node: >Some words< with no braces. Deliberately conservative —
// this catches the common shape rather than parsing TSX properly, so it errs
// toward silence rather than noise. JSX only exists in .tsx.
//
// The negative lookbehind excludes `=>` — an arrow function's `>` reads as a
// tag close to this regex, and the return-type/expression that follows
// ("Promise<void>", "Record<string, boolean>", a chained call before a
// generic's `<`) is not JSX text at all. Without it this fires on ordinary
// TypeScript, not just markup.
const TEXT_NODE = /(?<!=)>\s*([A-Z][A-Za-z][^<>{}\n]{2,})\s*</g;
const TEXT_PROP = /\b(?:title|placeholder|aria-label|alt)\s*=\s*"([^"]{3,})"/g;

// Object-literal properties: `desc:`, `description:`, `label:`, `title:`,
// `hint:`, `message:`, `tooltip:`, `subtitle:`, `header:` assigned either a
// string literal directly, or a simple reference/call falling back to one
// via `||`/`??` (e.g. `label: action.label.trim() || "term"`). Requiring the
// string immediately after the colon (or immediately after that one
// `||`/`??`) — rather than "the next quote anywhere in the statement" — is
// what keeps this from misreading a ternary's *comparison* as the property's
// value, e.g. `title: side === "remote" ? t(remoteKey) : t(localKey)`: the
// comparison sits before `===`, not `||`/`??`, so it's correctly not this
// property's value and this regex does not match there at all.
//
// Runs against both .ts and .tsx — this is exactly the shape the original
// JSX-only, four-prop scan missed (theme.ts has no JSX at all;
// NewProjectModal's `desc` and the command-palette group `label`s are plain
// object properties, not JSX props).
const PROP_RE = /\b(?:desc|description|label|title|hint|message|tooltip|subtitle|header)\s*:\s*(?:[\w.()]+\s*(?:\|\||\?\?)\s*)?"([A-Za-z][^"]{1,100})"/g;

describe("no bare user-facing strings", () => {
  it("every visible string goes through t()", () => {
    const offenders: string[] = [];
    for (const file of walk(SRC)) {
      const text = readFileSync(file, "utf8");
      const regexes = file.endsWith(".tsx") ? [TEXT_NODE, TEXT_PROP, PROP_RE] : [PROP_RE];
      for (const re of regexes) {
        re.lastIndex = 0;
        for (const m of text.matchAll(re)) {
          const value = m[1].trim();
          if (ALLOWED.has(value)) continue;
          if (/^[\d\s.,:%/×-]+$/.test(value)) continue;
          if (DOT_PATH.test(value)) continue;
          offenders.push(`${file.replace(SRC, "src")}: ${value}`);
        }
      }
    }
    expect(offenders, `hardcoded strings — move them to langs/en.json:\n  ${offenders.join("\n  ")}`).toEqual([]);
  });
});
