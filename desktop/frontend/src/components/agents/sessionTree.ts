import type { AgentSessionView } from "./AgentSessionCard";

export type AgentSessionLocation = {
  key: string;
  // Real branch/folder names are data and go straight in `label`. When there
  // is no branch to show, `label` is empty and `labelKey` names the
  // catalogue key instead — this file has no useT(), so the render site
  // (pages/Agents.tsx, which already has `t` in scope) resolves it.
  label: string;
  labelKey?: string;
  kind: AgentSessionView["launchMode"];
  sessions: AgentSessionView[];
};

export type AgentSessionProject = {
  name: string;
  locations: AgentSessionLocation[];
  sessionCount: number;
};

export type AgentSessionScope = "active" | "recent" | "archived";

const activeStates = new Set(["starting", "working", "needs-input", "done", "idle"]);

function pathLeaf(value?: string) {
  const parts = (value || "").replace(/\\/g, "/").split("/").filter(Boolean);
  return parts[parts.length - 1] || "worktree";
}

// Real branch/folder names are returned as `label` (data). When there is no
// branch to show, `label` is "" and `labelKey` names the catalogue key for
// the fallback phrase instead of a literal English string — this module has
// no useT(), so callers resolve it (pages/Agents.tsx, which already has `t`
// in scope for this same tree).
export function locationLabel(session: AgentSessionView): { label: string; labelKey?: string } {
  if (session.launchMode === "worktree") return { label: session.gitBranch || pathLeaf(session.worktreePath) };
  if (session.launchMode === "branch") {
    return session.gitBranch ? { label: session.gitBranch } : { label: "", labelKey: "agents.page.locationSelectedBranch" };
  }
  return session.gitBranch ? { label: session.gitBranch } : { label: "", labelKey: "agents.page.locationCurrentCheckout" };
}

function timestamp(value?: string) {
  const time = new Date(value || "").getTime();
  return Number.isFinite(time) ? time : 0;
}

// Live sessions keep their launch order (the same order as their terminal
// tabs) so the navigator never reshuffles while agents emit activity or get
// marked read — every provider signal bumps updatedAt. Stopped sessions are
// only touched by explicit actions, so recency stays a stable order for them.
export function compareSessions(left: AgentSessionView, right: AgentSessionView) {
  const leftLive = isActiveAgentSession(left);
  const rightLive = isActiveAgentSession(right);
  if (leftLive !== rightLive) return leftLive ? -1 : 1;
  if (leftLive) {
    return timestamp(left.startedAt || left.updatedAt) - timestamp(right.startedAt || right.updatedAt)
      || left.id.localeCompare(right.id);
  }
  return timestamp(right.updatedAt) - timestamp(left.updatedAt) || left.id.localeCompare(right.id);
}

export function groupAgentSessions(sessions: AgentSessionView[], projectNames: string[] = []): AgentSessionProject[] {
  const names = new Set(projectNames);
  for (const session of sessions) names.add(session.projectName);

  return [...names].sort((left, right) => left.localeCompare(right)).map((name) => {
    const projectSessions = sessions
      .filter((session) => session.projectName === name)
      .sort(compareSessions);
    const byLocation = new Map<string, AgentSessionLocation>();

    for (const session of projectSessions) {
      const identity = session.launchMode === "worktree"
        ? session.worktreePath || session.gitBranch || "worktree"
        : session.gitBranch || "current";
      const key = `${session.launchMode}:${identity}`;
      const current = byLocation.get(key);
      if (current) current.sessions.push(session);
      else {
        const loc = locationLabel(session);
        byLocation.set(key, { key, label: loc.label, labelKey: loc.labelKey, kind: session.launchMode, sessions: [session] });
      }
    }

    const rank = { current: 0, branch: 1, worktree: 2 } as const;
    // Sort key falls back to labelKey (untranslated) for the no-branch case
    // — a tiebreaker among same-kind locations only, not user-facing text.
    const sortKey = (l: AgentSessionLocation) => l.label || l.labelKey || "";
    const locations = [...byLocation.values()].sort((left, right) =>
      rank[left.kind] - rank[right.kind] || sortKey(left).localeCompare(sortKey(right)));
    return { name, locations, sessionCount: projectSessions.length };
  });
}

export function isActiveAgentSession(session: AgentSessionView) {
  return !session.archived && !!session.terminalId && activeStates.has(session.state);
}

export function filterAgentSessions(
  sessions: AgentSessionView[],
  scope: AgentSessionScope,
  query = "",
  projectName = "",
) {
  const normalized = query.trim().toLowerCase();
  return sessions.filter((session) => {
    const inScope = scope === "active"
      ? isActiveAgentSession(session)
      : scope === "archived" ? session.archived : !session.archived;
    if (!inScope || (projectName && session.projectName !== projectName)) return false;
    if (!normalized) return true;
    return [session.title, session.projectName, session.gitBranch || "", session.agentId, session.lastPreview || ""]
      .some((value) => value.toLowerCase().includes(normalized));
  });
}

export function sessionProjectNames(sessions: AgentSessionView[]) {
  return [...new Set(sessions.map((session) => session.projectName))]
    .sort((left, right) => left.localeCompare(right));
}

// SESSION_AGE_NOW_KEY is a catalogue key, not display text: sessionAge keeps
// returning a plain string (the compact "3m"/"2h"/"3d" shapes its test
// locks), and this module has no useT() (see locationLabel above), so the
// sub-minute case returns the key itself rather than the literal English
// "now" — the call site (Agents.tsx, which already has `t` in scope) checks
// for this exact sentinel and resolves it before rendering.
export const SESSION_AGE_NOW_KEY = "agents.page.sessionAgeNow";

export function sessionAge(value: string, now = Date.now()) {
  const timestamp = new Date(value).getTime();
  if (!Number.isFinite(timestamp)) return "";
  const minutes = Math.max(0, Math.floor((now - timestamp) / 60000));
  if (minutes < 1) return SESSION_AGE_NOW_KEY;
  if (minutes < 60) return `${minutes}m`;
  const hours = Math.floor(minutes / 60);
  if (hours < 48) return `${hours}h`;
  return `${Math.floor(hours / 24)}d`;
}

// Resolves sessionAge's result for display, same shape as AgentStatusGlyph's
// agentStatusLabel(t, state) — the compact "3m"/"2h"/"3d" forms pass through
// unchanged, the sub-minute sentinel gets translated.
export function sessionAgeLabel(t: (key: string) => string, value: string, now = Date.now()) {
  const age = sessionAge(value, now);
  return age === SESSION_AGE_NOW_KEY ? t(age) : age;
}
