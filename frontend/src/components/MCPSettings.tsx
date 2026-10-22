import { useCallback, useEffect, useRef, useState } from "react";
import clsx from "clsx";
import { Check, Loader2, Plug, RefreshCw } from "lucide-react";
import { GetMCPStatus, RegisterMCPClient, SetMCPEnabled } from "../../wailsjs/go/main/App";
import type { main as generatedMain, mcpclient } from "../../wailsjs/go/models";
import { EventsOn } from "../../wailsjs/runtime";
import GlassCard from "./GlassCard";
import { useI18n } from "../lib/i18n";

type MCPClientStatus = mcpclient.Status;
type MCPActivity = generatedMain.MCPActivity;
type MCPStatus = Omit<generatedMain.MCPStatus, "convertValues">;

function recentActivity(entries: MCPActivity[]): MCPActivity[] {
  return [...new Map(entries.map((entry) => [entry.id, entry])).values()]
    .sort((a, b) => Date.parse(b.time) - Date.parse(a.time))
    .slice(0, 5);
}

export default function MCPSettings({ flash }: { flash: (message: string) => void }) {
  const { t, formatDate, formatNumber } = useI18n();
  const [status, setStatus] = useState<MCPStatus | null>(null);
  const [activity, setActivity] = useState<MCPActivity[]>([]);
  const [loading, setLoading] = useState(true);
  const [loadFailed, setLoadFailed] = useState(false);
  const [saving, setSaving] = useState(false);
  const [registering, setRegistering] = useState<string | null>(null);
  const [actionError, setActionError] = useState<string | null>(null);
  const mounted = useRef(false);
  const revision = useRef(0);
  const loadRequest = useRef(0);

  const applyStatus = useCallback((next: MCPStatus) => {
    setStatus({ ...next, clients: next.clients ?? [], activity: next.activity ?? [] });
    setActivity((current) => recentActivity([...current, ...(next.activity ?? [])]));
    setLoadFailed(false);
  }, []);

  const refresh = useCallback(async () => {
    const request = ++loadRequest.current;
    const startingRevision = revision.current;
    setLoading(true);
    setLoadFailed(false);
    try {
      const next = await GetMCPStatus();
      if (mounted.current && request === loadRequest.current && startingRevision === revision.current) applyStatus(next);
    } catch {
      if (mounted.current && request === loadRequest.current && startingRevision === revision.current) setLoadFailed(true);
    } finally {
      if (mounted.current && request === loadRequest.current) setLoading(false);
    }
  }, [applyStatus]);

  useEffect(() => {
    mounted.current = true;
    const offStatus = EventsOn("mcp:status-changed", (next: MCPStatus) => {
      revision.current++;
      applyStatus(next);
    });
    const offActivity = EventsOn("mcp:activity", (entry: MCPActivity) => {
      setActivity((current) => recentActivity([...current, entry]));
    });
    void refresh();
    return () => {
      mounted.current = false;
      offStatus();
      offActivity();
    };
  }, [applyStatus, refresh]);

  const toggle = async () => {
    if (!status || saving || registering) return;
    setSaving(true);
    setActionError(null);
    try {
      const next = await SetMCPEnabled(!status.enabled);
      if (!mounted.current) return;
      revision.current++;
      applyStatus(next);
      if (next.enabled && !next.running) {
        setActionError(t("settings.experimental.mcp.unavailableHint"));
      } else {
        flash(next.enabled ? t("settings.experimental.mcp.enabledFlash") : t("settings.experimental.mcp.disabledFlash"));
      }
    } catch {
      if (mounted.current) setActionError(t("settings.experimental.mcp.changeFailed"));
    } finally {
      if (mounted.current) setSaving(false);
    }
  };

  const register = async (client: MCPClientStatus) => {
    if (!status?.enabled || !status.running || saving || registering) return;
    setRegistering(client.id);
    setActionError(null);
    try {
      const result = await RegisterMCPClient(client.id);
      if (!mounted.current) return;
      revision.current++;
      setStatus((current) => current && ({ ...current, clients: current.clients.map((item) => item.id === client.id ? result : item) }));
      if (result.registered && !result.error) flash(t("settings.experimental.mcp.registeredFlash", { client: result.label }));
      else setActionError(t("settings.experimental.mcp.registerFailed", { client: client.label }));
    } catch {
      if (mounted.current) setActionError(t("settings.experimental.mcp.registerFailed", { client: client.label }));
    } finally {
      if (mounted.current) setRegistering(null);
    }
  };

  const ready = !!status?.enabled && status.running;
  const stateLabel = !status
    ? t("settings.experimental.mcp.checking")
    : !status.enabled
      ? t("settings.experimental.mcp.disabled")
      : ready
        ? t("settings.experimental.mcp.ready")
        : t("settings.experimental.mcp.unavailable");

  return (
    <GlassCard title={t("settings.experimental.mcp.cardTitle")} icon={<Plug className="w-4 h-4" />}>
      <div className="space-y-4">
        <p className="max-w-2xl text-xs leading-relaxed text-white/55">{t("settings.experimental.mcp.description")}</p>
        <div className="flex items-center justify-between gap-4">
          <div>
            <div className="text-sm font-medium text-white">{t("settings.experimental.mcp.enableLabel")}</div>
            <p className="mt-1 text-xs text-white/45">{t("settings.experimental.mcp.keepOpen")}</p>
          </div>
          <button
            type="button"
            role="switch"
            aria-label={t("settings.experimental.mcp.enableLabel")}
            aria-checked={!!status?.enabled}
            disabled={loading || !status || saving || !!registering}
            onClick={toggle}
            className={clsx("relative h-6 w-11 shrink-0 rounded-full border transition-colors disabled:opacity-50", status?.enabled ? "border-accent bg-accent/70" : "border-white/20 bg-white/10")}
          >
            <span className={clsx("absolute top-0.5 h-4 w-4 rounded-full bg-white transition-all", status?.enabled ? "left-[22px]" : "left-1")} />
          </button>
        </div>
        {loadFailed ? (
          <div className="flex items-center justify-between gap-3">
            <p role="alert" className="text-xs text-amber-200">{t("settings.experimental.mcp.loadFailed")}</p>
            <button type="button" disabled={loading} onClick={() => void refresh()} className="flex items-center gap-1 rounded-md border border-white/15 px-2.5 py-1.5 text-xs text-white/70 hover:bg-white/5 disabled:opacity-50">
              <RefreshCw className="h-3 w-3" />{t("settings.experimental.mcp.retry")}
            </button>
          </div>
        ) : (
          <div className="flex flex-wrap items-center gap-2">
            <span role="status" className={clsx("rounded-full border px-2 py-1 text-[11px]", ready ? "border-emerald-400/30 bg-emerald-400/10 text-emerald-300" : "border-white/15 bg-white/5 text-white/60")}>{stateLabel}</span>
            {status?.message && <span className="text-xs text-white/50">{status.message}</span>}
          </div>
        )}
        {actionError && <p role="alert" className="text-xs text-amber-200">{actionError}</p>}
        {!!status?.clients.length && (
          <div className="space-y-2">
            <p className="text-xs text-white/60">{t("settings.experimental.mcp.clientsHint")}</p>
            <ul className="space-y-2">
              {status.clients.map((client) => (
                <li key={client.id} className="rounded-md border border-white/10 bg-white/[0.03] p-3">
                  <div className="flex items-center justify-between gap-3">
                    <div className="min-w-0">
                      <div className="flex items-center gap-2 text-xs text-white/80">
                        {client.label}
                        {client.registered && !client.error && <Check aria-hidden="true" className="h-3 w-3 text-emerald-300" />}
                        <span className="text-[11px] text-white/45">{client.error ? t("settings.experimental.mcp.registrationError") : client.registered ? t("settings.experimental.mcp.registered") : t("settings.experimental.mcp.notRegistered")}</span>
                      </div>
                      {client.path && <p className="mt-1 break-all text-[11px] text-white/40">{client.path}</p>}
                    </div>
                    <button
                      type="button"
                      aria-label={client.registered ? t("settings.experimental.mcp.updateClient", { client: client.label }) : t("settings.experimental.mcp.registerClient", { client: client.label })}
                      disabled={!ready || loading || saving || !!registering}
                      onClick={() => void register(client)}
                      className="flex shrink-0 items-center gap-1 rounded-md border border-accent/40 bg-accent/15 px-2.5 py-1.5 text-xs text-white hover:bg-accent/25 disabled:opacity-50"
                    >
                      {registering === client.id ? <Loader2 className="h-3 w-3 animate-spin" /> : null}
                      {registering === client.id ? t("settings.experimental.mcp.registering") : client.registered ? t("settings.experimental.mcp.updateButton") : t("settings.experimental.mcp.registerButton")}
                    </button>
                  </div>
                  {client.error && <p className="mt-2 text-xs text-amber-200">{client.error}</p>}
                  {client.registered && client.restartRequired && <p className="mt-2 text-xs text-white/55">{t("settings.experimental.mcp.restartHint", { client: client.label })}</p>}
                </li>
              ))}
            </ul>
          </div>
        )}
        <div className="border-t border-white/10 pt-3">
          <h3 className="text-xs font-medium text-white/70">{t("settings.experimental.mcp.activityTitle")}</h3>
          {activity.length ? (
            <ul aria-label={t("settings.experimental.mcp.activityTitle")} className="mt-2 space-y-2">
              {activity.map((entry) => (
                <li key={entry.id} className="flex flex-wrap items-center justify-between gap-x-4 gap-y-1 text-[11px]">
                  <div className="min-w-0 text-white/65">
                    <span>{entry.group} · {entry.action}</span>
                    {entry.target && <span className="ml-2 break-all text-white/45">{entry.target}</span>}
                  </div>
                  <div className="flex items-center gap-2 text-white/40">
                    <time dateTime={entry.time}>{Number.isNaN(Date.parse(entry.time)) ? "—" : formatDate(new Date(entry.time), { hour: "2-digit", minute: "2-digit", second: "2-digit" })}</time>
                    <span>{formatNumber(entry.durationMs)} ms</span>
                    <span className={entry.success ? "text-emerald-300" : "text-amber-200"}>{entry.success ? t("settings.experimental.mcp.activitySuccess") : t("settings.experimental.mcp.activityFailed")}</span>
                  </div>
                </li>
              ))}
            </ul>
          ) : <p className="mt-2 text-xs text-white/40">{t("settings.experimental.mcp.activityEmpty")}</p>}
        </div>
      </div>
    </GlassCard>
  );
}
