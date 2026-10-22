import { useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import clsx from "clsx";
import { Check, Loader2, Download, X, AlertTriangle } from "lucide-react";
// @ts-ignore
import { RecommendedStack, BootstrapRecommended, StartService } from "../../wailsjs/go/main/App";
// @ts-ignore
import { EventsOn } from "../../wailsjs/runtime";
import { useT } from "../lib/i18n";

type StackItem = {
  kind: string;
  version: string;
  installed: boolean;
  prerelease?: boolean;
};

type Progress = {
  kind: string;
  version: string;
  phase: "download" | "extract" | "done" | "error";
  percent: number;
  message?: string;
};

type Phase = "confirm" | "installing" | "starting" | "done" | "error";

const KIND_LABEL: Record<string, string> = {
  php: "PHP",
  nginx: "nginx",
  mysql: "MySQL",
  mailpit: "Mailpit",
  nodejs: "Node.js",
};

// Services to start after bootstrap finishes. PHP isn't here because its FPM
// definitions are auto-registered as nginx dependencies; starting nginx
// pulls in the matching php-fpm.
const POST_BOOTSTRAP_SERVICES = ["nginx", "mysql", "mailpit"];

export default function BootstrapStackModal({
  open, onClose, flash,
}: {
  open: boolean;
  onClose: () => void;
  flash: (m: string) => void;
}) {
  const t = useT();
  const [stack, setStack] = useState<StackItem[]>([]);
  const [phase, setPhase] = useState<Phase>("confirm");
  const [progress, setProgress] = useState<Record<string, Progress>>({});
  const [errors, setErrors] = useState<Record<string, string>>({});
  const cleanupRef = useRef<(() => void) | null>(null);

  useEffect(() => {
    if (!open) return;
    setPhase("confirm");
    setProgress({});
    setErrors({});
    RecommendedStack().then((list: StackItem[]) => setStack(list ?? []));

    const onProgress = (p: Progress) =>
      setProgress((prev) => ({ ...prev, [p.kind]: p }));

    const onDone = async (result: { ok: boolean; errors: Record<string, string> }) => {
      setErrors(result.errors ?? {});
      if (!result.ok && Object.keys(result.errors ?? {}).length === stack.length) {
        // Every install failed — don't try to start anything.
        setPhase("error");
        return;
      }
      setPhase("starting");
      try {
        for (const name of POST_BOOTSTRAP_SERVICES) {
          await StartService(name).catch(() => { /* surfaces as service:error event */ });
        }
        setPhase("done");
        flash(t("projects.bootstrap.readyFlash"));
        setTimeout(onClose, 1500);
      } catch (e) {
        setPhase("error");
      }
    };

    // Per-listener cancelers — EventsOff("install:progress") would also
    // remove the kept-alive Downloads page's listener for the same event.
    const offProgress = EventsOn("install:progress", onProgress);
    const offDone = EventsOn("bootstrap:done", onDone);
    cleanupRef.current = () => {
      offProgress();
      offDone();
    };
    return () => cleanupRef.current?.();
  }, [open]);

  if (!open) return null;

  const missing = stack.filter((s) => !s.installed);
  const allInstalled = stack.length > 0 && missing.length === 0;

  const start = async () => {
    setPhase("installing");
    try {
      await BootstrapRecommended();
    } catch (e) {
      setPhase("error");
      flash(t("projects.bootstrap.errorFlash", { message: String(e) }));
    }
  };

  const close = () => {
    if (phase === "installing" || phase === "starting") return;
    onClose();
  };

  return createPortal(
    <div className="modal-backdrop">
      <div
        onClick={(e) => e.stopPropagation()}
        className="modal-card w-[560px] max-w-[92vw] max-h-[90vh] flex flex-col p-5"
      >
        <header className="flex items-center justify-between mb-4">
          <h2 className="text-base font-semibold">
            {phase === "confirm" && t("projects.bootstrap.titleConfirm")}
            {phase === "installing" && t("projects.bootstrap.titleInstalling")}
            {phase === "starting" && t("projects.bootstrap.titleStarting")}
            {phase === "done" && t("projects.bootstrap.titleDone")}
            {phase === "error" && t("projects.bootstrap.titleError")}
          </h2>
          <button
            onClick={close}
            disabled={phase === "installing" || phase === "starting"}
            title={t("common.closeTooltip")}
            aria-label={t("common.closeTooltip")}
            className="p-1 rounded-md hover:bg-white/10 disabled:opacity-30"
          >
            <X className="w-4 h-4" />
          </button>
        </header>

        {phase === "confirm" && (
          <p className="text-sm opacity-70 mb-3">
            {allInstalled
              ? t("projects.bootstrap.descAllInstalled")
              : t("projects.bootstrap.descNeedsInstall")}
          </p>
        )}

        <div className="flex flex-col gap-1.5 mb-4">
          {stack.map((item) => {
            const p = progress[item.kind];
            const err = errors[item.kind];
            const pct = p?.percent ?? (item.installed ? 100 : 0);
            const phaseStr: string = err
              ? "error"
              : p?.phase === "done" || item.installed
              ? "done"
              : p?.phase ?? "pending";
            return (
              <div
                key={item.kind}
                className="bg-white/5 border border-white/10 rounded-lg px-3 py-2 flex items-center gap-3"
              >
                <div className="flex-1 min-w-0">
                  <div className="text-sm font-medium flex items-center gap-2">
                    <span>
                      {KIND_LABEL[item.kind] ?? item.kind} {item.version}
                    </span>
                    {item.prerelease && (
                      <span
                        className="text-[10px] uppercase tracking-wider text-amber-400 bg-amber-400/10 border border-amber-400/30 rounded px-1.5 py-0.5 flex items-center gap-1"
                        title={t("projects.bootstrap.prereleaseTooltip")}
                      >
                        <AlertTriangle className="w-3 h-3" /> {t("projects.bootstrap.prereleaseBadge")}
                      </span>
                    )}
                  </div>
                  <div className="text-[10px] opacity-50 truncate">
                    {err
                      ? err
                      : item.installed
                      ? t("projects.bootstrap.alreadyInstalled")
                      : phaseStr === "pending"
                      ? t("projects.bootstrap.queued")
                      : phaseStr === "download"
                      ? t("projects.bootstrap.downloadingPercent", { percent: pct })
                      : phaseStr === "extract"
                      ? t("projects.bootstrap.extracting")
                      : phaseStr === "done"
                      ? t("projects.bootstrap.doneLabel")
                      : phaseStr}
                  </div>
                  {phase !== "confirm" && phaseStr !== "done" && phaseStr !== "error" && (
                    <div className="mt-1 h-1 rounded-full bg-white/5 overflow-hidden">
                      <div
                        className="h-full bg-accent transition-[width] duration-200"
                        style={{ width: `${pct}%` }}
                      />
                    </div>
                  )}
                </div>
                <div className="shrink-0">
                  {err ? (
                    <AlertTriangle className="w-4 h-4 text-red-300" />
                  ) : phaseStr === "done" ? (
                    <Check className="w-4 h-4 text-emerald-300" />
                  ) : phase === "installing" && phaseStr !== "pending" ? (
                    <Loader2 className="w-4 h-4 animate-spin opacity-70" />
                  ) : (
                    <Download className="w-4 h-4 opacity-40" />
                  )}
                </div>
              </div>
            );
          })}
        </div>

        <div className="flex justify-end gap-2 mt-auto">
          {phase === "confirm" && (
            <>
              <button
                onClick={close}
                className="px-4 py-1.5 rounded-lg border border-white/10 bg-white/5 hover:bg-white/10 text-sm"
              >
                {t("common.cancel")}
              </button>
              <button
                onClick={start}
                className="px-4 py-1.5 rounded-lg bg-accent/30 border border-accent text-sm hover:bg-accent/50"
              >
                {allInstalled ? t("projects.bootstrap.startServices") : t("projects.bootstrap.installAndStartButton")}
              </button>
            </>
          )}
          {(phase === "installing" || phase === "starting") && (
            <div className="flex items-center gap-2 text-sm opacity-70">
              <Loader2 className="w-4 h-4 animate-spin" /> {t("projects.common.workingLabel")}
            </div>
          )}
          {(phase === "done" || phase === "error") && (
            <button
              onClick={close}
              className={clsx(
                "px-4 py-1.5 rounded-lg border text-sm",
                phase === "done"
                  ? "bg-emerald-500/20 border-emerald-400/40 hover:bg-emerald-500/30 text-emerald-100"
                  : "bg-red-500/20 border-red-400/40 hover:bg-red-500/30 text-red-100"
              )}
            >
              {t("common.close")}
            </button>
          )}
        </div>
      </div>
    </div>,
    document.body
  );
}
