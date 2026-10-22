// Pure geometry for Windows-style edge/quadrant snapping and maximize.

import type { SnapState } from "./types";

export type Rect = { x: number; y: number; w: number; h: number };

// Rendered geometry for a snapped group given the canvas size. Returns null for
// the free-floating state (caller uses the group's restore geometry instead).
export function snapGeometry(cw: number, ch: number, snap: SnapState): Rect | null {
  const hw = Math.round(cw / 2);
  const hh = Math.round(ch / 2);
  switch (snap) {
    case "max":    return { x: 0, y: 0, w: cw, h: ch };
    case "left":   return { x: 0, y: 0, w: hw, h: ch };
    case "right":  return { x: hw, y: 0, w: cw - hw, h: ch };
    case "top":    return { x: 0, y: 0, w: cw, h: hh };
    case "bottom": return { x: 0, y: hh, w: cw, h: ch - hh };
    case "tl":     return { x: 0, y: 0, w: hw, h: hh };
    case "tr":     return { x: hw, y: 0, w: cw - hw, h: hh };
    case "bl":     return { x: 0, y: hh, w: hw, h: ch - hh };
    case "br":     return { x: hw, y: hh, w: cw - hw, h: ch - hh };
    default:       return null;
  }
}

// Which snap zone (if any) the pointer is hovering, in canvas-relative coords.
// Corners take priority (quadrants), then the top edge (maximize), then the
// remaining side edges (halves). Returns null when the pointer is in the
// interior — i.e. a plain free move.
export function snapZoneForPointer(
  px: number,
  py: number,
  cw: number,
  ch: number,
  threshold = 24,
): SnapState {
  const nearL = px <= threshold;
  const nearR = px >= cw - threshold;
  const nearT = py <= threshold;
  const nearB = py >= ch - threshold;

  if (nearT && nearL) return "tl";
  if (nearT && nearR) return "tr";
  if (nearB && nearL) return "bl";
  if (nearB && nearR) return "br";
  if (nearT) return "max";
  if (nearB) return "bottom";
  if (nearL) return "left";
  if (nearR) return "right";
  return null;
}
