import { describe, it, expect } from "vitest";
import { decodePane, decodeTab, encode, regionForPointer, regionToSplit } from "../dnd";

describe("dnd", () => {
  it("tab payload round-trips; rejects malformed", () => {
    const raw = encode({ srcGroupId: "g1", tabId: "t1" });
    expect(decodeTab(raw)).toEqual({ srcGroupId: "g1", tabId: "t1" });
    expect(decodeTab("not json")).toBeNull();
    expect(decodeTab(JSON.stringify({ srcGroupId: "g1" }))).toBeNull();
  });

  it("pane payload requires leafId", () => {
    const raw = encode({ srcGroupId: "g1", tabId: "t1", leafId: "l1" });
    expect(decodePane(raw)).toEqual({ srcGroupId: "g1", tabId: "t1", leafId: "l1" });
    expect(decodePane(encode({ srcGroupId: "g1", tabId: "t1" } as any))).toBeNull();
  });

  it("regionForPointer maps corners/edges/center", () => {
    expect(regionForPointer(100, 100, 50, 5)).toBe("top");
    expect(regionForPointer(100, 100, 50, 95)).toBe("bottom");
    expect(regionForPointer(100, 100, 5, 50)).toBe("left");
    expect(regionForPointer(100, 100, 95, 50)).toBe("right");
    expect(regionForPointer(100, 100, 50, 50)).toBe("center");
  });

  it("regionToSplit: top/bottom => vertical, left/right => horizontal", () => {
    expect(regionToSplit("top")).toEqual({ direction: "vertical", side: "before" });
    expect(regionToSplit("bottom")).toEqual({ direction: "vertical", side: "after" });
    expect(regionToSplit("left")).toEqual({ direction: "horizontal", side: "before" });
    expect(regionToSplit("right")).toEqual({ direction: "horizontal", side: "after" });
  });
});
