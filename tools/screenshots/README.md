# Screenshot harness

Regenerates the marketing banners (`baner1.png`–`baner6.png`) and raw documentation
screenshots for LaraEnv 0.5.5, by running the real frontend headlessly against a
fake Wails backend. The fixtures use Alex Rivera, fictional projects under
`C:\sites`, and documentation-only network addresses.

## Usage

    cd tools/screenshots
    npm install
    npm run capture                  # banners and documentation, into ./out
    node capture.mjs --only=projects # one scenario
    node capture.mjs --out=C:/laraenv/www/laraenv-server/public

The Projects banner also writes `projects-workspace.png`. Raw-only scenarios
are selected by their names: `project-actions`, `project-runtimes`, `site-doctor`,
`site-doctor-overview`, `workspace`, and `workspace-files`. Their matching PNGs
show the real Projects list, saved shortcut customization, detected requirements,
Doctor repairs and health overview, and the terminal with Monaco source and Git
reviews. The workspace fixtures exercise read bindings for a fictional checkout;
they do not run Git commands or write project files.

The SSH scenario also writes `ssh-manager.png`, an unframed in-app capture for
README and documentation pages. `baner4.png` remains the framed marketing image.

`frontend/dist` must be built first (`npm run build` in `frontend/`) — the
harness captures the built bundle, not the dev server.

## How it works

Every Wails binding is a `window.go.main.App.<Name>()` wrapper, so the backend
is replaced entirely from JavaScript. `frontend/dist` is served read-only and
never modified; the fake backend is injected with Playwright's
`addInitScript`, which runs before any page script.

Playwright uses the machine's installed Chrome (`channel: "chrome"`), so no
Chromium is downloaded.

The window frame — rounded corners, drop shadow, purple starfield — is a
second capture pass over `frame/frame.html`, which draws everything in CSS.
No image asset is involved, so retuning the frame does not mean recapturing
the app.

## Adding a scenario

Add an entry to `mock/scenarios.mjs`. `ready` must be a string of data the
scenario itself injects — waiting for it proves the binding resolved, React
painted, and the right page is showing. Never replace it with a timeout.

Readiness strings match the DOM, not the rendered pixels. The dashboard
tagline, for example, is uppercased by CSS, so the scenario waits on the
lowercase source text.

Return shapes for bindings are generated in `frontend/wailsjs/go/models.ts`.
A field name that does not match renders an empty row rather than failing, so
read the model rather than guessing.

## When a run fails

Failure is deliberate: a half-rendered banner would be published to the site,
including as the `og:image`. A scenario aborts and writes nothing if an
unmocked binding is called (the error names it), the page logs an error, the
app's ErrorBoundary renders, or the readiness string never appears.

That check earns its keep. It caught `StartPtyKind` being called on the
dashboard — the terminal workspace stays mounted across pages — which a
timeout-based capture would have silently shipped.

To find every binding a new page needs, let it fail: the error lists them.

All scenario data is fictional on purpose — these images are public and
indexed.

## Clips (animated GIFs)

    node record.mjs --only=command-center
    node record.mjs --out=C:/laraenv/www/laraenv-server/public

A clip is a scenario that produces a sequence instead of a single frame.
Steps are declarative (`hold`, `type`, `press`), so `totalFrames()` knows the
length before recording and a test can keep a clip down to a size a README can
carry. Frames are composited onto `frame/desktop.html` — a faux desktop, so
the command bar reads as appearing *over* something rather than floating
alone.

**ffmpeg is required** for clips, and only for clips. GIF is a 256-colour
format and a naive encode bands badly on this dark UI, so the recorder uses
ffmpeg's `palettegen`/`paletteuse`. If ffmpeg is missing the run stops with an
explanation rather than writing a poor file.

The command-center clip flips `commandBar: true` in its state, which makes
`IsCommandBar()` return true and App.tsx render the standalone bar. Note the
clip shows a global hotkey of `Ctrl+Alt+K`; the shipped default is **empty**,
i.e. the global hotkey is opt-in in Settings → Command Center
(`internal/config/config.go:176`). Any copy around the GIF should say so.

## The terminal banner

`baner3` shows the agent workspace: two agent tabs restored from a persisted
layout (`LoadTerminalWorkspace`), a project shell promoted by an adopted
session, the navigator, the usage strip, and a scripted Claude Code transcript
in the active pane.

Two things make the pane paint under headless Chrome:

- `webgl: false` in the scenario state. The app renders xterm through
  `xterm-addon-webgl`, whose context never paints headlessly. The init script
  refuses WebGL contexts, the addon throws, and `Terminal.tsx` falls back to
  xterm's DOM renderer (`frontend/src/components/Terminal.tsx`, the
  `WebglAddon` try/catch). DOM rows are text nodes, so the readiness string
  can be a line of the transcript.
- A synthetic `window` resize before writing. The pane opens while hidden and
  stays at xterm's default 24 rows; the ResizeObserver refit does not fire
  headlessly, but the window-resize refit does. The scenario dispatches it and
  waits for more than 30 rows, otherwise the top of the transcript scrolls
  away.

The transcript avoids box-drawing characters: the bundled JetBrains Mono is the
latin subset, so `─` and `│` fall back to another font and render as dashes.
Everything in it — project, files, test names — is fictional.

## Agent map proof

`agent-map` is a raw documentation capture of the real App bundle with synthetic
Wails activity metadata installed before the app loads. It restores existing
agent tabs, opens the activity counter and emits `agent:activity-changed`; it
never launches a provider or creates a new PTY. `mock/agentActivity.mjs` defines
the snapshots and the four activity methods; `base.mjs` also supplies explicit
empty activity responses so unrelated captures do not depend on this feature.

```powershell
$env:NODE_OPTIONS = '--no-experimental-webstorage'
$env:WORKSPACE_CHROMIUM = 'C:\Program Files\Google\Chrome\Application\chrome.exe'
npm.cmd --prefix frontend run build
npm.cmd --prefix tools/screenshots test -- __tests__/agentMap.integration.test.mjs --maxWorkers=2 --minWorkers=1
node tools/screenshots/capture.mjs --only=agent-map --out=build/verification/agent-map
```

The browser integration blocks external requests, checks main and detached
windows, exact counter focus restoration after Escape, explicit terminal focus,
independent counter/dialog interests, child/task selection, late events,
auto-switch blocking and dirty document decisions. It saves and measures
`agent-map-1334x871.png` and `agent-map-640x720.png` under
`build/verification/agent-map/`, including horizontal overflow, close-button
visibility and actual content scrolling. Companion `agent-map-counter-*`
images check the activity counter fits inside the viewport before opening the
map. The capture command adds `agent-map.png`.

In the app, open the map from the counter or a session's **View map** action.
Counts cover activity observed in the current run, including activity observed
after collection starts; they do not reconstruct unobserved history. Claude
Code and Codex have different protocol/hook coverage, and Kimi/OpenCode do not
offer child observation. Model, duration, live state, task inventory and tokens
remain unavailable without a supporting source; an empty confirmed task list
differs from unavailable inventory. Snapshots are cleared when the app restarts.
