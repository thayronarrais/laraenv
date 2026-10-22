// Drag-and-drop payload codecs and pointer-region hit-testing.
//
// Two HTML5 DnD channels:
//   MIME_TAB  — dragging a tab (reorder within a strip, merge into another
//               group's strip, or tear-off onto empty canvas).
//   MIME_PANE — dragging a pane to split a target leaf via edge drop zones.

export const MIME_TAB = "application/x-lara4ever-tab";
export const MIME_PANE = "application/x-lara4ever-pane";

export type TabPayload = { srcGroupId: string; tabId: string };
export type PanePayload = { srcGroupId: string; tabId: string; leafId: string };

export function encode(payload: TabPayload | PanePayload): string {
  return JSON.stringify(payload);
}

export function decodeTab(raw: string): TabPayload | null {
  try {
    const o = JSON.parse(raw);
    if (o && typeof o.srcGroupId === "string" && typeof o.tabId === "string") return o;
  } catch {}
  return null;
}

export function decodePane(raw: string): PanePayload | null {
  try {
    const o = JSON.parse(raw);
    if (o && typeof o.srcGroupId === "string" && typeof o.tabId === "string" && typeof o.leafId === "string") return o;
  } catch {}
  return null;
}

export type DropRegion = "top" | "bottom" | "left" | "right" | "center";

// VS Code-style 5-region hit test. `px,py` are pointer coords relative to the
// target rect's top-left. `edge` is the fraction of each side that counts as an
// edge zone (0.25 = outer quarter).
export function regionForPointer(
  rectW: number,
  rectH: number,
  px: number,
  py: number,
  edge = 0.25,
): DropRegion {
  const rx = rectW > 0 ? px / rectW : 0.5;
  const ry = rectH > 0 ? py / rectH : 0.5;
  if (ry < edge) return "top";
  if (ry > 1 - edge) return "bottom";
  if (rx < edge) return "left";
  if (rx > 1 - edge) return "right";
  return "center";
}

// Maps a drop region to a split direction + which side the new pane goes.
// top/bottom => vertical stack; left/right => horizontal side-by-side. Matches
// the old splitActivePane semantics (split → = horizontal, split ↓ = vertical).
export function regionToSplit(
  region: Exclude<DropRegion, "center">,
): { direction: "horizontal" | "vertical"; side: "before" | "after" } {
  switch (region) {
    case "top":    return { direction: "vertical", side: "before" };
    case "bottom": return { direction: "vertical", side: "after" };
    case "left":   return { direction: "horizontal", side: "before" };
    case "right":  return { direction: "horizontal", side: "after" };
  }
}
