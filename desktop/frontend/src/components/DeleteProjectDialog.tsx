import { useState } from "react";
import { createPortal } from "react-dom";
import { AlertTriangle, Trash2, X } from "lucide-react";
import { useDialogs } from "./dialogs/DialogProvider";
// @ts-ignore
import { RemoveProject, DeleteProject } from "../../wailsjs/go/main/App";
import { useT } from "../lib/i18n";

export default function DeleteProjectDialog({
  projectName, projectPath, open, onClose, flash, onDone,
}: {
  projectName: string | null;
  projectPath: string;
  open: boolean;
  onClose: () => void;
  flash: (m: string) => void;
  onDone: () => void;
}) {
  const t = useT();
  const [dropDb, setDropDb] = useState(true);
  const dialogs = useDialogs();
  if (!open || !projectName) return null;

  const removeVhostOnly = async () => {
    try {
      await RemoveProject(projectName);
      flash(t("projects.deleteDialog.vhostRemovedFlash"));
      onDone();
      onClose();
    } catch (e) { flash(t("projects.errors.generic", { message: String(e) })); }
  };

  const deleteEverything = async () => {
    if (!(await dialogs.confirm({
      title: t("projects.deleteDialog.confirmTitle", { name: projectName }),
      message: t(dropDb ? "projects.deleteDialog.confirmMessageWithDb" : "projects.deleteDialog.confirmMessage", { path: projectPath }),
      confirmLabel: t("projects.common.deleteLabel"),
      danger: true,
    }))) return;
    try {
      await DeleteProject(projectName, dropDb);
      flash(t(dropDb ? "projects.deleteDialog.deletedFlashWithDb" : "projects.deleteDialog.deletedFlash", { name: projectName }));
      onDone();
      onClose();
    } catch (e) { flash(t("projects.errors.generic", { message: String(e) })); }
  };

  return createPortal(
    <div className="modal-backdrop">
      <div
        onClick={(e) => e.stopPropagation()}
        className="modal-card w-[480px] max-w-[92vw] p-5 flex flex-col gap-4"
      >
        <header className="flex items-center justify-between">
          <h2 className="text-base font-semibold">{t("projects.deleteDialog.title", { name: projectName })}</h2>
          <button onClick={onClose} className="p-1 rounded-md hover:bg-white/10">
            <X className="w-4 h-4" />
          </button>
        </header>

        <p className="text-sm opacity-80">
          {t("projects.deleteDialog.prompt")} <code className="text-accent2">{projectName}</code>?
        </p>

        <div className="flex flex-col gap-2">
          <button
            onClick={removeVhostOnly}
            className="text-left p-3 rounded-lg border border-white/10 bg-white/5 hover:bg-white/10 flex items-start gap-3"
          >
            <Trash2 className="w-4 h-4 mt-0.5 text-amber-300 flex-shrink-0" />
            <div>
              <div className="text-sm font-medium">{t("projects.deleteDialog.vhostOnlyTitle")}</div>
              <div className="text-[11px] opacity-60">
                {t("projects.deleteDialog.vhostOnlyDescPrefix")}
                {" "}<code>{projectPath}</code> {t("projects.deleteDialog.vhostOnlyDescSuffix")}
              </div>
            </div>
          </button>

          <button
            onClick={deleteEverything}
            className="text-left p-3 rounded-lg border border-red-500/40 bg-red-500/10 hover:bg-red-500/20 flex items-start gap-3"
          >
            <AlertTriangle className="w-4 h-4 mt-0.5 text-red-400 flex-shrink-0" />
            <div>
              <div className="text-sm font-medium text-red-200">{t("projects.deleteDialog.fullDeleteTitle")}</div>
              <div className="text-[11px] opacity-70">
                {t("projects.deleteDialog.fullDeleteDescPrefix")}
                {" "}<code>{projectPath}</code>{t("projects.deleteDialog.fullDeleteDescSuffix")}
              </div>
            </div>
          </button>
        </div>

        <label className="flex items-center gap-2 text-[11px] opacity-80 cursor-pointer select-none">
          <input
            type="checkbox"
            checked={dropDb}
            onChange={(e) => setDropDb(e.target.checked)}
            className="accent-red-400"
          />
          {t("projects.deleteDialog.dropDbLabel")}
        </label>

        <div className="flex justify-end">
          <button
            onClick={onClose}
            className="px-4 py-1.5 rounded-lg border border-white/10 bg-white/5 hover:bg-white/10 text-sm"
          >{t("common.cancel")}</button>
        </div>
      </div>
    </div>,
    document.body
  );
}
