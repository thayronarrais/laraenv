import { useEffect, useState } from "react";
import clsx from "clsx";
import { useDialogs } from "./dialogs/DialogProvider";
import { createPortal } from "react-dom";
import { X, Save, RotateCcw, AlertTriangle } from "lucide-react";
import { GetProjectVhost, GetProjectVhostPath, SaveProjectVhost, ResetProjectVhost } from "../../wailsjs/go/main/App";
import { useT } from "../lib/i18n";

type Server = "nginx" | "apache" | "caddy";

const SERVERS: { id: Server; label: string }[] = [
  { id: "nginx", label: "nginx" },
  { id: "apache", label: "apache" },
  { id: "caddy", label: "caddy" },
];

export default function VhostEditorModal({
  projectName, open, onClose, flash,
}: {
  projectName: string | null;
  open: boolean;
  onClose: () => void;
  flash: (m: string) => void;
}) {
  const t = useT();
  const [content, setContent] = useState("");
  const dialogs = useDialogs();
  const [original, setOriginal] = useState("");
  const [loading, setLoading] = useState(false);
  const [server, setServer] = useState<Server>("nginx");
  const [notGenerated, setNotGenerated] = useState(false);
  const [path, setPath] = useState("");

  useEffect(() => {
    if (!open || !projectName) return;
    setLoading(true);
    setNotGenerated(false);
    GetProjectVhostPath(projectName, server)
      .then((p: string) => setPath(p))
      .catch(() => setPath(""));
    GetProjectVhost(projectName, server)
      .then((c: string) => { setContent(c); setOriginal(c); })
      .catch(() => { setContent(""); setOriginal(""); setNotGenerated(true); })
      .finally(() => setLoading(false));
  }, [open, projectName, server]);

  if (!open || !projectName) return null;

  const dirty = content !== original;

  const save = async () => {
    try {
      await SaveProjectVhost(projectName, server, content);
      setOriginal(content);
      flash(t("projects.vhostModal.savedFlash", { server }));
    } catch (e) { flash(t("projects.errors.generic", { message: String(e) })); }
  };

  const reset = async () => {
    if (!(await dialogs.confirm({ title: t("projects.vhostModal.resetConfirmTitle"), message: t("projects.vhostModal.resetConfirmMessage"), confirmLabel: t("projects.vhostModal.resetConfirmLabel"), danger: true }))) return;
    try {
      await ResetProjectVhost(projectName);
      try {
        const fresh: string = await GetProjectVhost(projectName, server);
        setContent(fresh);
        setOriginal(fresh);
        setNotGenerated(false);
        flash(t("projects.vhostModal.resetFlash"));
      } catch {
        setContent("");
        setOriginal("");
        setNotGenerated(true);
      }
    } catch (e) { flash(t("projects.errors.generic", { message: String(e) })); }
  };

  const close = async () => {
    if (dirty && !(await dialogs.confirm({ title: t("projects.vhostModal.discardConfirmTitle"), confirmLabel: t("projects.vhostModal.discardConfirmLabel"), danger: true }))) return;
    onClose();
  };

  return createPortal(
    <div className="modal-backdrop">
      <div
        onClick={(e) => e.stopPropagation()}
        className="modal-card w-[860px] max-w-[94vw] h-[80vh] flex flex-col p-5"
      >
        <header className="flex items-center justify-between mb-3">
          <div>
            <h2 className="text-base font-semibold">{t("projects.vhostModal.title", { name: projectName })}</h2>
            <div className="text-[11px] opacity-50">
              {path}
            </div>
          </div>
          <div className="flex items-center gap-3">
            <div className="flex items-center gap-1 bg-white/5 border border-white/10 rounded-lg p-0.5">
              {SERVERS.map((s) => (
                <button
                  key={s.id}
                  onClick={() => setServer(s.id)}
                  className={clsx(
                    "px-2.5 py-1 rounded-md text-[11px] transition-colors",
                    server === s.id
                      ? "bg-accent/25 border border-accent text-white"
                      : "border border-transparent text-white/60 hover:bg-white/10",
                  )}
                >
                  {s.label}
                </button>
              ))}
            </div>
            <button
              onClick={close}
              className="p-1 rounded-md hover:bg-white/10"
            >
              <X className="w-4 h-4" />
            </button>
          </div>
        </header>

        <div className="flex items-center gap-2 text-[11px] text-amber-300 bg-amber-500/10 border border-amber-500/30 rounded-md px-3 py-1.5 mb-3">
          <AlertTriangle className="w-3.5 h-3.5 flex-shrink-0" />
          {t("projects.vhostModal.warning")}
        </div>

        {notGenerated ? (
          <div className="flex-1 min-h-0 flex items-center justify-center bg-black/40 border border-white/10 rounded-lg p-3 text-xs text-white/40 text-center">
            {t("projects.vhostModal.notGenerated", { server })}
          </div>
        ) : (
          <textarea
            value={content}
            onChange={(e) => setContent(e.target.value)}
            disabled={loading}
            spellCheck={false}
            className="flex-1 min-h-0 bg-black/40 border border-white/10 rounded-lg p-3 font-mono text-[12px] text-white/90 resize-none focus:outline-none focus:border-accent"
          />
        )}

        <div className="flex justify-between items-center mt-3">
          <button
            onClick={reset}
            className="px-3 py-1.5 rounded-lg border border-white/10 bg-white/5 hover:bg-white/10 text-xs flex items-center gap-1.5"
            title={t("projects.vhostModal.resetTooltip")}
          >
            <RotateCcw className="w-3.5 h-3.5" /> {t("projects.vhostModal.resetButton")}
          </button>
          <div className="flex gap-2">
            <button
              onClick={close}
              className="px-4 py-1.5 rounded-lg border border-white/10 bg-white/5 hover:bg-white/10 text-sm"
            >
              {t("common.cancel")}
            </button>
            <button
              onClick={save}
              disabled={!dirty || notGenerated}
              className="px-4 py-1.5 rounded-lg bg-accent/30 border border-accent text-sm hover:bg-accent/50 disabled:opacity-40 disabled:cursor-not-allowed flex items-center gap-1.5"
            >
              <Save className="w-3.5 h-3.5" /> {t("projects.vhostModal.saveButton")}
            </button>
          </div>
        </div>
      </div>
    </div>,
    document.body
  );
}
