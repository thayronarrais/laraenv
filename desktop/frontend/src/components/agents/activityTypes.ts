export type ActivityProvider = "claude" | "codex" | "kimi" | "opencode";
export type ActivityState = "starting" | "working" | "needs-input" | "reply-finished" | "idle" | "stopped" | "interrupted" | "error" | "unknown";
export type ActivityStatus = "loading" | "available" | "partial" | "unavailable" | "error";
export type ActivityMessageCode = "none" | "no-provider-session" | "hooks-unobserved" | "hooks-disabled" | "hook-trust-required" | "unsupported-provider" | "unsupported-protocol" | "observation-ended" | "refresh-failed" | "owner-unavailable" | "owner-conflict" | "truncated";
export type ActivityNodeKind = "agent" | "background-task";
export type ActivityModelKind = "configured" | "observed";
export interface ActivityModel { name: string; kind: ActivityModelKind }
export interface ActivityTokens { input?: number; output?: number; total?: number }
export interface ActivityCapabilities {
  children: boolean; backgroundTasks: boolean; liveState: boolean;
  duration: boolean; model: boolean; tokens: boolean;
}
export interface ActivityNode {
  id: string; providerNodeId: string; parentId?: string; kind: ActivityNodeKind;
  label: string; agentType?: string; state: ActivityState;
  firstObservedAt: string; lastObservedAt: string; startedAt?: string; endedAt?: string;
  model?: ActivityModel; tokens?: ActivityTokens;
  projectName?: string; projectPath?: string; worktreePath?: string; terminalId?: string;
}
export interface ActivitySnapshot {
  sessionId: string; runId: string; sourceId: string; revision: number; rootId: string;
  provider: ActivityProvider; observedSince: string; observedAt: string;
  status: ActivityStatus; messageCode: ActivityMessageCode; capabilities: ActivityCapabilities;
  nodes: ActivityNode[]; backgroundTasks: ActivityNode[]; truncated: boolean; stale: boolean;
}
export type ActivityInterestPurpose = "counter" | "dialog";
export interface ActivityInterest { windowId: string; purpose: ActivityInterestPurpose; sessionId: string }
export type ActivityEventKind = "subagent-start" | "subagent-turn-stop" | "subagent-needs-input" | "subagent-error" | "background-inventory";
export interface ActivityBackgroundTask {
  providerNodeId: string; label: string; agentType?: string; state: ActivityState;
  startedAt?: string; endedAt?: string;
}
export interface ActivityEvent {
  ownerId: string; terminalId: string; sessionId: string; runId: string;
  provider: ActivityProvider; providerSessionId: string; nodeId?: string;
  parentProviderId?: string; turnId?: string; kind: ActivityEventKind; agentType?: string;
  model?: ActivityModel; receivedAt: string; backgroundTasks?: ActivityBackgroundTask[]; truncated: boolean;
}
