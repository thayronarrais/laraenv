import { afterAll, beforeAll, describe, expect, it } from "vitest";
import path from "node:path";
import { mkdir } from "node:fs/promises";
import { fileURLToPath } from "node:url";
import { chromium } from "playwright";
import { startServer } from "../serve.mjs";
import { buildInitScript } from "../mock/base.mjs";
import { BASE_STATE, mergeState } from "../mock/state.mjs";

const here = path.dirname(fileURLToPath(import.meta.url));
const images = process.env.WORKSPACE_PROOF ? path.resolve(process.env.WORKSPACE_PROOF, "documents") : path.resolve(here, "../../../build/verification/workspace-source-control/task-10/documents");
const rootA = "a".repeat(64), rootB = "b".repeat(64);
const reference = (projectName, rootKey, relativePath) => ({ context: { kind: "project", projectName }, rootKey, relativePath });
const layout = (tabs = [], activeTabIndex = -1) => ({ version: 1, tabs, activeTabIndex, visible: true, panelPercent: 45 });
const tab = (ref, mode = "source") => ({ references: [ref], mode });
const terminalLayout = {
  version: 2, groups: [{ x: 0, y: 0, w: 900, h: 600, snap: "max", activeTabIndex: 0, tabs: [
    { label: "worktree-a", activeLeafIndex: 0, tree: { type: "leaf", label: "worktree-a", kind: "pwsh", spawn: { kind: "project", projectName: "worktree-a", terminalKind: "pwsh" } } },
  ] }],
};
const scenario = (documents = layout()) => mergeState(BASE_STATE, {
  version: "0.5.8", webgl: false, workspace: terminalLayout, documents,
  projects: [{ name: "worktree-a", path: "C:/fake/worktree-a", url: "http://worktree-a.test", proxyPort: 0 }, { name: "worktree-b", path: "C:/fake/worktree-b", url: "http://worktree-b.test", proxyPort: 0 }],
  agents: { enabled: true, profiles: [], sessions: [], usage: [] },
});
const appScript = (state) => buildInitScript({ state }) + `
  localStorage.setItem("laraenv.agentNavigatorVisible", "true");
  if (${JSON.stringify(state.autoSwitch ?? false)}) localStorage.setItem("laraenv.agentAutoSwitch", "true");
  const baseApp = window.go.main.App;
  const keyA = "${rootA}", keyB = "${rootB}";
  const files = {
    "worktree-a/README.md": { canonicalPath: "C:/fake/worktree-a/README.md", text: "# A\\ninitial", revision: "rev-a-1", size: 11, bom: false, eol: "lf", readOnly: false },
    "worktree-b/README.md": { canonicalPath: "C:/fake/worktree-b/README.md", text: "# B\\nother", revision: "rev-b-1", size: 9, bom: false, eol: "lf", readOnly: false },
    "worktree-a/mixed.txt": { canonicalPath: "C:/fake/worktree-a/mixed.txt", text: "mixed\\nline", revision: "rev-m-1", size: 11, bom: false, eol: "mixed", readOnly: true },
  };
  const s = window.__docs = { files, roots: { "worktree-a": keyA, "worktree-b": keyB }, ptySpawns: [], ptyWrites: [], ptyStops: [], focusReports: [], reads: [], checks: [], saves: [], layouts: [], reports: [], replies: [], continuations: [], deferSave: false, releaseSave: null };
  const find = (ctx, root, relative) => {
    if (root !== s.roots[ctx.projectName]) return null;
    return files[ctx.projectName + "/" + relative] || null;
  };
  const unavailable = () => ({ code: "unavailable", message: "Workspace source unavailable" });
  const startProjectPty = (name) => { s.ptySpawns.push(name); return Promise.resolve("pty-" + name); };
  const actions = {
    StartPtyForProject: startProjectPty,
    StartPtyForProjectCommand: startProjectPty,
    WritePty: (id, data) => { s.ptyWrites.push({ id, data }); return Promise.resolve(); },
    StopPty: (id) => { s.ptyStops.push(id); return Promise.resolve(); },
    SetFocusedAgentSession: (id) => { s.focusReports.push(id); return Promise.resolve(); },
    GetWorkspaceFileContext: (ctx) => Promise.resolve({ code: "ok", context: { rootKey: s.roots[ctx.projectName], canonicalRoot: "C:/fake/" + ctx.projectName, label: ctx.projectName, path: "C:/fake/" + ctx.projectName, canRead: true, canWrite: true } }),
    ListWorkspaceDirectory: (ctx, root, relative, cursor) => Promise.resolve(root === s.roots[ctx.projectName] ? {
      code: "ok", directory: { entries: relative || cursor ? [] : Object.keys(files).filter(k => k.startsWith(ctx.projectName + "/")).map(k => ({ name: k.split("/")[1], relativePath: k.split("/")[1], kind: "file" })), nextCursor: "" }
    } : unavailable()),
    ReadWorkspaceDocument: (ctx, root, relative) => {
      s.reads.push({ ctx, root, relative });
      const file = find(ctx, root, relative);
      return Promise.resolve(file ? { code: "ok", document: { ...file } } : unavailable());
    },
    CheckWorkspaceDocument: (ctx, root, relative, revision) => {
      s.checks.push({ ctx, root, relative, revision });
      const file = find(ctx, root, relative);
      return Promise.resolve(file ? (file.revision === revision ? { code: "unchanged" } : { code: "changed", document: { ...file } }) : unavailable());
    },
    SaveWorkspaceDocument: async (ctx, root, relative, content, expectedRevision) => {
      s.saves.push({ ctx, root, relative, content, expectedRevision });
      if (s.deferSave) await new Promise(resolve => { s.releaseSave = resolve; });
      const file = find(ctx, root, relative);
      if (!file) return unavailable();
      if (file.revision !== expectedRevision) return { code: "conflict", message: "Changed on disk", document: { ...file } };
      file.text = content; file.revision = "rev-" + (s.saves.length + 1); file.size = content.length;
      return { code: "ok", document: { ...file } };
    },
    SaveDocumentWorkspace: (raw) => { s.layouts.push(JSON.parse(raw)); return Promise.resolve(); },
    SetDocumentWorkspaceState: (state) => { s.reports.push({ ...state }); return Promise.resolve(); },
    ResolveDocumentShutdown: (reply) => { s.replies.push({ ...reply }); if (reply.decision === "allow" && s.reports.at(-1)?.dirtyCount === 0 && s.reports.at(-1)?.stateVersion === reply.stateVersion) s.continuations.push(reply.requestId); return Promise.resolve({ released: false }); },
    QuitApp: () => { s.continuations.push("quit-direct"); return Promise.resolve(); },
    InstallUpdate: () => { s.continuations.push("update-direct"); return Promise.resolve(); },
  };
  window.go.main.App = new Proxy({}, { get: (_, name) => actions[name] || baseApp[name] });
`;
let browser, server;
beforeAll(async () => { server = await startServer(path.resolve(here, "../../../frontend/dist")); browser = await chromium.launch({ headless: true, ...(process.env.WORKSPACE_CHROMIUM ? { executablePath: process.env.WORKSPACE_CHROMIUM } : {}) }); await mkdir(images, { recursive: true }); }, 30_000);
afterAll(async () => { await browser?.close(); await server?.close(); });

async function openPage(documents = layout(), viewport = { width: 1366, height: 768 }, delayLoader = false, statePatch = {}) {
  const context = await browser.newContext({ viewport });
  const external = [], errors = [];
  let releaseLoader, loaderReached;
  const loaderRequest = new Promise(resolve => { loaderReached = resolve; });
  await context.route("**/*", route => {
    const url = route.request().url();
    if (/^https?:/.test(url) && new URL(url).origin !== new URL(server.url).origin) { external.push(url); return route.abort(); }
    return route.continue();
  });
  if (delayLoader) {
    const held = new Promise(resolve => { releaseLoader = resolve; });
    await context.route("**/vs/loader.js", async route => { loaderReached(); await held; await route.continue().catch(() => {}); });
  }
  await context.addInitScript({ content: appScript(mergeState(scenario(documents), statePatch)) });
  const page = await context.newPage();
  page.setDefaultTimeout(8_000);
  page.on("pageerror", error => errors.push(error.stack || String(error)));
  await page.goto(`${server.url}/index.html`, { waitUntil: "domcontentloaded" });
  try { await page.getByRole("button", { name: "Terminal", exact: true }).click({ timeout: 4000 }); }
  catch (error) { throw new Error(`${error}\nbody=${(await page.locator("body").innerText()).slice(0, 1000)}\nerrors=${errors.join(" | ")}\nmissing=${JSON.stringify(await page.evaluate(() => window.__harness?.missing))}`); }
  await page.locator(".xterm:visible .xterm-rows").waitFor();
  return { page, context, external, errors, releaseLoader, loaderRequest };
}
async function typeInSource(page, value) {
  const surface = page.getByTestId("document-surface");
  await surface.locator(".monaco-editor .view-lines").first().click();
  await page.keyboard.type(value);
}
const counts = page => page.evaluate(() => ({ spawns: window.__docs.ptySpawns.length, writes: [...window.__docs.ptyWrites], saves: [...window.__docs.saves] }));
const noMissing = async ({ page, external, errors }) => {
  expect(external).toEqual([]);
  expect(errors).toEqual([]);
  expect(await page.evaluate(() => window.__harness.missing)).toEqual([]);
};

describe("workspace documents in production Chromium with real Monaco and xterm", () => {
  it("routes editor keys, ownership, conflicts and layouts without restarting PTYs", async () => {
    const a = reference("worktree-a", rootA, "README.md");
    const run = await openPage(layout([tab(a, "preview")], 0));
    const { page, context } = run;
    try {
      const initial = await counts(page);
      await page.getByTestId("document-surface").getByTestId("document-tab-strip").locator('button[title$="README.md"]').first().waitFor();
      await page.getByRole("button", { name: "Source", exact: true }).click();
      const editor = page.locator("[data-testid=document-surface] .monaco-editor textarea.inputarea").first();
      await editor.waitFor();
      await page.locator("[data-testid=document-surface] .monaco-editor .view-lines").first().click();
      expect(await editor.evaluate(input => input === document.activeElement)).toBe(true);
      await page.keyboard.type(" typed");
      await page.keyboard.press("Control+s");
      await page.waitForFunction(() => window.__docs.saves.length === 1);
      expect((await counts(page)).writes).toEqual(initial.writes);
      expect((await counts(page)).saves[0].content).toContain("typed");
      const width = await page.evaluate(() => ({
        available: document.querySelector("[data-testid=workspace-surfaces]").getBoundingClientRect().width,
        source: document.querySelector("[data-testid=document-surface] .monaco-editor").getBoundingClientRect().width,
        panelPercent: document.querySelector('[data-testid=workspace-surfaces] [role=separator][aria-orientation=vertical]').getAttribute("aria-valuenow"),
        overflow: document.documentElement.scrollWidth > innerWidth,
      }));
      console.info("wide source measurements", width);
      expect(width.panelPercent).toBe("45");
      expect(width.available).toBeLessThan(1366);
      expect(width.source).toBeGreaterThanOrEqual(320);
      expect(width.overflow).toBe(false);
      await page.screenshot({ path: path.join(images, "wide-source-1366x768.png") });
      await page.getByRole("button", { name: "Preview", exact: true }).click();
      await page.keyboard.type("blocked");
      expect((await counts(page)).writes).toEqual(initial.writes);
      await page.getByRole('button', { name: 'Hide editor', exact: true }).click();
      await page.getByRole('button', { name: 'Show editor', exact: true }).click();
      expect((await counts(page)).spawns).toBe(initial.spawns);
      await page.screenshot({ path: path.join(images, "wide-preview-1366x768.png") });
      await page.waitForFunction(() => window.__docs.layouts.length > 0);
      expect(JSON.stringify(await page.evaluate(() => window.__docs.layouts))).not.toContain("typed");
      expect(JSON.stringify(await page.evaluate(() => window.__docs.layouts))).not.toContain("revision");
      // A real browser Tab enters xterm while documents still own the
      // workspace. Focusing the header alone is not a terminal claim.
      await page.getByRole('button', { name: 'Hide editor', exact: true }).focus();
      for (let i = 0; i < 25 && !await page.evaluate(() => document.activeElement?.matches(".xterm textarea")); i++) await page.keyboard.press("Tab");
      expect(await page.evaluate(() => document.activeElement?.matches(".xterm textarea"))).toBe(true);
      await page.waitForTimeout(50);
      await page.keyboard.type("tabfrompreview");
      await page.waitForFunction(() => window.__docs.ptyWrites.map(w => w.data).join("").includes("tabfrompreview"));
      const afterTab = (await counts(page)).writes.length;
      await page.getByRole("button", { name: "Preview", exact: true }).click();
      await page.keyboard.type("blocked-again");
      expect((await counts(page)).writes).toHaveLength(afterTab);
      await page.locator(".xterm:visible .xterm-screen").click();
      await page.keyboard.type("echo ok");
      await page.keyboard.press("Enter");
      await page.waitForFunction(() => window.__docs.ptyWrites.some(w => w.data === "\r"));
      expect((await counts(page)).writes.map(w => w.data).join("")).toContain("echo ok\r");
      await noMissing(run);
    } finally { await context.close(); }
  }, 60_000);

  it("restores distinct worktree buffers and keeps last document model clear after delayed loader", async () => {
    const a = reference("worktree-a", rootA, "README.md"), b = reference("worktree-b", rootB, "README.md");
    const run = await openPage(layout([tab(a), tab(b)], 0));
    try {
      await run.page.locator("[data-testid=document-surface] .monaco-editor").waitFor();
      const strip = run.page.getByTestId("document-tab-strip");
      expect(await strip.locator('button[title$="README.md"]').count()).toBe(2);
      const sourceModels = () => run.page.evaluate(() => window.monaco.editor.getModels().filter(m => m.uri.toString().startsWith("inmemory://workspace/")).map(m => ({ uri: m.uri.toString(), text: m.getValue() })));
      const modelText = uri => run.page.evaluate(uri => window.monaco.editor.getModels().find(model => model.uri.toString() === uri)?.getValue(), uri);
      const modelA = (await sourceModels())[0].uri;
      expect(await modelText(modelA)).toBe("# A\ninitial");
      await typeInSource(run.page, " A-only");
      expect(await modelText(modelA)).toContain("A-only");
      await strip.locator('button[title="C:/fake/worktree-b/README.md"]').click();
      await run.page.waitForFunction(() => [...document.querySelectorAll('[data-testid=document-surface] .monaco-editor .view-line')].some(line => line.textContent?.includes('other')));
      const modelB = (await sourceModels()).find(m => m.uri !== modelA).uri;
      expect(await modelText(modelB)).toBe("# B\nother");
      expect(await run.page.locator("[data-testid=document-surface] .monaco-editor .view-lines .view-line").allInnerTexts()).toContain("other");
      await strip.locator('button[title="C:/fake/worktree-a/README.md"]').click();
      expect(await modelText(modelA)).toContain("A-only");
      expect(await modelText(modelB)).toBe("# B\nother");
      const readRoots = await run.page.evaluate(() => window.__docs.reads.map(x => x.root));
      expect(readRoots).toContain(rootA); expect(readRoots).toContain(rootB);
      await noMissing(run);
    } finally { await run.context.close(); }
    const late = await openPage(layout([tab(a)], 0), { width: 900, height: 700 }, true);
    try {
      await late.loaderRequest;
      await late.page.getByRole('button', { name: 'Hide navigator', exact: true }).click();
      const filesTab = late.page.getByTestId("workspace-surfaces").getByRole("tab", { name: "Files" });
      if (await filesTab.count()) await filesTab.click();
      await late.page.getByRole("button", { name: /Close tab README.md/ }).click();
      late.releaseLoader();
      await late.page.waitForTimeout(400);
      const models = await late.page.evaluate(() => window.monaco?.editor.getModels().map(model => model.uri.toString()) || []);
      expect(models.filter(uri => uri.startsWith("inmemory://workspace/"))).toEqual([]);
      expect(await late.page.evaluate(() => window.__docs.ptyWrites)).toEqual([]);
      await noMissing(late);
    } finally { late.releaseLoader(); await late.context.close(); }
  }, 60_000);

  it("shows a diff after a stale save, rejects a second stale merge and keeps mixed EOL read only", async () => {
    const a = reference("worktree-a", rootA, "README.md");
    const mixed = reference("worktree-a", rootA, "mixed.txt");
    const run = await openPage(layout([tab(a), tab(mixed)], 0));
    const { page } = run;
    try {
      await typeInSource(page, " local");
      await page.evaluate(() => { const file = window.__docs.files["worktree-a/README.md"]; file.text = "# A\ndisk one"; file.revision = "rev-a-2"; });
      await page.getByTestId("document-surface").getByRole("button", { name: "Save", exact: true }).click();
      await page.waitForFunction(() => window.__docs.saves.length === 1);
      await page.getByRole("button", { name: "Compare changes" }).click();
      await page.locator("[data-testid=document-surface] .monaco-diff-editor").waitFor();
      const comparison = await page.evaluate(() => ({
        width: document.querySelector("[data-testid=document-surface] .monaco-diff-editor").getBoundingClientRect().width,
        presentation: document.querySelector("[data-testid=document-diff]")?.getAttribute("data-presentation"),
        saveBottom: document.querySelector("[data-testid=document-diff] button").getBoundingClientRect().bottom,
        overflow: document.documentElement.scrollWidth > innerWidth,
      }));
      console.info("wide diff measurements", comparison);
      expect(comparison.width).toBeGreaterThanOrEqual(320);
      expect(comparison.presentation).toBe("inline");
      expect(comparison.saveBottom).toBeLessThanOrEqual(768);
      expect(comparison.overflow).toBe(false);
      await page.screenshot({ path: path.join(images, "wide-diff-1366x768.png") });
      expect(run.errors).toEqual([]);
      await page.evaluate(() => { const file = window.__docs.files["worktree-a/README.md"]; file.text = "# A\ndisk two"; file.revision = "rev-a-3"; });
      await page.getByRole("button", { name: "Save merged text" }).click();
      await page.waitForFunction(() => window.__docs.saves.length === 2);
      expect(run.errors).toEqual([]);
      expect(await page.evaluate(() => window.__docs.files["worktree-a/README.md"].text)).toBe("# A\ndisk two");
      expect(await page.getByRole("button", { name: "Compare changes" }).isVisible()).toBe(true);
      await page.getByTestId("document-tab-strip").locator('button[title$="mixed.txt"]').click();
      expect(run.errors).toEqual([]);
      expect(await page.getByRole("button", { name: "Save", exact: true }).isDisabled()).toBe(true);
      expect(await page.getByText(/Mixed line endings/).count()).toBeGreaterThan(0);
      expect(await page.evaluate(() => window.__docs.ptyWrites)).toEqual([]);
      await noMissing(run);
    } finally { await run.context.close(); }
  }, 60_000);

  it("reveals dirty Preview conflict and unavailability without losing the local text", async () => {
    const run = await openPage(layout([tab(reference("worktree-a", rootA, "README.md"))], 0));
    const { page } = run;
    try {
      await typeInSource(page, " preview-local");
      await page.getByRole("button", { name: "Preview", exact: true }).click();
      await page.evaluate(() => { const file = window.__docs.files["worktree-a/README.md"]; file.text = "disk conflict"; file.revision = "external-r2"; });
      await page.getByText("File changed on disk", { exact: true }).waitFor();
      expect(await page.locator("[data-testid=document-surface] .prose").innerText()).toContain("preview-local");
      await page.getByRole("button", { name: "Compare changes" }).click();
      await page.locator("[data-testid=document-surface] .monaco-diff-editor").waitFor();
      await page.getByRole("button", { name: "Preview", exact: true }).click();
      await page.evaluate(() => { window.__docs.roots["worktree-a"] = "c".repeat(64); });
      await page.getByText("Workspace unavailable", { exact: true }).waitFor();
      expect(await page.locator("[data-testid=document-surface] .prose").innerText()).toContain("preview-local");
      let nativeDialogs = 0; page.on("dialog", dialog => { nativeDialogs++; void dialog.dismiss(); });
      await page.getByRole("button", { name: "Reload", exact: true }).click();
      const dialog = page.getByRole("dialog", { name: "Unsaved documents" }); await dialog.waitFor();
      expect(nativeDialogs).toBe(0); await dialog.getByRole("button", { name: "Cancel", exact: true }).click();
      await noMissing(run);
    } finally { await run.context.close(); }
  }, 60_000);

  it("retains old dirty Monaco generation while a replacement at the same pathname opens independently", async () => {
    const workspace = structuredClone(terminalLayout);
    workspace.groups[0].tabs.push({ label: "worktree-b", activeLeafIndex: 0, tree: { type: "leaf", label: "worktree-b", kind: "pwsh", spawn: { kind: "project", projectName: "worktree-b", terminalKind: "pwsh" } } });
    const run = await openPage(layout([tab(reference("worktree-a", rootA, "README.md"))], 0), { width: 1366, height: 768 }, false, { workspace });
    const { page } = run;
    try {
      await typeInSource(page, " retained-A");
      const oldURI = await page.evaluate(() => window.monaco.editor.getModels().find(m => m.getValue().includes("retained-A")).uri.toString());
      await page.evaluate(() => { const s = window.__docs; s.files["worktree-b/README.md"] = { ...s.files["worktree-a/README.md"] }; s.roots["worktree-a"] = "c".repeat(64); });
      await page.getByTestId("terminal-surface").locator("span.whitespace-nowrap").filter({ hasText: /^worktree-b$/ }).click();
      if(await page.getByRole('button', {name:'Show navigator',exact:true}).isVisible()) await page.getByRole('button', {name:'Show navigator',exact:true}).click();
      await page.getByRole('tablist', {name:'Workspace navigation',exact:true}).getByRole('tab', {name:'Files',exact:true}).click();
      await page.getByRole('tabpanel', { name: 'Files', exact: true }).getByRole("button", { name: "README.md", exact: true }).click();
      const tabs = page.getByTestId("document-tab-strip").locator('button[title="C:/fake/worktree-a/README.md"]');
      await page.waitForFunction(() => document.querySelectorAll('[data-testid=document-tab-strip] button[title="C:/fake/worktree-a/README.md"]').length === 2);
      await page.getByRole("button", { name: "Source", exact: true }).click();
      await page.waitForFunction(() => window.monaco.editor.getModels().filter(m => m.uri.toString().startsWith("inmemory://workspace/")).length === 2);
      const newURI = await page.evaluate(oldURI => window.monaco.editor.getModels().find(m => m.uri.toString() !== oldURI && m.uri.toString().startsWith("inmemory://workspace/")).uri.toString(), oldURI);
      expect(newURI).not.toBe(oldURI);
      expect(await page.evaluate(uri => window.monaco.editor.getModels().find(m => m.uri.toString() === uri).getValue(), newURI)).toBe("# A\ninitial");
      await typeInSource(page, " new-B");
      await page.getByRole("button", { name: "Save", exact: true }).click();
      await page.waitForFunction(() => window.__docs.saves.length === 1);
      expect((await counts(page)).saves[0].root).toBe(rootB);
      await tabs.first().click();
      await page.getByText("Workspace unavailable", { exact: true }).waitFor();
      expect(await page.evaluate(uri => window.monaco.editor.getModels().find(m => m.uri.toString() === uri).getValue(), oldURI)).toContain("retained-A");
      await page.getByRole("button", { name: "Save", exact: true }).click();
      expect((await counts(page)).saves).toHaveLength(1);
      await tabs.last().click();
      await page.getByRole("button", { name: "Close tab README.md", exact: true }).last().click();
      expect(await page.evaluate(uri => window.monaco.editor.getModels().some(m => m.uri.toString() === uri), newURI)).toBe(false);
      expect(await page.evaluate(uri => window.monaco.editor.getModels().find(m => m.uri.toString() === uri)?.getValue(), oldURI)).toContain("retained-A");
      await noMissing(run);
    } finally { await run.context.close(); }
  }, 60_000);

  it("rechecks Save all after an edit during the pending save and cancels update continuation", async () => {
    const a = reference("worktree-a", rootA, "README.md");
    const run = await openPage(layout([tab(a)], 0));
    const { page } = run;
    try {
      await typeInSource(page, " first");
      await page.evaluate(() => { window.__docs.deferSave = true; window.__harness.emit("documents:shutdown-request", { requestId: "update-1", intent: "update" }); });
      const dialog = page.getByRole("dialog", { name: "Unsaved documents" });
      await dialog.waitFor();
      await dialog.getByRole("button", { name: "Save all" }).click();
      await page.waitForFunction(() => !!window.__docs.releaseSave);
      await typeInSource(page, " second");
      await page.evaluate(() => { window.__docs.deferSave = false; window.__docs.releaseSave(); });
      await dialog.waitFor({ state: "visible" });
      await dialog.getByRole("button", { name: "Cancel" }).click();
      await page.waitForFunction(() => window.__docs.replies.some(r => r.requestId === "update-1"));
      const gate = await page.evaluate(() => ({ saves: window.__docs.saves, replies: window.__docs.replies, continuations: window.__docs.continuations, reports: window.__docs.reports }));
      expect(gate.saves).toHaveLength(1);
      expect(gate.replies.at(-1).decision).toBe("cancel");
      expect(gate.continuations).toEqual([]);
      expect(gate.reports.at(-1).dirtyCount).toBeGreaterThan(0);
      await noMissing(run);
    } finally { await run.context.close(); }
  }, 60_000);

  it("keeps both mounts in compact view and transfers ownership by real Tab navigation", async () => {
    const a = reference("worktree-a", rootA, "README.md");
    const run = await openPage(layout([tab(a, "preview")], 0), { width: 900, height: 768 });
    const { page } = run;
    try {
      const surfaces = page.getByTestId("workspace-surfaces");
      const available = await surfaces.evaluate(element => element.getBoundingClientRect().width);
      console.info("narrow workspace available width", available);
      expect(available).toBeLessThan(720);
      const initialSpawns = (await counts(page)).spawns;
      await page.getByRole('button', { name: 'Hide navigator', exact: true }).click();
      await surfaces.getByRole("tab", { name: "Files" }).click();
      await page.getByRole("button", { name: "Preview", exact: true }).click();
      await page.keyboard.type("no-terminal-write");
      expect((await counts(page)).writes).toEqual([]);
      await page.getByRole("button", { name: "Source", exact: true }).click();
      await page.locator("[data-testid=document-surface] .monaco-editor .view-lines").waitFor();
      const narrow = await page.evaluate(() => ({
        source: document.querySelector("[data-testid=document-surface] .monaco-editor").getBoundingClientRect().width,
        overflow: document.documentElement.scrollWidth > innerWidth,
      }));
      console.info("narrow source measurements", narrow);
      expect(narrow.source).toBeGreaterThanOrEqual(360);
      expect(narrow.overflow).toBe(false);
      await page.screenshot({ path: path.join(images, "narrow-source-900x768.png") });
      if(await page.getByRole('button', {name:'Show navigator',exact:true}).isVisible()) await page.getByRole('button', {name:'Show navigator',exact:true}).click();
      await page.getByRole('tablist', {name:'Workspace navigation',exact:true}).getByRole('tab', {name:'Files',exact:true}).click();
      await page.getByRole('tabpanel', { name: 'Files', exact: true }).getByRole("button", { name: "README.md", exact: true }).waitFor();
      await page.getByRole('button', { name: 'Hide navigator', exact: true }).click();
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
      expect(await surfaces.getByTestId("terminal-surface").count()).toBe(1);
      await surfaces.getByRole("tab", { name: "Terminal" }).click();
      await surfaces.getByRole("tab", { name: "Files" }).click();
      expect((await counts(page)).spawns).toBe(initialSpawns);
      await surfaces.getByRole("tab", { name: "Terminal" }).click();
      await surfaces.getByRole('tab', { name: 'Terminal', exact: true }).focus();
      for (let i = 0; i < 25 && !await page.evaluate(() => document.activeElement?.matches(".xterm textarea")); i++) await page.keyboard.press("Tab");
      expect(await page.evaluate(() => document.activeElement?.matches(".xterm textarea"))).toBe(true);
      await page.waitForTimeout(50);
      await page.keyboard.type("tabok");
      await page.waitForTimeout(100);
      const tabState = await page.evaluate(() => ({ writes: window.__docs.ptyWrites, active: document.activeElement?.outerHTML, terminalDisplay: document.querySelector('[data-testid="terminal-surface"]')?.getAttribute("style"), filesDisplay: document.querySelector('[data-testid="document-surface"]')?.getAttribute("style") }));
      expect(tabState.writes.map(w => w.data).join("").includes("tabok"), JSON.stringify(tabState)).toBe(true);
      await noMissing(run);
    } finally { await run.context.close(); }
  }, 60_000);

  it("retains an unsaved document after its last PTY exits", async () => {
    const a = reference("worktree-a", rootA, "README.md");
    const run = await openPage(layout([tab(a)], 0));
    try {
      await typeInSource(run.page, " survives");
      await run.page.evaluate(() => window.__harness.emit("term:exit:pty-worktree-a", "done"));
      await run.page.waitForFunction(() => document.querySelectorAll(".xterm").length === 0);
      expect(await run.page.locator(".xterm").count()).toBe(0);
      await run.page.getByTestId("document-surface").getByRole("button", { name: /README.md •/ }).waitFor();
      expect(await run.page.evaluate(() => window.monaco.editor.getModels().some(model => model.getValue().includes("survives")))).toBe(true);
      expect(await run.page.getByTestId("document-surface").isVisible()).toBe(true);
      await noMissing(run);
    } finally { await run.context.close(); }
  }, 60_000);

  it("holds agent auto-switch while Preview and the close dialog own focus", async () => {
    const a = reference("worktree-a", rootA, "README.md");
    const workspace = structuredClone(terminalLayout);
    workspace.groups[0].tabs.push({ label: "Agent B", activeLeafIndex: 0, tree: { type: "leaf", label: "Agent B", kind: "agent:codex", spawn: { kind: "agent", agentId: "codex", projectName: "worktree-a", laraSessionId: "agent-b" } } });
    const agent = { id: "agent-b", title: "Agent B", projectName: "worktree-a", projectPath: "C:/fake/worktree-a", agentId: "codex", launchMode: "current", terminalId: "pty-agent-b", providerSessionId: "", state: "running", updatedAt: "2026-10-08T12:00:00Z", unread: false, archived: false };
    const run = await openPage(layout([tab(a, "preview")], 0), { width: 1366, height: 768 }, false, {
      workspace, autoSwitch: true, agents: { enabled: true, profiles: [{ id: "codex", label: "Codex", available: true, capabilities: { launch: true, resume: true, richStatus: true, history: true, usage: true } }], sessions: [agent], usage: [] },
    });
    const { page } = run;
    const agentTabClass = () => page.getByTestId("terminal-surface").locator("span.whitespace-nowrap").filter({ hasText: /^Agent B$/ }).evaluate(el => el.parentElement.className);
    try {
      await page.getByRole("button", { name: "Preview", exact: true }).click();
      await page.evaluate(() => window.__harness.emit("agent:session-changed", { id: "agent-b", state: "needs-input", terminalId: "pty-agent-b" }));
      await page.waitForTimeout(1100);
      expect(await agentTabClass()).toContain("text-white/55");
      await page.getByRole("button", { name: "Source", exact: true }).click();
      await typeInSource(page, " pending");
      await page.evaluate(() => window.__harness.emit("documents:shutdown-request", { requestId: "quit-1", intent: "quit" }));
      const dialog = page.getByRole("dialog", { name: "Unsaved documents" });
      await dialog.waitFor();
      await page.evaluate(() => window.__harness.emit("agent:session-changed", { id: "agent-b", state: "needs-input", terminalId: "pty-agent-b" }));
      await page.waitForTimeout(600);
      expect(await agentTabClass()).toContain("text-white/55");
      expect(await page.evaluate(() => window.__docs.continuations)).toEqual([]);
      await dialog.getByRole("button", { name: "Cancel" }).click();
      await page.waitForFunction(() => window.__docs.replies.some(reply => reply.requestId === "quit-1"));
      await page.locator(".xterm:visible .xterm-screen").click();
      await page.waitForFunction(() => {
        const span = [...document.querySelectorAll('[data-testid="terminal-surface"] span.whitespace-nowrap')].find(el => el.textContent === "Agent B");
        return span?.parentElement?.className.includes("bg-[#171717]");
      }, null, { timeout: 5000 });
      await noMissing(run);
    } finally { await run.context.close(); }
  }, 60_000);
});
