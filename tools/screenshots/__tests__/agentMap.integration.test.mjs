import { afterAll, beforeAll, describe, expect, it } from "vitest";
import path from "node:path";
import { mkdir } from "node:fs/promises";
import { fileURLToPath } from "node:url";
import { chromium } from "playwright";
import { startServer } from "../serve.mjs";
import { buildInitScript } from "../mock/base.mjs";
import { BASE_STATE, mergeState } from "../mock/state.mjs";
import { AGENT_MAP_STATE } from "../mock/agentActivity.mjs";

const here = path.dirname(fileURLToPath(import.meta.url));
const images = path.resolve(here, "../../../build/verification/agent-map");
let browser, server;
beforeAll(async () => {
  server = await startServer(path.resolve(here, "../../../frontend/dist"));
  browser = await chromium.launch({ headless: true, ...(process.env.WORKSPACE_CHROMIUM ? { executablePath: process.env.WORKSPACE_CHROMIUM } : {}) });
  await mkdir(images, { recursive: true });
}, 30_000);
afterAll(async () => { await browser?.close(); await server?.close(); });

async function openPage({ detached = false, viewport = { width: 1334, height: 871 }, patch = {} } = {}) {
  const context = await browser.newContext({ viewport, reducedMotion: "reduce" });
  const external = [], errors = [];
  await context.route("**/*", route => {
    const url = route.request().url();
    if (/^https?:/.test(url) && new URL(url).origin !== new URL(server.url).origin) { external.push(url); return route.abort(); }
    return route.continue();
  });
  await context.addInitScript({ content: buildInitScript({ state: mergeState(mergeState(BASE_STATE, AGENT_MAP_STATE), { detached, ...patch }) }) });
  const page = await context.newPage();
  page.setDefaultTimeout(8_000);
  page.on("pageerror", error => errors.push(String(error)));
  await page.goto(`${server.url}/index.html`, { waitUntil: "domcontentloaded" });
  if (!detached) await page.getByRole("button", { name: "Terminal", exact: true }).click();
  await page.locator(".xterm:visible .xterm-rows").waitFor();
  return { page, context, external, errors };
}
const dialogFor = page => page.getByRole("dialog", { name: "Agent map", exact: true });
const counterFor = page => page.getByRole("button", { name: /^4 agents/ });
const activeInterests = page => page.evaluate(() => Object.values(window.__agentActivity.interests));
const noMissing = async run => {
  expect(run.external).toEqual([]); expect(run.errors).toEqual([]);
  expect(await run.page.evaluate(() => window.__harness.missing)).toEqual([]);
  expect(await run.page.evaluate(() => window.__agentActivity.ptySpawns.length)).toBe(0);
};
const navigationState = page => page.evaluate(async () => {
  // Let the real App commit selection and its focus-report effects before
  // measuring. Keep DOM references so a replaced terminal also fails equality.
  await new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve)));
  window.__selectionTerminals ??= [...document.querySelectorAll('[data-testid="terminal-group"] .xterm')];
  return {
    focusedSessions: [...window.__agentActivity.focusedSessions],
    activeTabs: [...document.querySelectorAll('[data-testid="terminal-group"] [draggable] > span.whitespace-nowrap')]
      .filter(label => label.parentElement.classList.contains("text-white")).map(label => label.textContent),
    visibleTerminals: [...document.querySelectorAll('[data-testid="terminal-group"] .xterm')]
      .filter(terminal => getComputedStyle(terminal).visibility === "visible" && terminal.getClientRects().length > 0)
      .map(terminal => window.__selectionTerminals.indexOf(terminal)),
    counterPrincipals: Object.values(window.__agentActivity.interests).filter(interest => interest.purpose === "counter").map(interest => interest.sessionId),
  };
});
async function selectWithoutNavigation(page, dialog, name, kind) {
  const before = await navigationState(page);
  expect(before.activeTabs).toEqual(["Checkout implementation"]);
  expect(before.visibleTerminals).toEqual([0]);
  expect(before.counterPrincipals).toEqual(["map-claude"]);
  expect(await page.evaluate(() => window.__agentActivity.ptySpawns.length)).toBe(0);
  await dialog.getByRole("button", { name }).click();
  expect(await dialog.getByRole("region", { name: "Selected agent or task details" }).innerText()).toContain(kind);
  expect(await navigationState(page)).toEqual(before);
  expect(await page.evaluate(() => window.__agentActivity.ptySpawns.length)).toBe(0);
}
async function emitSnapshot(page, mutate) {
  await page.evaluate(mutate => {
    const snapshot = structuredClone(window.__agentActivity.snapshots["map-claude"]);
    Object.assign(snapshot, mutate);
    window.__agentActivity.snapshots["map-claude"] = snapshot;
    window.__harness.emit("agent:activity-changed", snapshot);
  }, mutate);
}
async function openCounter(page) {
  const opener = counterFor(page);
  try { await opener.waitFor(); }
  catch (error) { throw new Error(`${error}\nActivity fixture missing bindings: ${JSON.stringify(await page.evaluate(() => window.__harness.missing))}\nUI: ${(await page.locator("body").innerText()).slice(-1200)}`); }
  await opener.focus();
  await opener.evaluate(element => { window.__mapOpener = element; });
  await opener.click(); await dialogFor(page).waitFor();
  return dialogFor(page);
}

describe("agent map in production Chromium with synthetic Wails", () => {
  for (const detached of [false, true]) it(`restores the EXACT counter opener after Escape and gives Open terminal precedence (${detached ? "detached" : "main"})`, async () => {
    const run = await openPage({ detached }); const { page } = run;
    try {
      const dialog = await openCounter(page);
      expect(await page.evaluate(() => window.__mapOpener.closest("[inert]") !== null)).toBe(true);
      await selectWithoutNavigation(page, dialog, /Review coupon calculations/, "Child agent");
      await selectWithoutNavigation(page, dialog, /Checkout test runner/, "Background task");
      expect((await activeInterests(page)).map(x => [x.purpose, x.sessionId]).sort()).toEqual([["counter", "map-claude"], ["dialog", "map-claude"]]);
      await page.keyboard.press("Escape"); await dialog.waitFor({ state: "detached" });
      await page.waitForFunction(() => document.activeElement === window.__mapOpener);
      await page.waitForTimeout(100); // opener ownership must outlast the terminal's delayed refit/focus
      expect(await page.evaluate(() => document.activeElement === window.__mapOpener)).toBe(true);
      await page.waitForFunction(() => Object.values(window.__agentActivity.interests).length === 1);
      expect((await activeInterests(page)).map(x => x.purpose)).toEqual(["counter"]);
      await openCounter(page);
      await dialog.getByRole("button", { name: "Open terminal", exact: true }).click();
      await dialog.waitFor({ state: "detached" });
      await page.waitForFunction(() => document.activeElement?.matches(".xterm textarea"));
      expect(await page.evaluate(() => document.activeElement === window.__mapOpener)).toBe(false);
      await noMissing(run);
    } finally { await run.context.close(); }
  }, 40_000);

  it("keeps main and detached observers independent while showing the same synthetic owner/run", async () => {
    const main = await openPage(), detached = await openPage({ detached: true });
    try {
      await openCounter(main.page); await openCounter(detached.page);
      const mainInterests = await activeInterests(main.page), detachedInterests = await activeInterests(detached.page);
      expect(mainInterests.map(x => x.purpose).sort()).toEqual(["counter", "dialog"]);
      expect(detachedInterests.map(x => x.purpose).sort()).toEqual(["counter", "dialog"]);
      expect(mainInterests[0].windowId).not.toBe(detachedInterests[0].windowId);
      const epoch = page => page.evaluate(() => {
        const snapshot = window.__agentActivity.snapshots["map-claude"];
        return [snapshot.sourceId, snapshot.runId];
      });
      expect(await epoch(main.page)).toEqual(await epoch(detached.page));
      await main.page.keyboard.press("Escape");
      await dialogFor(main.page).waitFor({ state: "detached" });
      expect(await dialogFor(detached.page).isVisible()).toBe(true);
      expect((await activeInterests(detached.page)).map(x => x.purpose).sort()).toEqual(["counter", "dialog"]);
      await noMissing(main); await noMissing(detached);
    } finally { await main.context.close(); await detached.context.close(); }
  }, 40_000);

  it("renders children and tasks, ignores a late revision and blocks agent auto-switch while the map owns focus", async () => {
    const run = await openPage(); const { page } = run;
    try {
      const dialog = await openCounter(page);
      await selectWithoutNavigation(page, dialog, /Review coupon calculations/, "Child agent");
      const autoSwitchBefore = await navigationState(page);
      await page.evaluate(() => window.__harness.emit("agent:session-changed", { id: "map-codex", terminalId: "map-pty-codex", state: "needs-input" }));
      await page.waitForTimeout(1100); // exceeds the real auto-switch debounce
      expect(await navigationState(page)).toEqual(autoSwitchBefore);
      expect(await dialog.getByRole("button", { name: /Review coupon calculations/ }).getAttribute("aria-pressed")).toBe("true");
      await selectWithoutNavigation(page, dialog, /Checkout test runner/, "Background task");
      await emitSnapshot(page, { revision: 4, truncated: true });
      await dialog.getByText("Some observations were removed by the retention limit. Counts are minimums.").waitFor();
      await page.evaluate(() => window.__harness.emit("agent:activity-changed", { ...window.__agentActivity.snapshots["map-claude"], revision: 2, nodes: [], backgroundTasks: [] }));
      expect(await dialog.getByRole("button", { name: /Checkout test runner/ }).count()).toBe(1);
      await dialog.getByRole("button", { name: "Refresh activity" }).click();
      await page.waitForFunction(() => window.__agentActivity.refreshes.length > 0);
      await page.evaluate(() => {
        const mock = window.__agentActivity, old = structuredClone(mock.snapshots["map-claude"]);
        window.__retiredActivity = old;
        const next = { ...old, runId: "synthetic-replacement-run", revision: 5, truncated: false, nodes: [{ ...old.nodes[0], label: "New observation run" }], backgroundTasks: [] };
        mock.snapshots["map-claude"] = next;
        window.__harness.emit("agent:activity-changed", next);
      });
      await dialog.getByRole("button", { name: /New observation run/ }).waitFor();
      await page.evaluate(() => window.__harness.emit("agent:activity-changed", { ...window.__retiredActivity, revision: 999 }));
      expect(await dialog.getByRole("button", { name: /New observation run/ }).count()).toBe(1);
      expect(await dialog.getByRole("button", { name: /Review coupon calculations/ }).count()).toBe(0);
      await noMissing(run);
    } finally { await run.context.close(); }
  }, 40_000);

  it("dismisses the map for a dirty document decision without deciding, reopening a terminal or stealing focus", async () => {
    const rootKey = "a".repeat(64);
    const run = await openPage({ patch: {
      documents: { version: 1, tabs: [{ references: [{ context: { kind: "project", projectName: "acme-shop" }, rootKey, relativePath: "README.md" }], mode: "source" }], activeTabIndex: 0, visible: true, panelPercent: 45 },
      workspaceFiles: { project: "acme-shop", root: { rootKey, canonicalRoot: "C:/sites/acme-shop", label: "acme-shop", path: "C:/sites/acme-shop", canRead: true, canWrite: true }, files: { "README.md": { canonicalPath: "C:/sites/acme-shop/README.md", text: "# Checkout\nSynthetic project", revision: "synthetic-r1", size: 28, bom: false, eol: "lf", readOnly: false } }, entries: [] },
    } });
    const { page } = run;
    try {
      await page.getByTestId("document-surface").locator(".monaco-editor .view-lines").first().click();
      await page.keyboard.type(" pending-local");
      await openCounter(page);
      await page.evaluate(() => window.__harness.emit("documents:shutdown-request", { requestId: "map-dirty-quit", intent: "quit" }));
      const decision = page.getByRole("dialog", { name: "Unsaved documents", exact: true });
      await decision.waitFor(); await dialogFor(page).waitFor({ state: "detached" });
      await page.waitForFunction(() => document.querySelector('[role="dialog"][aria-modal="true"]')?.contains(document.activeElement));
      expect(await page.evaluate(() => window.__agentActivity.documentReplies)).toEqual([]);
      expect(await page.evaluate(() => document.activeElement === window.__mapOpener)).toBe(false);
      await decision.getByRole("button", { name: "Cancel", exact: true }).click();
      await page.waitForFunction(() => window.__agentActivity.documentReplies.some(reply => reply.requestId === "map-dirty-quit"));
      expect(await page.evaluate(() => window.__agentActivity.documentReplies.at(-1).decision)).toBe("cancel");
      expect(await page.evaluate(() => window.monaco.editor.getModels().some(model => model.getValue().includes("pending-local")))).toBe(true);
      await noMissing(run);
    } finally { await run.context.close(); }
  }, 40_000);

  it("keeps the counter and dialog on distinct principals and distinguishes unavailable versus empty task inventory", async () => {
    const run = await openPage(); const { page } = run;
    try {
      await counterFor(page).waitFor();
      await page.getByRole("button", { name: "Actions for Checkout review", exact: true }).click();
      await page.getByRole("button", { name: "View map", exact: true }).click();
      const dialog = dialogFor(page); await dialog.waitFor();
      await dialog.getByText("Background task inventory is unavailable.").waitFor();
      expect((await activeInterests(page)).map(x => [x.purpose, x.sessionId]).sort()).toEqual([["counter", "map-claude"], ["dialog", "map-codex"]]);
      expect(new Set((await activeInterests(page)).map(x => x.windowId)).size).toBe(1);
      await page.keyboard.press("Escape"); await dialog.waitFor({ state: "detached" });
      await openCounter(page);
      await emitSnapshot(page, { revision: 4, backgroundTasks: [] });
      await dialog.getByText("No background tasks observed.").waitFor();
      await emitSnapshot(page, { revision: 5, provider: "kimi", status: "unavailable", messageCode: "unsupported-provider", capabilities: { children: false, backgroundTasks: false, liveState: false, duration: false, model: false, tokens: false } });
      await dialog.getByText("Child observation is unavailable for this provider.").waitFor();
      expect(await dialog.locator("[data-node-id]").count()).toBe(1);
      await emitSnapshot(page, { revision: 6, provider: "opencode", messageCode: "unsupported-protocol" });
      await dialog.getByText("This provider protocol does not support activity observation.").waitFor();
      await noMissing(run);
    } finally { await run.context.close(); }
  }, 40_000);

  for (const viewport of [{ width: 1334, height: 871 }, { width: 640, height: 720 }]) it(`keeps the close button visible and content scrollable at ${viewport.width}×${viewport.height}`, async () => {
    const run = await openPage({ viewport }); const { page } = run;
    try {
      await counterFor(page).waitFor();
      const counterBounds = await counterFor(page).evaluate(element => {
        const rect = element.getBoundingClientRect();
        return { top: rect.top, bottom: rect.bottom, height: rect.height, viewportHeight: innerHeight };
      });
      console.info("activity counter viewport bounds", viewport, counterBounds);
      await page.screenshot({ path: path.join(images, `agent-map-counter-${viewport.width}x${viewport.height}.png`), animations: "disabled" });
      expect(counterBounds.top).toBeGreaterThanOrEqual(0);
      expect(counterBounds.bottom).toBeLessThanOrEqual(viewport.height);
      await counterFor(page).evaluate(element => { window.__mapCounter = element; });
      const base = AGENT_MAP_STATE.agentActivity.snapshots["map-claude"];
      const notices = [
        [{ status: "partial", messageCode: "hooks-unobserved" }, "No live hook activity has been observed."],
        [{ status: "partial", messageCode: "truncated", truncated: true }, "Some observations were removed by the retention limit."],
        [{ status: "error", messageCode: "refresh-failed", stale: true }, "Activity could not be refreshed."],
        [{ status: "unavailable", messageCode: "unsupported-provider", capabilities: { ...base.capabilities, children: false } }, "Child observation is unavailable for this provider."],
        [{ status: "loading", messageCode: "none" }, "Loading agent activity"],
      ];
      for (const [index, [patch, text]] of notices.entries()) {
        await emitSnapshot(page, { ...base, ...patch, revision: 4 + index });
        await page.waitForFunction(text => (window.__mapCounter.getAttribute("aria-label") || window.__mapCounter.textContent).includes(text), text);
        const bounds = await page.evaluate(() => {
          const rect = window.__mapCounter.getBoundingClientRect(); return { top: rect.top, bottom: rect.bottom };
        });
        expect(bounds.top).toBeGreaterThanOrEqual(0);
        expect(bounds.bottom).toBeLessThanOrEqual(viewport.height);
      }
      await emitSnapshot(page, { ...base, revision: 9 });
      const dialog = await openCounter(page);
      await page.screenshot({ path: path.join(images, `agent-map-${viewport.width}x${viewport.height}.png`), animations: "disabled" });
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
      const metrics = await dialog.evaluate(element => {
        const rect = element.getBoundingClientRect();
        const close = element.querySelector('[aria-label="Close agent map"]').getBoundingClientRect();
        return { width: rect.width, height: rect.height, closeVisible: close.top >= 0 && close.bottom <= innerHeight && close.right <= innerWidth,
          scrollable: [...element.querySelectorAll("div, section")].some(item => /auto|scroll/.test(getComputedStyle(item).overflowY) && item.scrollHeight > item.clientHeight) };
      });
      expect(metrics.width).toBeLessThanOrEqual(viewport.width * .94 + 1);
      expect(metrics.height).toBeLessThanOrEqual(viewport.height * .8 + 1);
      expect(metrics.closeVisible).toBe(true); expect(metrics.scrollable).toBe(true);
      expect(await dialog.evaluate(element => {
        const scroller = [...element.querySelectorAll("div, section")].find(item => /auto|scroll/.test(getComputedStyle(item).overflowY) && item.scrollHeight > item.clientHeight);
        if (!scroller) return false;
        scroller.scrollTop = 40;
        return scroller.scrollTop > 0;
      })).toBe(true);
      await noMissing(run);
    } finally { await run.context.close(); }
  }, 40_000);
});
