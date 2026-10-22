import { afterAll, beforeAll, describe, expect, it } from "vitest";
import path from "node:path";
import { mkdir } from "node:fs/promises";
import { fileURLToPath } from "node:url";
import { chromium } from "playwright";
import { startServer } from "../serve.mjs";
import { buildInitScript } from "../mock/base.mjs";
import { BASE_STATE, mergeState } from "../mock/state.mjs";

const here = path.dirname(fileURLToPath(import.meta.url));
let browser;
let server;
beforeAll(async () => {
  server = await startServer(path.resolve(here, "../../../frontend/dist"));
  browser = await chromium.launch({ headless: true });
}, 30_000);

afterAll(async () => { await browser?.close(); await server?.close(); });

describe("command palette with real terminal input", () => {
  it("opens from xterm and switches to an existing SSH tab without restarting history", async () => {
    const context = await browser.newContext({ viewport: { width: 1334, height: 871 } });
    try {
      const state = mergeState(BASE_STATE, {
        webgl: false,
        agents: {
          enabled: true,
          profiles: [{ id: "codex", label: "Codex", available: true, capabilities: { launch: true, resume: true, richStatus: true, history: true, usage: true } }],
          sessions: [
            { id: "closed-resumable", title: "Closed resumable history", projectName: "old-project", projectPath: "C:/sites/old-project", agentId: "codex", launchMode: "current", terminalId: "", providerSessionId: "provider-closed", state: "error", updatedAt: "2026-10-03T12:00:00Z", unread: false, archived: false },
            { id: "closed-restartable", title: "Closed restartable history", projectName: "old-project", projectPath: "C:/sites/old-project", agentId: "codex", launchMode: "current", terminalId: "", providerSessionId: "", state: "error", updatedAt: "2026-10-03T11:00:00Z", unread: false, archived: false },
          ],
          usage: [],
        },
        workspace: {
          version: 2,
          groups: [{ x: 0, y: 0, w: 900, h: 600, snap: "max", activeTabIndex: 0, tabs: [
            { label: "Demo shell", activeLeafIndex: 0, tree: { type: "leaf", label: "Demo shell", kind: "pwsh", spawn: { kind: "local", terminalKind: "pwsh" } } },
            { label: "ssh · contaboo", activeLeafIndex: 0, tree: { type: "leaf", label: "ssh · contaboo", kind: "ssh", spawn: { kind: "ssh", sshHostName: "contaboo" } } },
          ] }],
        },
      });
      await context.addInitScript({ content: buildInitScript({ state }) + `
        localStorage.setItem("laraenv.agentNavigatorVisible", "false");
        window.__commandPalette = { spawns: [], writes: [] };
        const baseApp = window.go.main.App;
        const actions = {
          StartPtyKind: (kind) => { window.__commandPalette.spawns.push({ kind: "local", shell: kind }); return Promise.resolve("term-demo"); },
          StartPtyForSSH: (host) => { window.__commandPalette.spawns.push({ kind: "ssh", host }); return Promise.resolve("term-ssh"); },
          WritePty: (id, data) => { window.__commandPalette.writes.push({ id, data }); return Promise.resolve(); },
          ResizePty: () => Promise.resolve(),
          StopPty: () => Promise.resolve(),
        };
        window.go.main.App = new Proxy({}, { get: (_, name) => actions[name] || baseApp[name] });
      ` });
      const page = await context.newPage();
      page.setDefaultTimeout(8_000);
      const errors = [];
      page.on("pageerror", (error) => errors.push(String(error)));
      await page.goto(`${server.url}/index.html`, { waitUntil: "domcontentloaded" });
      await page.getByRole("button", { name: "Terminal", exact: true }).click();
      await page.waitForFunction(() => window.__harness.hasListener("term:data:term-demo") && window.__harness.hasListener("term:data:term-ssh"));
      await page.locator(".xterm:visible .xterm-rows").waitFor();
      const localInput = page.locator(".xterm:visible textarea");
      await page.locator(".xterm:visible .xterm-screen").click();
      await page.waitForFunction(() => document.activeElement?.matches(".xterm textarea"));
      expect(await localInput.evaluate((input) => input === document.activeElement)).toBe(true);
      expect(await page.evaluate(() => window.__commandPalette.spawns)).toEqual([
        { kind: "local", shell: "pwsh" },
        { kind: "ssh", host: "contaboo" },
      ]);

      // Real xterm cancels bubbling keydowns. This catches a palette listener
      // that works on the dashboard but never receives Ctrl+K from its textarea.
      await page.keyboard.press("Control+k");
      const palette = page.locator("body > div.fixed").filter({ has: page.locator("input") });
      const search = palette.locator("input");
      await search.waitFor({ state: "visible" });
      await page.waitForFunction(() => document.activeElement?.tagName === "INPUT");
      const terminalSection = palette.locator("div.mb-1").filter({ has: page.getByText("Open terminals", { exact: true }) });
      await terminalSection.getByRole("button", { name: /Demo shell/ }).waitFor();
      expect(await terminalSection.locator("button").count()).toBe(2);
      expect(await terminalSection.getByRole("button", { name: /ssh · contaboo/ }).count()).toBe(1);
      expect(await palette.getByRole("button", { name: /Closed (?:resumable|restartable) history|\b(?:Resume|Restart)\b/ }).count()).toBe(0);
      if (process.env.COMMAND_PALETTE_SCREENSHOT) {
        const previewPath = path.resolve(here, "../../../build/verification/command-palette-open-terminals.png");
        await mkdir(path.dirname(previewPath), { recursive: true });
        await page.screenshot({ path: previewPath });
      }

      await search.fill("contaboo");
      await palette.getByRole("button", { name: /ssh · contaboo/ }).waitFor();
      expect(await palette.locator("button[data-idx]").count()).toBe(1);
      await page.keyboard.press("Enter");
      await search.waitFor({ state: "detached" });
      await page.waitForFunction(() => document.activeElement?.matches(".xterm textarea"));
      const sshInput = page.locator(".xterm:visible textarea");
      expect(await sshInput.evaluate((input) => input === document.activeElement)).toBe(true);

      const marker = "PALETTE_SWITCH_TO_EXISTING_SSH";
      await page.keyboard.type(marker);
      await page.waitForFunction((text) => window.__commandPalette.writes.filter((write) => write.id === "term-ssh").map((write) => write.data).join("") === text, marker);
      const writes = await page.evaluate(() => window.__commandPalette.writes);
      expect(writes.every((write) => write.id === "term-ssh")).toBe(true);
      expect(writes.map((write) => write.data).join("")).toBe(marker);
      expect(await page.evaluate(() => window.__commandPalette.spawns)).toEqual([
        { kind: "local", shell: "pwsh" },
        { kind: "ssh", host: "contaboo" },
      ]);
      expect(errors).toEqual([]);
      expect(await page.evaluate(() => window.__harness.missing)).toEqual([]);
    } finally { await context.close(); }
  }, 30_000);

  it("records the palette shortcut in Settings without opening the palette", async () => {
    const context = await browser.newContext({ viewport: { width: 1334, height: 871 } });
    try {
      const state = mergeState(BASE_STATE, { webgl: false });
      await context.addInitScript({ content: buildInitScript({ state }) + `
        window.__hotkeyRecording = {
          config: { paletteHotkey: "Ctrl+K", globalHotkey: "", commands: [] },
          saves: [],
        };
        const baseApp = window.go.main.App;
        const actions = {
          GetCommandCenter: () => Promise.resolve(window.__hotkeyRecording.config),
          SetCommandCenter: (config) => {
            window.__hotkeyRecording.config = config;
            window.__hotkeyRecording.saves.push(config);
            return Promise.resolve();
          },
        };
        window.go.main.App = new Proxy({}, { get: (_, name) => actions[name] || baseApp[name] });
      ` });
      const page = await context.newPage();
      page.setDefaultTimeout(8_000);
      await page.goto(`${server.url}/index.html`, { waitUntil: "domcontentloaded" });
      await page.getByRole("button", { name: "Settings", exact: true }).click();
      await page.getByRole("button", { name: "Command Center", exact: true }).click();
      await page.getByRole("button", { name: "Ctrl+K", exact: true }).click();
      const recorder = page.getByRole("button", { name: "Press keys… (Esc cancel)", exact: true });
      await recorder.waitFor({ state: "visible" });
      await page.keyboard.press("Control+k");
      // Wait for the recording to complete or for the conflicting shortcut to
      // open its overlay, so either outcome is observed after React renders.
      await page.waitForFunction(() => window.__hotkeyRecording.saves.length > 0 || !!document.querySelector("body > div.fixed input"));

      const palette = page.locator("body > div.fixed").filter({ has: page.locator("input") });
      expect(await palette.count()).toBe(0);
      await page.waitForFunction(() => window.__hotkeyRecording.saves.length === 1);
      await recorder.waitFor({ state: "detached" });
      expect(await page.getByRole("button", { name: "Command Center", exact: true }).isVisible()).toBe(true);
      expect(await page.getByRole("button", { name: "Ctrl+K", exact: true }).isVisible()).toBe(true);
      expect(await page.evaluate(() => window.__hotkeyRecording.saves)).toEqual([
        { paletteHotkey: "Ctrl+K", globalHotkey: "", commands: [] },
      ]);
    } finally { await context.close(); }
  }, 30_000);
});
