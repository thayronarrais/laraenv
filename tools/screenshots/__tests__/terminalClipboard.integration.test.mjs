import { afterAll, beforeAll, describe, expect, it } from "vitest";
import path from "node:path";
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

describe("real xterm clipboard input", () => {
  it("copies with right-click and pastes exactly once into local and SSH panes", async () => {
    const context = await browser.newContext({ viewport: { width: 1334, height: 871 } });
    try {
      const state = mergeState(BASE_STATE, {
        webgl: false,
        workspace: {
          version: 2,
          groups: [{ x: 0, y: 0, w: 900, h: 600, snap: "max", activeTabIndex: 0, tabs: [
            { label: "Clipboard local", activeLeafIndex: 0, tree: { type: "leaf", label: "Clipboard local", kind: "powershell", spawn: { kind: "project", projectName: "clipboard-local", terminalKind: "powershell" } } },
            { label: "Clipboard SSH", activeLeafIndex: 0, tree: { type: "leaf", label: "Clipboard SSH", kind: "ssh", spawn: { kind: "ssh", sshHostName: "clipboard-host" } } },
          ] }],
        },
      });
      await context.addInitScript({ content: buildInitScript({ state }) + `
        localStorage.setItem("l4e:terminal:copyMode", "mouse");
        window.__terminalClipboard = { text: "one\\ntwo", writes: [], copies: [] };
        const baseApp = window.go.main.App;
        const startProjectPty = () => Promise.resolve("smoke-local");
        const actions = {
          StartPtyForProject: startProjectPty,
          StartPtyForProjectCommand: startProjectPty,
          StartPtyForSSH: () => Promise.resolve("smoke-ssh"),
          WritePty: (id, data) => { window.__terminalClipboard.writes.push({ id, data }); return Promise.resolve(); },
          ResizePty: () => Promise.resolve(),
          StopPty: () => Promise.resolve(),
          ReadClipboardText: () => Promise.resolve(window.__terminalClipboard.text),
          WriteClipboardText: (text) => { window.__terminalClipboard.copies.push(text); window.__terminalClipboard.text = text; return Promise.resolve(); },
        };
        window.go.main.App = new Proxy({}, { get: (_, name) => actions[name] || baseApp[name] });
      ` });
      const page = await context.newPage();
      page.setDefaultTimeout(8_000);
      const errors = [];
      page.on("pageerror", (error) => errors.push(String(error)));
      await page.goto(`${server.url}/index.html`, { waitUntil: "domcontentloaded" });
      await page.getByRole("button", { name: "Terminal", exact: true }).click();
      await page.locator(".xterm:visible .xterm-rows").waitFor();
      await page.evaluate(() => window.__harness.emit("term:data:smoke-local", "\x1b[?2004hREADY_LOCAL\r\n"));
      const output = page.locator(".xterm:visible").getByText("READY_LOCAL", { exact: true });
      await output.waitFor({ state: "visible" });
      // xterm's link canvas overlays its DOM text; send real pointer input at
      // the rendered word rather than requiring the span itself to receive it.
      const box = await output.boundingBox();
      const x = box.x + box.width / 2;
      const y = box.y + box.height / 2;
      await page.mouse.dblclick(x, y);
      await page.mouse.click(x, y, { button: "right" });
      await page.waitForFunction(() => window.__terminalClipboard.copies.length === 1);
      expect(await page.evaluate(() => window.__terminalClipboard.copies)).toEqual(["READY_LOCAL"]);
      await page.evaluate(() => { window.__terminalClipboard.text = "one\ntwo"; });
      await page.locator(".xterm:visible .xterm-screen").click({ button: "right" });
      await page.waitForFunction(() => window.__terminalClipboard.writes.length === 1);
      await page.keyboard.press("Control+Shift+V");
      await page.waitForFunction(() => window.__terminalClipboard.writes.length === 2);

      await page.getByText("Clipboard SSH", { exact: true }).click();
      await page.waitForFunction(() => window.__harness.hasListener("term:data:smoke-ssh"));
      await page.locator(".xterm:visible .xterm-rows").waitFor();
      await page.evaluate(() => window.__harness.emit("term:data:smoke-ssh", "\x1b[?2004hREADY_SSH\r\n"));
      await page.locator(".xterm:visible").getByText("READY_SSH", { exact: true }).waitFor();
      await page.locator(".xterm:visible .xterm-screen").click({ button: "right" });
      await page.waitForFunction(() => window.__terminalClipboard.writes.length === 3);
      expect(await page.evaluate(() => window.__terminalClipboard.writes)).toEqual([
        { id: "smoke-local", data: "\x1b[200~one\rtwo\x1b[201~" },
        { id: "smoke-local", data: "\x1b[200~one\rtwo\x1b[201~" },
        { id: "smoke-ssh", data: "\x1b[200~one\rtwo\x1b[201~" },
      ]);
      expect(errors).toEqual([]);
      expect(await page.evaluate(() => window.__harness.missing)).toEqual([]);
    } finally { await context.close(); }
  }, 30_000);
});
