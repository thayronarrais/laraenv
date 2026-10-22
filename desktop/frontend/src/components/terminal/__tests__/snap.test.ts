import { describe, it, expect } from "vitest";
import { snapGeometry, snapZoneForPointer } from "../snap";

describe("snap", () => {
  it("snapGeometry left/right split the canvas in half", () => {
    expect(snapGeometry(1000, 600, "left")).toEqual({ x: 0, y: 0, w: 500, h: 600 });
    expect(snapGeometry(1000, 600, "right")).toEqual({ x: 500, y: 0, w: 500, h: 600 });
  });

  it("snapGeometry max fills the canvas; null = floating", () => {
    expect(snapGeometry(800, 480, "max")).toEqual({ x: 0, y: 0, w: 800, h: 480 });
    expect(snapGeometry(800, 480, null)).toBeNull();
  });

  it("snapGeometry quadrants", () => {
    expect(snapGeometry(1000, 600, "br")).toEqual({ x: 500, y: 300, w: 500, h: 300 });
  });

  it("snapZoneForPointer: corners beat edges", () => {
    expect(snapZoneForPointer(5, 5, 1000, 600)).toBe("tl");
    expect(snapZoneForPointer(995, 595, 1000, 600)).toBe("br");
  });

  it("snapZoneForPointer: top edge maximizes, sides halve, interior = null", () => {
    expect(snapZoneForPointer(500, 4, 1000, 600)).toBe("max");
    expect(snapZoneForPointer(4, 300, 1000, 600)).toBe("left");
    expect(snapZoneForPointer(996, 300, 1000, 600)).toBe("right");
    expect(snapZoneForPointer(500, 300, 1000, 600)).toBeNull();
  });
});
