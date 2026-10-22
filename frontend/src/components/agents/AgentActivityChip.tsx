import { Network } from "lucide-react";
import { useI18n, type Vars } from "../../lib/i18n";
import { activityCounts } from "./activityModel";
import type { ActivityMessageCode, ActivitySnapshot, ActivityState } from "./activityTypes";
import type { AgentActivityError } from "./useAgentActivity";

// Literal keys live here so all collection states share one translation map.
export const activityMessageKeys: Record<Exclude<ActivityMessageCode, "none">, string> = {
  "no-provider-session": "agents.map.messages.noProviderSession",
  "hooks-unobserved": "agents.map.messages.hooksUnobserved",
  "hooks-disabled": "agents.map.messages.hooksDisabled",
  "hook-trust-required": "agents.map.messages.hookTrustRequired",
  "unsupported-provider": "agents.map.messages.unsupportedProvider",
  "unsupported-protocol": "agents.map.messages.unsupportedProtocol",
  "observation-ended": "agents.map.messages.observationEnded",
  "refresh-failed": "agents.map.messages.refreshFailed",
  "owner-unavailable": "agents.map.messages.ownerUnavailable",
  "owner-conflict": "agents.map.messages.ownerConflict",
  truncated: "agents.map.messages.truncated",
};
export const activityStateKeys: Record<ActivityState, string> = {
  starting: "agents.map.states.starting", working: "agents.map.states.working",
  "needs-input": "agents.map.states.needsInput", "reply-finished": "agents.map.states.replyFinished",
  idle: "agents.map.states.idle", stopped: "agents.map.states.stopped", interrupted: "agents.map.states.interrupted",
  error: "agents.map.states.error", unknown: "agents.map.states.unknown",
};
export function activityCountLabel(snapshot: ActivitySnapshot, t: (key: string, vars?: Vars) => string) {
  const count = activityCounts(snapshot);
  return t(count.lowerBound ? "agents.map.atLeastAgents" : "agents.map.agents", {count: count.agents});
}
export function activityNotice(snapshot: ActivitySnapshot | null, loading: boolean, error: AgentActivityError | null, t: (key: string) => string): string {
  if (error) return t(activityMessageKeys[error]);
  if (loading || snapshot?.status === "loading") return t("agents.map.loading");
  if (snapshot?.messageCode && snapshot.messageCode !== "none") return t(activityMessageKeys[snapshot.messageCode]);
  if (snapshot?.status === "error") return t("agents.map.messages.refreshFailed");
  if (snapshot?.status === "unavailable") return t("agents.map.unavailable");
  return "";
}
export type AgentActivityChipProps = {
  snapshot: ActivitySnapshot | null;
  loading: boolean;
  error?: AgentActivityError | null;
  onOpen(): void;
  onSelectSession(): void;
};

export default function AgentActivityChip({snapshot, loading, error = null, onOpen, onSelectSession}: AgentActivityChipProps) {
  const {t, formatNumber} = useI18n();
  const count = snapshot && activityCounts(snapshot);
  const notice = activityNotice(snapshot, loading, error, t);
  const canCount = snapshot?.capabilities.children && snapshot.status !== "loading" && snapshot.status !== "unavailable";
  const heading = canCount && snapshot ? activityCountLabel(snapshot, t) : notice || t(snapshot ? "agents.map.title" : "agents.map.selectSession");
  const summary = canCount && count ? t("agents.map.activitySummary", {working: formatNumber(count.working), waiting: formatNumber(count.waiting), unknown: formatNumber(count.unknown)}) : "";
  const detail = [summary, canCount ? notice : ""].filter(Boolean).join(" · ");
  const label = [heading, detail].filter(Boolean).join(" · ");
  return <button type="button" aria-haspopup="dialog" aria-label={label} title={label} onClick={snapshot || loading || error ? onOpen : onSelectSession}
    className="flex h-8 max-w-[min(50vw,360px)] items-center gap-2 rounded-md border border-white/10 bg-white/[0.04] px-2.5 py-0.5 text-left text-[11px] leading-3 text-white/80 hover:bg-white/[0.08] focus-visible:outline focus-visible:outline-2 focus-visible:outline-accent2">
    <Network aria-hidden="true" className="h-3.5 w-3.5 shrink-0 text-accent2" />
    <span className="min-w-0">
      <span className="block truncate">{heading}</span>
      {detail && <span className={`block truncate text-[10px] ${notice ? "text-amber-200/85" : "text-white/55"}`}>{detail}</span>}
    </span>
  </button>;
}
