import { afterAll, beforeAll, expect, it } from "vitest";
import { readFile } from "node:fs/promises";
import { createHash } from "node:crypto";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { chromium } from "playwright";
import { startServer } from "../serve.mjs";
import { buildInitScript } from "../mock/base.mjs";
import { BASE_STATE, mergeState } from "../mock/state.mjs";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "../../..");
let browser, server;
beforeAll(async () => {
  server = await startServer(path.join(root, "frontend/dist"));
  browser = await chromium.launch({ headless: true });
}, 30_000);
afterAll(async () => { await browser?.close(); await server?.close(); }, 30_000);

it("reads complete bundled licenses on demand with external traffic blocked", async () => {
  const context = await browser.newContext({ viewport: { width: 1334, height: 871 } });
  try {
    await context.addInitScript({ content: buildInitScript({ state: mergeState(BASE_STATE, { cloud: { enabled: false, loggedIn: false } }) }) });
    const external = [], legalRequests = [], errors = [];
    await context.route("**/*", route => {
      if (!route.request().url().startsWith(server.url + "/")) { external.push(route.request().url()); return route.abort(); }
      return route.continue();
    });
    const page = await context.newPage();
    page.on("pageerror", error => errors.push(String(error)));
    page.on("request", request => { if (new URL(request.url()).pathname.startsWith("/legal/")) legalRequests.push(new URL(request.url()).pathname); });
    await page.goto(server.url);
    await page.getByRole("button", { name: "Settings", exact: true }).click();
    const opener = page.getByRole("button", { name: "View licenses", exact: true });
    await opener.waitFor();
    expect(legalRequests).toEqual([]);
    await opener.click();
    const dialog = page.getByRole("dialog", { name: "Licenses and acknowledgements", exact: true });
    const texts = dialog.locator("pre");
    await texts.first().waitFor();
    const digest = value => createHash("sha256").update(value).digest("hex");
    // Compare complete payloads without dumping a megabyte of upstream terms
    // into the test log if a stale production bundle is accidentally used.
    expect((await texts.allTextContents()).map(digest)).toEqual(([
      await readFile(path.join(root, "LICENSE"), "utf8"),
      await readFile(path.join(root, "THIRD_PARTY_NOTICES.txt"), "utf8"),
    ]).map(digest));
    expect(legalRequests.sort()).toEqual(["/legal/LICENSE.txt", "/legal/THIRD_PARTY_NOTICES.txt"]);
    expect(external).toEqual([]);
    expect(errors).toEqual([]);
    expect(await page.evaluate(() => window.__harness.missing)).toEqual([]);
    const evidenceDir = process.env.LARAENV_LEGAL_VERIFICATION_DIR || path.join(root, "build/verification/opensource-licensing-20261021");
    await page.screenshot({ path: path.join(evidenceDir, "legal-notices.png"), animations: "disabled" });
    await page.keyboard.press("Escape");
    await dialog.waitFor({ state: "hidden" });
    expect(await opener.evaluate(element => document.activeElement === element)).toBe(true);
  } finally { await context.close(); }
}, 30_000);
