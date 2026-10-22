// Synthetic metadata only: no provider accounts, transcripts or real sessions.
const observed = "2026-10-15T10:00:00Z";
const node = (id, label, state, extra = {}) => ({
  id, providerNodeId: id.split(":").at(-1), kind: "agent", label, state,
  firstObservedAt: observed, lastObservedAt: observed, ...extra,
});
export const AGENT_MAP_SESSIONS = [
  { id: "map-claude", title: "Checkout implementation", agentId: "claude", terminalId: "map-pty-claude" },
  { id: "map-codex", title: "Checkout review", agentId: "codex", terminalId: "map-pty-codex" },
].map(session => ({ ...session, projectName: "acme-shop", projectPath: "C:/sites/acme-shop", launchMode: "current", providerSessionId: `synthetic-${session.id}`, state: "working", updatedAt: observed, unread: false, archived: false }));

export const AGENT_MAP_SNAPSHOT = {
  sessionId: "map-claude", runId: "synthetic-run-claude", sourceId: "synthetic-owner", revision: 3,
  rootId: "claude:map-claude:root", provider: "claude", observedSince: observed, observedAt: observed,
  status: "available", messageCode: "none", stale: false, truncated: false,
  capabilities: { children: true, backgroundTasks: true, liveState: true, duration: true, model: true, tokens: false },
  nodes: [
    node("claude:map-claude:root", "Checkout implementation", "working", { projectName: "acme-shop", projectPath: "C:/sites/acme-shop", terminalId: "map-pty-claude", model: { name: "Claude Sonnet", kind: "configured" } }),
    node("claude:map-claude:child-review", "Review coupon calculations", "needs-input", { parentId: "claude:map-claude:root", agentType: "reviewer" }),
    node("claude:map-claude:child-tests", "Verify checkout tests", "reply-finished", { parentId: "claude:map-claude:root", agentType: "tester", startedAt: observed, endedAt: "2026-10-15T10:00:22Z" }),
    node("claude:map-claude:grandchild", "Inspect edge cases", "unknown", { parentId: "claude:map-claude:child-review" }),
  ],
  backgroundTasks: [node("claude:map-claude:task-tests", "Checkout test runner", "working", { kind: "background-task", agentType: "shell", parentId: "claude:map-claude:root" }), ...Array.from({ length: 5 }, (_, index) => node(`claude:map-claude:task-${index}`, ["Format checkout files", "Build storefront assets", "Validate coupon fixtures", "Check cart totals", "Summarize test results"][index], index % 2 ? "reply-finished" : "unknown", { kind: "background-task", agentType: "shell" }))],
};
export const CODEX_MAP_SNAPSHOT = {
  ...AGENT_MAP_SNAPSHOT, sessionId: "map-codex", runId: "synthetic-run-codex", rootId: "codex:map-codex:root", provider: "codex",
  status: "partial", messageCode: "hooks-unobserved",
  capabilities: { children: true, backgroundTasks: false, liveState: false, duration: false, model: false, tokens: false },
  nodes: [node("codex:map-codex:root", "Checkout review", "unknown", { terminalId: "map-pty-codex", projectName: "acme-shop", projectPath: "C:/sites/acme-shop" }), node("codex:map-codex:child", "Review data model", "unknown", { parentId: "codex:map-codex:root" })], backgroundTasks: [],
};
export const AGENT_MAP_STATE = {
  webgl: false,
  storage: { "laraenv.agentNavigatorVisible": true, "laraenv.agentAutoSwitch": true },
  projects: [{ name: "acme-shop", path: "C:/sites/acme-shop", url: "https://acme-shop.test", proxyPort: 0 }],
  agents: { enabled: true, profiles: [{ id: "claude", label: "Claude Code" }, { id: "codex", label: "Codex" }].map(profile => ({ ...profile, available: true, capabilities: { launch: true, resume: true, richStatus: true, history: true, usage: false } })), sessions: AGENT_MAP_SESSIONS, usage: [] },
  agentActivity: { snapshots: { "map-claude": AGENT_MAP_SNAPSHOT, "map-codex": CODEX_MAP_SNAPSHOT } },
  workspace: { version: 2, groups: [{ x: 0, y: 0, w: 900, h: 600, snap: "max", activeTabIndex: 0, tabs: AGENT_MAP_SESSIONS.map(session => ({ label: session.title, activeLeafIndex: 0, tree: { type: "leaf", label: session.title, kind: `agent:${session.agentId}`, spawn: { kind: "agent", projectName: session.projectName, agentId: session.agentId, laraSessionId: session.id } } })) }] },
};

// Serialized into the page by base.mjs, so keep this function self-contained.
// These are the same four Wails methods the production activity API calls.
export function installAgentActivityMock(state, overrides) {
  const mock = window.__agentActivity = { snapshots: state.agentActivity?.snapshots || {}, gets: [], refreshes: [], interests: {}, setInterests: [], removedInterests: [], focusedSessions: [], ptySpawns: [], ptyWrites: [], documentReports: [], documentReplies: [] };
  const empty = sessionId => {
    const provider = state.agents?.sessions?.find(session => session.id === sessionId)?.agentId || "claude";
    return { sessionId, runId: "", sourceId: "synthetic-owner", revision: 0, rootId: provider + ":" + sessionId + ":root", provider, observedSince: "", observedAt: "", status: "unavailable", messageCode: "no-provider-session", capabilities: { children: false, backgroundTasks: false, liveState: false, duration: false, model: false, tokens: false }, nodes: [], backgroundTasks: [], truncated: false, stale: false };
  };
  Object.assign(overrides, {
    GetAgentActivity: sessionId => { mock.gets.push(sessionId); return Promise.resolve(mock.snapshots[sessionId] || empty(sessionId)); },
    RefreshAgentActivity: sessionId => { mock.refreshes.push(sessionId); return Promise.resolve(mock.snapshots[sessionId] || empty(sessionId)); },
    SetAgentActivityInterest: interest => { mock.setInterests.push({ ...interest }); mock.interests[interest.windowId + ":" + interest.purpose] = { ...interest }; return Promise.resolve(); },
    RemoveAgentActivityInterest: (windowId, purpose) => { mock.removedInterests.push({ windowId, purpose }); delete mock.interests[windowId + ":" + purpose]; return Promise.resolve(); },
  });
}
