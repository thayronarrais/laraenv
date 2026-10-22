import { useEffect, useState } from "react";
import clsx from "clsx";
import { RefreshCw, Download as DownloadIcon, Loader2, AlertTriangle, Check, Sparkles, Power } from "lucide-react";
// @ts-ignore
import { AppVersion, CheckForUpdate, DownloadUpdate, InstallUpdate } from "../../wailsjs/go/main/App";
// @ts-ignore
import { EventsOn } from "../../wailsjs/runtime";
import GlassCard from "../components/GlassCard";
// @ts-ignore
import type { updater } from "../../wailsjs/go/models";
import { useI18n } from "../lib/i18n";

// Mirrored by hand until now, which meant every field added on the Go side
// broke this file at build time. Use the generated model instead.
type Release = updater.Release;

type CheckResult = {
  current: string;
  latest?: Release;
  updateAvail: boolean;
  // Which kind of update this copy applies. The two do very different things
  // to the machine, and the wrong one is not obviously wrong until afterwards,
  // so the card says which before the user commits to it.
  portable: boolean;
};

type Progress = {
  phase: "download" | "verify" | "ready" | "error";
  version: string;
  bytes: number;
  total: number;
  percent: number;
  bytesPerSec: number;
  path?: string;
  message?: string;
};

function fmtBytes(n: number): string {
  if (!n) return "0 B";
  const u = ["B", "KB", "MB", "GB"];
  let i = 0;
  while (n >= 1024 && i < u.length - 1) { n /= 1024; i++; }
  return `${n.toFixed(i ? 1 : 0)} ${u[i]}`;
}

// Defect fix: this used to format in the BROWSER locale via toLocaleDateString(),
// not the app's chosen language. Routed through the shared formatDate instead.
function fmtDate(iso: string, formatDate: (d: Date | number, opts?: Intl.DateTimeFormatOptions) => string): string {
  if (!iso) return "";
  try { return formatDate(new Date(iso)); }
  catch { return iso; }
}

type Props = {
  flash: (msg: string) => void;
  onAvailabilityChange?: (avail: boolean) => void;
};

export default function Updates({ flash, onAvailabilityChange }: Props) {
  const { t, formatDate } = useI18n();
  const [current, setCurrent] = useState<string>("");
  const [check, setCheck] = useState<CheckResult | null>(null);
  const [checking, setChecking] = useState(false);
  const [prog, setProg] = useState<Progress | null>(null);

  useEffect(() => {
    AppVersion().then((v: string) => setCurrent(v ?? ""));
    // Per-listener canceler from EventsOn — a global EventsOff("update:progress")
    // would remove every listener for the event app-wide.
    const off = EventsOn("update:progress", (p: Progress) => setProg(p));
    return () => off();
  }, []);

  const runCheck = async (silent = false) => {
    setChecking(true);
    try {
      const res: CheckResult = await CheckForUpdate();
      setCheck(res);
      onAvailabilityChange?.(!!res?.updateAvail);
      if (!silent) {
        flash(res.updateAvail
          ? t("updates.flash.updateAvailable", { version: res.latest?.version ?? "" })
          : t("updates.upToDate.title"));
      }
    } catch (e) {
      if (!silent) flash(t("updates.flash.checkFailed", { error: String(e) }));
    } finally {
      setChecking(false);
    }
  };

  // Silent check on mount.
  useEffect(() => { runCheck(true); /* eslint-disable-line */ }, []);

  const startDownload = async () => {
    if (!check?.latest) return;
    setProg({ phase: "download", version: check.latest.version, bytes: 0, total: check.latest.sizeBytes, percent: 0, bytesPerSec: 0 });
    try {
      await DownloadUpdate(check.latest);
    } catch (e) {
      flash(t("updates.flash.downloadFailed", { error: String(e) }));
    }
  };

  const restartAndInstall = async () => {
    if (!prog?.path) return;
    try {
      await InstallUpdate(prog.path);
    } catch (e) {
      flash(t("updates.flash.installFailed", { error: String(e) }));
    }
  };

  const busy = prog && (prog.phase === "download" || prog.phase === "verify");
  const ready = prog?.phase === "ready";
  const errored = prog?.phase === "error";
  const latest = check?.latest;
  // A portable copy downloads the executable, not the installer, so quoting
  // sizeBytes at it advertised a file it never fetches.
  const downloadSize = (check?.portable ? latest?.exeSizeBytes : latest?.sizeBytes) ?? 0;

  return (
    <div className="h-full overflow-y-auto pr-2">
      <GlassCard
        className="mb-4"
        title={t("updates.page.title")}
        icon={<RefreshCw className="w-4 h-4" />}
      >
        <div className="flex items-center justify-between gap-3">
          <div className="flex flex-col">
            <span className="text-[11px] uppercase tracking-wider opacity-50">{t("updates.page.currentVersionLabel")}</span>
            <span className="font-mono text-base">{current ? `v${current}` : "—"}</span>
          </div>
          <button
            onClick={() => runCheck(false)}
            disabled={checking}
            className={clsx(
              "px-3 py-1.5 rounded-md text-xs border flex items-center gap-2 transition-colors",
              checking
                ? "bg-white/5 border-white/10 opacity-60 cursor-wait"
                : "bg-white/5 border-white/10 hover:bg-white/10"
            )}
          >
            {checking ? <Loader2 className="w-3.5 h-3.5 animate-spin" /> : <RefreshCw className="w-3.5 h-3.5" />}
            {t("updates.page.checkButton")}
          </button>
        </div>
      </GlassCard>

      {check && !check.updateAvail && (
        <GlassCard title={t("updates.upToDate.title")} icon={<Check className="w-4 h-4" />}>
          <div className="text-sm text-white/70">
            {t("updates.upToDate.message", { version: current })}
          </div>
        </GlassCard>
      )}

      {check?.updateAvail && latest && (
        <GlassCard
          title={t("updates.available.title", { version: latest.version })}
          icon={<Sparkles className="w-4 h-4" />}
        >
          <div className="flex flex-col gap-3">
            <div className="flex items-center justify-between text-[11px] opacity-60">
              <span>{t("updates.available.released", { date: fmtDate(latest.publishedAt, formatDate) })}</span>
              {downloadSize > 0 && <span>{fmtBytes(downloadSize)}</span>}
            </div>

            <div className="text-[11px] opacity-70">
              {check.portable
                ? t("updates.available.kindPortable")
                : t("updates.available.kindInstalled")}
            </div>

            {latest.notes && (
              <pre className="glass p-3 text-[12px] whitespace-pre-wrap font-mono leading-relaxed max-h-64 overflow-y-auto">
                {latest.notes}
              </pre>
            )}

            {prog && prog.phase !== "ready" && (
              <div className="flex flex-col gap-1">
                <div className="h-1.5 bg-white/5 rounded-full overflow-hidden">
                  <div
                    className={clsx(
                      "h-full transition-all",
                      errored
                        ? "bg-red-500"
                        : "bg-gradient-to-r from-accent to-accent2"
                    )}
                    style={{ width: `${Math.max(prog.percent, prog.phase === "verify" ? 100 : 2)}%` }}
                  />
                </div>
                <div className="text-[10px] opacity-60 flex justify-between">
                  <span>
                    {prog.phase === "download" && prog.total > 0
                      ? `${fmtBytes(prog.bytes)} / ${fmtBytes(prog.total)}`
                      : prog.phase === "verify"
                      ? t("updates.available.verifyingIntegrity")
                      : prog.phase === "error"
                      ? <span className="text-red-400 flex items-center gap-1"><AlertTriangle className="w-3 h-3" />{prog.message}</span>
                      : ""}
                  </span>
                  {prog.phase === "download" && prog.bytesPerSec > 0 && (
                    <span>{fmtBytes(prog.bytesPerSec)}/s</span>
                  )}
                </div>
              </div>
            )}

            <div className="flex items-center justify-end gap-2">
              {!ready && (
                <button
                  onClick={startDownload}
                  disabled={!!busy}
                  className={clsx(
                    "px-3 py-1.5 rounded-md text-xs border transition-colors flex items-center gap-1.5",
                    busy
                      ? "bg-white/5 border-white/10 opacity-60 cursor-wait"
                      : "bg-accent/30 border-accent hover:bg-accent/50"
                  )}
                >
                  {busy
                    ? <><Loader2 className="w-3.5 h-3.5 animate-spin" /> {prog?.phase === "verify" ? t("updates.phase.verify") : t("updates.phase.download")}</>
                    : <><DownloadIcon className="w-3.5 h-3.5" /> {t("updates.available.downloadButton")}</>}
                </button>
              )}
              {ready && (
                <button
                  onClick={restartAndInstall}
                  className="px-3 py-1.5 rounded-md text-xs border bg-emerald-500/20 border-emerald-500/60 hover:bg-emerald-500/30 flex items-center gap-1.5"
                >
                  <Power className="w-3.5 h-3.5" /> {t("updates.available.restartButton")}
                </button>
              )}
            </div>

            <div className="text-[10px] opacity-50">
              {check.portable
                ? t("updates.available.preserveNotePortable")
                : <>{t("updates.available.preserveNotePrefix")}<span className="font-mono">C:\laraenv\</span>{t("updates.available.preserveNoteSuffix")}</>}
            </div>
          </div>
        </GlassCard>
      )}
    </div>
  );
}
