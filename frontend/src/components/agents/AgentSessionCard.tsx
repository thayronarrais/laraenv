import { Archive, Boxes, ExternalLink, GitBranch, Play, RotateCcw, ScrollText, Server, Square, Tag, Trash2, Unlink } from "lucide-react";
import AgentStatusGlyph, { agentStatusLabel } from "./AgentStatusGlyph";
import { useT } from "../../lib/i18n";

export type AgentSessionView = {
  id: string;
  projectName: string;
  projectPath: string;
  agentId: "claude" | "codex" | "kimi" | "opencode";
  launchMode: "current" | "branch" | "worktree";
  gitBranch?: string;
  worktreePath?: string;
  additionalDirectories?: string[];
  terminalId?: string;
  providerSessionId?: string;
  title: string;
  state: string;
  startedAt?: string;
  updatedAt: string;
  lastPreview?: string;
  unread: boolean;
  archived: boolean;
  exitError?: string;
  adopted?: boolean;
};

export type AgentWorktreeRuntimeView = {
  sessionId: string;
  projectName: string;
  worktreePath: string;
  needsDevServer: boolean;
  running: boolean;
  pid: number;
  port: number;
  configuredPort: number;
  script?: string;
  routedHostnames: string[];
  worktreeDirty: boolean;
  canRemove: boolean;
  blockers: string[];
};

const liveStates = new Set(["starting", "working", "needs-input", "done", "idle"]);

// This session card's own compact "3m"/"2h"/"3d" notation, duplicated (not
// imported) from sessionTree.ts's sessionAge, which is out of scope for this
// batch. Left as-is: it is unit-letter shorthand, not English prose — "m"/
// "h"/"d" are the same abbreviations in Portuguese (minuto/hora/dia) — and
// swapping it for formatRelative's prose ("3 minutes ago") would change this
// dense card's layout, not just its language.
function age(value: string) {
  const timestamp = new Date(value).getTime();
  if (!Number.isFinite(timestamp)) return "";
  const minutes = Math.max(0, Math.floor((Date.now() - timestamp) / 60000));
  if (minutes < 1) return "now";
  if (minutes < 60) return `${minutes}m`;
  const hours = Math.floor(minutes / 60);
  if (hours < 48) return `${hours}h`;
  return `${Math.floor(hours / 24)}d`;
}

function launchModeLabel(t: (key: string) => string, mode: string): string {
  switch (mode) {
    case "current": return t("agents.launchModeLabel.current");
    case "branch": return t("agents.launchModeLabel.branch");
    case "worktree": return t("agents.launchModeLabel.worktree");
    default: return mode;
  }
}

export default function AgentSessionCard({
  session, providerLabel, canResume, worktreeRuntime, onOpen, onResume, onFresh, onRename, onStop, onArchive, onMarkRead,
  onStartWorktreeServer, onStopWorktreeServer, onWorktreeLogs, onDetachRoutes, onRemoveWorktree,
}: {
  session: AgentSessionView;
  providerLabel: string;
  canResume: boolean;
  worktreeRuntime?: AgentWorktreeRuntimeView;
  onOpen: () => void;
  onResume: () => void;
  onFresh: () => void;
  onRename: () => void;
  onStop: () => void;
  onArchive: () => void;
  onMarkRead: () => void;
  onStartWorktreeServer?: () => void;
  onStopWorktreeServer?: () => void;
  onWorktreeLogs?: () => void;
  onDetachRoutes?: () => void;
  onRemoveWorktree?: () => void;
}) {
  const t = useT();
  const live = liveStates.has(session.state) && !!session.terminalId;
  const preview = (session.exitError || session.lastPreview || t("agents.sessionCard.noPreviewFallback")).replace(/\s+/g, " ").trim();

  return (
    <article className={`rounded-xl border p-3 ${session.unread ? "border-accent2/35 bg-accent2/[0.055]" : "border-white/10 bg-white/[0.025]"}`}>
      <div className="flex items-start gap-3">
        <AgentStatusGlyph state={session.state} unread={session.unread} />
        <button type="button" onClick={live ? onOpen : onMarkRead} className="min-w-0 flex-1 text-left">
          <div className="flex flex-wrap items-center gap-2">
            <span className="truncate text-[12px] font-medium">{session.title}</span>
            <span className="rounded border border-white/10 bg-white/5 px-1.5 py-0.5 font-mono text-[8px] uppercase opacity-55">{providerLabel}</span>
            {session.adopted && (
              <span className="rounded border border-white/15 px-1 text-[8px] uppercase tracking-wide text-white/50" title={t("agents.sessionCard.terminalBadgeTooltip")}>{t("agents.sessionCard.terminalBadge")}</span>
            )}
            <span className="text-[9px] uppercase tracking-wider opacity-40">{agentStatusLabel(t, session.state)}</span>
          </div>
          <div className="mt-1 line-clamp-2 font-mono text-[9px] leading-4 opacity-45">{preview}</div>
        </button>
        <span className="shrink-0 font-mono text-[9px] opacity-35">{age(session.updatedAt)}</span>
      </div>

      <div className="mt-2.5 flex flex-wrap items-center gap-1.5 border-t border-white/[0.06] pt-2.5">
        <span className="flex items-center gap-1 rounded bg-white/5 px-1.5 py-1 font-mono text-[8px] opacity-55">
          {session.launchMode === "worktree" ? <Boxes className="h-2.5 w-2.5" /> : <GitBranch className="h-2.5 w-2.5" />}
          {launchModeLabel(t, session.launchMode)}{session.gitBranch ? ` · ${session.gitBranch}` : ""}
        </span>
        {live ? (
          <>
            <button type="button" onClick={onOpen} className="ml-auto flex items-center gap-1 rounded px-2 py-1 text-[9px] text-accent2 hover:bg-white/5"><ExternalLink className="h-3 w-3" /> {t("agents.sessionCard.openButton")}</button>
            <button type="button" onClick={onStop} title={session.adopted ? t("agents.sessionCard.stopTooltipAdopted") : t("agents.sessionCard.stopTooltipDefault")} className="flex items-center gap-1 rounded px-2 py-1 text-[9px] text-red-200/80 hover:bg-red-500/10"><Square className="h-3 w-3" /> {t("agents.sessionCard.stopButton")}</button>
          </>
        ) : canResume ? (
          <button type="button" onClick={onResume} className="ml-auto flex items-center gap-1 rounded bg-accent/20 px-2 py-1 text-[9px] text-white hover:bg-accent/35"><RotateCcw className="h-3 w-3" /> {t("agents.sessionCard.resumeButton")}</button>
        ) : (
          <button type="button" onClick={onFresh} className="ml-auto flex items-center gap-1 rounded bg-accent/20 px-2 py-1 text-[9px] text-white hover:bg-accent/35"><Play className="h-3 w-3" /> {t("agents.sessionCard.newSessionButton")}</button>
        )}
        <button type="button" onClick={onRename} className="flex items-center gap-1 rounded px-2 py-1 text-[9px] opacity-55 hover:bg-white/5 hover:opacity-100"><Tag className="h-3 w-3" /> {t("agents.sessionCard.renameButton")}</button>
        {!live && !session.archived && <button type="button" onClick={onArchive} className="flex items-center gap-1 rounded px-2 py-1 text-[9px] opacity-55 hover:bg-white/5 hover:opacity-100"><Archive className="h-3 w-3" /> {t("agents.sessionCard.archiveButton")}</button>}
      </div>

      {session.launchMode === "worktree" && worktreeRuntime && (
        <div className="mt-2.5 rounded-lg border border-white/[0.07] bg-black/15 px-2.5 py-2">
          <div className="flex flex-wrap items-center gap-1.5">
            <span className="flex items-center gap-1 text-[9px] font-medium text-white/65"><Server className="h-3 w-3" /> {t("agents.sessionCard.worktreeRuntimeLabel")}</span>
            {worktreeRuntime.running && <span className="rounded border border-emerald-400/25 bg-emerald-400/10 px-1.5 py-0.5 font-mono text-[8px] text-emerald-200">{t("agents.sessionCard.runningBadge", { port: worktreeRuntime.port })}</span>}
            {worktreeRuntime.worktreeDirty && <span className="rounded border border-amber-400/25 bg-amber-400/10 px-1.5 py-0.5 text-[8px] text-amber-200">{t("agents.sessionCard.dirtyBadge")}</span>}
          </div>
          {worktreeRuntime.routedHostnames.length > 0 && (
            <div className="mt-1.5 flex flex-wrap gap-1">
              {worktreeRuntime.routedHostnames.map((hostname) => <span key={hostname} className="rounded bg-accent/10 px-1.5 py-0.5 font-mono text-[8px] text-accent2/80">{hostname}</span>)}
            </div>
          )}
          <div className="mt-2 flex flex-wrap items-center gap-1">
            {worktreeRuntime.needsDevServer && (worktreeRuntime.running ? (
              <>
                <button type="button" onClick={onWorktreeLogs} className="flex items-center gap-1 rounded px-2 py-1 text-[8px] text-white/55 hover:bg-white/5 hover:text-white"><ScrollText className="h-2.5 w-2.5" /> {t("agents.sessionCard.logsButton")}</button>
                <button type="button" onClick={onStopWorktreeServer} className="flex items-center gap-1 rounded px-2 py-1 text-[8px] text-red-200/75 hover:bg-red-500/10"><Square className="h-2.5 w-2.5" /> {t("agents.sessionCard.stopServerButton")}</button>
              </>
            ) : (
              <button type="button" onClick={onStartWorktreeServer} className="flex items-center gap-1 rounded bg-emerald-400/10 px-2 py-1 text-[8px] text-emerald-200 hover:bg-emerald-400/20"><Play className="h-2.5 w-2.5" /> {t("agents.sessionCard.startServerButton", { port: worktreeRuntime.configuredPort })}</button>
            ))}
            {worktreeRuntime.routedHostnames.length > 0 && <button type="button" onClick={onDetachRoutes} className="ml-auto flex items-center gap-1 rounded px-2 py-1 text-[8px] text-white/45 hover:bg-white/5 hover:text-white"><Unlink className="h-2.5 w-2.5" /> {t("agents.sessionCard.disconnectUrlsButton")}</button>}
            {!session.archived && <button type="button" disabled={!worktreeRuntime.canRemove} title={worktreeRuntime.blockers.join(" · ") || t("agents.sessionCard.removeWorktreeDefaultTooltip")} onClick={onRemoveWorktree} className="flex items-center gap-1 rounded px-2 py-1 text-[8px] text-red-200/55 hover:bg-red-500/10 hover:text-red-100 disabled:cursor-not-allowed disabled:opacity-25"><Trash2 className="h-2.5 w-2.5" /> {t("agents.sessionCard.removeWorktreeButton")}</button>}
          </div>
          {!worktreeRuntime.canRemove && worktreeRuntime.blockers.length > 0 && <div className="mt-1.5 truncate text-[8px] text-white/30" title={worktreeRuntime.blockers.join(" · ")}>{worktreeRuntime.blockers[0]}</div>}
        </div>
      )}
    </article>
  );
}
