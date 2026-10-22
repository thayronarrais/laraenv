import { describe, expect, it } from "vitest";
import { wheelScrollDelta } from "../tabStrip";

// The tab strip scrolls horizontally and has no vertical overflow, so Chromium
// does nothing with a plain mouse wheel over it: it only scrolls the strip from
// deltaX, which is what a trackpad swipe or shift+wheel produces. With more
// tabs than fit, reaching the ones off-screen meant dragging the strip by hand.

const strip = { scrollWidth: 900, clientWidth: 300 };

describe("wheelScrollDelta", () => {
  it("turns a mouse wheel into horizontal movement", () => {
    expect(wheelScrollDelta({ deltaX: 0, deltaY: 120 }, strip)).toBe(120);
    expect(wheelScrollDelta({ deltaX: 0, deltaY: -120 }, strip)).toBe(-120);
  });

  it("leaves a trackpad's own horizontal swipe to the browser", () => {
    // Chromium already scrolls the strip from deltaX. Adding it again here
    // would move the strip twice as far as the fingers did.
    expect(wheelScrollDelta({ deltaX: -80, deltaY: 0 }, strip)).toBe(0);
  });

  it("ignores the vertical drift in a diagonal trackpad swipe", () => {
    expect(wheelScrollDelta({ deltaX: 60, deltaY: 4 }, strip)).toBe(0);
  });

  it("still scrolls when a mostly-vertical gesture drifts sideways", () => {
    expect(wheelScrollDelta({ deltaX: 4, deltaY: 60 }, strip)).toBe(60);
  });

  it("does nothing when every tab already fits", () => {
    expect(wheelScrollDelta({ deltaX: 0, deltaY: 120 }, { scrollWidth: 300, clientWidth: 300 })).toBe(0);
  });
});
