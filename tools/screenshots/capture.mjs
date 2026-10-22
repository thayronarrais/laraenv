import { writeFile, mkdir } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";
import { chromium } from "playwright";
import { buildInitScript } from "./mock/base.mjs";
import { BASE_STATE, mergeState, buildMetricsSeries } from "./mock/state.mjs";
import { startServer } from "./serve.mjs";
import { composeFrame } from "./frame/compose.mjs";
import { SCENARIOS, DOC_SCENARIOS, getScenario } from "./mock/scenarios.mjs";

// Matches the framed area of the banners currently on the site.
export const VIEWPORT = { width: 1334, height: 871 };

const READY_TIMEOUT_MS = 15_000;

export class CaptureError extends Error {
  constructor(scenarioId, reason, detail) {
    super(`scenario "${scenarioId}" failed: ${reason}${detail ? ` — ${detail}` : ""}`);
    this.name = "CaptureError";
    this.scenarioId = scenarioId;
    this.reason = reason;
  }
}

export async function captureRaw({ browser, baseUrl, scenario }) {
  const context = await browser.newContext({
    viewport: VIEWPORT,
    deviceScaleFactor: 2,
    locale: "en-US",
    timezoneId: "UTC",
    colorScheme: "dark",
    reducedMotion: "reduce", // no half-finished transitions in the capture
  });

  const pageErrors = [];

  try {
    const state = mergeState(BASE_STATE, scenario.state);
    await context.addInitScript({
      content: buildInitScript({ state, theme: scenario.theme }),
    });

    const page = await context.newPage();
    page.on("pageerror", (e) => pageErrors.push(String(e)));
    page.on("console", (m) => { if (m.type() === "error") pageErrors.push(m.text()); });

    await page.goto(`${baseUrl}/index.html`, { waitUntil: "domcontentloaded" });

    await scenario.navigate(page);

    // Fill the sparkline history. SystemMetrics keeps 32 samples and draws
    // them as a line; with the single sample GetSystemMetrics returns, the
    // chart renders flat.
    //
    // Order matters. The component pushes its mount sample asynchronously
    // (GetSystemMetrics().then(push), SystemMetrics.tsx:34) while these
    // samples arrive synchronously. If the mount sample lands last it evicts
    // the oldest of ours and the sparkline shifts by one — which made the
    // output non-reproducible under parallel load. Sparkline only emits
    // <defs> once it has data, so waiting for that orders the two.
    if (scenario.seedMetrics !== false) {
      try {
        await page.locator("svg > defs").first()
          .waitFor({ state: "attached", timeout: 10_000 });
      } catch {
        throw new CaptureError(scenario.id, "metrics never rendered a first sample",
          "set seedMetrics:false if this scenario shows no metrics card");
      }
      await page.evaluate((series) => {
        for (const s of series) window.__harness.emit("sys:metrics", s);
      }, buildMetricsSeries(state.metrics));
    }

    try {
      await page.getByText(scenario.ready, { exact: false })
        .first()
        .waitFor({ state: "visible", timeout: READY_TIMEOUT_MS });
    } catch {
      throw new CaptureError(scenario.id, "readiness string never became visible",
        JSON.stringify(scenario.ready));
    }

    if (scenario.verify) await scenario.verify(page);

    // The ErrorBoundary renders instead of the app when a render throws.
    if (await page.getByText("Something went wrong").count() > 0) {
      throw new CaptureError(scenario.id, "the app's ErrorBoundary rendered",
        pageErrors[0] || "no console detail");
    }

    const missing = await page.evaluate(() => window.__harness.missing);
    if (missing.length > 0) {
      throw new CaptureError(scenario.id, "unmocked bindings were called", missing.join(", "));
    }

    if (pageErrors.length > 0) {
      throw new CaptureError(scenario.id, "the page reported errors", pageErrors.join(" | "));
    }

    // Fonts must be resolved or glyphs shift between runs.
    await page.evaluate(() => document.fonts.ready);

    // animations:"disabled" finishes in-flight CSS transitions and animations
    // before the shot. The metric bars use Tailwind's transition-all
    // (frontend/src/components/SystemMetrics.tsx:102), which is not gated on
    // prefers-reduced-motion, so without this the bar widths depend on how
    // busy the machine was — the capture stops being byte-reproducible.
    return await page.screenshot({ type: "png", animations: "disabled" });
  } finally {
    await context.close();
  }
}

export async function runScenario({ browser, baseUrl, scenario, outDir }) {
  const raw = await captureRaw({ browser, baseUrl, scenario });
  const framed = scenario.framed === false ? raw : await composeFrame({ browser, pngBuffer: raw });
  await mkdir(outDir, { recursive: true });
  if (scenario.docsOut) {
    await writeFile(path.join(outDir, scenario.docsOut), raw);
  }
  const target = path.join(outDir, scenario.out);
  await writeFile(target, framed);
  return target;
}

function parseArgs(argv) {
  const args = { only: null, out: null };
  for (const a of argv) {
    if (a.startsWith("--only=")) args.only = a.slice("--only=".length);
    else if (a.startsWith("--out=")) args.out = a.slice("--out=".length);
    else throw new Error(`unknown argument: ${a}`);
  }
  return args;
}

async function main() {
  const here = path.dirname(fileURLToPath(import.meta.url));
  const args = parseArgs(process.argv.slice(2));
  const outDir = args.out ? path.resolve(args.out) : path.join(here, "out");
  const dist = path.resolve(here, "../../frontend/dist");

  let chosen = [...SCENARIOS, ...DOC_SCENARIOS];
  if (args.only) {
    const one = getScenario(args.only);
    if (!one) {
      console.error(`unknown scenario "${args.only}" — known: ${chosen.map((s) => s.id).join(", ")}`);
      process.exit(2);
    }
    chosen = [one];
  }

  const browser = await chromium.launch({ channel: "chrome" });
  const server = await startServer(dist);
  const failures = [];

  try {
    for (const scenario of chosen) {
      try {
        const written = await runScenario({ browser, baseUrl: server.url, scenario, outDir });
        console.log(`ok   ${scenario.id.padEnd(12)} -> ${written}`);
      } catch (err) {
        failures.push(scenario.id);
        console.error(`FAIL ${scenario.id.padEnd(12)} ${err.message}`);
      }
    }
  } finally {
    await server.close();
    await browser.close();
  }

  if (failures.length > 0) {
    console.error(`
${failures.length} scenario(s) failed: ${failures.join(", ")}`);
    process.exit(1);
  }
  console.log(`
${chosen.length} capture scenario(s) written to ${outDir}`);
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  main();
}
