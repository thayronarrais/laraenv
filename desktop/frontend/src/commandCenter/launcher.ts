// Command palette launcher: turns a free-form query into project/SSH-aware
// autocompletion. Three recognized shapes:
//
//   <project> <provider>  →  start Claude Code, Codex, Kimi or OpenCode in the
//                             project's current checkout, e.g. "blog codex"
//   <project> <command…>   →  run the command in that project's terminal,
//                             e.g. "blog php artisan migrate"
//   ssh:<host>             →  open an SSH session, e.g. "ssh:contaboo"
//
// Suggestions are completed with Tab (replacing the token being typed) and the
// "execute" row runs on Enter. php artisan / npm completions come from the
// project itself (ListProjectCommands), fetched lazily and cached by the host.

import type { CommandIntent } from "./types";
// @ts-ignore — native (external) launchers; xterm.js paths go through LauncherData.
import { OpenProjectTerminalCmd, OpenExternalSSH } from "../../wailsjs/go/main/App";

export type LauncherRow =
  // Enter runs it (and closes the palette). `intent` lets the command-bar window
  // forward it to the main process instead of running locally. altRun is the
  // Shift+Enter alternate (run = native external terminal, altRun = in-app xterm).
  | { kind: "execute"; key: string; label: string; hint?: string; intent?: CommandIntent; run: () => void; altRun?: () => void }
  // Tab/Enter replaces the query with `complete` (keeps the palette open).
  | { kind: "suggest"; key: string; label: string; hint?: string; complete: string };

export type LauncherMode = "none" | "exclusive";

export type LauncherData = {
  projects: string[];
  sshHosts: string[];
  agentProviders: { id: "claude" | "codex" | "kimi" | "opencode"; label: string }[];
  getProjectCommands: (name: string) => { artisan: string[]; npm: string[] } | null;
  ensureProjectCommands: (name: string) => void;
  runInTerminal: (o: { command: string; project?: string }) => void;
  openSSH: (host: string) => void;
  launchAgent: (project: string, agentId: "claude" | "codex" | "kimi" | "opencode", label: string) => void;
  // This module is a plain function (no React, no useT()), so the catalogue
  // translate function is threaded in the same way ctx.t reaches
  // commandCenter/builtins.ts.
  t: (key: string, vars?: Record<string, string | number>) => string;
};

// npm subcommands worth surfacing alongside the project's own scripts.
const NPM_SUBCOMMANDS = ["run", "install", "ci", "update", "test", "start", "build"];
// First-token command runners offered once a project is chosen.
const BASE_RUNNERS = ["php artisan", "npm run", "npm", "composer", "yarn", "pnpm"];

const ci = (s: string) => s.toLowerCase();

export function computeLauncher(query: string, d: LauncherData): { mode: LauncherMode; rows: LauncherRow[] } {
  const q = query;
  const trimmed = q.trim();

  // --- ssh:<host> -------------------------------------------------------
  if (/^ssh:/i.test(trimmed) && !/\s/.test(trimmed)) {
    const partial = trimmed.slice(4);
    const rows: LauncherRow[] = [];
    const exact = d.sshHosts.find((h) => ci(h) === ci(partial));
    if (exact) rows.push({ kind: "execute", key: `ssh-run:${exact}`, label: d.t("commandCenter.launcher.connectSsh", { host: exact }), intent: { action: "ssh", host: exact }, run: () => { void OpenExternalSSH(exact); }, altRun: () => d.openSSH(exact) });
    const pool = partial ? d.sshHosts.filter((h) => ci(h).includes(ci(partial))) : d.sshHosts;
    for (const h of pool) {
      if (ci(h) === ci(partial)) continue;
      rows.push({ kind: "suggest", key: `ssh:${h}`, label: h, hint: d.t("commandCenter.launcher.sshHostHint"), complete: `ssh:${h}` });
    }
    return { mode: "exclusive", rows };
  }

  const firstSpace = q.indexOf(" ");

  // No space yet → not a launcher command. Project names are still completable
  // from the normal "Open terminal · X" rows via Tab (see CommandPalette).
  if (firstSpace === -1) return { mode: "none", rows: [] };

  // --- "<project> <command…>" ------------------------------------------
  const projToken = q.slice(0, firstSpace);
  const projName = d.projects.find((p) => ci(p) === ci(projToken));
  if (!projName) return { mode: "none", rows: [] };

  const rest = q.slice(firstSpace + 1);
  const cmdLine = rest.trim();
  const providerMatches = cmdLine && !/\s/.test(cmdLine)
    ? d.agentProviders.filter((provider) => ci(provider.id).startsWith(ci(cmdLine)) || ci(provider.label).startsWith(ci(cmdLine)))
    : [];
  if (providerMatches.length > 0) {
    const exact = providerMatches.find((provider) => ci(provider.id) === ci(cmdLine) || ci(provider.label) === ci(cmdLine));
    const rows: LauncherRow[] = [];
    if (exact) {
      rows.push({
        kind: "execute",
        key: `agent:${projName}:${exact.id}`,
        label: `${projName} + ${exact.label}`,
        hint: d.t("commandCenter.launcher.startAgentHint"),
        intent: { action: "launchAgent", project: projName, agentId: exact.id, agentLabel: exact.label },
        run: () => d.launchAgent(projName, exact.id, exact.label),
      });
    }
    for (const provider of providerMatches) {
      if (provider.id === exact?.id) continue;
      rows.push({ kind: "suggest", key: `agent-suggest:${provider.id}`, label: provider.label, hint: d.t("commandCenter.launcher.agentForProject", { project: projName }), complete: `${projName} ${provider.id}` });
    }
    return { mode: "exclusive", rows };
  }

  d.ensureProjectCommands(projName);
  const cmds = d.getProjectCommands(projName);
  const loading = cmds === null;
  const artisan = cmds?.artisan ?? [];
  const npm = cmds?.npm ?? [];

  const rows: LauncherRow[] = [];

  if (cmdLine) {
    rows.push({ kind: "execute", key: "run", label: d.t("commandCenter.launcher.runCommand", { command: cmdLine }), hint: d.t("commandCenter.launcher.inProject", { project: projName }), intent: { action: "terminal", command: cmdLine, project: projName }, run: () => { void OpenProjectTerminalCmd(projName, "", cmdLine); }, altRun: () => d.runInTerminal({ command: cmdLine, project: projName }) });
  }

  const tokens = rest.split(/\s+/);
  const last = tokens[tokens.length - 1];
  const prefixTokens = tokens.slice(0, -1);
  const prefixStr = prefixTokens.join(" ");
  const completeWith = (token: string) => `${projName} ${[...prefixTokens, token].join(" ")}`;
  const completeWithSpace = (token: string) => `${completeWith(token)} `;

  if (prefixTokens.length === 0) {
    if (!last) {
      for (const provider of d.agentProviders) {
        rows.push({ kind: "suggest", key: `agent:${provider.id}`, label: provider.label, hint: d.t("commandCenter.launcher.agentForProject", { project: projName }), complete: `${projName} ${provider.id}` });
      }
    }
    for (const b of BASE_RUNNERS) {
      if (!last || ci(b).startsWith(ci(last))) rows.push({ kind: "suggest", key: `base:${b}`, label: b, complete: `${projName} ${b} ` });
    }
  } else if (prefixStr === "php artisan") {
    if (loading) rows.push({ kind: "suggest", key: "loading", label: d.t("commandCenter.launcher.loadingArtisan"), complete: q });
    for (const c of artisan) {
      if (!last || ci(c).includes(ci(last))) rows.push({ kind: "suggest", key: `art:${c}`, label: c, hint: d.t("commandCenter.launcher.artisanHint"), complete: completeWith(c) });
    }
  } else if (prefixStr === "npm" || prefixStr === "yarn" || prefixStr === "pnpm") {
    for (const c of NPM_SUBCOMMANDS) {
      if (!last || ci(c).startsWith(ci(last))) rows.push({ kind: "suggest", key: `npmsub:${c}`, label: c, hint: prefixStr, complete: completeWithSpace(c) });
    }
  } else if (prefixStr === "npm run" || prefixStr === "yarn run" || prefixStr === "pnpm run" || prefixStr === "yarn") {
    if (loading) rows.push({ kind: "suggest", key: "loading", label: d.t("commandCenter.launcher.loadingNpmScripts"), complete: q });
    for (const s of npm) {
      if (!last || ci(s).includes(ci(last))) rows.push({ kind: "suggest", key: `npm:${s}`, label: s, hint: d.t("commandCenter.launcher.npmScriptHint"), complete: completeWith(s) });
    }
  }

  return { mode: "exclusive", rows };
}
