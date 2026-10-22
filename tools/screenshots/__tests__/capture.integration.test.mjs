import { describe, it, expect, beforeAll, afterAll } from "vitest";
import { chromium } from "playwright";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { existsSync } from "node:fs";
import { startServer } from "../serve.mjs";
import { captureRaw, VIEWPORT } from "../capture.mjs";
import { DOC_SCENARIOS, getScenario } from "../mock/scenarios.mjs";

const here = path.dirname(fileURLToPath(import.meta.url));
const DIST = path.resolve(here, "../../../frontend/dist");

let browser, server;

beforeAll(async () => {
  if (!existsSync(path.join(DIST, "index.html"))) {
    throw new Error(`frontend/dist not built — run "npm run build" in frontend/ first`);
  }
  browser = await chromium.launch({ channel: "chrome" });
  server = await startServer(DIST);
}, 120_000);

afterAll(async () => {
  await server?.close();
  await browser?.close();
});

describe("captureRaw", () => {
  it("produces a PNG of the dashboard at the declared viewport", async () => {
    const png = await captureRaw({ browser, baseUrl: server.url, scenario: getScenario("dashboard") });

    expect(png.subarray(0, 8)).toEqual(Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]));
    // PNG header: width and height are big-endian uint32 at bytes 16 and 20.
    // deviceScaleFactor 2 doubles both.
    expect(png.readUInt32BE(16)).toBe(VIEWPORT.width * 2);
    expect(png.readUInt32BE(20)).toBe(VIEWPORT.height * 2);
  }, 120_000);

  it.each(DOC_SCENARIOS.map(s => [s.id]))("captures the release workflow %s with its readiness and UI proofs", async id => {
    const png = await captureRaw({ browser, baseUrl: server.url, scenario: getScenario(id) });
    expect(png.readUInt32BE(16)).toBe(VIEWPORT.width * 2);
    expect(png.readUInt32BE(20)).toBe(VIEWPORT.height * 2);
  }, 120_000);
});
