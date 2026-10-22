import { describe, it, expect, beforeAll, afterAll } from "vitest";
import { chromium } from "playwright";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { startServer } from "../serve.mjs";
import { captureRaw, CaptureError } from "../capture.mjs";
import { getScenario } from "../mock/scenarios.mjs";

const here = path.dirname(fileURLToPath(import.meta.url));
const DIST = path.resolve(here, "../../../frontend/dist");

let browser, server;

beforeAll(async () => {
  browser = await chromium.launch({ channel: "chrome" });
  server = await startServer(DIST);
}, 120_000);

afterAll(async () => {
  await server?.close();
  await browser?.close();
});

describe("captureRaw failure behaviour", () => {
  it("fails and names the binding when the mock is incomplete", async () => {
    // AppVersion is deliberately blanked so the Proxy fallback records it.
    const broken = { ...getScenario("dashboard"), state: { __dropBinding: "AppVersion" } };
    await expect(captureRaw({ browser, baseUrl: server.url, scenario: broken }))
      .rejects.toThrow(/AppVersion/);
  }, 120_000);

  it("fails when the readiness string never appears", async () => {
    const broken = { ...getScenario("dashboard"), ready: "this string is never rendered" };
    await expect(captureRaw({ browser, baseUrl: server.url, scenario: broken }))
      .rejects.toBeInstanceOf(CaptureError);
  }, 120_000);
});
