import path from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

const here = path.dirname(fileURLToPath(import.meta.url));
const FRAME_URL = pathToFileURL(path.join(here, "frame.html")).href;

// Outer canvas: the 1334x871 capture (capture.mjs VIEWPORT) plus a 44x38
// margin, so the window dominates the image and the starfield reads as a
// border rather than a backdrop.
export const FRAME = { width: 1422, height: 947 };

export async function composeFrame({ browser, pngBuffer }) {
  const context = await browser.newContext({
    viewport: FRAME,
    deviceScaleFactor: 2,
    colorScheme: "dark",
  });

  try {
    const page = await context.newPage();
    await page.goto(FRAME_URL, { waitUntil: "load" });

    const dataUri = `data:image/png;base64,${pngBuffer.toString("base64")}`;
    await page.evaluate(async (src) => {
      const img = document.getElementById("shot");
      img.src = src;
      // Draw at CSS pixels, not the doubled capture pixels.
      await img.decode();
      img.style.width = `${img.naturalWidth / 2}px`;
      img.style.height = `${img.naturalHeight / 2}px`;
    }, dataUri);

    return await page.screenshot({ type: "png", animations: "disabled" });
  } finally {
    await context.close();
  }
}
