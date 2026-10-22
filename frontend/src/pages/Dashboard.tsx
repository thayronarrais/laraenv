import { useEffect, useState } from "react";
import { RefreshCw, Server, Code2, Play, Square, Activity } from "lucide-react";
// @ts-ignore
import {
  StartService, StopService, ServiceStatus, ListServices,
  ListPhpVersions, SetActivePhp,
  ListNodeVersions, SetActiveNode,
  IsStackBootstrapped,
  StartManagedAll, StopManagedAll,
} from "../../wailsjs/go/main/App";
import BootstrapStackModal from "../components/BootstrapStackModal";
import VcRedistModal, { REDIST_PROBE } from "../components/VcRedistModal";
// @ts-ignore
import { EventsOn } from "../../wailsjs/runtime";
import GlassCard from "../components/GlassCard";
import ServiceCard from "../components/ServiceCard";
import PhpFpmStatus from "../components/PhpFpmStatus";
import logoUrl from "../../icon.png";
import SystemMetrics from "../components/SystemMetrics";
import { useT } from "../lib/i18n";

type Status = "running" | "stopped" | "starting" | "stopping";
type Pending = "starting" | "stopping" | undefined;
type PhpVersion = { version: string; path: string; active: boolean };
type NodeVersion = { version: string; path: string; active: boolean };
type Service = { name: string; label: string };

function StartStopAll({
  services, status, flash, onBootstrapNeeded, onPendingAll,
}: {
  services: Service[];
  status: Record<string, Status>;
  flash: (m: string) => void;
  onBootstrapNeeded: () => void;
  onPendingAll: (phase: Pending) => void;
}) {
  const t = useT();
  const [busy, setBusy] = useState<Pending>(undefined);
  const anyRunning = services.some((s) => status[s.name] === "running");
  const action = async () => {
    if (busy) return;
    // Fresh-install gate: when the recommended stack isn't installed yet,
    // route Start All into the bootstrap modal instead of trying to start
    // services that have no binary on disk.
    if (!anyRunning) {
      const bootstrapped = await IsStackBootstrapped();
      if (!bootstrapped) {
        onBootstrapNeeded();
        return;
      }
    }
    const phase: Pending = anyRunning ? "stopping" : "starting";
    setBusy(phase);
    onPendingAll(phase); // flip every card to STARTING…/STOPPING… immediately
    try {
      // StartManagedAll/StopManagedAll handle php-fpm explicitly in
      // addition to the user-visible services, so the PHP-FPM card on
      // the Dashboard tracks the rest of the stack regardless of how
      // nginx Deps happen to be wired at any given moment.
      if (anyRunning) {
        await StopManagedAll();
        flash(t("dashboard.services.stoppedAll"));
      } else {
        await StartManagedAll();
        flash(t("dashboard.services.startedAll"));
      }
    } catch (e) {
      flash(t("dashboard.errors.generic", { message: String(e) }));
    } finally {
      setBusy(undefined);
      onPendingAll(undefined);
    }
  };
  return (
    <button
      onClick={action}
      disabled={!!busy}
      className={
        "w-full px-3 py-2 rounded-lg text-sm border flex items-center justify-center gap-2 transition-colors " +
        (busy
          ? "bg-white/5 border-white/15 text-white/70 cursor-wait"
          : anyRunning
          ? "bg-red-500/15 border-red-500/40 hover:bg-red-500/30 text-red-200"
          : "bg-emerald-500/15 border-emerald-500/40 hover:bg-emerald-500/30 text-emerald-200")
      }
    >
      {busy ? (
        <RefreshCw className="w-3.5 h-3.5 animate-spin" />
      ) : anyRunning ? (
        <Square className="w-3.5 h-3.5" />
      ) : (
        <Play className="w-3.5 h-3.5" />
      )}
      {busy === "starting"
        ? t("dashboard.services.starting")
        : busy === "stopping"
          ? t("dashboard.services.stopping")
          : anyRunning
            ? t("dashboard.services.stopAll")
            : t("dashboard.services.startAll")}
    </button>
  );
}

export default function Dashboard({ flash }: { flash: (m: string) => void }) {
  const t = useT();
  const SERVICE_TOOLTIPS: Record<string, string> = {
    nginx: t("dashboard.serviceTooltips.webServer"),
    apache: t("dashboard.serviceTooltips.webServer"),
    caddy: t("dashboard.serviceTooltips.webServer"),
    mysql: t("dashboard.serviceTooltips.mysql"),
    postgres: t("dashboard.serviceTooltips.postgres"),
    mailpit: t("dashboard.serviceTooltips.mailpit"),
    redis: t("dashboard.serviceTooltips.redis"),
  };
  const [services, setServices] = useState<Service[]>([]);
  const [status, setStatus] = useState<Record<string, Status>>({});
  // Optimistic transitional state set on click and cleared by the real
  // service:status event (or a safety timeout). Lets the card show
  // STARTING…/STOPPING… and disables itself so a double-click can't fire.
  const [pending, setPending] = useState<Record<string, Pending>>({});
  const [phpVersions, setPhpVersions] = useState<PhpVersion[]>([]);
  const [nodeVersions, setNodeVersions] = useState<NodeVersion[]>([]);
  const [bootstrapOpen, setBootstrapOpen] = useState(false);
  const [vcRedist, setVcRedist] = useState<{ open: boolean; binary?: string; redistUrl?: string }>({ open: false });
  const quote = {
    text: t("dashboard.hero.quoteFallbackText"),
    author: t("dashboard.hero.quoteFallbackAuthor"),
  };

  const loadServices = async () => {
    const list: Service[] = await ListServices();
    setServices(list);
    const entries = await Promise.all(
      list.map(async (s) => [s.name, (await ServiceStatus(s.name)) as Status] as const)
    );
    setStatus(Object.fromEntries(entries));
  };

  useEffect(() => {
    loadServices();
    ListPhpVersions().then((v: PhpVersion[]) => setPhpVersions(v ?? []));
    ListNodeVersions().then((v: NodeVersion[]) => setNodeVersions(v ?? []));

    // Keep the cancelers EventsOn returns and call them on cleanup.
    // EventsOff("<name>") removes EVERY listener for that event app-wide —
    // with kept-alive pages that permanently deafens other components
    // (the ServiceLogsModal close used to freeze these cards this way).
    const offs = [
      EventsOn("service:status", (name: string, st: string) => {
        setStatus((prev) => ({ ...prev, [name]: st as Status }));
        // The backend is now authoritative — drop any optimistic pending state.
        setPending((p) => (p[name] ? { ...p, [name]: undefined } : p));
      }),
      EventsOn("service:error", (e: { service: string; message: string; redistUrl?: string }) => {
        // Surface the VC++ redist problem as a proper modal — the toast
        // message has a long URL that's painful to read or copy from a
        // 4-second flash. redistUrl comes straight from the backend
        // (vcruntime.Check via ErrorEvent.redistUrl) — different PHP
        // builds need different redistributables, so it's threaded
        // through as data rather than re-derived here.
        if (e.message.includes(REDIST_PROBE)) {
          const binMatch = e.message.match(/^([^:]+\.exe)/);
          setVcRedist({ open: true, binary: binMatch ? binMatch[1] : undefined, redistUrl: e.redistUrl });
          return;
        }
        flash(`${e.service}: ${e.message.slice(0, 200)}`);
      }),
      EventsOn("stack:changed", () => loadServices()),
      // Generic install event (kind carried in the payload) so the Runtimes
      // card picks up a freshly installed PHP or Node version without a
      // manual refresh-button click or a full page reload.
      EventsOn("install:installed", (data: { kind?: string }) => {
        if (data?.kind === "php") ListPhpVersions().then((v: PhpVersion[]) => setPhpVersions(v ?? []));
        if (data?.kind === "nodejs") ListNodeVersions().then((v: NodeVersion[]) => setNodeVersions(v ?? []));
      }),
    ];
    return () => offs.forEach((off) => off());
  }, []);

  const toggle = async (name: string) => {
    if (pending[name]) return; // already starting/stopping — ignore re-clicks
    const isRunning = status[name] === "running";
    const phase: Pending = isRunning ? "stopping" : "starting";
    setPending((p) => ({ ...p, [name]: phase }));
    // Safety net: if no status change arrives (e.g. the process crashed on
    // launch with no status transition), drop the spinner after a while.
    window.setTimeout(
      () => setPending((p) => (p[name] === phase ? { ...p, [name]: undefined } : p)),
      8000,
    );
    try {
      if (isRunning) await StopService(name);
      else await StartService(name);
    } catch (e) {
      setPending((p) => ({ ...p, [name]: undefined }));
      flash(t("dashboard.errors.generic", { message: String(e) }));
    }
  };

  const switchPhp = async (v: string) => {
    try {
      await SetActivePhp(v);
      setPhpVersions(await ListPhpVersions());
      flash(t("dashboard.runtimes.phpActive", { version: v }));
    } catch (e) { flash(t("dashboard.errors.generic", { message: String(e) })); }
  };

  const switchNode = async (v: string) => {
    try {
      await SetActiveNode(v);
      setNodeVersions(await ListNodeVersions());
      flash(t("dashboard.runtimes.nodeActive", { version: v }));
    } catch (e) { flash(t("dashboard.errors.generic", { message: String(e) })); }
  };

  return (
    <div className="grid grid-cols-12 gap-3 h-full min-h-0">
      {/* LEFT column: Services + Shortcuts (under) */}
      <div className="col-span-7 flex flex-col gap-3">
        <GlassCard title={t("dashboard.services.title")} icon={<Server className="w-4 h-4" />}>
          <div className="flex flex-col gap-3">
            <div className="grid grid-cols-3 gap-2">
              {services.map((s) => (
                <ServiceCard
                  key={s.name}
                  name={s.name}
                  label={s.label}
                  status={pending[s.name] ?? status[s.name]}
                  onToggle={() => toggle(s.name)}
                  tooltip={SERVICE_TOOLTIPS[s.name]}
                />
              ))}
              {services.length === 0 && (
                <div className="col-span-3 text-sm opacity-50">
                  {t("dashboard.services.emptyPrefix")} <b>{t("dashboard.services.setupAndStart")}</b>{" "}
                  {t("dashboard.services.emptySuffix")}
                </div>
              )}
            </div>
            {services.length > 0 ? (
              <StartStopAll
                services={services}
                status={status}
                flash={flash}
                onBootstrapNeeded={() => setBootstrapOpen(true)}
                onPendingAll={(phase) => {
                  // Only mark cards that are actually transitioning: starting
                  // skips already-running services, stopping skips stopped ones.
                  setPending(() => {
                    if (!phase) return {};
                    const next: Record<string, Pending> = {};
                    for (const s of services) {
                      const running = status[s.name] === "running";
                      if ((phase === "starting" && !running) || (phase === "stopping" && running)) {
                        next[s.name] = phase;
                      }
                    }
                    return next;
                  });
                }}
              />
            ) : (
              <button
                onClick={() => setBootstrapOpen(true)}
                className="w-full px-3 py-2 rounded-lg text-sm border flex items-center justify-center gap-2 bg-accent/20 border-accent/40 hover:bg-accent/30"
              >
                <Play className="w-3.5 h-3.5" /> {t("dashboard.services.setupAndStart")}
              </button>
            )}
          </div>
        </GlassCard>

        {/* Brand hero — fills the remaining vertical space on the left. */}
        <div className="glass-strong flex-1 min-h-0 rounded-2xl p-6 flex flex-col items-center justify-center text-center gap-4 relative overflow-hidden">
          <div
            className="absolute inset-0 opacity-30 pointer-events-none"
            style={{
              background:
                "radial-gradient(circle at 30% 20%, rgba(31,107,255,0.35), transparent 60%), radial-gradient(circle at 70% 80%, rgba(24,221,198,0.22), transparent 55%)",
            }}
          />
          <img
            src={logoUrl}
            alt="LaraEnv"
            className="w-24 h-24 rounded-2xl object-contain shadow-[0_0_60px_rgba(31,107,255,0.55)] relative"
          />
          <div className="relative">
            <div className="text-3xl font-semibold tracking-wide">
              Lara<span className="brand-env">Env</span>
            </div>
            <div className="text-[11px] opacity-50 tracking-[0.2em] uppercase mt-1">
              {t("dashboard.hero.tagline")}
            </div>
          </div>
          <blockquote className="relative max-w-sm text-sm italic opacity-75 leading-relaxed">
            “{quote.text}”
            {quote.author && (
              <div className="text-[10px] not-italic opacity-50 mt-2 tracking-wider uppercase">
                — {quote.author}
              </div>
            )}
          </blockquote>
        </div>
      </div>

      {/* RIGHT column: System Metrics + PHP Version + PHP-FPM */}
      <div className="col-span-5 flex flex-col gap-3">
        <GlassCard title={t("dashboard.system.title")} icon={<Activity className="w-4 h-4" />}>
          <SystemMetrics />
        </GlassCard>

        <GlassCard title={t("dashboard.runtimes.title")} icon={<Code2 className="w-4 h-4" />}>
          <div className="flex flex-col gap-1.5">
            <div className="flex items-center gap-2">
              <span className="text-[10px] uppercase tracking-wider opacity-60 w-10 shrink-0">PHP</span>
              {phpVersions.length === 0 ? (
                <span className="text-[11px] opacity-50">{t("dashboard.runtimes.noneInstalled")}</span>
              ) : (
                <div className="flex flex-wrap gap-1 flex-1">
                  {phpVersions.map((v) => (
                    <button
                      key={v.version}
                      onClick={() => switchPhp(v.version)}
                      className={
                        "px-2 py-0.5 rounded-md text-xs border transition-all " +
                        (v.active
                          ? "bg-accent/30 border-accent text-white shadow-[0_0_12px_rgba(124,92,255,0.45)]"
                          : "bg-white/5 border-white/10 hover:bg-white/10 text-white/80")
                      }
                    >{v.version}</button>
                  ))}
                </div>
              )}
              <button
                onClick={() => ListPhpVersions().then(setPhpVersions)}
                className="p-1 rounded-md border border-white/10 hover:bg-white/10 shrink-0"
                title={t("dashboard.runtimes.reloadPhpTooltip")}
              ><RefreshCw className="w-3 h-3" /></button>
            </div>

            <div className="flex items-center gap-2">
              <span className="text-[10px] uppercase tracking-wider opacity-60 w-10 shrink-0">Node</span>
              {nodeVersions.length === 0 ? (
                <span className="text-[11px] opacity-50">{t("dashboard.runtimes.noneInstalled")}</span>
              ) : (
                <div className="flex flex-wrap gap-1 flex-1">
                  {nodeVersions.map((v) => (
                    <button
                      key={v.version}
                      onClick={() => switchNode(v.version)}
                      className={
                        "px-2 py-0.5 rounded-md text-xs border transition-all " +
                        (v.active
                          ? "bg-accent/30 border-accent text-white shadow-[0_0_12px_rgba(124,92,255,0.45)]"
                          : "bg-white/5 border-white/10 hover:bg-white/10 text-white/80")
                      }
                    >{v.version}</button>
                  ))}
                </div>
              )}
              <button
                onClick={() => ListNodeVersions().then(setNodeVersions)}
                className="p-1 rounded-md border border-white/10 hover:bg-white/10 shrink-0"
                title={t("dashboard.runtimes.reloadNodeTooltip")}
              ><RefreshCw className="w-3 h-3" /></button>
            </div>
          </div>
        </GlassCard>

        <GlassCard title="PHP-FPM" icon={<Code2 className="w-4 h-4" />}>
          <PhpFpmStatus flash={flash} />
        </GlassCard>
      </div>

      <BootstrapStackModal
        open={bootstrapOpen}
        onClose={() => {
          setBootstrapOpen(false);
          loadServices();
          ListPhpVersions().then((v: PhpVersion[]) => setPhpVersions(v ?? []));
          ListNodeVersions().then((v: NodeVersion[]) => setNodeVersions(v ?? []));
        }}
        flash={flash}
      />
      <VcRedistModal
        open={vcRedist.open}
        binary={vcRedist.binary}
        redistUrl={vcRedist.redistUrl}
        onClose={() => setVcRedist({ open: false })}
      />
    </div>
  );
}
