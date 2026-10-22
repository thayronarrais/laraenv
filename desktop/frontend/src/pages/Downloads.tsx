import { useEffect, useMemo, useState } from "react";
import { Download as DownloadIcon, Check, Loader2, AlertTriangle, RefreshCw, ExternalLink, FolderOpen, Search } from "lucide-react";
import clsx from "clsx";
// @ts-ignore
import { ListSources, InstallSource, OpenSourcePage, InstallFromFile, GetEnabledServices, SetServiceEnabled, ServiceStatus, OpenURL } from "../../wailsjs/go/main/App";
// @ts-ignore
import { EventsOn } from "../../wailsjs/runtime";
import GlassCard from "../components/GlassCard";
import FilterChip from "../components/FilterChip";
import { KIND_LABELS, GROUP_LABEL_KEYS, groupForKind } from "../lib/installKinds";
import { useI18n } from "../lib/i18n";

// Chip order. Server first because it is what a project cannot run without,
// then the rest by how often someone comes here for them.
const GROUP_ORDER = ["server", "database", "language", "mail", "extras"];
import { ALL_CATEGORIES, filterSources } from "../lib/downloadsFilter";

type Source = {
  kind: string;
  version: string;
  url: string;
  installTo: string;
  flatten: boolean;
  manual?: boolean;
  browserUrl?: string;
  notes?: string;
  installed: boolean;
  prerelease?: boolean;
};

type Progress = {
  kind: string;
  version: string;
  phase: "download" | "extract" | "done" | "error";
  bytes: number;
  total: number;
  percent: number;
  bytesPerSec: number;
  message?: string;
};

const ORDER = ["nginx", "apache", "caddy", "mysql", "postgres", "pgvector", "redis", "mailpit", "php", "python", "nodejs", "composer", "cmder"];

// Kinds that map to a startable service in the manager (so they can be
// toggled visible/hidden on the Dashboard). PHP/Python/Node are runtimes,
// not services — they're managed via the Runtimes card.
const SERVICE_KIND: Record<string, string> = {
  nginx: "nginx",
  apache: "apache",
  caddy: "caddy",
  mysql: "mysql",
  postgres: "postgres",
  mailpit: "mailpit",
  redis: "redis",
};

function fmtBytes(n: number): string {
  if (!n) return "0 B";
  const u = ["B", "KB", "MB", "GB"];
  let i = 0;
  while (n >= 1024 && i < u.length - 1) { n /= 1024; i++; }
  return `${n.toFixed(i ? 1 : 0)} ${u[i]}`;
}

function key(kind: string, version: string) { return `${kind}::${version}`; }

// Progress.phase is compared against the raw values elsewhere ("download"/
// "extract"/"done"/"error"); only this display word is translated.
function phaseLabel(t: (key: string) => string, phase?: string): string {
  if (phase === "download") return t("downloads.phase.download");
  if (phase === "extract") return t("downloads.phase.extract");
  return phase ?? "";
}

export type RuntimeDownloadRequest = { seq: number; kind: 'php' | 'nodejs'; version?: string };
export default function Downloads({ flash, runtimeRequest }: { flash: (m: string) => void; runtimeRequest?: RuntimeDownloadRequest }) {
  const { t, formatNumber } = useI18n();
  const GROUP_LABELS = useMemo(
    () => Object.fromEntries(Object.entries(GROUP_LABEL_KEYS).map(([k, key]) => [k, t(key)])),
    [t],
  );
  const [sources, setSources] = useState<Source[]>([]);
  const [progress, setProgress] = useState<Record<string, Progress>>({});
  const [enabled, setEnabled] = useState<Record<string, boolean>>({});
  const [svcStatus, setSvcStatus] = useState<Record<string, string>>({});
  const [category, setCategory] = useState<string>(ALL_CATEGORIES);
  const [query, setQuery] = useState("");
  const [runtimeFocus, setRuntimeFocus] = useState<RuntimeDownloadRequest | null>(null);

  useEffect(() => {
    if (!runtimeRequest) return;
    setRuntimeFocus(runtimeRequest);
    setCategory('language');
    setQuery('');
  }, [runtimeRequest]);

  const refresh = () => ListSources().then((s: Source[]) => setSources(s ?? []));
  const refreshEnabled = () => GetEnabledServices().then((m: Record<string, boolean>) => setEnabled(m ?? {}));
  const refreshStatus = async () => {
    const svcs = Array.from(new Set(Object.values(SERVICE_KIND)));
    const entries = await Promise.all(svcs.map(async (n) => [n, await ServiceStatus(n)] as const));
    setSvcStatus(Object.fromEntries(entries));
  };

  useEffect(() => {
    refresh();
    refreshEnabled();
    refreshStatus();
    // Per-listener cancelers from EventsOn — a global EventsOff("<name>")
    // would deafen every other kept-alive page listening to the same event.
    const offs = [
      EventsOn("service:status", () => refreshStatus()),
      EventsOn("install:progress", (p: Progress) => {
        setProgress((prev) => ({ ...prev, [key(p.kind, p.version)]: p }));
        if (p.phase === "done") {
          flash(t("downloads.flash.installed", { name: KIND_LABELS[p.kind] ?? p.kind, version: p.version }));
          refresh();
        } else if (p.phase === "error") {
          flash(t("downloads.errors.generic", { message: p.message ?? "" }));
        }
      }),
    ];
    return () => offs.forEach((off) => off());
  }, []);

  const isEnabled = (kind: string) => {
    const svc = SERVICE_KIND[kind];
    return svc ? !!enabled[svc] : false;
  };
  const anyInstalled = (kind: string) => sources.some((s) => s.kind === kind && s.installed);

  const toggleEnabled = async (kind: string) => {
    const svc = SERVICE_KIND[kind];
    if (!svc) return;
    const next = !isEnabled(kind);
    setEnabled((prev) => ({ ...prev, [svc]: next }));
    try {
      await SetServiceEnabled(svc, next);
      flash(t("downloads.flash.dashboardVisibility", {
        name: KIND_LABELS[kind] ?? kind,
        state: next ? t("downloads.flash.enabledState") : t("downloads.flash.hiddenState"),
      }));
    } catch (e) { flash(t("downloads.errors.generic", { message: String(e) })); }
  };

  // Chip counts always reflect the full catalog, independent of the current
  // search text or selected group — same idiom as the tag filter chips on
  // Projects.
  const groupCounts = useMemo(() => {
    const counts: Record<string, number> = {};
    for (const s of sources) {
      const g = groupForKind(s.kind);
      counts[g] = (counts[g] ?? 0) + 1;
    }
    return counts;
  }, [sources]);

  const suggestedAvailable = !!runtimeFocus?.version && sources.some(source => source.kind === runtimeFocus.kind && source.version === runtimeFocus.version);
  const filtered = useMemo(() => {
    const matches = filterSources(sources, KIND_LABELS, category, query, groupForKind, GROUP_LABELS);
    if (!runtimeFocus) return matches;
    return matches.filter(source => source.kind === runtimeFocus.kind && (!suggestedAvailable || source.version === runtimeFocus.version));
  }, [sources, category, query, runtimeFocus, suggestedAvailable, GROUP_LABELS]);

  const grouped = useMemo(() => {
    const g: Record<string, Source[]> = {};
    for (const s of filtered) (g[s.kind] ||= []).push(s);
    return g;
  }, [filtered]);

  const install = async (s: Source) => {
    setProgress((prev) => ({
      ...prev,
      [key(s.kind, s.version)]: {
        kind: s.kind, version: s.version, phase: "download",
        bytes: 0, total: 0, percent: 0, bytesPerSec: 0,
      },
    }));
    try { await InstallSource(s.kind, s.version); }
    catch (e) { flash(t("downloads.errors.generic", { message: String(e) })); }
  };

  const visibleKinds = ORDER.filter((k) => grouped[k]);

  return (
    <div className="h-full overflow-y-auto pr-2">
      <div className="flex items-center justify-between mb-4">
        <h1 className="text-lg font-semibold">{t("downloads.page.title")}</h1>
        <button
          onClick={refresh}
          className="glass glass-hover px-3 py-1.5 text-xs flex items-center gap-2"
        >
          <RefreshCw className="w-3.5 h-3.5" /> {t("downloads.page.reloadButton")}
        </button>
      </div>

      {runtimeFocus && <div className="glass mb-3 p-3 text-xs flex items-start justify-between gap-3">
        <div>
          <p>{t('downloads.runtimeRequest.title', { runtime: KIND_LABELS[runtimeFocus.kind] })}</p>
          {runtimeFocus.version && !suggestedAvailable && sources.length > 0 && <p role="status" className="text-amber-300 mt-1">
            {t('downloads.runtimeRequest.unavailable', { runtime: KIND_LABELS[runtimeFocus.kind], version: runtimeFocus.version })}
          </p>}
        </div>
        <button type="button" className="text-accent2 shrink-0 hover:underline" onClick={() => { setRuntimeFocus(null); setCategory(ALL_CATEGORIES); setQuery(''); }}>
          {t('downloads.runtimeRequest.showAll')}
        </button>
      </div>}

      <div className="flex items-center gap-2 mb-3">
        <label className="flex h-8 min-w-0 flex-1 max-w-sm items-center gap-2 rounded-lg border border-white/[0.12] bg-black/25 px-2.5 focus-within:border-accent/45">
          <Search className="h-3.5 w-3.5 shrink-0 text-white/45" />
          <input
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            className="min-w-0 flex-1 bg-transparent text-[12px] text-white/85 outline-none placeholder:text-white/45"
            placeholder={t("downloads.page.searchPlaceholder")}
          />
        </label>
      </div>

      <div className="flex items-center gap-1.5 flex-wrap mb-4">
        <span className="text-[10px] uppercase tracking-wider opacity-50 mr-1">{t("downloads.page.filterLabel")}</span>
        <FilterChip active={category === ALL_CATEGORIES} onClick={() => setCategory(ALL_CATEGORIES)} label={`${t("downloads.page.filterAll")} (${formatNumber(sources.length)})`} />
        {GROUP_ORDER.filter((g) => groupCounts[g]).map((g) => (
          <FilterChip
            key={g}
            active={category === g}
            onClick={() => setCategory(g)}
            label={`${GROUP_LABELS[g]} (${formatNumber(groupCounts[g])})`}
          />
        ))}
      </div>

      {visibleKinds.length === 0 && (
        <div className="text-sm opacity-50 px-1">{t("downloads.page.noMatch")}</div>
      )}

      <div className="columns-1 lg:columns-2 gap-4">
        {visibleKinds.map((k) => {
          const isService = !!SERVICE_KIND[k];
          const installed = anyInstalled(k);
          const on = isEnabled(k);
          const running = isService && svcStatus[SERVICE_KIND[k]] === "running";
          return (
          <GlassCard
            key={k}
            title={KIND_LABELS[k] ?? k}
            icon={<DownloadIcon className="w-4 h-4" />}
            className="break-inside-avoid mb-4"
          >
            {isService && (
              <div className="flex items-center justify-between mb-3 -mt-1 gap-2">
                {installed ? (
                  <span className={clsx(
                    "text-[10px] uppercase tracking-wider flex items-center gap-1.5",
                    running ? "text-emerald-400" : "text-zinc-500",
                  )}>
                    <span className={clsx("w-1.5 h-1.5 rounded-full", running ? "bg-emerald-400 shadow-[0_0_6px_rgba(52,211,153,0.7)]" : "bg-zinc-600")} />
                    {running ? t("downloads.card.running") : t("downloads.card.stopped")}
                  </span>
                ) : <span className="text-[10px] uppercase tracking-wider text-zinc-600">{t("downloads.card.notInstalled")}</span>}
                <div className="flex items-center gap-2">
                  <span className="text-[11px] opacity-60">{t("downloads.card.showOnDashboard")}</span>
                <button
                  onClick={() => toggleEnabled(k)}
                  disabled={!installed}
                  className={clsx(
                    "relative inline-flex h-5 w-9 shrink-0 items-center rounded-full transition-colors",
                    on
                      ? "bg-accent"
                      : "bg-white/10",
                    !installed && "opacity-40 cursor-not-allowed"
                  )}
                  title={!installed ? t("downloads.card.installFirstTooltip") : on ? t("downloads.card.hideFromDashboardTooltip") : t("downloads.card.showOnDashboardTooltip")}
                >
                  <span
                    className={clsx(
                      "inline-block h-3.5 w-3.5 transform rounded-full bg-white transition-transform",
                      on ? "translate-x-5" : "translate-x-1"
                    )}
                  />
                </button>
                </div>
              </div>
            )}
            <div className="flex flex-col gap-1.5">
              {grouped[k].map((s) => {
                const p = progress[key(s.kind, s.version)];
                const busy = p && (p.phase === "download" || p.phase === "extract");
                return (
                  <div
                    key={s.version}
                    className={clsx(
                      "glass px-3 py-2 flex flex-col gap-1",
                      s.installed && "border-emerald-400/25 bg-emerald-400/[0.06]",
                      runtimeFocus?.kind === s.kind && runtimeFocus.version === s.version && "border-accent/50 bg-accent/10",
                    )}
                  >
                    <div className="flex items-center gap-2 min-w-0">
                      <span className="font-mono text-sm shrink-0">{s.version}</span>
                      {runtimeFocus?.kind === s.kind && runtimeFocus.version === s.version && <span className="text-[10px] text-accent2">
                        {t('downloads.runtimeRequest.suggested')}
                      </span>}
                      {s.prerelease && (
                        <span
                          className="text-[10px] uppercase tracking-wider text-amber-400 bg-amber-400/10 border border-amber-400/30 rounded px-1.5 py-0.5 flex items-center gap-1 shrink-0"
                          title={t("downloads.source.betaTooltip")}
                        >
                          <AlertTriangle className="w-3 h-3" /> {t("downloads.source.betaBadge")}
                        </span>
                      )}
                      {s.installed && (
                        <span className="text-[10px] uppercase tracking-wider text-emerald-400 flex items-center gap-1 shrink-0">
                          <Check className="w-3 h-3" /> {t("downloads.source.installedBadge")}
                        </span>
                      )}
                      <span
                        className="text-[10px] opacity-40 truncate flex-1 min-w-0 text-right"
                        title={s.installTo}
                      >
                        {s.installTo}
                      </span>
                      <div className="flex gap-1.5 shrink-0">
                        {s.manual ? (
                          <>
                            <button
                              onClick={() => OpenSourcePage(s.kind, s.version)}
                              className="px-3 py-1 rounded-md text-xs border bg-white/5 border-white/10 hover:bg-white/10 flex items-center gap-1.5"
                              title={t("downloads.source.openPageTooltip")}
                            >
                              <ExternalLink className="w-3 h-3" /> {t("downloads.source.downloadButton")}
                            </button>
                            <button
                              onClick={() => InstallFromFile(s.kind, s.version)}
                              disabled={busy}
                              className={clsx(
                                "px-3 py-1 rounded-md text-xs border transition-colors flex items-center gap-1.5",
                                busy
                                  ? "bg-white/5 border-white/10 opacity-60 cursor-wait"
                                  : "bg-accent/30 border-accent hover:bg-accent/50"
                              )}
                              title={t("downloads.source.pickZipTooltip")}
                            >
                              {busy ? (
                                <><Loader2 className="w-3 h-3 animate-spin" /> {phaseLabel(t, p?.phase)}</>
                              ) : (
                                <><FolderOpen className="w-3 h-3" /> {t("downloads.source.installFromFileButton")}</>
                              )}
                            </button>
                          </>
                        ) : (
                          <button
                            onClick={() => install(s)}
                            disabled={busy}
                            className={clsx(
                              "px-3 py-1 rounded-md text-xs border transition-colors flex items-center gap-1.5",
                              busy
                                ? "bg-white/5 border-white/10 opacity-60 cursor-wait"
                                : s.installed
                                ? "bg-white/5 border-white/10 hover:bg-white/10"
                                : "bg-accent/30 border-accent hover:bg-accent/50"
                            )}
                          >
                            {busy ? (
                              <><Loader2 className="w-3 h-3 animate-spin" /> {phaseLabel(t, p?.phase)}</>
                            ) : s.installed ? (
                              t("downloads.source.reinstallButton")
                            ) : (
                              <><DownloadIcon className="w-3 h-3" /> {t("downloads.source.installButton")}</>
                            )}
                          </button>
                        )}
                      </div>
                    </div>

                    {s.notes && (
                      <div className="text-[11px] opacity-60 leading-relaxed">{s.notes}</div>
                    )}

                    {p && p.phase !== "done" && (
                      <div className="flex flex-col gap-1 pt-0.5">
                        <div className="h-1.5 bg-white/5 rounded-full overflow-hidden">
                          <div
                            className={clsx(
                              "h-full transition-all",
                              p.phase === "error"
                                ? "bg-red-500"
                                : "bg-gradient-to-r from-accent to-accent2"
                            )}
                            style={{ width: `${Math.max(p.percent, p.phase === "extract" ? 100 : 2)}%` }}
                          />
                        </div>
                        <div className="text-[10px] opacity-60 flex justify-between">
                          <span>
                            {p.phase === "download" && p.total > 0
                              ? `${fmtBytes(p.bytes)} / ${fmtBytes(p.total)}`
                              : p.phase === "extract"
                              ? t("downloads.source.extractingFiles")
                              : p.phase === "error"
                              ? <span className="text-red-400 flex items-center gap-1"><AlertTriangle className="w-3 h-3" />{p.message}</span>
                              : ""}
                          </span>
                          {p.phase === "download" && p.bytesPerSec > 0 && (
                            <span>{fmtBytes(p.bytesPerSec)}/s</span>
                          )}
                        </div>
                      </div>
                    )}
                  </div>
                );
              })}
            </div>
          </GlassCard>
          );
        })}
      </div>
    </div>
  );
}
