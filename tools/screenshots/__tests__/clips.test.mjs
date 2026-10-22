import { describe, it, expect } from "vitest";
import { CLIPS, getClip, totalFrames } from "../clips.mjs";

describe("clip catalog", () => {
  it("declares every field the recorder reads", () => {
    for (const c of CLIPS) {
      expect(typeof c.id, `${c.id}.id`).toBe("string");
      expect(c.out, `${c.id}.out`).toMatch(/\.gif$/);
      expect(typeof c.theme, `${c.id}.theme`).toBe("string");
      expect(typeof c.state, `${c.id}.state`).toBe("object");
      expect(typeof c.ready, `${c.id}.ready`).toBe("string");
      expect(c.viewport.width, `${c.id}.viewport.width`).toBeGreaterThan(0);
      expect(c.viewport.height, `${c.id}.viewport.height`).toBeGreaterThan(0);
      expect(Array.isArray(c.steps), `${c.id}.steps`).toBe(true);
      expect(c.steps.length, `${c.id} needs steps`).toBeGreaterThan(0);
      expect(c.fps, `${c.id}.fps`).toBeGreaterThan(0);
    }
  });

  it("uses each id and output filename exactly once", () => {
    expect(new Set(CLIPS.map((c) => c.id)).size).toBe(CLIPS.length);
    expect(new Set(CLIPS.map((c) => c.out)).size).toBe(CLIPS.length);
  });

  it("only uses step kinds the recorder knows", () => {
    // `hold` may ride along with an input action ("press ArrowDown, then let
    // the selection sit"), but two input actions in one step would be
    // ambiguous about ordering.
    const known = new Set(["hold", "type", "press", "delayMs", "repeat"]);
    for (const c of CLIPS) {
      for (const s of c.steps) {
        for (const k of Object.keys(s)) {
          expect(known.has(k), `${c.id}: unknown step key "${k}"`).toBe(true);
        }
        const actions = ["hold", "type", "press"].filter((k) => k in s);
        expect(actions.length, `${c.id}: step ${JSON.stringify(s)} does nothing`).toBeGreaterThan(0);
        expect(!("type" in s && "press" in s), `${c.id}: step mixes type and press`).toBe(true);
      }
    }
  });

  it("looks a clip up by id", () => {
    expect(getClip("command-center").out).toBe("command-center.gif");
  });

  it("returns undefined for an unknown id", () => {
    expect(getClip("nope")).toBeUndefined();
  });
});

describe("totalFrames", () => {
  it("counts one frame per hold interval", () => {
    const clip = { fps: 10, steps: [{ hold: 500 }] };
    expect(totalFrames(clip)).toBe(5);
  });

  it("counts one frame per typed character", () => {
    const clip = { fps: 10, steps: [{ type: "abc" }] };
    expect(totalFrames(clip)).toBe(3);
  });

  it("counts a keypress plus the hold that follows it", () => {
    const clip = { fps: 10, steps: [{ press: "ArrowDown", repeat: 2, hold: 300 }] };
    expect(totalFrames(clip)).toBe(2 + 3);
  });

  it("sums across steps", () => {
    const clip = { fps: 10, steps: [{ hold: 200 }, { type: "ab" }, { hold: 100 }] };
    expect(totalFrames(clip)).toBe(2 + 2 + 1);
  });

  it("keeps the real clip to a length a README can carry", () => {
    for (const c of CLIPS) {
      expect(totalFrames(c), `${c.id} frame count`).toBeLessThanOrEqual(160);
    }
  });
});
