import { useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { X, FileCode, Save, RotateCcw, Search, Download, Loader2, AlertCircle, Package, ChevronDown, ExternalLink } from "lucide-react";
import clsx from "clsx";
import Editor, { OnMount } from "@monaco-editor/react";
// @ts-ignore
import { EventsOn } from "../../wailsjs/runtime/runtime";
// @ts-ignore
import {
  ListPhpExtensions, SetPhpExtension, GetPhpIni, SavePhpIni,
  FetchPhpExtensionDLL, ListPeclExtensions, InstallPeclExtension,
  OpenFileInDefaultApp, PhpIniPath,
} from "../../wailsjs/go/main/App";
import { useI18n, useT } from "../lib/i18n";

type FetchProgress = {
  kind: string;
  version: string;
  phase: "download" | "extract" | "done" | "error";
  bytes: number;
  total: number;
  percent: number;
  bytesPerSec: number;
  message?: string;
};

// Units (MB/s, kB/s, B/s) are code, not prose — left untranslated. The
// number itself still goes through the caller's formatNumber.
const fmtKB = (n: number, formatNumber: (n: number, opts?: Intl.NumberFormatOptions) => string) => {
  if (n >= 1024 * 1024) return formatNumber(n / 1024 / 1024, { minimumFractionDigits: 1, maximumFractionDigits: 1 }) + " MB/s";
  if (n >= 1024) return formatNumber(Math.round(n / 1024)) + " kB/s";
  return formatNumber(n) + " B/s";
};

type Extension = { name: string; enabled: boolean; available: boolean };

type PeclExt = {
  name: string;
  displayName: string;
  description: string;
  zend: boolean;
  version: string;
  installed: boolean;
  enabled: boolean;
};

export default function PhpExtensionsModal({
  version, open, onClose, flash,
}: {
  version: string | null;
  open: boolean;
  onClose: () => void;
  flash: (m: string) => void;
}) {
  const t = useT();
  const { formatNumber } = useI18n();
  const [exts, setExts] = useState<Extension[]>([]);
  const [pecl, setPecl] = useState<PeclExt[]>([]);
  const [showPecl, setShowPecl] = useState(false);
  const [peclProgress, setPeclProgress] = useState<Record<string, FetchProgress>>({});
  const [filter, setFilter] = useState("");
  const [iniMode, setIniMode] = useState(false);
  const [ini, setIni] = useState("");
  const [iniOriginal, setIniOriginal] = useState("");
  const [iniPath, setIniPath] = useState("");
  const [iniChooser, setIniChooser] = useState(false);
  const iniEditorRef = useRef<any>(null);
  const iniRef = useRef("");
  const [fetching, setFetching] = useState<Set<string>>(new Set());
  const [progress, setProgress] = useState<FetchProgress | null>(null);
  const [fetchError, setFetchError] = useState<string | null>(null);
  const activeRef = useRef(false);

  useEffect(() => {
    if (!open || !version) return;
    setIniMode(false);
    setFilter("");
    setProgress(null);
    setFetchError(null);
    setPeclProgress({});
    refresh();

    // Listen to installer progress while the modal is open. The events fire
    // for every catalog install — we route kind="php" to the bundled DLL
    // fetch lane and kind="pecl-ext" to per-extension PECL lanes.
    // Per-listener canceler — EventsOff("install:progress") would also
    // remove the kept-alive Downloads page's listener for the same event.
    const off = EventsOn("install:progress", (p: FetchProgress) => {
      if (p.kind === "php" && activeRef.current) {
        setProgress(p);
        return;
      }
      if (p.kind === "pecl-ext") {
        setPeclProgress((prev) => ({ ...prev, [p.version]: p }));
        if (p.phase === "done") {
          flash(t("settings.phpExtensions.flash.peclInstalled", { version: p.version }));
          refresh();
          refreshPecl();
        } else if (p.phase === "error") {
          flash(t("settings.phpExtensions.errors.itemMessage", { name: p.version, message: p.message ?? t("settings.phpExtensions.flash.installFailed") }));
        }
      }
    });
    return () => off();
  }, [open, version]);

  const refresh = () => {
    if (!version) return;
    ListPhpExtensions(version)
      .then((list: Extension[]) => setExts(list ?? []))
      .catch((e: any) => flash(t("settings.phpExtensions.errors.generic", { error: String(e) })));
  };

  const refreshPecl = () => {
    if (!version) return;
    ListPeclExtensions(version)
      .then((list: PeclExt[]) => setPecl(list ?? []))
      .catch((e: any) => flash(t("settings.phpExtensions.errors.generic", { error: String(e) })));
  };

  useEffect(() => { if (open && version) refreshPecl(); }, [open, version]);

  const installPecl = async (p: PeclExt) => {
    if (!version) return;
    setPeclProgress((prev) => ({
      ...prev,
      [p.name]: { kind: "pecl-ext", version: p.name, phase: "download", bytes: 0, total: 0, percent: 0, bytesPerSec: 0, message: "starting…" },
    }));
    try {
      await InstallPeclExtension(version, p.name);
    } catch (err) {
      flash(t("settings.phpExtensions.errors.generic", { error: String(err) }));
      setPeclProgress((prev) => { const n = { ...prev }; delete n[p.name]; return n; });
    }
  };

  const toggle = async (e: Extension) => {
    if (!version) return;
    try {
      await SetPhpExtension(version, e.name, !e.enabled);
      flash(t("settings.phpExtensions.flash.extensionToggled", {
        name: e.name,
        state: !e.enabled ? t("settings.phpExtensions.flash.enabledWord") : t("settings.phpExtensions.flash.disabledWord"),
      }));
      refresh();
    } catch (err) { flash(t("settings.phpExtensions.errors.generic", { error: String(err) })); }
  };

  const fetchDLL = async (e: Extension) => {
    if (!version) return;
    activeRef.current = true;
    setFetchError(null);
    setProgress({ kind: "php", version, phase: "download", bytes: 0, total: 0, percent: 0, bytesPerSec: 0, message: "starting…" });
    setFetching((prev) => { const n = new Set(prev); n.add(e.name); return n; });
    try {
      await FetchPhpExtensionDLL(version, e.name);
      flash(t("settings.phpExtensions.flash.dllInstalled", { name: e.name }));
      refresh();
    } catch (err) {
      const msg = String(err).replace(/^Error:\s*/, "");
      setFetchError(t("settings.phpExtensions.errors.itemMessage", { name: e.name, message: msg }));
      flash(t("settings.phpExtensions.errors.generic", { error: msg }));
    } finally {
      activeRef.current = false;
      setProgress(null);
      setFetching((prev) => { const n = new Set(prev); n.delete(e.name); return n; });
    }
  };

  const openIniInBuiltIn = async () => {
    if (!version) return;
    try {
      const c: string = await GetPhpIni(version);
      setIni(c); setIniOriginal(c); iniRef.current = c; setIniMode(true);
    } catch (err) {
      flash(t("settings.phpExtensions.errors.generic", { error: String(err) }));
      return;
    }
    // The subtitle only labels what is already open, so resolving it must
    // never be what keeps the editor closed.
    try { setIniPath(await PhpIniPath(version)); } catch { setIniPath(""); }
  };

  const openIniInSystem = async () => {
    if (!version) return;
    try {
      const p: string = await PhpIniPath(version);
      await OpenFileInDefaultApp(p);
      flash(t("settings.phpExtensions.flash.openedInSystemEditor", { path: p }));
    } catch (err) { flash(t("settings.phpExtensions.errors.generic", { error: String(err) })); }
  };

  const saveIni = async () => {
    if (!version) return;
    try {
      await SavePhpIni(version, ini);
      setIniOriginal(ini);
      flash(t("settings.phpExtensions.flash.iniSaved"));
      refresh();
    } catch (err) { flash(t("settings.phpExtensions.errors.generic", { error: String(err) })); }
  };

  const onIniEditorMount: OnMount = (editor, monaco) => {
    iniEditorRef.current = editor;
    editor.addCommand(monaco.KeyMod.CtrlCmd | monaco.KeyCode.KeyS, () => { saveIni(); });
  };

  if (!open || !version) return null;

  const filtered = exts.filter((e) =>
    !filter || e.name.toLowerCase().includes(filter.toLowerCase())
  );

  const enabledCount = exts.filter((e) => e.enabled).length;

  return createPortal(
    <div className="modal-backdrop">
      <div
        onClick={(e) => e.stopPropagation()}
        className={clsx(
          "modal-card max-w-[94vw] max-h-[90vh] flex flex-col p-4 relative",
          iniMode ? "w-[1100px] h-[800px]" : "w-[560px] h-[640px]",
        )}
      >
        <header className="flex items-center justify-between mb-3">
          <div>
            <h2 className="text-base font-semibold">
              PHP {version} · {iniMode ? "php.ini" : t("settings.phpExtensions.header.extensionsLabel")}
            </h2>
            <div className="text-[11px] opacity-50">
              {iniMode
                ? iniPath
                : t("settings.phpExtensions.header.summary", { enabled: formatNumber(enabledCount), total: formatNumber(exts.length) })}
            </div>
          </div>
          <div className="flex gap-1">
            <button
              onClick={() => (iniMode ? setIniMode(false) : setIniChooser(true))}
              className="px-3 py-1.5 rounded-lg border border-white/10 bg-white/5 hover:bg-white/10 text-xs flex items-center gap-1.5"
            >
              <FileCode className="w-3.5 h-3.5" />
              {iniMode ? t("settings.phpExtensions.header.backToExtensions") : t("settings.phpExtensions.header.viewIni")}
            </button>
            <button onClick={onClose} className="p-1 rounded-md hover:bg-white/10">
              <X className="w-4 h-4" />
            </button>
          </div>
        </header>

        {iniMode ? (
          <>
            <div className="flex-1 min-h-0 rounded-lg overflow-hidden border border-white/10">
              <Editor
                height="100%"
                theme="vs-dark"
                language="ini"
                value={ini}
                onChange={(v) => { const val = v ?? ""; setIni(val); iniRef.current = val; }}
                onMount={onIniEditorMount}
                options={{
                  fontSize: 13,
                  minimap: { enabled: true },
                  automaticLayout: true,
                  tabSize: 2,
                  wordWrap: "on",
                  scrollBeyondLastLine: false,
                }}
              />
            </div>
            <div className="flex justify-end gap-2 mt-3">
              <button
                onClick={() => setIni(iniOriginal)}
                disabled={ini === iniOriginal}
                className="px-3 py-1.5 rounded-lg border border-white/10 bg-white/5 hover:bg-white/10 text-xs flex items-center gap-1.5 disabled:opacity-40"
              >
                <RotateCcw className="w-3.5 h-3.5" /> {t("settings.phpExtensions.ini.revertButton")}
              </button>
              <button
                onClick={saveIni}
                disabled={ini === iniOriginal}
                className="px-4 py-1.5 rounded-lg bg-accent/30 border border-accent text-sm hover:bg-accent/50 disabled:opacity-40 flex items-center gap-1.5"
              >
                <Save className="w-3.5 h-3.5" /> {t("settings.phpExtensions.ini.saveButton")}
              </button>
            </div>
          </>
        ) : (
          <>
            <div className="relative mb-2">
              <Search className="absolute left-2 top-1/2 -translate-y-1/2 w-3.5 h-3.5 opacity-50" />
              <input
                value={filter}
                onChange={(e) => setFilter(e.target.value)}
                placeholder={t("settings.phpExtensions.search.placeholder")}
                className="w-full bg-white/5 border border-white/10 rounded-lg pl-7 pr-3 py-1.5 text-sm focus:outline-none focus:border-accent"
                autoFocus
              />
            </div>

            {pecl.length > 0 && (
              <div className="mb-2">
                <button
                  type="button"
                  onClick={() => setShowPecl((v) => !v)}
                  className="w-full flex items-center justify-between px-2 py-1.5 rounded-lg border border-white/10 bg-white/5 hover:bg-white/10 text-[11px]"
                >
                  <span className="flex items-center gap-1.5">
                    <Package className="w-3.5 h-3.5 opacity-70" />
                    {t("settings.phpExtensions.pecl.summary", {
                      installed: formatNumber(pecl.filter((p) => p.installed).length),
                      total: formatNumber(pecl.length),
                    })}
                  </span>
                  <ChevronDown className={clsx("w-3.5 h-3.5 transition-transform", showPecl && "rotate-180")} />
                </button>
                {showPecl && (
                  <div className="mt-1.5 px-2 py-1.5 rounded-lg border border-white/10 bg-black/20 space-y-1">
                    {pecl.map((p) => {
                      const pp = peclProgress[p.name];
                      const busy = pp && (pp.phase === "download" || pp.phase === "extract");
                      return (
                        <div key={p.name} className="flex items-start gap-2 px-2 py-1.5 rounded hover:bg-white/5">
                          <div className="flex flex-col flex-1 min-w-0">
                            <div className="flex items-center gap-2">
                              <span className="font-mono text-[11px]">{p.displayName}</span>
                              <span className="opacity-40 text-[9px] font-mono">v{p.version}</span>
                              {p.zend && (
                                <span className="text-[9px] uppercase tracking-wider text-purple-300/80 px-1 rounded bg-purple-500/15">
                                  zend
                                </span>
                              )}
                              {p.installed && (
                                <span className="text-[9px] uppercase tracking-wider text-emerald-400">{t("settings.phpExtensions.pecl.installedBadge")}</span>
                              )}
                            </div>
                            <div className="opacity-50 text-[10px] truncate">{p.description}</div>
                            {busy && (
                              <div className="mt-1 h-1 bg-white/10 rounded-full overflow-hidden">
                                <div
                                  className="h-full bg-accent transition-all"
                                  style={{ width: `${Math.max(2, pp?.phase === "extract" ? 100 : pp?.percent ?? 0)}%` }}
                                />
                              </div>
                            )}
                            {pp?.phase === "error" && (
                              <div className="mt-0.5 text-[10px] text-red-300 truncate">{pp.message}</div>
                            )}
                          </div>
                          <button
                            type="button"
                            onClick={() => installPecl(p)}
                            disabled={busy}
                            className={clsx(
                              "px-2 py-0.5 rounded text-[10px] border flex-shrink-0 flex items-center gap-1",
                              busy
                                ? "bg-white/5 border-white/10 opacity-60 cursor-wait"
                                : p.installed
                                  ? "bg-white/5 border-white/10 hover:bg-white/10"
                                  : "bg-accent/30 border-accent hover:bg-accent/50"
                            )}
                            title={p.installed ? t("settings.phpExtensions.pecl.reinstallTooltip") : t("settings.phpExtensions.pecl.installTooltip")}
                          >
                            {busy
                              ? <><Loader2 className="w-2.5 h-2.5 animate-spin" /> {pp?.phase === "extract" ? t("settings.phpExtensions.pecl.phaseExtract") : t("settings.phpExtensions.pecl.phaseDownload")}</>
                              : p.installed
                                ? t("settings.phpExtensions.pecl.reinstallButton")
                                : <><Download className="w-2.5 h-2.5" /> {t("settings.phpExtensions.pecl.installButton")}</>}
                          </button>
                        </div>
                      );
                    })}
                  </div>
                )}
              </div>
            )}

            {progress && (
              <div className="mb-2 px-3 py-2 rounded-lg border border-white/10 bg-white/5">
                <div className="flex items-center justify-between text-[11px] mb-1.5">
                  <span className="font-mono opacity-80">
                    {progress.phase === "download"
                      ? t("settings.phpExtensions.download.downloading", { percent: formatNumber(progress.percent) })
                      : progress.phase === "extract"
                        ? (progress.message ?? t("settings.phpExtensions.download.extracting"))
                        : progress.message ?? progress.phase}
                  </span>
                  <span className="opacity-50 font-mono">
                    {progress.phase === "download" && progress.total > 0
                      ? t("settings.phpExtensions.download.progressDetail", {
                          done: formatNumber(progress.bytes / 1024 / 1024, { minimumFractionDigits: 1, maximumFractionDigits: 1 }),
                          total: formatNumber(progress.total / 1024 / 1024, { minimumFractionDigits: 1, maximumFractionDigits: 1 }),
                          speed: fmtKB(progress.bytesPerSec, formatNumber),
                        })
                      : ""}
                  </span>
                </div>
                <div className="h-1 bg-white/10 rounded-full overflow-hidden">
                  <div
                    className="h-full bg-accent transition-all"
                    style={{ width: `${Math.max(2, progress.percent)}%` }}
                  />
                </div>
              </div>
            )}

            {fetchError && (
              <div className="mb-2 px-3 py-2 rounded-lg border border-red-500/30 bg-red-500/10 text-red-200 text-[11px] flex items-start gap-2">
                <AlertCircle className="w-3.5 h-3.5 flex-shrink-0 mt-0.5" />
                <div className="flex-1 break-words">{fetchError}</div>
                <button
                  type="button"
                  title={t("settings.phpExtensions.errors.dismissTooltip")}
                  onClick={() => setFetchError(null)}
                  className="opacity-60 hover:opacity-100 flex-shrink-0"
                >
                  <X className="w-3.5 h-3.5" />
                </button>
              </div>
            )}
            <div className="flex-1 min-h-0 overflow-y-auto pr-1 grid grid-cols-2 gap-1">
              {filtered.map((e) => (
                <label
                  key={e.name}
                  className={clsx(
                    "flex items-center gap-1.5 px-2 py-1 rounded border cursor-pointer transition-colors",
                    e.enabled
                      ? "bg-emerald-500/15 border-emerald-400/30 hover:bg-emerald-500/25"
                      : "bg-white/5 border-white/10 hover:bg-white/10"
                  )}
                >
                  <input
                    type="checkbox"
                    checked={e.enabled}
                    onChange={() => toggle(e)}
                    className="accent-emerald-400 w-3 h-3"
                  />
                  <span className="font-mono text-[11px] flex-1 truncate">{e.name}</span>
                  {!e.available && (
                    <button
                      type="button"
                      onClick={(ev) => { ev.preventDefault(); fetchDLL(e); }}
                      disabled={fetching.has(e.name)}
                      title={t("settings.phpExtensions.list.downloadDllTooltip")}
                      className="text-[9px] uppercase tracking-wider text-amber-300 hover:text-amber-200 flex-shrink-0 flex items-center gap-1 px-1 py-0.5 rounded hover:bg-amber-400/10 disabled:opacity-50"
                    >
                      {fetching.has(e.name)
                        ? <Loader2 className="w-2.5 h-2.5 animate-spin" />
                        : <Download className="w-2.5 h-2.5" />}
                      {fetching.has(e.name) ? t("settings.phpExtensions.list.fetchingLabel") : t("settings.phpExtensions.list.noDllLabel")}
                    </button>
                  )}
                </label>
              ))}
              {filtered.length === 0 && (
                <div className="col-span-2 text-xs opacity-50 px-3 py-4">
                  {t("settings.phpExtensions.list.empty")}
                </div>
              )}
            </div>
          </>
        )}

        {iniChooser && (
          <div
            className="absolute inset-0 bg-black/40 z-20 flex items-center justify-center rounded-2xl"
            onClick={() => setIniChooser(false)}
          >
            <div
              onClick={(e) => e.stopPropagation()}
              className="rounded-lg border border-white/15 bg-zinc-900 p-4 w-[380px] flex flex-col gap-3"
            >
              <h3 className="text-sm font-semibold">{t("settings.phpExtensions.iniChooser.title")}</h3>
              <p className="text-[11px] opacity-60 font-mono break-all">C:\laraenv\php\{version}\php.ini</p>
              <button
                type="button"
                onClick={() => { openIniInBuiltIn(); setIniChooser(false); }}
                className="px-3 py-2 rounded-md border border-accent/60 bg-accent/20 hover:bg-accent/30 text-xs text-left flex items-start gap-2"
              >
                <FileCode className="w-4 h-4 mt-0.5" />
                <span className="flex-1">
                  <span className="font-medium">{t("settings.phpExtensions.iniChooser.builtinTitle")}</span>
                  <span className="block text-[10px] opacity-70">{t("settings.phpExtensions.iniChooser.builtinDesc")}</span>
                </span>
              </button>
              <button
                type="button"
                onClick={() => { openIniInSystem(); setIniChooser(false); }}
                className="px-3 py-2 rounded-md border border-white/15 hover:bg-white/10 text-xs text-left flex items-start gap-2"
              >
                <ExternalLink className="w-4 h-4 mt-0.5" />
                <span className="flex-1">
                  <span className="font-medium">{t("settings.phpExtensions.iniChooser.systemTitle")}</span>
                  <span className="block text-[10px] opacity-70">{t("settings.phpExtensions.iniChooser.systemDesc")}</span>
                </span>
              </button>
              <button
                type="button"
                onClick={() => setIniChooser(false)}
                className="text-[11px] opacity-60 hover:opacity-100 self-end"
              >
                {t("settings.phpExtensions.iniChooser.cancelButton")}
              </button>
            </div>
          </div>
        )}
      </div>
    </div>,
    document.body
  );
}
