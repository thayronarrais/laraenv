import { readFile, mkdir } from "node:fs/promises";
import { spawn } from "node:child_process";
import { once } from "node:events";
import path from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";
import { chromium } from "playwright";
import { captureRaw, CaptureError } from "./capture.mjs";
import { startServer } from "./serve.mjs";
import { CAPTURES, TOURS, getTour, totalFrames, validateTour } from "./tours.mjs";

const here = path.dirname(fileURLToPath(import.meta.url));
const STAGE_URL = pathToFileURL(path.join(here, "frame", "stage.html")).href;
const ICON_PATH = path.resolve(here, "../../build/appicon.png");

// Captures every screen a tour needs, once each, and measures the boxes the
// storyboard refers to in the very frame that was captured.
export async function captureTour({ browser, baseUrl, tour }) {
  const out = {};
  for (const id of new Set(tour.scenes.map((s) => s.capture))) {
    const cap = CAPTURES[id];
    let boxes = {};
    const png = await captureRaw({
      browser, baseUrl, scenario: cap,
      onReady: async (page) => {
        for (const [name, find] of Object.entries(cap.measure)) {
          const box = await find(page).boundingBox();
          if (!box) throw new CaptureError(cap.id, `"${name}" has no box`, "is it rendered and visible?");
          boxes[name] = box;
        }
      },
    });
    out[id] = { src: `data:image/png;base64,${png.toString("base64")}`, boxes };
  }
  return out;
}

function encoder({ fps, outPath, bin = "ffmpeg" }) {
  const p = spawn(bin, [
    "-v", "error", "-y",
    "-f", "image2pipe", "-framerate", String(fps), "-c:v", "png", "-i", "-",
    "-c:v", "libx264", "-preset", "medium", "-crf", "18",
    "-pix_fmt", "yuv420p", "-movflags", "+faststart",
    outPath,
  ], { stdio: ["pipe", "ignore", "pipe"] });
  let err = "";
  p.stderr.on("data", (d) => { err += d; });
  const done = new Promise((resolve, reject) => {
    p.on("error", (e) => {
      if (e.code === "ENOENT") {
        reject(new Error("ffmpeg not found on PATH. Install it (winget install Gyan.FFmpeg) and retry."));
      } else reject(e);
    });
    p.on("close", (code) => (code === 0 ? resolve() : reject(new Error(err.slice(-400) || `ffmpeg exited ${code}`))));
  });
  return {
    async write(buf) {
      if (!p.stdin.write(buf)) await once(p.stdin, "drain");
    },
    finish() { p.stdin.end(); return done; },
  };
}

export async function renderTour({ browser, tour, captures, outDir, onProgress }) {
  const problems = validateTour(tour);
  if (problems.length > 0) throw new Error(`tour "${tour.id}" is inconsistent:\n  ${problems.join("\n  ")}`);

  const icon = `data:image/png;base64,${(await readFile(ICON_PATH)).toString("base64")}`;
  const context = await browser.newContext({
    viewport: { width: tour.width, height: tour.height },
    deviceScaleFactor: 1,
    colorScheme: "dark",
  });
  await mkdir(outDir, { recursive: true });
  const outPath = path.join(outDir, tour.out);

  try {
    const page = await context.newPage();
    const pageErrors = [];
    page.on("pageerror", (e) => pageErrors.push(String(e)));
    await page.goto(STAGE_URL, { waitUntil: "load" });
    await page.evaluate((args) => window.setup(args), { tour, captures, icon });
    if (pageErrors.length > 0) throw new Error(`stage failed to set up: ${pageErrors.join(" | ")}`);

    const enc = encoder({ fps: tour.fps, outPath });
    const frames = totalFrames(tour);
    for (let i = 0; i < frames; i++) {
      const t = i / tour.fps;
      await page.evaluate((t) => window.seek(t), t);
      if (pageErrors.length > 0) throw new Error(`stage threw at ${t.toFixed(2)}s: ${pageErrors[0]}`);
      await enc.write(await page.screenshot({ type: "png", animations: "disabled" }));
      if (onProgress && i % tour.fps === 0) onProgress(i, frames);
    }
    await enc.finish();
    return outPath;
  } finally {
    await context.close();
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

  let chosen = TOURS;
  if (args.only) {
    const one = getTour(args.only);
    if (!one) {
      console.error(`unknown tour "${args.only}" — known: ${TOURS.map((t) => t.id).join(", ")}`);
      process.exit(2);
    }
    chosen = [one];
  }

  const browser = await chromium.launch({ channel: "chrome" });
  const server = await startServer(dist);
  const failures = [];

  try {
    for (const tour of chosen) {
      try {
        console.log(`capturing ${tour.id}…`);
        const captures = await captureTour({ browser, baseUrl: server.url, tour });
        console.log(`rendering ${tour.id} (${totalFrames(tour)} frames @ ${tour.fps}fps)…`);
        const written = await renderTour({
          browser, tour, captures, outDir,
          onProgress: (i, n) => process.stdout.write(`  ${Math.round((i / n) * 100)}%\r`),
        });
        console.log(`ok   ${tour.id.padEnd(12)} -> ${written}`);
      } catch (err) {
        failures.push(tour.id);
        console.error(`FAIL ${tour.id.padEnd(12)} ${err.message}`);
      }
    }
  } finally {
    await server.close();
    await browser.close();
  }

  if (failures.length > 0) {
    console.error(`\n${failures.length} tour(s) failed: ${failures.join(", ")}`);
    process.exit(1);
  }
  console.log(`\n${chosen.length} tour(s) written to ${outDir}`);
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  main();
}
