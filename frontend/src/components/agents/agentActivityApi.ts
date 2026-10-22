import type { ActivityInterest, ActivityInterestPurpose, ActivitySnapshot } from "./activityTypes";
// @ts-ignore — generated locally by Wails, excluded from Git.
import { GetAgentActivity, RefreshAgentActivity, SetAgentActivityInterest, RemoveAgentActivityInterest } from "../../../wailsjs/go/main/App";
// @ts-ignore — generated locally by Wails, excluded from Git.
import { EventsOn } from "../../../wailsjs/runtime";
export interface AgentActivityApi {
  get(sessionId: string): Promise<ActivitySnapshot>;
  refresh(sessionId: string): Promise<ActivitySnapshot>;
  setInterest(interest: ActivityInterest): Promise<void>;
  removeInterest(windowId: string, purpose: ActivityInterestPurpose): Promise<void>;
  onChanged(listener: (snapshot: ActivitySnapshot) => void): () => void;
  onReset(listener: () => void): () => void;
}
let windowId: string | undefined;
export function getAgentActivityWindowId(): string {
  return windowId ??= crypto.randomUUID();
}

const record = (value: unknown): value is Record<string, unknown> => value !== null && typeof value === "object" && !Array.isArray(value);
const oneOf = (value: unknown, choices: readonly string[]) => typeof value === "string" && choices.includes(value);
const strings = (value: Record<string, unknown>, keys: readonly string[]) => keys.every(key => typeof value[key] === "string");
const states = ["starting", "working", "needs-input", "reply-finished", "idle", "stopped", "interrupted", "error", "unknown"];
function validNode(value: unknown): boolean {
  if (!record(value) || !strings(value, ["id", "providerNodeId", "label", "firstObservedAt", "lastObservedAt"])
    || !oneOf(value.kind, ["agent", "background-task"]) || !oneOf(value.state, states)) return false;
  if (!["parentId", "agentType", "startedAt", "endedAt", "projectName", "projectPath", "worktreePath", "terminalId"]
    .every(key => value[key] === undefined || typeof value[key] === "string")) return false;
  if (value.model !== undefined && (!record(value.model) || typeof value.model.name !== "string" || !oneOf(value.model.kind, ["configured", "observed"]))) return false;
  if (value.tokens !== undefined) {
    if (!record(value.tokens)) return false;
    const tokens = value.tokens;
    if (!["input", "output", "total"].every(key => tokens[key] === undefined || Number.isSafeInteger(tokens[key]))) return false;
  }
  return true;
}

// Wails widens Go enum fields to strings. Validate once at the native boundary
// before narrowing to the frontend contract; do not infer absent metrics.
function snapshot(raw: unknown): ActivitySnapshot | null {
  if (!record(raw) || !strings(raw, ["sessionId", "runId", "sourceId", "rootId", "observedSince", "observedAt"])
    || !Number.isSafeInteger(raw.revision) || (raw.revision as number) < 0
    || !oneOf(raw.provider, ["claude", "codex", "kimi", "opencode"])
    || !oneOf(raw.status, ["loading", "available", "partial", "unavailable", "error"])
    || !oneOf(raw.messageCode, ["none", "no-provider-session", "hooks-unobserved", "hooks-disabled", "hook-trust-required", "unsupported-provider", "unsupported-protocol", "observation-ended", "refresh-failed", "owner-unavailable", "owner-conflict", "truncated"])
    || typeof raw.truncated !== "boolean" || typeof raw.stale !== "boolean" || !record(raw.capabilities)) return null;
  const capabilities = raw.capabilities;
  if (!["children", "backgroundTasks", "liveState", "duration", "model", "tokens"].every(key => typeof capabilities[key] === "boolean")) return null;
  const nodes = raw.nodes ?? [];
  const backgroundTasks = raw.backgroundTasks ?? [];
  if (!Array.isArray(nodes) || !nodes.every(validNode) || !Array.isArray(backgroundTasks) || !backgroundTasks.every(validNode)) return null;
  // Empty source/run and revision zero represent a valid unobserved root.
  return { ...raw, nodes, backgroundTasks } as unknown as ActivitySnapshot;
}

function requiredSnapshot(raw: unknown): ActivitySnapshot {
  const value = snapshot(raw);
  if (!value) throw new Error("Invalid agent activity snapshot");
  return value;
}

export const agentActivityApi: AgentActivityApi = {
  get: async sessionId => requiredSnapshot(await GetAgentActivity(sessionId)),
  refresh: async sessionId => requiredSnapshot(await RefreshAgentActivity(sessionId)),
  setInterest: interest => SetAgentActivityInterest(interest),
  removeInterest: (id, purpose) => RemoveAgentActivityInterest(id, purpose),
  onChanged: listener => EventsOn("agent:activity-changed", (raw: unknown) => {
    const value = snapshot(raw);
    if (value) listener(value);
  }),
  onReset: listener => EventsOn("agent:sessions-reset", listener),
};
