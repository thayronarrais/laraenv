// Shared data model for the floating-groups terminal workspace.
//
// A Workspace is a canvas of floating Groups. Each Group is a window with its
// own tab strip; each Tab holds a split tree of Leaves (terminal panes). This
// mirrors the old single-tab-bar model (Leaf/Split/Pane/Tab) but wraps the tab
// collection in multiple movable windows instead of one global bar.

import type { WorkspaceContextRef } from "../workspace/types";

export type LeafStatus = "pending" | "ready" | "error";

// How a pane can be re-created on app restart. Persisted with the layout so the
// startup respawn loop can bring each terminal back.
export type SpawnKind = "local" | "project" | "ssh" | "agent";
export type SpawnDescriptor = {
  kind: SpawnKind;
  terminalKind?: string; // local/project: "powershell" | "cmd" | "gitbash" | "cmder"
  projectName?: string;  // project
  sshHostName?: string;  // ssh
  cwd?: string;          // local: optional explicit working directory
  phpVersion?: string;   // local: explicit PHP version to prepend to PATH; "" = active
  nodeVersion?: string;  // local: explicit Node version to prepend to PATH; "" = active
  agentId?: "claude" | "codex" | "kimi" | "opencode"; // agent
  laraSessionId?: string; // agent: LaraEnv-owned session metadata id
  workspaceContext?: WorkspaceContextRef; // scoped shell source, separate from agent PTY identity
  workspaceRootKey?: string; // physical checkout identity for safe restore
  worktreePath?: string;  // agent: confirmed isolated checkout
  routeCanonical?: boolean; // agent worktree currently owns the primary domain
  previewHostname?: string; // agent worktree's first preview route
  adoptedShell?: { kind: string; label: string }; // project/local: the shell leaf an adopted agent replaced
};

// One terminal pane (leaf in the split tree). `id` matches the backend PTY id
// once ready; for pending leaves it's a placeholder. `kind` drives rendering
// (ssh timeline vs xterm); `spawn` drives respawn.
export type Leaf = {
  type: "leaf";
  id: string;
  label: string;
  kind: string;
  status?: LeafStatus;
  errorMsg?: string;
  sshStage?: string;
  spawn: SpawnDescriptor;
};

// Internal node: two children separated by a draggable resize handle.
export type Split = {
  type: "split";
  direction: "horizontal" | "vertical";
  children: [Pane, Pane];
};

export type Pane = Leaf | Split;

export type Tab = {
  tabId: string; // synthetic, separate from leaf ids
  label: string;
  tree: Pane;
  activeLeafId: string;
};

// Where a group is docked. null = free-floating at its restore geometry.
export type SnapState =
  | null
  | "left" | "right" | "top" | "bottom"
  | "tl" | "tr" | "bl" | "br"
  | "max";

export type Group = {
  id: string;
  // Restore geometry — always the un-snapped box. When `snap` is set the
  // rendered geometry is derived via snapGeometry(); un-snapping returns here.
  x: number; y: number; w: number; h: number;
  z: number;
  snap: SnapState;
  tabs: Tab[];
  activeTabId: string;
};

export type Workspace = {
  groups: Group[];
  zCounter: number;   // monotonic; FOCUS_GROUP bumps the focused group's z
  nextGroupNum: number;
};

// A mounted terminal pane, including panes in inactive tabs. terminalId is
// the leaf id (a temporary id while connecting), never an agent history id.
export type OpenTerminal = {
  terminalId: string;
  laraSessionId?: string;
  label: string;
  hint: string;
  active: boolean;
};

export type TerminalFocusRequest = { terminalId: string; seq: number };

export type OpenProjectRequest =
  | { kind: "project"; name: string; terminalKind?: string; seq: number }
  | { kind: "agent"; project: string; agentId: "claude" | "codex" | "kimi" | "opencode"; agentLabel: string; mode: "current" | "branch" | "worktree"; branch?: string; createBranch?: boolean; baseRef?: string; worktreeName?: string; routeCanonical?: boolean; previewHostname?: string; routeProxyPort?: number; additionalDirectories?: string[]; seq: number }
  | { kind: "agent-resume"; sessionId: string; project: string; agentId: "claude" | "codex" | "kimi" | "opencode"; agentLabel: string; seq: number }
  | { kind: "ssh"; host: string; seq: number }
  | { kind: "blank"; shell?: string; project?: string; seq: number }
  | { kind: "command"; command: string; shell?: string; project?: string; seq: number };

// An installed PHP/Node runtime, as returned by ListPhpVersions/ListNodeVersions.
export type RuntimeVersion = { version: string; path: string; active: boolean };

export type TerminalProfile = { kind: string; label: string; available: boolean; path: string };
export type TerminalsInfo = { profiles: TerminalProfile[]; defaultKind: string; projectKinds: Record<string, string> };
