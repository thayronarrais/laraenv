import { useEffect, useMemo, useRef, useState } from "react";
import type { ReactNode } from "react";
import { Gauge, Loader2, RefreshCw } from "lucide-react";
import PortalMenu from "../PortalMenu";
import AgentUsagePopover, { type AgentUsageView, usageWindows, visibleUsageSnapshots } from "./AgentUsagePopover";
import AgentProviderIcon from "./AgentProviderIcon";
// @ts-ignore
import { ListAgentUsage, RefreshAgentUsage } from "../../../wailsjs/go/main/App";
// @ts-ignore
import { EventsOn } from "../../../wailsjs/runtime";
import { useI18n } from "../../lib/i18n";

const labels: Record<string, string> = { claude: "CLAUDE", codex: "CODEX", kimi: "KIMI", opencode: "OPENCODE" };

export default function AgentUsageBar({leading}: {leading?: ReactNode}) {
  const { t, formatNumber } = useI18n();
  const [snapshots, setSnapshots] = useState<AgentUsageView[]>([]);
  const [open, setOpen] = useState(false);
  const [refreshing, setRefreshing] = useState(false);
  const [showRemaining, setShowRemaining] = useState(false);
  const anchorRef = useRef<HTMLButtonElement>(null);

  useEffect(() => {
    ListAgentUsage().then((value: AgentUsageView[]) => setSnapshots(value ?? [])).catch(() => {});
    const off = EventsOn("agent:usage-changed", (value: AgentUsageView[]) => setSnapshots(value ?? []));
    return () => off();
  }, []);

  const ordered = useMemo(() => visibleUsageSnapshots(snapshots).sort((left, right) => {
    const peak = (snapshot: AgentUsageView) => Math.max(-1, ...usageWindows(snapshot).map((window) => window.usedPct ?? -1));
    return peak(right) - peak(left);
  }), [snapshots]);

  const refresh = async () => {
    if (refreshing) return;
    setRefreshing(true);
    try { setSnapshots(await RefreshAgentUsage() as AgentUsageView[]); } catch { /* status remains visible */ } finally { setRefreshing(false); }
  };

  return (
    <div className="flex h-9 shrink-0 items-center border-t border-white/[0.12] bg-[#101214] px-3.5 text-[11px]">
      {leading && <div className="mr-4 shrink-0">{leading}</div>}
      <button ref={anchorRef} type="button" onClick={() => setOpen(!open)} className="mr-4 flex items-center gap-2 font-semibold uppercase tracking-[0.12em] text-white/65 hover:text-white"><Gauge className="h-3.5 w-3.5" /> {t("agents.usageBar.limitsLabel")}</button>
      <div className="flex min-w-0 flex-1 items-center gap-4 overflow-x-auto no-scrollbar">
        {ordered.map((snapshot) => {
          const windows = usageWindows(snapshot);
          // Show the session window (5 hour) and the weekly window side by side;
          // fall back to the busiest window when a provider reports neither.
          const session = windows.find((window) => /hour/i.test(window.label));
          const weekly = windows.find((window) => /week/i.test(window.label) && window !== session);
          const shown = [session && { prefix: t("agents.usageBar.sessionWindowPrefix"), window: session }, weekly && { prefix: t("agents.usageBar.weeklyWindowPrefix"), window: weekly }].filter(Boolean) as { prefix: string; window: typeof windows[number] }[];
          if (shown.length === 0 && windows.length > 0) {
            const peak = [...windows].sort((a, b) => (b.usedPct ?? 0) - (a.usedPct ?? 0))[0];
            shown.push({ prefix: "", window: peak });
          }
          const tone = (used: number | undefined) => used !== undefined && used >= 95 ? "text-red-300" : used !== undefined && used >= 80 ? "text-amber-200" : "text-white/95";
          return <button key={snapshot.provider} type="button" onClick={() => setOpen(true)} title={snapshot.stale ? t("agents.usageBar.staleTooltip") : `${labels[snapshot.provider] || snapshot.provider}: ${shown.map((item) => `${item.window.label} ${showRemaining ? t("agents.usage.remainingLabel") : t("agents.usage.usedLabel")}`).join(" · ")}`} className="flex shrink-0 items-center gap-2 font-mono text-white/70 hover:text-white">
            <AgentProviderIcon provider={snapshot.provider} compact />
            <span className="font-semibold tracking-wide">{labels[snapshot.provider] || snapshot.provider.toUpperCase()}</span>
            {shown.map((item, index) => (
              <span key={item.window.id || index} className="flex items-center gap-1">
                {index > 0 && <span className="text-white/30">·</span>}
                {item.prefix && <span className="text-[9px] text-white/45">{item.prefix}</span>}
                <span className={`font-bold ${tone(item.window.usedPct)}`}>{`${formatNumber(Math.round(showRemaining ? 100 - (item.window.usedPct ?? 0) : item.window.usedPct ?? 0))}%`}{snapshot.stale ? "*" : ""}</span>
              </span>
            ))}
          </button>;
        })}
      </div>
      <button type="button" disabled={refreshing} onClick={() => void refresh()} className="ml-2 rounded-md p-1.5 text-white/50 hover:bg-white/10 hover:text-white disabled:opacity-25" title={t("agents.usageBar.refreshTooltip")}>{refreshing ? <Loader2 className="h-3.5 w-3.5 animate-spin" /> : <RefreshCw className="h-3.5 w-3.5" />}</button>
      <PortalMenu open={open} anchorRef={anchorRef} onClose={() => setOpen(false)} align="left" placement="top" minWidth={410}>
        <AgentUsagePopover snapshots={ordered} showRemaining={showRemaining} onToggle={() => setShowRemaining((value) => !value)} />
      </PortalMenu>
    </div>
  );
}
