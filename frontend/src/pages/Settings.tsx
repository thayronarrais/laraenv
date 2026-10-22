import { useEffect, useRef, useState } from "react";
import { useDialogs } from "../components/dialogs/DialogProvider";
import clsx from "clsx";
import { Palette, Check, RefreshCw, User, Settings as SettingsIcon, Terminal as TerminalIcon, TerminalSquare, Server, Command, FlaskConical, Archive, ShieldCheck } from "lucide-react";
import GlassCard from "../components/GlassCard";
import LegalNotices from "../components/LegalNotices";
import CommandCenterSettings from "../components/CommandCenterSettings";
import { LanguageSelector } from "../components/LanguageSelector";
import MCPSettings from "../components/MCPSettings";
import { THEMES, ThemeId, applyTheme, getCurrentTheme } from "../theme";
import {
  TerminalCopyMode,
  getTerminalCopyMode,
  setTerminalCopyMode,
} from "../terminalPrefs";
import Updates from "./Updates";
import Account, { Status as CloudStatusType } from "./Account";
import {
  GetServicesConfig,
  SetNginxTuning,
  SetApacheTuning,
  SetPhpTuning,
  SetAgentSessionsEnabled,
  ResetAgentMetadata,
  GetAgentHooksEnabled,
  SetAgentHooksEnabled,
  GetAgentHooksStatus,
  LocalCAStatus,
  TrustLocalCA,
} from "../../wailsjs/go/main/App";
import { agents, vhost } from "../../wailsjs/go/models";
import { EventsOn } from "../../wailsjs/runtime";
import { useT } from "../lib/i18n";

export type Tab = "general" | "account" | "appearance" | "services" | "commandcenter" | "experimental" | "updates";

type Props = {
  flash: (msg: string) => void;
  onUpdateAvailabilityChange?: (avail: boolean) => void;
  initialTab?: Tab;
  scrollToPlan?: boolean;
  onConsumed?: () => void;
  onStatusChange?: (s: CloudStatusType | undefined) => void;
  onCommandCenterChange?: () => void;
  agentSessionsEnabled?: boolean;
  onAgentSessionsEnabledChange?: (enabled: boolean) => void;
};

export default function Settings({
  flash,
  onUpdateAvailabilityChange,
  initialTab,
  scrollToPlan,
  onConsumed,
  onStatusChange,
  onCommandCenterChange,
  agentSessionsEnabled = false,
  onAgentSessionsEnabledChange,
}: Props) {
  const t = useT();
  const [tab, setTab] = useState<Tab>(initialTab ?? "general");
  const [active, setActive] = useState<ThemeId>(getCurrentTheme());
  const [copyMode, setCopyMode] = useState<TerminalCopyMode>(getTerminalCopyMode());
  const planRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (initialTab) setTab(initialTab);
  }, [initialTab]);

  useEffect(() => {
    if (scrollToPlan && tab === "account") {
      const t = setTimeout(() => {
        planRef.current?.scrollIntoView({ behavior: "smooth", block: "start" });
        onConsumed?.();
      }, 60);
      return () => clearTimeout(t);
    }
  }, [scrollToPlan, tab, onConsumed]);

  const pick = (id: ThemeId) => {
    if (id === active) return;
    applyTheme(id);
    setActive(id);
    flash(t("settings.appearance.themeAppliedFlash"));
  };

  const pickMode = (m: TerminalCopyMode) => {
    if (m === copyMode) return;
    setTerminalCopyMode(m);
    setCopyMode(m);
    flash(t("settings.appearance.terminalModeSavedFlash"));
  };

  return (
    <div className="h-full flex flex-col gap-3">
      <div className="flex items-center gap-1 px-1">
        <TabButton active={tab === "general"} onClick={() => setTab("general")} icon={<SettingsIcon className="w-3.5 h-3.5" />} label={t("cloudOptIn.general")} />
        <TabButton
          active={tab === "account"}
          onClick={() => setTab("account")}
          icon={<User className="w-3.5 h-3.5" />}
          label={t("cloudOptIn.title")}
        />
        <TabButton
          active={tab === "appearance"}
          onClick={() => setTab("appearance")}
          icon={<Palette className="w-3.5 h-3.5" />}
          label={t("settings.tabs.appearance")}
        />
        <TabButton
          active={tab === "services"}
          onClick={() => setTab("services")}
          icon={<Server className="w-3.5 h-3.5" />}
          label={t("settings.tabs.services")}
        />
        <TabButton
          active={tab === "commandcenter"}
          onClick={() => setTab("commandcenter")}
          icon={<Command className="w-3.5 h-3.5" />}
          label={t("settings.tabs.commandcenter")}
        />
        <TabButton
          active={tab === "experimental"}
          onClick={() => setTab("experimental")}
          icon={<FlaskConical className="w-3.5 h-3.5" />}
          label={t("settings.tabs.experimental")}
        />
        <TabButton
          active={tab === "updates"}
          onClick={() => setTab("updates")}
          icon={<RefreshCw className="w-3.5 h-3.5" />}
          label={t("settings.tabs.updates")}
        />
      </div>

      <div className="flex-1 min-h-0 overflow-y-auto">
        {(tab === "general" || tab === "appearance") && (
          <div className="space-y-3">
          <GlassCard title={t("settings.tabs.appearance")} icon={<Palette className="w-4 h-4" />}>
            <div className="space-y-2">
              <LanguageSelector />
              <p className="text-xs text-white/60">
                {t("settings.appearance.themeHint")}
              </p>
              <div className="grid grid-cols-2 lg:grid-cols-3 gap-3 pt-2">
                {THEMES.map((theme) => {
                  const isActive = active === theme.id;
                  return (
                    <button
                      key={theme.id}
                      onClick={() => pick(theme.id)}
                      className={clsx(
                        "relative text-left rounded-xl p-4 border transition-all",
                        "bg-white/[0.03] hover:bg-white/[0.06]",
                        isActive
                          ? "border-accent shadow-[0_0_0_1px_var(--c-accent)]"
                          : "border-white/10 hover:border-white/25"
                      )}
                    >
                      {isActive && (
                        <span className="absolute top-2 right-2 w-5 h-5 rounded-full bg-accent flex items-center justify-center">
                          <Check className="w-3 h-3 text-white" />
                        </span>
                      )}
                      <div
                        className="h-16 rounded-lg mb-3 border border-white/10"
                        style={{
                          background: `linear-gradient(135deg, ${theme.swatch[2]} 0%, ${theme.swatch[2]} 50%, ${theme.swatch[0]}33 100%)`,
                        }}
                      >
                        <div className="h-full flex items-end gap-2 p-2">
                          <span
                            className="w-6 h-6 rounded-full border border-white/20"
                            style={{ background: theme.swatch[0] }}
                          />
                          <span
                            className="w-6 h-6 rounded-full border border-white/20"
                            style={{ background: theme.swatch[1] }}
                          />
                          <span
                            className="w-6 h-6 rounded-full border border-white/20"
                            style={{ background: theme.swatch[2] }}
                          />
                        </div>
                      </div>
                      {/* theme.label is a theme NAME (Nebula, Aurora, …) — a brand,
                          left untranslated. Only the description is prose. */}
                      <div className="text-sm font-medium text-white">{theme.label}</div>
                      <div className="text-[11px] text-white/50">{t(theme.descriptionKey)}</div>
                    </button>
                  );
                })}
              </div>
            </div>
          </GlassCard>
          <GlassCard title={t("settings.appearance.terminalCardTitle")} icon={<TerminalIcon className="w-4 h-4" />}>
            <div className="space-y-2">
              <p className="text-xs text-white/60">
                {t("settings.appearance.terminalHint")}
              </p>
              <div className="grid grid-cols-1 sm:grid-cols-2 gap-3 pt-2">
                <ModeButton
                  active={copyMode === "keyboard"}
                  onClick={() => pickMode("keyboard")}
                  title={t("settings.appearance.terminalMode.keyboardTitle")}
                  desc="Ctrl+Shift+C / Ctrl+Shift+V"
                />
                <ModeButton
                  active={copyMode === "mouse"}
                  onClick={() => pickMode("mouse")}
                  title={t("settings.appearance.terminalMode.mouseTitle")}
                  desc={t("settings.appearance.terminalMode.mouseDesc")}
                />
              </div>
            </div>
          </GlassCard>
          {tab === "general" && <LegalNotices />}
          </div>
        )}

        {tab === "account" && (
          <Account flash={flash} planSectionRef={planRef} onStatusChange={onStatusChange} />
        )}

        {tab === "services" && <ServicesTab flash={flash} />}

        {tab === "commandcenter" && (
          <CommandCenterSettings flash={flash} onChange={onCommandCenterChange} />
        )}

        {tab === "experimental" && (
          <ExperimentalTab
            enabled={agentSessionsEnabled}
            onEnabledChange={onAgentSessionsEnabledChange}
            flash={flash}
          />
        )}

        {tab === "updates" && (
          <Updates flash={flash} onAvailabilityChange={onUpdateAvailabilityChange} />
        )}
      </div>
    </div>
  );
}

function ExperimentalTab({
  enabled,
  onEnabledChange,
  flash,
}: {
  enabled: boolean;
  onEnabledChange?: (enabled: boolean) => void;
  flash: (message: string) => void;
}) {
  const t = useT();
  const [saving, setSaving] = useState(false);
  const [resetting, setResetting] = useState(false);
  const dialogs = useDialogs();
  const [hooksEnabled, setHooksEnabled] = useState(false);
  const [hooksSaving, setHooksSaving] = useState(false);
  const [hookStatus, setHookStatus] = useState<agents.HookInstallStatus[]>([]);

  useEffect(() => {
    GetAgentHooksEnabled().then(setHooksEnabled).catch(() => {});
    GetAgentHooksStatus().then(setHookStatus).catch(() => {});
    return EventsOn("agent:hooks-changed", (next: boolean) => {
      setHooksEnabled(next);
      GetAgentHooksStatus().then(setHookStatus).catch(() => {});
    });
  }, []);

  const toggleHooks = async () => {
    const next = !hooksEnabled;
    setHooksSaving(true);
    try {
      const statuses = await SetAgentHooksEnabled(next);
      setHooksEnabled(next);
      setHookStatus(statuses);
      flash(next ? t("settings.experimental.terminalAgents.enabledFlash") : t("settings.experimental.terminalAgents.disabledFlash"));
    } catch (error) {
      flash(String(error));
    } finally {
      setHooksSaving(false);
    }
  };

  const toggleAgents = async () => {
    const next = !enabled;
    setSaving(true);
    try {
      await SetAgentSessionsEnabled(next);
      onEnabledChange?.(next);
      flash(next ? t("settings.experimental.agentSessions.enabledFlash") : t("settings.experimental.agentSessions.disabledFlash"));
    } catch (error) {
      flash(String(error));
    } finally {
      setSaving(false);
    }
  };

  const resetMetadata = async () => {
    if (!(await dialogs.confirm({
      title: t("settings.experimental.localMetadata.confirmTitle"),
      message: t("settings.experimental.localMetadata.confirmMessage"),
      confirmLabel: t("settings.experimental.localMetadata.confirmLabel"),
      danger: true,
    }))) return;
    setResetting(true);
    try {
      const archived = await ResetAgentMetadata();
      flash(archived
        ? t("settings.experimental.localMetadata.resetFlashWithPath", { path: archived })
        : t("settings.experimental.localMetadata.resetFlash"));
    } catch (error) {
      flash(String(error));
    } finally {
      setResetting(false);
    }
  };

  return (
    <div className="space-y-3">
      <MCPSettings flash={flash} />
      <GlassCard title={t("settings.experimental.agentSessions.cardTitle")} icon={<FlaskConical className="w-4 h-4" />}>
        <div className="flex items-start justify-between gap-6">
          <div>
            <div className="text-sm font-medium text-white">{t("settings.experimental.agentSessions.title")}</div>
            <p className="mt-1 max-w-2xl text-xs leading-relaxed text-white/55">
              {t("settings.experimental.agentSessions.description")}
            </p>
          </div>
          <button
            type="button"
            role="switch"
            aria-checked={enabled}
            disabled={saving}
            onClick={toggleAgents}
            className={clsx(
              "relative mt-0.5 h-6 w-11 shrink-0 rounded-full border transition-colors disabled:opacity-50",
              enabled ? "border-accent bg-accent/70" : "border-white/20 bg-white/10",
            )}
          >
            <span className={clsx("absolute top-0.5 h-4 w-4 rounded-full bg-white transition-all", enabled ? "left-[22px]" : "left-1")} />
          </button>
        </div>
      </GlassCard>

      <GlassCard title={t("settings.experimental.terminalAgents.cardTitle")} icon={<TerminalSquare className="w-4 h-4" />}>
        <div className="flex items-start justify-between gap-6">
          <div>
            <div className="text-sm font-medium text-white">{t("settings.experimental.terminalAgents.title")}</div>
            <p className="mt-1 max-w-2xl text-xs leading-relaxed text-white/55">
              {t("settings.experimental.terminalAgents.description")}
            </p>
            {hookStatus.length > 0 && (
              <ul className="mt-2 space-y-0.5 text-[11px] text-white/45">
                {hookStatus.map((s) => (
                  <li key={s.provider} className="flex items-center gap-2">
                    <span className={clsx("h-1.5 w-1.5 rounded-full", s.installed ? "bg-emerald-400" : "bg-white/20")} />
                    <span className="text-white/70">{s.label}</span>
                    <span className="truncate" title={s.path}>{s.reason || (s.installed ? t("settings.experimental.terminalAgents.hookInstalled") : t("settings.experimental.terminalAgents.hookNotInstalled"))}</span>
                  </li>
                ))}
              </ul>
            )}
          </div>
          <button
            type="button"
            role="switch"
            aria-checked={hooksEnabled}
            disabled={hooksSaving || (!enabled && !hooksEnabled)}
            onClick={toggleHooks}
            className={clsx(
              "relative mt-0.5 h-6 w-11 shrink-0 rounded-full border transition-colors disabled:opacity-50",
              hooksEnabled ? "border-accent bg-accent/70" : "border-white/20 bg-white/10",
            )}
          >
            <span className={clsx("absolute top-0.5 h-4 w-4 rounded-full bg-white transition-all", hooksEnabled ? "left-[22px]" : "left-1")} />
          </button>
        </div>
      </GlassCard>

      <GlassCard title={t("settings.experimental.localMetadata.cardTitle")} icon={<Archive className="w-4 h-4" />}>
        <div className="flex items-center justify-between gap-6">
          <div>
            <div className="text-sm font-medium text-white">{t("settings.experimental.localMetadata.title")}</div>
            <p className="mt-1 max-w-2xl text-xs leading-relaxed text-white/55">
              {t("settings.experimental.localMetadata.description")}
            </p>
          </div>
          <button
            type="button"
            disabled={resetting}
            onClick={resetMetadata}
            className="shrink-0 rounded-md border border-amber-400/35 bg-amber-500/10 px-3 py-1.5 text-xs text-amber-100 transition-colors hover:bg-amber-500/20 disabled:opacity-50"
          >
            {resetting ? t("settings.experimental.localMetadata.resettingLabel") : t("settings.experimental.localMetadata.resetButton")}
          </button>
        </div>
      </GlassCard>
    </div>
  );
}

function TabButton({
  active,
  onClick,
  icon,
  label,
}: {
  active: boolean;
  onClick: () => void;
  icon: React.ReactNode;
  label: string;
}) {
  return (
    <button
      onClick={onClick}
      className={clsx(
        "flex items-center gap-1.5 px-3 py-1.5 rounded-md text-xs transition-colors",
        active
          ? "bg-accent/25 border border-accent/60 text-white"
          : "border border-transparent hover:bg-white/5 text-white/60"
      )}
    >
      {icon}
      <span>{label}</span>
    </button>
  );
}

// Defaults mirror vhost.NginxDefaults / vhost.ApacheDefaults so the UI
// shows what the backend will use when a field is left at zero.
const NGINX_DEFAULTS = {
  serverNamesHashBucketSize: 128,
  serverNamesHashMaxSize: 1024,
  clientMaxBodyMB: 512,
  clientBodyTimeoutSec: 300,
  fastcgiReadTimeoutSec: 300,
  proxyReadTimeoutSec: 300,
  keepaliveTimeoutSec: 75,
  workerConnections: 1024,
};
const APACHE_DEFAULTS = {
  timeoutSec: 300,
  keepAliveTimeoutSec: 5,
  limitRequestBodyMB: 100,
  proxyTimeoutSec: 300,
};
// Mirrors php.PhpTuningDefaults (sizes shown in MB in the UI, stored as "<N>M").
const PHP_DEFAULTS = {
  uploadMaxMB: 512,
  postMaxMB: 512,
  memoryLimitMB: 512,
  maxExecutionTime: 300,
  maxInputTime: 300,
  workers: 4,
};

// mbFromIni parses a php.ini size string ("512M", "2G", "128") into MB.
function mbFromIni(s: string | undefined, dflt: number): number {
  if (!s) return dflt;
  const m = String(s).trim().match(/^(\d+)\s*([KMG])?/i);
  if (!m) return dflt;
  let n = parseInt(m[1], 10);
  const unit = (m[2] || "M").toUpperCase();
  if (unit === "G") n *= 1024;
  if (unit === "K") n = Math.max(1, Math.round(n / 1024));
  return n;
}

// LocalHttpsCard shows the local certificate authority LaraEnv uses to sign
// HTTPS certs for local sites (shop.test, and so on) and, when this machine
// doesn't yet trust it, a button to trust it explicitly. An installed copy
// trusts its CA automatically and this card just confirms that; a portable
// copy does not touch the machine's trust store on its own, so trusting is
// always this deliberate, explained action here.
function LocalHttpsCard({ flash }: { flash: (msg: string) => void }) {
  const t = useT();
  const [status, setStatus] = useState<vhost.CAStatus | null>(null);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [trusting, setTrusting] = useState(false);

  // LocalCAStatus is load-only — it never creates a CA and never touches
  // the trust store, so opening this card (or reopening it) has no side
  // effect. A CA is only ever created by turning a project's SSL on, or by
  // clicking Trust below.
  const refresh = () => {
    setLoading(true);
    setLoadError(null);
    LocalCAStatus()
      .then((s: vhost.CAStatus) => setStatus(s))
      .catch((e: any) => {
        setLoadError(String(e?.message ?? e));
        flash(t("settings.services.localHttps.loadErrorDetail", { error: e?.message ?? e }));
      })
      .finally(() => setLoading(false));
  };

  useEffect(() => { refresh(); }, []);

  const trust = async () => {
    setTrusting(true);
    try {
      await TrustLocalCA();
      flash(t("settings.services.localHttps.trustedFlash"));
      refresh();
    } catch (e: any) {
      flash(t("settings.services.localHttps.trustFailedFlash", { message: e?.message ?? e }));
    } finally {
      setTrusting(false);
    }
  };

  // "Could not check" (checkError set) is deliberately distinct from "Not
  // trusted": the latter is a checked fact the Trust button addresses, the
  // former means the check itself didn't run (e.g. certutil unresolvable)
  // and reporting it as "Not trusted" would be a claim this card can't back up.
  const pillLabel = status?.checkError
    ? t("settings.services.localHttps.pillCheckError")
    : status?.trusted
    ? t("settings.services.localHttps.pillTrusted")
    : t("settings.services.localHttps.pillNotTrusted");
  const pillClass = status?.checkError
    ? "border-white/15 bg-white/5 text-white/50"
    : status?.trusted
    ? "border-emerald-400/30 bg-emerald-400/10 text-emerald-300"
    : "border-amber-400/30 bg-amber-400/10 text-amber-300";

  return (
    <GlassCard title={t("settings.services.localHttps.cardTitle")} icon={<ShieldCheck className="w-4 h-4" />}>
      <div className="space-y-3">
        <p className="text-xs text-white/60">
          {t("settings.services.localHttps.certIntro")}{" "}
          <code className="text-white/80">shop.test</code>{t("settings.services.localHttps.certMiddle")}{" "}
          <span className="text-white/80">{t("settings.services.localHttps.trustHighlight")}</span>{" "}
          {t("settings.services.localHttps.certOutro")}
        </p>

        {status?.exists ? (
          <div className="rounded-md border border-white/10 bg-white/[0.03] p-3 space-y-2">
            <div className="flex items-center justify-between gap-3">
              <div className="min-w-0">
                <div className="text-xs text-white/80 truncate" title={status.commonName}>{status.commonName}</div>
                <div className="text-[10px] text-white/40 font-mono truncate" title={status.thumbprint}>
                  {status.thumbprint}
                </div>
              </div>
              <span className={clsx("flex-none text-[10px] px-2 py-1 rounded-full border", pillClass)}>
                {pillLabel}
              </span>
            </div>
            {status.checkError && (
              <p className="text-[10px] text-white/40">{t("settings.services.localHttps.checkErrorDetail", { error: status.checkError })}</p>
            )}
            {!status.trusted && (
              <div className="flex items-center justify-end pt-1">
                <button
                  onClick={trust}
                  disabled={trusting}
                  className={clsx(
                    "px-3 py-1.5 rounded-md text-xs border transition-colors",
                    "bg-accent/25 border-accent/60 text-white hover:bg-accent/35",
                    "disabled:opacity-50 disabled:cursor-not-allowed"
                  )}
                >
                  {trusting ? t("settings.services.localHttps.trustingLabel") : t("settings.services.localHttps.trustButton")}
                </button>
              </div>
            )}
          </div>
        ) : (
          <p className="text-xs text-white/40">
            {loading
              ? t("settings.services.localHttps.loadingLabel")
              : loadError
              ? t("settings.services.localHttps.loadErrorDetail", { error: loadError })
              : t("settings.services.localHttps.noneYet")}
          </p>
        )}
      </div>
    </GlassCard>
  );
}

function ServicesTab({ flash }: { flash: (msg: string) => void }) {
  const t = useT();
  const [nginx, setNginx] = useState({ ...NGINX_DEFAULTS });
  const [apache, setApache] = useState({ ...APACHE_DEFAULTS });
  const [php, setPhp] = useState({ ...PHP_DEFAULTS });
  const [loaded, setLoaded] = useState(false);
  const [saving, setSaving] = useState(false);

  useEffect(() => {
    GetServicesConfig()
      .then((cfg: any) => {
        setNginx({
          serverNamesHashBucketSize: cfg.nginx?.serverNamesHashBucketSize || NGINX_DEFAULTS.serverNamesHashBucketSize,
          serverNamesHashMaxSize: cfg.nginx?.serverNamesHashMaxSize || NGINX_DEFAULTS.serverNamesHashMaxSize,
          clientMaxBodyMB: cfg.nginx?.clientMaxBodyMB || NGINX_DEFAULTS.clientMaxBodyMB,
          clientBodyTimeoutSec: cfg.nginx?.clientBodyTimeoutSec || NGINX_DEFAULTS.clientBodyTimeoutSec,
          fastcgiReadTimeoutSec: cfg.nginx?.fastcgiReadTimeoutSec || NGINX_DEFAULTS.fastcgiReadTimeoutSec,
          proxyReadTimeoutSec: cfg.nginx?.proxyReadTimeoutSec || NGINX_DEFAULTS.proxyReadTimeoutSec,
          keepaliveTimeoutSec: cfg.nginx?.keepaliveTimeoutSec || NGINX_DEFAULTS.keepaliveTimeoutSec,
          workerConnections: cfg.nginx?.workerConnections || NGINX_DEFAULTS.workerConnections,
        });
        setApache({
          timeoutSec: cfg.apache?.timeoutSec || APACHE_DEFAULTS.timeoutSec,
          keepAliveTimeoutSec: cfg.apache?.keepAliveTimeoutSec || APACHE_DEFAULTS.keepAliveTimeoutSec,
          limitRequestBodyMB:
            cfg.apache?.limitRequestBodyMB === undefined
              ? APACHE_DEFAULTS.limitRequestBodyMB
              : cfg.apache.limitRequestBodyMB,
          proxyTimeoutSec: cfg.apache?.proxyTimeoutSec || APACHE_DEFAULTS.proxyTimeoutSec,
        });
        setPhp({
          uploadMaxMB: mbFromIni(cfg.php?.uploadMaxFilesize, PHP_DEFAULTS.uploadMaxMB),
          postMaxMB: mbFromIni(cfg.php?.postMaxSize, PHP_DEFAULTS.postMaxMB),
          memoryLimitMB: mbFromIni(cfg.php?.memoryLimit, PHP_DEFAULTS.memoryLimitMB),
          maxExecutionTime: cfg.php?.maxExecutionTime || PHP_DEFAULTS.maxExecutionTime,
          maxInputTime: cfg.php?.maxInputTime || PHP_DEFAULTS.maxInputTime,
          workers: cfg.php?.workers || PHP_DEFAULTS.workers,
        });
      })
      .finally(() => setLoaded(true));
  }, []);

  const isPow2 = (n: number) => n > 0 && (n & (n - 1)) === 0;
  const setN = (k: keyof typeof NGINX_DEFAULTS, v: number) => setNginx((p) => ({ ...p, [k]: v }));
  const setA = (k: keyof typeof APACHE_DEFAULTS, v: number) => setApache((p) => ({ ...p, [k]: v }));
  const setP = (k: keyof typeof PHP_DEFAULTS, v: number) => setPhp((p) => ({ ...p, [k]: v }));

  const saveNginx = async () => {
    if (!isPow2(nginx.serverNamesHashBucketSize)) {
      flash(t("settings.services.nginx.bucketSizeError"));
      return;
    }
    if (nginx.serverNamesHashMaxSize < nginx.serverNamesHashBucketSize) {
      flash(t("settings.services.nginx.maxSizeError"));
      return;
    }
    setSaving(true);
    try {
      await SetNginxTuning(nginx);
      flash(t("settings.services.nginx.savedFlash"));
    } catch (e: any) {
      flash(t("settings.common.saveFailed", { message: e?.message ?? e }));
    } finally {
      setSaving(false);
    }
  };

  const saveApache = async () => {
    setSaving(true);
    try {
      await SetApacheTuning(apache);
      flash(t("settings.services.apache.savedFlash"));
    } catch (e: any) {
      flash(t("settings.common.saveFailed", { message: e?.message ?? e }));
    } finally {
      setSaving(false);
    }
  };

  const savePhp = async () => {
    if (php.workers < 1 || php.workers > 16) {
      flash(t("settings.services.php.workersRangeError"));
      return;
    }
    if (php.postMaxMB < php.uploadMaxMB) {
      flash(t("settings.services.php.postMaxWarning"));
      return;
    }
    if (nginx.clientMaxBodyMB < php.postMaxMB) {
      flash(t("settings.services.php.clientMaxBodyTip"));
    }
    setSaving(true);
    try {
      await SetPhpTuning({
        uploadMaxFilesize: `${php.uploadMaxMB}M`,
        postMaxSize: `${php.postMaxMB}M`,
        memoryLimit: `${php.memoryLimitMB}M`,
        maxExecutionTime: php.maxExecutionTime,
        maxInputTime: php.maxInputTime,
        workers: php.workers,
      });
      flash(t("settings.services.php.savedFlash"));
    } catch (e: any) {
      flash(t("settings.common.saveFailed", { message: e?.message ?? e }));
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="space-y-3">
      <LocalHttpsCard flash={flash} />

      <GlassCard title="Nginx" icon={<Server className="w-4 h-4" />}>
        <div className="space-y-3">
          <p className="text-xs text-white/60">
            {t("settings.services.nginx.intro1")} <code className="text-white/80">nginx.conf</code>{t("settings.services.nginx.intro2")}
            <code className="text-white/80"> client_max_body_size</code> {t("settings.services.nginx.intro3")}{" "}
            <em>504 Gateway Timeout</em>.
          </p>

          <div className="grid grid-cols-1 sm:grid-cols-2 gap-3 pt-1">
            <NumberField
              label="client_max_body_size (MB)"
              hint={t("settings.services.nginx.clientMaxBodyHint")}
              value={nginx.clientMaxBodyMB}
              onChange={(v) => setN("clientMaxBodyMB", v)}
              disabled={!loaded || saving}
            />
            <NumberField
              label="client_body_timeout (s)"
              hint={t("settings.services.nginx.clientBodyTimeoutHint")}
              value={nginx.clientBodyTimeoutSec}
              onChange={(v) => setN("clientBodyTimeoutSec", v)}
              disabled={!loaded || saving}
            />
            <NumberField
              label="fastcgi_read_timeout (s)"
              hint={t("settings.services.nginx.fastcgiReadTimeoutHint")}
              value={nginx.fastcgiReadTimeoutSec}
              onChange={(v) => setN("fastcgiReadTimeoutSec", v)}
              disabled={!loaded || saving}
            />
            <NumberField
              label="proxy_read_timeout (s)"
              hint={t("settings.services.nginx.proxyReadTimeoutHint")}
              value={nginx.proxyReadTimeoutSec}
              onChange={(v) => setN("proxyReadTimeoutSec", v)}
              disabled={!loaded || saving}
            />
            <NumberField
              label="keepalive_timeout (s)"
              hint={t("settings.services.nginx.keepaliveTimeoutHint")}
              value={nginx.keepaliveTimeoutSec}
              onChange={(v) => setN("keepaliveTimeoutSec", v)}
              disabled={!loaded || saving}
            />
            <NumberField
              label="worker_connections"
              hint={t("settings.services.nginx.workerConnectionsHint")}
              value={nginx.workerConnections}
              onChange={(v) => setN("workerConnections", v)}
              disabled={!loaded || saving}
            />
            <NumberField
              label="server_names_hash_bucket_size"
              hint={t("settings.services.nginx.bucketSizeHint")}
              value={nginx.serverNamesHashBucketSize}
              onChange={(v) => setN("serverNamesHashBucketSize", v)}
              disabled={!loaded || saving}
            />
            <NumberField
              label="server_names_hash_max_size"
              hint={t("settings.services.nginx.maxSizeHint")}
              value={nginx.serverNamesHashMaxSize}
              onChange={(v) => setN("serverNamesHashMaxSize", v)}
              disabled={!loaded || saving}
            />
          </div>

          <div className="flex items-center justify-end pt-1">
            <button
              onClick={saveNginx}
              disabled={!loaded || saving}
              className={clsx(
                "px-3 py-1.5 rounded-md text-xs border transition-colors",
                "bg-accent/25 border-accent/60 text-white hover:bg-accent/35",
                "disabled:opacity-50 disabled:cursor-not-allowed"
              )}
            >
              {saving ? t("settings.common.savingLabel") : t("settings.services.nginx.saveButton")}
            </button>
          </div>
        </div>
      </GlassCard>

      <GlassCard title="PHP" icon={<Server className="w-4 h-4" />}>
        <div className="space-y-3">
          <p className="text-xs text-white/60">
            {t("settings.services.php.intro1")}{" "}
            <code className="text-white/80">php.ini</code>{t("settings.services.php.intro2")}{" "}
            <code className="text-white/80">php-cgi</code>{" "}
            {t("settings.services.php.intro3")}
          </p>

          <div className="grid grid-cols-1 sm:grid-cols-2 gap-3 pt-1">
            <NumberField
              label="upload_max_filesize (MB)"
              hint={t("settings.services.php.uploadMaxHint")}
              value={php.uploadMaxMB}
              onChange={(v) => setP("uploadMaxMB", v)}
              disabled={!loaded || saving}
            />
            <NumberField
              label="post_max_size (MB)"
              hint={t("settings.services.php.postMaxHint")}
              value={php.postMaxMB}
              onChange={(v) => setP("postMaxMB", v)}
              disabled={!loaded || saving}
            />
            <NumberField
              label="memory_limit (MB)"
              hint={t("settings.services.php.memoryLimitHint")}
              value={php.memoryLimitMB}
              onChange={(v) => setP("memoryLimitMB", v)}
              disabled={!loaded || saving}
            />
            <NumberField
              label={t("settings.services.php.workersLabel")}
              hint={t("settings.services.php.workersHint")}
              value={php.workers}
              onChange={(v) => setP("workers", v)}
              disabled={!loaded || saving}
            />
            <NumberField
              label="max_execution_time (s)"
              hint={t("settings.services.php.maxExecutionTimeHint")}
              value={php.maxExecutionTime}
              onChange={(v) => setP("maxExecutionTime", v)}
              disabled={!loaded || saving}
            />
            <NumberField
              label="max_input_time (s)"
              hint={t("settings.services.php.maxInputTimeHint")}
              value={php.maxInputTime}
              onChange={(v) => setP("maxInputTime", v)}
              disabled={!loaded || saving}
            />
          </div>

          <div className="flex items-center justify-end pt-1">
            <button
              onClick={savePhp}
              disabled={!loaded || saving}
              className={clsx(
                "px-3 py-1.5 rounded-md text-xs border transition-colors",
                "bg-accent/25 border-accent/60 text-white hover:bg-accent/35",
                "disabled:opacity-50 disabled:cursor-not-allowed"
              )}
            >
              {saving ? t("settings.common.savingLabel") : t("settings.services.php.saveButton")}
            </button>
          </div>
        </div>
      </GlassCard>

      <GlassCard title="Apache" icon={<Server className="w-4 h-4" />}>
        <div className="space-y-3">
          <p className="text-xs text-white/60">
            {t("settings.services.apache.intro1")} <code className="text-white/80">httpd.conf</code>{t("settings.services.apache.intro2")}
          </p>

          <div className="grid grid-cols-1 sm:grid-cols-2 gap-3 pt-1">
            <NumberField
              label="Timeout (s)"
              hint={t("settings.services.apache.timeoutHint")}
              value={apache.timeoutSec}
              onChange={(v) => setA("timeoutSec", v)}
              disabled={!loaded || saving}
            />
            <NumberField
              label="KeepAliveTimeout (s)"
              hint={t("settings.services.apache.keepAliveTimeoutHint")}
              value={apache.keepAliveTimeoutSec}
              onChange={(v) => setA("keepAliveTimeoutSec", v)}
              disabled={!loaded || saving}
            />
            <NumberField
              label="LimitRequestBody (MB)"
              hint={t("settings.services.apache.limitRequestBodyHint")}
              value={apache.limitRequestBodyMB}
              onChange={(v) => setA("limitRequestBodyMB", v)}
              disabled={!loaded || saving}
            />
            <NumberField
              label="ProxyTimeout (s)"
              hint={t("settings.services.apache.proxyTimeoutHint")}
              value={apache.proxyTimeoutSec}
              onChange={(v) => setA("proxyTimeoutSec", v)}
              disabled={!loaded || saving}
            />
          </div>

          <div className="flex items-center justify-end pt-1">
            <button
              onClick={saveApache}
              disabled={!loaded || saving}
              className={clsx(
                "px-3 py-1.5 rounded-md text-xs border transition-colors",
                "bg-accent/25 border-accent/60 text-white hover:bg-accent/35",
                "disabled:opacity-50 disabled:cursor-not-allowed"
              )}
            >
              {saving ? t("settings.common.savingLabel") : t("settings.services.apache.saveButton")}
            </button>
          </div>
        </div>
      </GlassCard>
    </div>
  );
}

function NumberField({
  label,
  hint,
  value,
  onChange,
  disabled,
}: {
  label: string;
  hint?: string;
  value: number;
  onChange: (n: number) => void;
  disabled?: boolean;
}) {
  return (
    <label className="block">
      <div className="text-[11px] text-white/70 font-mono">{label}</div>
      <input
        type="number"
        min={1}
        value={value}
        disabled={disabled}
        onChange={(e) => onChange(parseInt(e.target.value || "0", 10))}
        className={clsx(
          "mt-1 w-full px-2 py-1.5 rounded-md text-sm",
          "bg-white/[0.04] border border-white/15 text-white",
          "focus:outline-none focus:border-accent/60",
          "disabled:opacity-50"
        )}
      />
      {hint && <div className="text-[10px] text-white/45 mt-1">{hint}</div>}
    </label>
  );
}

function ModeButton({
  active,
  onClick,
  title,
  desc,
}: {
  active: boolean;
  onClick: () => void;
  title: string;
  desc: string;
}) {
  return (
    <button
      onClick={onClick}
      className={clsx(
        "relative text-left rounded-xl p-3 border transition-all",
        "bg-white/[0.03] hover:bg-white/[0.06]",
        active
          ? "border-accent shadow-[0_0_0_1px_var(--c-accent)]"
          : "border-white/10 hover:border-white/25"
      )}
    >
      {active && (
        <span className="absolute top-2 right-2 w-5 h-5 rounded-full bg-accent flex items-center justify-center">
          <Check className="w-3 h-3 text-white" />
        </span>
      )}
      <div className="text-sm font-medium text-white">{title}</div>
      <div className="text-[11px] text-white/50">{desc}</div>
    </button>
  );
}
