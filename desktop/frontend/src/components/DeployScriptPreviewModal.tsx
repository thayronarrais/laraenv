import { useEffect, useState } from "react";
import { createPortal } from "react-dom";
import { X, AlertTriangle, Loader2, Eye } from "lucide-react";
import Editor from "@monaco-editor/react";
// @ts-ignore
import { RenderDeployScriptPreview } from "../../wailsjs/go/main/App";
import { useT } from "../lib/i18n";

type Rendered = {
  script: string;
  pipeline: string;
  legacy: boolean;
  unresolved?: string[] | null;
  warnings?: string[] | null;
};

// Read-only view of the exact script a deployment config will execute —
// rendered by the same Go function the apply step / autopull cron / Deploy
// now runner use, so what you see here is what runs on the server.
export default function DeployScriptPreviewModal({
  cfg, forDeploy, onClose,
}: {
  cfg: any;            // ApplyConfig-shaped object from the form
  forDeploy?: boolean; // true = the manual "Deploy now" variant
  onClose: () => void;
}) {
  const t = useT();
  const [rendered, setRendered] = useState<Rendered | null>(null);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    RenderDeployScriptPreview(cfg, !!forDeploy)
      .then((r: Rendered) => { if (!cancelled) setRendered(r); })
      .catch((e: any) => { if (!cancelled) setError(String(e?.message ?? e)); });
    return () => { cancelled = true; };
  }, []);

  const unresolved = rendered?.unresolved ?? [];
  const warnings = rendered?.warnings ?? [];

  return createPortal(
    <div className="fixed inset-0 bg-black/60 backdrop-blur-sm flex items-center justify-center z-[60] p-4" onClick={onClose}>
      <div
        className="glass-strong w-full max-w-4xl flex flex-col p-4"
        style={{ height: "min(720px, 90vh)" }}
        onClick={(e) => e.stopPropagation()}
      >
        <div className="flex items-center justify-between mb-3 flex-shrink-0">
          <h2 className="text-sm font-semibold flex items-center gap-2">
            <Eye className="w-4 h-4" />
            {forDeploy ? t("deployments.previewModal.titleDeploy") : t("deployments.previewModal.titleApply")}
            {rendered?.legacy && <span className="text-[10px] uppercase tracking-wider text-amber-300/80 border border-amber-500/30 rounded px-1.5 py-0.5">{t("deployments.previewModal.legacyBadge")}</span>}
          </h2>
          <button onClick={onClose} className="p-1 rounded hover:bg-white/10"><X className="w-4 h-4" /></button>
        </div>

        <div className="text-[11px] opacity-60 mb-2 flex-shrink-0">
          {t("deployments.previewModal.subtitle")}
        </div>

        {error && (
          <div className="mb-2 px-3 py-1.5 text-[11px] rounded-md border border-red-400/40 bg-red-500/10 text-red-100 flex items-center gap-2 flex-shrink-0">
            <AlertTriangle className="w-3 h-3 flex-shrink-0" />
            <span className="flex-1 break-all">{error}</span>
          </div>
        )}

        {(unresolved.length > 0 || warnings.length > 0) && (
          <div className="mb-2 px-3 py-1.5 text-[11px] rounded-md border border-amber-400/40 bg-amber-500/10 text-amber-100 flex-shrink-0 space-y-0.5">
            {unresolved.length > 0 && (
              <div className="flex items-center gap-2">
                <AlertTriangle className="w-3 h-3 flex-shrink-0" />
                <span>{t("deployments.previewModal.unresolvedVars", { tokens: unresolved.map((u) => `{${u}}`).join(", ") })}</span>
              </div>
            )}
            {warnings.map((w, i) => (
              <div key={i} className="flex items-center gap-2">
                <AlertTriangle className="w-3 h-3 flex-shrink-0" />
                <span>{w}</span>
              </div>
            ))}
          </div>
        )}

        <div className="flex-1 min-h-0 rounded-lg overflow-hidden border border-white/10">
          {!rendered && !error ? (
            <div className="h-full flex items-center justify-center text-xs opacity-70">
              <Loader2 className="w-4 h-4 animate-spin mr-2" /> {t("deployments.previewModal.renderingLabel")}
            </div>
          ) : (
            <Editor
              height="100%"
              theme="vs-dark"
              language="shell"
              value={rendered?.script || "# no deploy script configured"}
              options={{
                readOnly: true,
                fontSize: 12,
                minimap: { enabled: false },
                automaticLayout: true,
                wordWrap: "on",
                scrollBeyondLastLine: false,
              }}
            />
          )}
        </div>
      </div>
    </div>,
    document.body,
  );
}
