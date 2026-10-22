import { describe, it, expect, beforeAll, afterAll } from "vitest";
import { chromium } from "playwright";
import { composeFrame, FRAME } from "../frame/compose.mjs";

let browser;
beforeAll(async () => { browser = await chromium.launch({ channel: "chrome" }); }, 120_000);
afterAll(async () => { await browser?.close(); });

// A 2x2 solid red PNG, enough to prove the image is embedded and drawn.
const RED_2X2 = Buffer.from(
  "iVBORw0KGgoAAAANSUhEUgAAAAIAAAACCAIAAAD91JpzAAAAEklEQVR4nGP8z4AATAxQxjAUAgCQuwICVjmfPwAAAABJRU5ErkJggg==",
  "base64",
);

describe("composeFrame", () => {
  it("returns a PNG at the framed dimensions", async () => {
    const out = await composeFrame({ browser, pngBuffer: RED_2X2 });
    expect(out.subarray(0, 8)).toEqual(Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]));
    expect(out.readUInt32BE(16)).toBe(FRAME.width * 2);
    expect(out.readUInt32BE(20)).toBe(FRAME.height * 2);
  }, 120_000);

  it("produces a larger image than the bare capture", async () => {
    const out = await composeFrame({ browser, pngBuffer: RED_2X2 });
    expect(out.length).toBeGreaterThan(RED_2X2.length);
  }, 120_000);
});
