// @vitest-environment jsdom
import { existsSync } from "node:fs";
import { execFileSync } from "node:child_process";
import { basename, join, resolve } from "node:path";
import { beforeEach, describe, expect, it, vi } from "vitest";

// Issue #30 — "The built-in editor doesn't launch". @monaco-editor/react
// defaults to pulling Monaco from cdn.jsdelivr.net the first time an editor
// mounts, so every built-in editor (php.ini, SFTP files, deploy scripts)
// depended on a public CDN being reachable from inside WebView2. When that
// fetch fails the loader promise never settles: the editor sits on
// "Loading..." forever and the only trace is a console.error a packaged build
// has no devtools to show. LaraEnv manages a *local* environment, so Monaco
// has to ship inside the app.

describe("monaco bootstrap", () => {
  beforeEach(() => {
    vi.resetModules();
    document.body.innerHTML = "";
  });

  it("fetches the AMD loader from the app, never from a CDN", async () => {
    const { loader } = await import("@monaco-editor/react");
    await import("../monaco");

    // Injects the <script> synchronously; jsdom never runs it, so the promise
    // stays pending — the src is what we are asserting on.
    loader.init().catch(() => {});

    const sources = [...document.querySelectorAll("script")].map((s) => s.getAttribute("src") ?? "");
    expect(sources).toContain("/vs/loader.js");
    expect(sources.some((s) => /^(https?:)?\/\//.test(s))).toBe(false);
  });

  it("vendors the Monaco AMD bundle where that path resolves", async () => {
    const { MONACO_VS_PATH } = await import("../monaco");
    // Build scripts run in Node, where import.meta.url is a file URL. Importing
    // them through Vitest's jsdom transform supplies a browser URL instead.
    execFileSync(process.execPath, [resolve("scripts/vendor-monaco.mjs")], { windowsHide: true });
    const dest = resolve("public/vs");

    expect(basename(dest)).toBe(MONACO_VS_PATH.replace(/^\//, ""));
    expect(existsSync(join(dest, "loader.js"))).toBe(true);
    expect(existsSync(join(dest, "editor", "editor.main.js"))).toBe(true);
  });
});
