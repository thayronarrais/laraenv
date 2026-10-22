import { describe, it, expect } from "vitest";
import { TOURS, CAPTURES, validateTour, totalFrames } from "../tours.mjs";

describe("tours", () => {
  it("every tour is consistent with its captures", () => {
    for (const tour of TOURS) expect(validateTour(tour)).toEqual([]);
  });

  it("every capture keeps the banner scenario's readiness proof", () => {
    for (const cap of Object.values(CAPTURES)) {
      expect(typeof cap.ready).toBe("string");
      expect(typeof cap.navigate).toBe("function");
      expect(Object.keys(cap.measure).length).toBeGreaterThan(0);
    }
  });

  it("the dashboard shows the services the storyboard names", () => {
    expect(CAPTURES.dashboard.state.services.map((s) => s.label)).toEqual(["Nginx", "Apache", "MySQL"]);
  });

  it("frame count follows duration and fps", () => {
    expect(totalFrames({ duration: 14, fps: 30 })).toBe(420);
  });

  it("reports what is wrong instead of rendering it", () => {
    const broken = {
      ...TOURS[0],
      fadeOutAt: 20,
      scenes: [
        { id: "a", capture: "dashboard", start: 0, cursor: [{ at: 3, target: "nope", move: 0.5, click: true }] },
        { id: "b", capture: "ghost", start: 2 },
        { id: "c", capture: "ssh", start: 1, launch: {} },
      ],
    };
    const problems = validateTour(broken);
    expect(problems).toEqual(expect.arrayContaining([
      expect.stringContaining('cursor target "nope"'),
      expect.stringContaining("cursor nope at 3s is outside"),
      expect.stringContaining('unknown capture "ghost"'),
      expect.stringContaining("starts before the previous scene"),
      expect.stringContaining("only the first scene can launch"),
      "fadeOutAt must be before the end",
    ]));
  });
});
