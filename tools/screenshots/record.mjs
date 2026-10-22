import { writeFile, mkdir, rm } from "node:fs/promises";
import { spawn } from "node:child_process";
import path from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";
import { chromium } from "playwright";
import { buildInitScript } from "./mock/base.mjs";
import { BASE_STATE, mergeState } from "./mock/state.mjs";
import { startServer } from "./serve.mjs";
import { CLIPS, getClip, totalFrames } from "./clips.mjs";
import { CaptureError } from "./capture.mjs";

const here = path.dirname(fileURLToPath(import.meta.url));
const DESKTOP_URL = pathToFileURL(path.join(here, "frame", "desktop.html")).href;

// Outer canvas the bar is composited onto.
export const STAGE = { width: 1000, height: 560 };

const READY_TIMEOUT_MS = 15_000;

// Captures the bar frame by frame, driving it through the clip's steps.
export async function captureFrames({ browser, baseUrl, clip }) {
  const context = await browser.newContext({
    viewport: clip.viewport,
    deviceScaleFactor: 1, // a GIF gains nothing from 2x and doubles the weight
    locale: "en-US",
    timezoneId: "UTC",
    colorScheme: "dark",
    reducedMotion: "reduce",
  });

  const pageErrors = [];

  try {
    const state = mergeState(BASE_STATE, clip.state);
    await context.addInitScript({ content: buildInitScript({ state, theme: clip.theme }) });

    const page = await context.newPage();
    page.on("pageerror", (e) => pageErrors.push(String(e)));
    page.on("console", (m) => { if (m.type() === "error") pageErrors.push(m.text()); });

    await page.goto(`${baseUrl}/index.html`, { waitUntil: "domcontentloaded" });

    try {
      await page.getByText(clip.ready, { exact: false }).first()
        .waitFor({ state: "visible", timeout: READY_TIMEOUT_MS });
    } catch {
      throw new CaptureError(clip.id, "readiness string never became visible",
        JSON.stringify(clip.ready));
    }

    if (await page.getByText("Something went wrong").count() > 0) {
      throw new CaptureError(clip.id, "the app's ErrorBoundary rendered",
        pageErrors[0] || "no console detail");
    }

    await page.evaluate(() => document.fonts.ready);

    const shot = () => page.screenshot({ type: "png", animations: "disabled" });
    const frames = [];
    const perHold = (ms) => Math.round((ms / 1000) * clip.fps);

    for (const step of clip.steps) {
      if (step.type) {
        for (const ch of step.type) {
          await page.keyboard.type(ch, { delay: step.delayMs ?? 0 });
          frames.push(await shot());
        }
      }
      if (step.press) {
        for (let i = 0; i < (step.repeat ?? 1); i++) {
          await page.keyboard.press(step.press);
          frames.push(await shot());
        }
      }
      if (step.hold) {
        // Sample at the real frame interval rather than duplicating one shot:
        // a hold is also the time the UI has to settle, and some state arrives
        // asynchronously (the palette fetches a project's artisan/npm
        // vocabulary after the keystroke). Freezing the first frame would show
        // the moment *before* the answer landed.
        const n = perHold(step.hold);
        const interval = 1000 / clip.fps;
        for (let i = 0; i < n; i++) {
          await page.waitForTimeout(interval);
          frames.push(await shot());
        }
      }
    }

    const missing = await page.evaluate(() => window.__harness.missing);
    if (missing.length > 0) {
      throw new CaptureError(clip.id, "unmocked bindings were called", missing.join(", "));
    }
    if (pageErrors.length > 0) {
      throw new CaptureError(clip.id, "the page reported errors", pageErrors.join(" | "));
    }

    return frames;
  } finally {
    await context.close();
  }
}

// Composites one captured bar frame onto the faux desktop.
export async function composeStage({ page, pngBuffer }) {
  const dataUri = `data:image/png;base64,${pngBuffer.toString("base64")}`;
  await page.evaluate(async (src) => {
    const img = document.getElementById("bar");
    img.src = src;
    await img.decode();
  }, dataUri);
  return page.screenshot({ type: "png", animations: "disabled" });
}

function run(cmd, args) {
  return new Promise((resolve, reject) => {
    const p = spawn(cmd, args, { stdio: ["ignore", "ignore", "pipe"] });
    let err = "";
    p.stderr.on("data", (d) => { err += d; });
    p.on("error", reject);
    p.on("close", (code) => (code === 0 ? resolve() : reject(new Error(err.slice(-400)))));
  });
}

// GIF is a 256-colour format, so a naive encode bands badly on this dark
// gradient UI. ffmpeg's palettegen/paletteuse builds a palette from the clip's
// own colours, which is the difference between usable and ugly.
export async function encodeGif({ frameDir, fps, outPath, bin = "ffmpeg" }) {
  const pattern = path.join(frameDir, "f%05d.png");
  const palette = path.join(frameDir, "palette.png");

  try {
    await run(bin, ["-v", "error", "-y", "-framerate", String(fps), "-i", pattern,
      "-vf", "palettegen=stats_mode=diff", palette]);
  } catch (e) {
    if (e.code === "ENOENT") {
      throw new Error(
        "ffmpeg not found on PATH. It encodes the GIF palette; without it the " +
        "output would band badly on the dark UI, so recording stops here rather " +
        "than writing a poor file. Install it (winget install Gyan.FFmpeg) and retry.",
      );
    }
    throw e;
  }

  await run(bin, ["-v", "error", "-y", "-framerate", String(fps), "-i", pattern,
    "-i", palette,
    "-lavfi", "paletteuse=dither=bayer:bayer_scale=3", "-loop", "0", outPath]);
}

export async function recordClip({ browser, baseUrl, clip, outDir }) {
  const frames = await captureFrames({ browser, baseUrl, clip });

  const stage = await browser.newContext({
    viewport: STAGE, deviceScaleFactor: 1, colorScheme: "dark",
  });
  const frameDir = path.join(outDir, `.frames-${clip.id}`);
  await mkdir(frameDir, { recursive: true });

  try {
    const page = await stage.newPage();
    await page.goto(DESKTOP_URL, { waitUntil: "load" });

    let i = 0;
    for (const f of frames) {
      const composed = await composeStage({ page, pngBuffer: f });
      await writeFile(path.join(frameDir, `f${String(i++).padStart(5, "0")}.png`), composed);
    }

    const target = path.join(outDir, clip.out);
    await encodeGif({ frameDir, fps: clip.fps, outPath: target });
    return target;
  } finally {
    await stage.close();
    await rm(frameDir, { recursive: true, force: true });
  }
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
  const args = parseArgs(process.argv.slice(2));
  const outDir = args.out ? path.resolve(args.out) : path.join(here, "out");
  const dist = path.resolve(here, "../../frontend/dist");

  let chosen = CLIPS;
  if (args.only) {
    const one = getClip(args.only);
    if (!one) {
      console.error(`unknown clip "${args.only}" — known: ${CLIPS.map((c) => c.id).join(", ")}`);
      process.exit(2);
    }
    chosen = [one];
  }

  const browser = await chromium.launch({ channel: "chrome" });
  const server = await startServer(dist);
  const failures = [];

  try {
    for (const clip of chosen) {
      try {
        console.log(`recording ${clip.id} (${totalFrames(clip)} frames @ ${clip.fps}fps)…`);
        const written = await recordClip({ browser, baseUrl: server.url, clip, outDir });
        console.log(`ok   ${clip.id.padEnd(16)} -> ${written}`);
      } catch (err) {
        failures.push(clip.id);
        console.error(`FAIL ${clip.id.padEnd(16)} ${err.message}`);
      }
    }
  } finally {
    await server.close();
    await browser.close();
  }

  if (failures.length > 0) {
    console.error(`\n${failures.length} clip(s) failed: ${failures.join(", ")}`);
    process.exit(1);
  }
  console.log(`\n${chosen.length} clip(s) written to ${outDir}`);
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  main();
}
