// Command Center data model.
//
// A CommandShortcut is the persisted, user-defined command (mirrors the Go
// config.CommandShortcut). A RuntimeCommand is the resolved, executable entry
// the palette renders and runs — built from both the user's custom commands and
// the auto-generated built-ins (navigation, services, projects).

import type { OpenTerminal } from "../components/terminal/types";

export type CommandAction = "terminal" | "service" | "navigate" | "openTerminal";
export type ServiceOp = "start" | "stop" | "reload" | "startAll" | "stopAll" | "reloadStack";

export interface CommandShortcut {
  id: string;
  label: string;
  action: CommandAction;
  command?: string;   // terminal: command line to run
  shell?: string;     // terminal kind (e.g. "powershell"); empty = default
  project?: string;   // optional project scope for terminal/openTerminal
  service?: string;   // service action target
  serviceOp?: ServiceOp;
  page?: string;      // navigate target
  hotkey?: string;    // optional, e.g. "Ctrl+Shift+1"
}

export interface CommandCenterConfig {
  paletteHotkey: string;
  globalHotkey: string;
  commands: CommandShortcut[];
}

// A serializable description of what a command does. The in-app palette runs
// commands via their run() closure; the standalone command-bar window (a
// separate process) instead forwards the intent to the main process, which
// runs it through the same executor. Covers built-in actions too (open
// folder/editor/browser, new project, ssh) — a superset of CommandAction.
export type IntentAction =
  | CommandAction
  | "openFolder"
  | "openEditor"
  | "openBrowser"
  | "newProject"
  | "ssh"
  | "openAgents"
  | "launchAgent"
  | "focusTerminal"
  | "focusAgentSession"
  | "resumeAgentSession";

export interface CommandIntent {
  action: IntentAction;
  command?: string;
  shell?: string;
  project?: string;
  service?: string;
  serviceOp?: ServiceOp;
  page?: string;
  url?: string;
  host?: string;
  sessionId?: string;
  terminalId?: string;
  agentId?: "claude" | "codex" | "kimi" | "opencode";
  agentLabel?: string;
  // For newProject: preselects the New Project form type (e.g. "laravel",
  // "git", "wordpress", "vite"). Empty/undefined opens the form as-is.
  projectKind?: string;
  // For terminal/openTerminal/ssh: where to open. "native" = external popup
  // terminal (default), "xterm" = the in-app xterm.js tab. Set by the palette
  // from Enter (native) vs Shift+Enter (xterm). Ignored for other actions.
  target?: "native" | "xterm";
}

// A resolved, runnable command shown in the palette.
export type RuntimeCommand = {
  id: string;
  label: string;
  group: string;
  hint?: string;
  hotkey?: string;
  // When set, pressing Tab on this row fills the palette input with this text
  // (entering launcher mode) instead of running. Used by "Open terminal · X"
  // rows so the project name can be completed into a "<project> <command>".
  complete?: string;
  // Serializable description, used by the command-bar window to forward the
  // action to the main process. Optional; run() is the in-app path.
  intent?: CommandIntent;
  run: () => void | Promise<void>;
  // Alternate action (Shift+Enter). For terminal/ssh rows, run() opens the
  // native external terminal and altRun() opens the in-app xterm.js tab.
  altRun?: () => void | Promise<void>;
};

// The side-effecting capabilities the executor/built-ins need from the host
// (MainApp owns navigation, terminal requests, and toasts).
export type CommandContext = {
  // The catalogue translate function, so builtins.ts (a module, not a
  // component) can resolve the palette labels it builds — never used to
  // translate ids or actions, only display text.
  t: (key: string, vars?: Record<string, string | number>) => string;
  navigate: (page: string) => void;
  openTerminal: (opts: { project?: string; shell?: string }) => void;
  runInTerminal: (opts: { command: string; project?: string; shell?: string }) => void;
  openSSH: (host: string) => void;
  newProject: (kind?: string) => void;
  flash: (msg: string) => void;
  // Current panes come from the mounted workspace, including inactive tabs.
  openTerminals?: OpenTerminal[];
  focusTerminal?: (terminalId: string) => void;
  openAgents?: () => void;
  launchAgent?: (project: string, agentId: "claude" | "codex" | "kimi" | "opencode", label: string) => void;
  focusAgentSession?: (sessionId: string) => void;
  resumeAgentSession?: (sessionId: string, project: string, agentId: "claude" | "codex" | "kimi" | "opencode", label: string) => void;
};
