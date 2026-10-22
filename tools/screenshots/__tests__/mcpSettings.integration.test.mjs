import { afterAll, beforeAll, describe, expect, it } from "vitest";
import path from "node:path";
import { mkdir } from "node:fs/promises";
import { fileURLToPath } from "node:url";
import { chromium } from "playwright";
import { startServer } from "../serve.mjs";
import { buildInitScript } from "../mock/base.mjs";
import { BASE_STATE, mergeState } from "../mock/state.mjs";

const here = path.dirname(fileURLToPath(import.meta.url));
const privateToken = "PRIVATE_MCP_TOKEN";
const privateEndpoint = "http://127.0.0.1:49876/mcp";
const privateContents = "PRIVATE_CLIENT_CONFIGURATION_CONTENTS";
const clients = [
  { id: "claude", label: "Claude Code", path: "C:\\Users\\demo\\.claude.json", registered: false, restartRequired: false },
  { id: "codex", label: "Codex", path: "C:\\Users\\demo\\.codex\\config.toml", registered: false, restartRequired: false },
  { id: "cursor", label: "Cursor", path: "C:\\Users\\demo\\.cursor\\mcp.json", registered: true, restartRequired: false },
];
let browser;
let server;

beforeAll(async () => {
  server = await startServer(path.resolve(here, "../../../frontend/dist"));
  browser = await chromium.launch({ headless: true });
}, 30_000);
afterAll(async () => { await browser?.close(); await server?.close(); });

async function openSettings({ failLoad = false } = {}) {
  const context = await browser.newContext({ viewport: { width: 1334, height: 871 }, timezoneId: "UTC" });
  const state = mergeState(BASE_STATE, { webgl: false });
  await context.addInitScript({ content: buildInitScript({ state }) + `
    window.__mcpSettings = {
      calls: { status: 0, enable: [], register: [] },
      failLoad: ${JSON.stringify(failLoad)}, failEnable: false, failRegister: false,
      status: {
        enabled: false, running: false,
        clients: ${JSON.stringify(clients)}, activity: [],
        token: ${JSON.stringify(privateToken)},
        endpoint: ${JSON.stringify(privateEndpoint)},
        configContents: ${JSON.stringify(privateContents)},
      },
    };
    const mock = window.__mcpSettings;
    const privateError = () => new Error(${JSON.stringify(`${privateToken} ${privateEndpoint} ${privateContents}`)});
    const baseApp = window.go.main.App;
    const actions = {
      GetLanguage: () => Promise.resolve("pt-BR"),
      GetAgentHooksEnabled: () => Promise.resolve(false),
      GetAgentHooksStatus: () => Promise.resolve([]),
      GetMCPStatus: () => {
        mock.calls.status++;
        return mock.failLoad ? Promise.reject(privateError()) : Promise.resolve(mock.status);
      },
      SetMCPEnabled: (enabled) => {
        mock.calls.enable.push(enabled);
        if (mock.failEnable) return Promise.reject(privateError());
        mock.status = { ...mock.status, enabled, running: enabled };
        return Promise.resolve(mock.status);
      },
      RegisterMCPClient: (id) => {
        mock.calls.register.push(id);
        if (mock.failRegister) return Promise.reject(privateError());
        const client = { ...mock.status.clients.find((item) => item.id === id), registered: true, restartRequired: true };
        mock.status.clients = mock.status.clients.map((item) => item.id === id ? client : item);
        return Promise.resolve(client);
      },
    };
    window.go.main.App = new Proxy({}, { get: (_, name) => actions[name] || baseApp[name] });
  ` });
  const page = await context.newPage();
  page.setDefaultTimeout(8_000);
  const errors = [];
  page.on("pageerror", (error) => errors.push(String(error)));
  await page.goto(`${server.url}/index.html`, { waitUntil: "domcontentloaded" });
  await page.getByRole("button", { name: "Configurações", exact: true }).click();
  await page.getByRole("button", { name: "Experimental", exact: true }).click();
  const card = page.locator("section").filter({ has: page.getByText("Conectar assistentes ao LaraEnv", { exact: true }) });
  await card.waitFor({ state: "visible" });
  return { context, page, card, errors };
}

async function expectSafeDOM(page) {
  const text = await page.locator("body").innerText();
  expect(text).not.toContain(privateToken);
  expect(text).not.toContain(privateEndpoint);
  expect(text).not.toContain(privateContents);
}

describe("MCP Settings in the compiled app", () => {
  it("enables MCP, registers only the chosen client, and receives safe activity updates", async () => {
    const { context, page, card, errors } = await openSettings();
    try {
      const toggle = card.getByRole("switch", { name: "Habilitar MCP local" });
      await page.waitForFunction(() => window.__mcpSettings.calls.status === 1);
      expect(await toggle.getAttribute("aria-checked")).toBe("false");
      expect(await card.getByRole("button", { name: "Registrar Codex" }).isDisabled()).toBe(true);
      expect(await page.evaluate(() => window.__mcpSettings.calls)).toEqual({ status: 1, enable: [], register: [] });

      await toggle.click();
      await card.getByRole("status").getByText("Pronto", { exact: true }).waitFor();
      expect(await toggle.getAttribute("aria-checked")).toBe("true");
      await card.getByRole("button", { name: "Registrar Codex" }).click();
      await card.getByRole("button", { name: "Atualizar registro de Codex" }).waitFor();
      await card.getByText("Reabra Codex para aplicar o registro.", { exact: true }).waitFor();
      expect(await card.getByRole("button", { name: "Registrar Claude Code" }).count()).toBe(1);
      expect(await card.getByRole("button", { name: "Atualizar registro de Cursor" }).count()).toBe(1);
      expect(await card.getByText(clients[1].path, { exact: true }).count()).toBe(1);
      expect(await page.evaluate(() => window.__mcpSettings.calls)).toEqual({ status: 1, enable: [true], register: ["codex"] });

      await page.evaluate(({ privateToken, privateContents }) => {
        window.__harness.emit("mcp:activity", { id: "project-list", time: "2026-10-06T09:40:00Z", group: "project", action: "list", target: "", success: true, durationMs: 12, output: privateContents });
        window.__harness.emit("mcp:activity", { id: "service-status", time: "2026-10-06T09:41:00Z", group: "service", action: "status", target: "nginx", success: true, durationMs: 8, token: privateToken });
        window.__harness.emit("mcp:activity", { id: "worker-start", time: "2026-10-06T09:42:00Z", group: "worker", action: "start", target: "demo-app", success: false, durationMs: 21, error: privateToken });
      }, { privateToken, privateContents });
      const activity = card.getByRole("list", { name: "Atividade recente do MCP" });
      await activity.getByText("worker · start", { exact: true }).waitFor();
      expect(await activity.locator("li").count()).toBe(3);
      expect(await activity.locator("li").first().innerText()).toContain("demo-app");
      expect(await activity.locator("li").first().innerText()).toContain("Falhou");
      await expectSafeDOM(page);

      // Remove transient confirmation to keep the review image deterministic.
      await page.getByText("Codex registrado", { exact: true }).waitFor({ state: "hidden" });
      await card.scrollIntoViewIfNeeded();
      const previewPath = path.resolve(here, "../../../build/verification/mcp-settings.png");
      await mkdir(path.dirname(previewPath), { recursive: true });
      await page.screenshot({ path: previewPath, animations: "disabled" });
      const box = await card.boundingBox();
      expect(box.y).toBeGreaterThanOrEqual(0);
      expect(box.y + box.height).toBeLessThanOrEqual(871);

      await page.evaluate(() => window.__harness.emit("mcp:status-changed", { ...window.__mcpSettings.status, enabled: true, running: false }));
      await card.getByRole("status").getByText("Indisponível", { exact: true }).waitFor();
      expect(await card.getByRole("button", { name: "Atualizar registro de Codex" }).isDisabled()).toBe(true);
      expect(errors).toEqual([]);
      expect(await page.evaluate(() => window.__harness.missing)).toEqual([]);
    } finally { await context.close(); }
  }, 30_000);

  it("handles load, enable, and registration failures without exposing private transport details", async () => {
    const { context, page, card, errors } = await openSettings({ failLoad: true });
    try {
      await card.getByRole("alert").getByText("Não foi possível verificar o estado do MCP. Tente novamente.", { exact: true }).waitFor();
      const toggle = card.getByRole("switch", { name: "Habilitar MCP local" });
      expect(await toggle.isDisabled()).toBe(true);
      expect(await page.evaluate(() => window.__mcpSettings.calls)).toEqual({ status: 1, enable: [], register: [] });
      await expectSafeDOM(page);

      await page.evaluate(() => { window.__mcpSettings.failLoad = false; window.__mcpSettings.failEnable = true; });
      await card.getByRole("button", { name: "Tentar novamente", exact: true }).click();
      await card.getByRole("status").getByText("Desativado", { exact: true }).waitFor();
      await toggle.click();
      await card.getByRole("alert").getByText("Não foi possível alterar o acesso ao MCP local. Tente novamente.", { exact: true }).waitFor();
      expect(await toggle.getAttribute("aria-checked")).toBe("false");
      await expectSafeDOM(page);

      await page.evaluate(() => { window.__mcpSettings.failEnable = false; window.__mcpSettings.failRegister = true; });
      await toggle.click();
      await card.getByRole("status").getByText("Pronto", { exact: true }).waitFor();
      await card.getByRole("button", { name: "Registrar Codex" }).click();
      await card.getByRole("alert").getByText("Não foi possível registrar Codex. Confira a configuração e tente novamente.", { exact: true }).waitFor();
      expect(await card.getByRole("button", { name: "Registrar Codex" }).isEnabled()).toBe(true);
      expect(await card.getByRole("button", { name: "Atualizar registro de Cursor" }).isEnabled()).toBe(true);
      expect(await page.evaluate(() => window.__mcpSettings.calls)).toEqual({ status: 2, enable: [true, true], register: ["codex"] });
      await expectSafeDOM(page);
      expect(errors).toEqual([]);
      expect(await page.evaluate(() => window.__harness.missing)).toEqual([]);
    } finally { await context.close(); }
  }, 30_000);
});
