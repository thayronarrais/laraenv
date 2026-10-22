// A clip is a scenario that produces a sequence instead of a single frame.
// Steps are declarative so the frame count is knowable before recording — see
// totalFrames, which the tests use to keep a clip down to a size a README can
// carry.
//
// Step kinds:
//   { hold: ms }                     hold the current state
//   { type: "text", delayMs }        type it, one frame per character
//   { press: "Key", repeat, hold }   press a key, then hold

const PROJECTS = [
  { name: "acme-shop", url: "https://acme-shop.test", path: "C:\\sites\\acme-shop",
    kind: "laravel", docRoot: "C:\\sites\\acme-shop\\public", phpVersion: "8.4",
    nodeVersion: "22.11.0", ssl: true, jsFramework: "vite", packageManager: "npm",
    proxyPort: 0, tags: [] },
  { name: "atlas-crm", url: "https://atlas-crm.test", path: "C:\\sites\\atlas-crm",
    kind: "laravel", docRoot: "C:\\sites\\atlas-crm\\public", phpVersion: "8.3",
    nodeVersion: "22.11.0", ssl: true, jsFramework: "vue", packageManager: "yarn",
    proxyPort: 0, tags: [] },
  { name: "beacon-api", url: "https://beacon-api.test", path: "C:\\sites\\beacon-api",
    kind: "public", docRoot: "C:\\sites\\beacon-api\\public", phpVersion: "8.4",
    nodeVersion: "22.11.0", ssl: true, jsFramework: "react", packageManager: "npm",
    proxyPort: 3001, tags: [] },
];

const SSH_HOSTS = [
  { name: "edge-01", address: "203.0.113.11", port: 22, user: "root", tags: ["prod"], authMode: "key" },
  { name: "staging", address: "203.0.113.90", port: 2222, user: "deploy", tags: ["staging"], authMode: "key" },
];

// What the real app reads out of the project (artisan list + package.json
// scripts). Real command names on purpose: invented ones would undercut the
// very point the clip makes.
const PROJECT_COMMANDS = {
  "acme-shop": {
    artisan: ["migrate", "migrate:fresh", "queue:work", "route:list",
              "optimize:clear", "storage:link", "tinker"],
    npmScripts: ["dev", "build", "preview"],
  },
};

export const CLIPS = [
  {
    id: "command-center",
    out: "command-center.gif",
    theme: "midnight",
    fps: 12,
    // The bar fills its own window, so the viewport is the bar.
    viewport: { width: 760, height: 400 },
    state: {
      commandBar: true,
      projects: PROJECTS,
      sshHosts: SSH_HOSTS,
      // Shown only in the demo. The real default is empty, i.e. the global
      // hotkey ships disabled and is opt-in in Settings -> Command Center
      // (internal/config/config.go:176).
      commandCenter: { paletteHotkey: "Ctrl+K", globalHotkey: "Ctrl+Alt+K", commands: [] },
      projectCommands: PROJECT_COMMANDS,
    },
    // Injected data, not chrome: this command only exists because the mocked
    // ListServices resolved and the palette rendered its results. (The input's
    // placeholder would be an attribute, which getByText cannot see.)
    ready: "Start Nginx",
    // No metrics card in the command bar.
    seedMetrics: false,
    steps: [
      { hold: 800 },
      // Name the project: its own actions appear.
      { type: "acme", delayMs: 90 },
      { hold: 1000 },
      // Tab completes the project token. It has to be the exact name: the
      // launcher resolves the project by equality, not fuzzily
      // (frontend/src/commandCenter/launcher.ts:68), so "acme" alone would
      // never reach the artisan/npm suggestions.
      { press: "Tab", hold: 800 },
      // Ask for artisan — these suggestions belong to this project, because
      // this project is a Laravel one.
      // No leading space: Tab already left a trailing one, and a double space
      // makes launcher.ts compute prefixStr as " php artisan", which fails its
      // === "php artisan" test and silently yields no suggestions.
      { type: "php artisan ", delayMs: 55 },
      { hold: 1200 },
      { type: "mig", delayMs: 100 },
      { hold: 1200 },
      // Back to the project and ask for npm instead: now the package.json
      // scripts are what shows up.
      { press: "Backspace", repeat: 15, hold: 300 },
      { type: "npm run ", delayMs: 60 },
      { hold: 1600 },
    ],
  },
];

export function getClip(id) {
  return CLIPS.find((c) => c.id === id);
}

// Frames a clip will produce. Holds are sampled at the clip's frame rate;
// typing and keypresses get one frame each so the motion reads as input
// rather than as a slideshow.
export function totalFrames(clip) {
  const perHold = (ms) => Math.round((ms / 1000) * clip.fps);
  let n = 0;
  for (const s of clip.steps) {
    if (s.type) n += s.type.length;
    if (s.press) n += s.repeat ?? 1;
    if (s.hold) n += perHold(s.hold);
  }
  return n;
}
