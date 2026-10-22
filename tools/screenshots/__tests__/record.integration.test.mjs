import { describe, it, expect, beforeAll, afterAll } from "vitest";
import { chromium } from "playwright";
import { mkdtemp, readFile, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { startServer } from "../serve.mjs";
import { recordClip, encodeGif } from "../record.mjs";
import { getClip, totalFrames } from "../clips.mjs";

const here = path.dirname(fileURLToPath(import.meta.url));
const DIST = path.resolve(here, "../../../frontend/dist");

let browser, server, outDir;

beforeAll(async () => {
  browser = await chromium.launch({ channel: "chrome" });
  server = await startServer(DIST);
  outDir = await mkdtemp(path.join(tmpdir(), "clip-out-"));
}, 120_000);

afterAll(async () => {
  await server?.close();
  await browser?.close();
});

// Counts Graphic Control Extension blocks (21 F9 04), one per animated frame.
// A bare 0x2C image-descriptor scan does not work: that byte occurs freely
// inside the LZW data.
function gifFrameCount(buf) {
  let n = 0;
  for (let i = 0; i < buf.length - 2; i++) {
    if (buf[i] === 0x21 && buf[i + 1] === 0xf9 && buf[i + 2] === 0x04) n++;
  }
  return n;
}

describe("recordClip", () => {
  it("writes an animated GIF for the command-center clip", async () => {
    const clip = getClip("command-center");
    const written = await recordClip({ browser, baseUrl: server.url, clip, outDir });

    expect(path.basename(written)).toBe("command-center.gif");
    const buf = await readFile(written);
    expect(buf.subarray(0, 6).toString("latin1")).toBe("GIF89a");
    // More than one frame is the point — a still would silently pass a
    // "file exists" check.
    expect(gifFrameCount(buf)).toBeGreaterThan(1);
    expect(buf.length).toBeGreaterThan(20_000);
  }, 300_000);

  it("stays small enough for a README to carry", async () => {
    const buf = await readFile(path.join(outDir, "command-center.gif"));
    expect(buf.length).toBeLessThan(2 * 1024 * 1024);
  }, 60_000);

  it("records the frame count the clip declares", async () => {
    const buf = await readFile(path.join(outDir, "command-center.gif"));
    // palettegen/paletteuse may drop frames that are byte-identical to their
    // predecessor, so this is a ceiling rather than an equality.
    expect(gifFrameCount(buf)).toBeLessThanOrEqual(totalFrames(getClip("command-center")));
  }, 60_000);
});

describe("encodeGif", () => {
  it("explains itself instead of failing obscurely when ffmpeg is absent", async () => {
    const dir = await mkdtemp(path.join(tmpdir(), "clip-noffmpeg-"));
    await writeFile(path.join(dir, "f00000.png"), Buffer.from(
      "iVBORw0KGgoAAAANSUhEUgAAAAIAAAACCAIAAAD91JpzAAAAEklEQVR4nGP8z4AATAxQxjAUAgCQuwICVjmfPwAAAABJRU5ErkJggg==",
      "base64",
    ));
    await expect(encodeGif({
      frameDir: dir, fps: 12, outPath: path.join(dir, "x.gif"),
      bin: "ffmpeg-does-not-exist",
    })).rejects.toThrow(/ffmpeg not found/);
  }, 60_000);
});
