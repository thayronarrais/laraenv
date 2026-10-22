import { AlertTriangle, Clock3 } from "lucide-react";
import { useI18n, type Vars } from "../../lib/i18n";

export type AgentUsageWindowView = { id: string; label: string; usedPct?: number; resetsAt?: string };
export type AgentUsageView = { provider: string; plan?: string; status: string; message?: string; readAt: string; stale: boolean; windows: AgentUsageWindowView[] | null };

export function usageWindows(snapshot: AgentUsageView): AgentUsageWindowView[] {
  return Array.isArray(snapshot.windows) ? snapshot.windows.filter((window) => typeof window.usedPct === "number" && Number.isFinite(window.usedPct)) : [];
}

export function visibleUsageSnapshots(snapshots: AgentUsageView[]): AgentUsageView[] {
  return snapshots.filter((snapshot) => snapshot.status === "available" && usageWindows(snapshot).length > 0);
}

const labels: Record<string, string> = { claude: "Claude Code", codex: "Codex", kimi: "Kimi Code", opencode: "OpenCode" };

function resetLabel(
  value: string | undefined,
  formatDate: (d: Date | number, opts?: Intl.DateTimeFormatOptions) => string,
  t: (key: string, vars?: Vars) => string,
) {
  if (!value) return t("agents.usage.resetUnknown");
  const date = new Date(value);
  if (!Number.isFinite(date.getTime())) return t("agents.usage.resetUnknown");
  return t("agents.usage.resetsAt", { when: formatDate(date, { month: "short", day: "numeric", hour: "2-digit", minute: "2-digit" }) });
}

export default function AgentUsagePopover({ snapshots, showRemaining, onToggle }: { snapshots: AgentUsageView[]; showRemaining: boolean; onToggle: () => void }) {
  const { t, formatNumber, formatDate } = useI18n();
  const visible = visibleUsageSnapshots(snapshots);
  return (
    <div className="w-[410px] p-3.5">
      <div className="mb-3 flex items-center justify-between">
        <div><div className="text-[13px] font-semibold text-white/95">{t("agents.usagePopover.title")}</div><div className="mt-1 text-[10px] text-white/55">{t("agents.usagePopover.subtitle")}</div></div>
        <button type="button" onClick={onToggle} className="rounded-md border border-white/15 bg-white/[0.07] px-2.5 py-1.5 text-[10px] font-medium uppercase tracking-wider text-white/70 hover:bg-white/10 hover:text-white">{showRemaining ? t("agents.usage.remainingLabel") : t("agents.usage.usedLabel")}</button>
      </div>
      <div className="grid gap-2">
        {visible.length === 0 && <p className="py-3 text-[12px] text-white/55">{t("agents.usagePopover.emptyMessage")}</p>}
        {visible.map((snapshot) => {
          const windows = usageWindows(snapshot);
          return <section key={snapshot.provider} className="rounded-lg border border-white/[0.12] bg-black/20 p-3">
            <div className="flex items-center justify-between gap-3">
              <span className="text-[12px] font-semibold text-white/90">{labels[snapshot.provider] || snapshot.provider}</span>
              <span className="font-mono text-[10px] uppercase text-white/50">{snapshot.plan || t("agents.usage.availableLabel")}</span>
            </div>
            {snapshot.stale && <div className="mt-2 flex items-start gap-2 text-[10px] leading-4 text-amber-200/80"><AlertTriangle className="mt-0.5 h-3 w-3 shrink-0" /> {t("agents.usagePopover.staleNotice", { message: snapshot.message ?? "" })}</div>}
            {snapshot.status === "available" && windows.length > 0 ? (
              <div className="mt-2 grid gap-2">
                {windows.map((window) => {
                  const used = window.usedPct;
                  const shown = used === undefined ? undefined : showRemaining ? Math.max(0, 100 - used) : used;
                  const critical = used !== undefined && used >= 95;
                  const warning = used !== undefined && used >= 80;
                  return (
                    <div key={window.id}>
                      <div className="mb-1.5 flex items-center justify-between font-mono text-[10px] text-white/75">
                        <span>{window.label}</span>
                        <span className={critical ? "text-red-300" : warning ? "text-amber-200" : "opacity-65"}>{shown === undefined ? t("agents.usage.unknownLabel") : `${formatNumber(Math.round(shown))}% ${showRemaining ? t("agents.usage.leftLabel") : t("agents.usage.usedLabel")}`}</span>
                      </div>
                      <div className="h-2 overflow-hidden rounded-full bg-white/[0.12]"><div className={`h-full rounded-full ${critical ? "bg-red-400" : warning ? "bg-amber-300" : "bg-accent2"}`} style={{ width: `${shown ?? 0}%` }} /></div>
                      <div className="mt-1.5 flex items-center gap-1.5 text-[10px] text-white/50"><Clock3 className="h-3 w-3" /> {resetLabel(window.resetsAt, formatDate, t)}</div>
                    </div>
                  );
                })}
              </div>
            ) : (
              <div className="mt-2 flex items-start gap-2 text-[10px] leading-4 text-white/55"><AlertTriangle className="mt-0.5 h-3 w-3 shrink-0" /> {snapshot.message || t("agents.usagePopover.unavailableFallback")}</div>
            )}
          </section>;
        })}
      </div>
    </div>
  );
}
