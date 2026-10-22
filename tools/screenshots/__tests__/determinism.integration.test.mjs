import { describe, it, expect, beforeAll, afterAll } from "vitest";
import { chromium } from "playwright";
import { mkdtemp, readFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { startServer } from "../serve.mjs";
import { runScenario } from "../capture.mjs";
import { getScenario } from "../mock/scenarios.mjs";

const here = path.dirname(fileURLToPath(import.meta.url));
const DIST = path.resolve(here, "../../../frontend/dist");

let browser, server, outDir;

beforeAll(async () => {
  browser = await chromium.launch({ channel: "chrome" });
  server = await startServer(DIST);
  outDir = await mkdtemp(path.join(tmpdir(), "shots-out-"));
}, 120_000);

afterAll(async () => {
  await server?.close();
  await browser?.close();
});

describe("runScenario", () => {
  it("writes the scenario's output file", async () => {
    const written = await runScenario({
      browser, baseUrl: server.url, scenario: getScenario("dashboard"), outDir,
    });
    expect(path.basename(written)).toBe("baner1.png");
    expect((await readFile(written)).length).toBeGreaterThan(10_000);
  }, 180_000);

  it("produces byte-identical output on a second run", async () => {
    const scenario = getScenario("dashboard");
    const a = await readFile(await runScenario({ browser, baseUrl: server.url, scenario, outDir }));
    const b = await readFile(await runScenario({ browser, baseUrl: server.url, scenario, outDir }));
    expect(a.equals(b)).toBe(true);
  }, 240_000);
});
