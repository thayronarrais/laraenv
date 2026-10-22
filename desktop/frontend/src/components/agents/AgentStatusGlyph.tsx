import clsx from "clsx";
import { useT } from "../../lib/i18n";

const tones: Record<string, string> = {
  starting: "bg-sky-400",
  working: "bg-emerald-400",
  "needs-input": "bg-amber-300",
  done: "bg-violet-400",
  idle: "bg-slate-400",
  stopped: "bg-zinc-500",
  error: "bg-red-400",
};

// Shared by every place that renders a session's status word (this glyph's
// tooltip, the session card, the page's own menu and title attributes, the
// filter dropdown). Only the LABEL is translated — the backend/comparison
// value (session.state) never changes shape.
export function agentStatusLabel(t: (key: string) => string, state: string): string {
  switch (state) {
    case "starting": return t("agents.status.starting");
    case "working": return t("agents.status.working");
    case "needs-input": return t("agents.status.needsInput");
    case "done": return t("agents.status.done");
    case "idle": return t("agents.status.idle");
    case "stopped": return t("agents.status.stopped");
    case "error": return t("agents.status.error");
    default: return state.replace(/-/g, " ");
  }
}

export default function AgentStatusGlyph({ state, unread = false }: { state: string; unread?: boolean }) {
  const t = useT();
  return (
    <span className="relative inline-flex h-3.5 w-3.5 items-center justify-center" title={agentStatusLabel(t, state)}>
      <span className={clsx("h-2.5 w-2.5 rounded-full", tones[state] || "bg-zinc-500", ["starting", "working"].includes(state) && "animate-pulse")} />
      {unread && <span className="absolute -right-0.5 -top-0.5 h-1.5 w-1.5 rounded-full border border-[#171717] bg-accent2" />}
    </span>
  );
}
