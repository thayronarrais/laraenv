import { Minus, Square, X } from "lucide-react";
// @ts-ignore
import { WindowMinimise, WindowToggleMaximise, Quit } from "../../wailsjs/runtime";
import { useT } from "../lib/i18n";

export default function WindowControls() {
  const t = useT();
  return (
    <div
      className="flex items-center gap-1"
      // @ts-ignore — Wails CSS hint
      style={{ "--wails-draggable": "no-drag" }}
    >
      <button
        onClick={() => WindowMinimise()}
        className="w-8 h-7 rounded-md hover:bg-white/10 flex items-center justify-center text-white/70 hover:text-white transition-colors"
        title={t("common.windowControls.minimize")}
      >
        <Minus className="w-3.5 h-3.5" />
      </button>
      <button
        onClick={() => WindowToggleMaximise()}
        className="w-8 h-7 rounded-md hover:bg-white/10 flex items-center justify-center text-white/70 hover:text-white transition-colors"
        title={t("common.windowControls.maximize")}
      >
        <Square className="w-3 h-3" />
      </button>
      <button
        onClick={() => Quit()}
        className="w-8 h-7 rounded-md hover:bg-red-500/80 flex items-center justify-center text-white/70 hover:text-white transition-colors"
        title={t("common.windowControls.close")}
      >
        <X className="w-3.5 h-3.5" />
      </button>
    </div>
  );
}
