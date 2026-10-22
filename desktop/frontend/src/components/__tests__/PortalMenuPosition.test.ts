import { describe, expect, it } from "vitest";
import { calculatePortalMenuPosition } from "../PortalMenu";

describe("calculatePortalMenuPosition", () => {
  it("keeps a tall menu fully inside the viewport", () => {
    const position = calculatePortalMenuPosition({
      anchor: { top: 390, bottom: 420, left: 170, right: 260 },
      menuWidth: 430,
      menuHeight: 640,
      minWidth: 430,
      viewportWidth: 1280,
      viewportHeight: 760,
      align: "left",
      placement: "bottom",
    });

    expect(position).toEqual({ top: 8, left: 170, maxHeight: 744 });
  });

  it("flips above the anchor when the full menu fits there", () => {
    const position = calculatePortalMenuPosition({
      anchor: { top: 600, bottom: 630, left: 1100, right: 1190 },
      menuWidth: 300,
      menuHeight: 240,
      minWidth: 300,
      viewportWidth: 1280,
      viewportHeight: 760,
      align: "right",
      placement: "bottom",
    });

    expect(position.top).toBe(356);
    expect(position.left).toBe(890);
  });

  it("clamps horizontal placement to the viewport", () => {
    const position = calculatePortalMenuPosition({
      anchor: { top: 20, bottom: 50, left: 2, right: 50 },
      menuWidth: 430,
      menuHeight: 100,
      minWidth: 430,
      viewportWidth: 800,
      viewportHeight: 600,
      align: "right",
      placement: "bottom",
    });

    expect(position.left).toBe(8);
  });
});
