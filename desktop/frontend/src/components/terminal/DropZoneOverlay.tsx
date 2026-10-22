// Translucent VS Code-style overlay shown over a pane while a tab/pane is being
// dragged onto it. Highlights the region (top/bottom/left/right/center) the
// pointer currently maps to.

import clsx from "clsx";
import type { DropRegion } from "./dnd";

export default function DropZoneOverlay({ region }: { region: DropRegion | null }) {
  if (!region) return null;
  // Geometry of the highlight rectangle per region.
  const box: Record<DropRegion, string> = {
    top:    "left-0 top-0 right-0 h-1/2",
    bottom: "left-0 bottom-0 right-0 h-1/2",
    left:   "left-0 top-0 bottom-0 w-1/2",
    right:  "right-0 top-0 bottom-0 w-1/2",
    center: "inset-0",
  };
  return (
    <div className="absolute inset-0 z-20 pointer-events-none">
      <div
        className={clsx(
          "absolute rounded-md border-2 transition-all duration-75",
          "border-accent2/80 bg-accent2/15",
          box[region],
        )}
      />
    </div>
  );
}
