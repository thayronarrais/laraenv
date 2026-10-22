// Promotes a shell leaf (project/local) into an agent leaf while an adopted
// agent session — one the user started by hand inside a LaraEnv terminal, not
// launched through LaraEnv's own agent flow — is bound to its terminal, and
// reverts it back to the shell once that session stops.
import type { Leaf } from "./types";

export type AdoptedSessionView = {
  id: string;
  terminalId?: string;
  agentId: "claude" | "codex" | "kimi" | "opencode";
  projectName: string;
  state: string;
  adopted?: boolean;
};

const liveStates = new Set(["starting", "working", "needs-input", "done", "idle", "error"]);
const agentLabels: Record<AdoptedSessionView["agentId"], string> = {
  claude: "Claude Code", codex: "Codex", kimi: "Kimi Code", opencode: "OpenCode",
};

// A shell leaf (project/local) becomes an agent leaf while an adopted session
// is bound to its terminal, and turns back into the shell when it stops.
// spawn.kind is never changed so restore always respawns a shell.
export function applyAdoptedSession(leaf: Leaf, session: AdoptedSessionView): Leaf {
  if (!session.adopted) return leaf;
  if (leaf.spawn.kind !== "project" && leaf.spawn.kind !== "local") return leaf;
  const live = liveStates.has(session.state) && !!session.terminalId;

  if (live && session.terminalId === leaf.id) {
    if (leaf.spawn.laraSessionId === session.id) return leaf;
    const adoptedShell = leaf.spawn.adoptedShell ?? { kind: leaf.kind, label: leaf.label };
    return {
      ...leaf,
      kind: `agent:${session.agentId}`,
      label: `${session.projectName} · ${agentLabels[session.agentId]}`,
      spawn: { ...leaf.spawn, laraSessionId: session.id, agentId: session.agentId, adoptedShell },
    };
  }

  if (!live && leaf.spawn.laraSessionId === session.id) {
    const { laraSessionId: _id, agentId: _agent, adoptedShell, ...spawn } = leaf.spawn;
    return {
      ...leaf,
      kind: adoptedShell?.kind ?? leaf.spawn.terminalKind ?? "powershell",
      label: adoptedShell?.label ?? leaf.label,
      spawn,
    };
  }
  return leaf;
}
