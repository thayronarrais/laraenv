// Loads the persisted Command Center config, builds the built-in commands from
// live app state, and maps the user's custom shortcuts to runnable commands via
// a single executor. Returns the merged palette command list plus the custom
// runtime commands (which carry hotkeys for the global keybinding listener).

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
// @ts-ignore
import {
  GetCommandCenter,
  StartService, StopService, ReloadService,
  StartManagedAll, StopManagedAll, ReloadStack,
  ListProjects, ListSSHHosts, ListProjectCommands,
  OpenProjectFolder, OpenProjectInEditor, OpenURL,
  GetAgentSessionsEnabled, ListAgentProfiles,
} from "../../wailsjs/go/main/App";
import { buildOpenTerminalCommands, buildBuiltinCommands } from "./builtins";
// @ts-ignore
import { EventsOn } from "../../wailsjs/runtime";
import type { CommandContext, CommandIntent, CommandShortcut, RuntimeCommand } from "./types";

async function runService(c: CommandShortcut, ctx: CommandContext) {
  // Reuses the exact keys builtins.ts's built-in service commands already
  // flash for the same actions — one place for "Starting {{service}}…" etc.,
  // whether the command came from the built-in list or a user's custom one.
  const { t } = ctx;
  try {
    switch (c.serviceOp) {
      case "start": if (c.service) { await StartService(c.service); ctx.flash(t("commandCenter.flash.startingService", { service: c.service })); } break;
      case "stop": if (c.service) { await StopService(c.service); ctx.flash(t("commandCenter.flash.stoppingService", { service: c.service })); } break;
      case "reload": if (c.service) { await ReloadService(c.service); ctx.flash(t("commandCenter.flash.reloadingService", { service: c.service })); } break;
      case "startAll": await StartManagedAll(); ctx.flash(t("commandCenter.flash.startingAllServices")); break;
      case "stopAll": await StopManagedAll(); ctx.flash(t("commandCenter.flash.stoppingAllServices")); break;
      case "reloadStack": await ReloadStack(); ctx.flash(t("commandCenter.flash.reloadingStack")); break;
    }
  } catch (e: any) {
    ctx.flash(t("commandCenter.errors.generic", { message: e?.message ?? String(e) }));
  }
}

export function useCommandCenter(ctx: CommandContext) {
  const { t, openTerminals } = ctx;
  const [paletteHotkey, setPaletteHotkey] = useState("Ctrl+K");
  const [custom, setCustom] = useState<CommandShortcut[]>([]);
  const [builtins, setBuiltins] = useState<RuntimeCommand[]>([]);
  const [agentProviders, setAgentProviders] = useState<{ id: "claude" | "codex" | "kimi" | "opencode"; label: string }[]>([]);
  // Launcher data: project + SSH host names, and a lazily-filled cache of each
  // project's artisan/npm vocabulary keyed by project name.
  const [projects, setProjects] = useState<string[]>([]);
  const [sshHosts, setSshHosts] = useState<string[]>([]);
  const projectCmdCache = useRef<Map<string, { artisan: string[]; npm: string[] }>>(new Map());
  const [cacheVersion, setCacheVersion] = useState(0);

  const ensureProjectCommands = useCallback((name: string) => {
    if (!name || projectCmdCache.current.has(name)) return;
    const pending = { artisan: [] as string[], npm: [] as string[] };
    projectCmdCache.current.set(name, pending); // placeholder → no refetch
    ListProjectCommands(name)
      .then((r: any) => {
        if (projectCmdCache.current.get(name) !== pending) return;
        projectCmdCache.current.set(name, { artisan: r?.artisan ?? [], npm: r?.npmScripts ?? [] });
        setCacheVersion((v) => v + 1);
      })
      .catch(() => {});
  }, []);

  useEffect(() => EventsOn('project:runtime-changed', (name?: string) => {
    if (name) projectCmdCache.current.delete(name);
    else projectCmdCache.current.clear();
    setCacheVersion(version => version + 1);
  }), []);

  const getProjectCommands = useCallback(
    (name: string) => projectCmdCache.current.get(name) ?? null,
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [cacheVersion],
  );

  // Keep the latest context in a ref so commands run with current closures
  // without forcing every command's identity to change each render.
  const ctxRef = useRef(ctx);
  useEffect(() => { ctxRef.current = ctx; });

  const execute = useCallback((c: CommandShortcut) => {
    const x = ctxRef.current;
    switch (c.action) {
      case "navigate": if (c.page) x.navigate(c.page); break;
      case "openTerminal": x.openTerminal({ project: c.project, shell: c.shell }); break;
      case "terminal": if (c.command) x.runInTerminal({ command: c.command, project: c.project, shell: c.shell }); break;
      case "service": void runService(c, x); break;
    }
  }, []);

  // runIntent executes a serializable intent — the shared sink for both custom
  // commands and intents forwarded from the standalone command-bar window.
  const runIntent = useCallback((i: CommandIntent) => {
    const x = ctxRef.current;
    switch (i.action) {
      case "navigate": if (i.page) x.navigate(i.page); break;
      case "newProject": x.newProject(i.projectKind); break;
      case "openTerminal": x.openTerminal({ project: i.project, shell: i.shell }); break;
      case "terminal": if (i.command) x.runInTerminal({ command: i.command, project: i.project, shell: i.shell }); break;
      case "ssh": if (i.host) x.openSSH(i.host); break;
      case "service": void runService({ serviceOp: i.serviceOp, service: i.service } as CommandShortcut, x); break;
      case "openFolder": if (i.project) void OpenProjectFolder(i.project); break;
      case "openEditor": if (i.project) void OpenProjectInEditor(i.project, ""); break;
      case "openBrowser": if (i.url) void OpenURL(i.url); break;
      case "openAgents": x.openAgents?.(); break;
      case "launchAgent": if (i.project && i.agentId) x.launchAgent?.(i.project, i.agentId, i.agentLabel || i.agentId); break;
      case "focusTerminal": if (i.terminalId) x.focusTerminal?.(i.terminalId); break;
      case "focusAgentSession": if (i.sessionId) x.focusAgentSession?.(i.sessionId); break;
      case "resumeAgentSession": if (i.sessionId && i.project && i.agentId) x.resumeAgentSession?.(i.sessionId, i.project, i.agentId, i.agentLabel || i.agentId); break;
    }
  }, []);

  const reload = useCallback(async () => {
    try {
      const cc = await GetCommandCenter();
      setPaletteHotkey(cc.paletteHotkey || "Ctrl+K");
      setCustom((cc.commands ?? []) as unknown as CommandShortcut[]);
    } catch (e) { console.error("[commandCenter] GetCommandCenter failed", e); }
    let agentsEnabled = false;
    try { agentsEnabled = await GetAgentSessionsEnabled(); } catch (e) { console.error(e); }
    try { setBuiltins(await buildBuiltinCommands(ctxRef.current, agentsEnabled)); } catch (e) { console.error(e); }
    if (agentsEnabled) {
      try {
        const profiles = await ListAgentProfiles();
        setAgentProviders((profiles ?? [])
          .filter((profile: any) => profile.available && profile.capabilities?.launch)
          .map((profile: any) => ({ id: profile.id, label: profile.label })));
      } catch (e) { console.error(e); }
    } else {
      setAgentProviders([]);
    }
    try {
      const [projs, hosts] = await Promise.all([ListProjects(), ListSSHHosts()]);
      setProjects((projs ?? []).map((p: any) => p.name));
      setSshHosts((hosts ?? []).map((h: any) => h.name));
    } catch (e) { console.error(e); }
  }, []);

  useEffect(() => { void reload(); }, [reload]);

  useEffect(() => EventsOn("agent:feature-changed", () => { void reload(); }), [reload]);

  const terminalCommands = useMemo(() => buildOpenTerminalCommands({
    t,
    openTerminals,
    focusTerminal: (terminalId) => ctxRef.current.focusTerminal?.(terminalId),
  }), [openTerminals, t]);

  const customCommands: RuntimeCommand[] = useMemo(
    () => custom.map((c) => ({
      id: `custom:${c.id}`, label: c.label, group: t("commandCenter.groups.custom"), hotkey: c.hotkey,
      intent: { action: c.action, command: c.command, shell: c.shell, project: c.project, service: c.service, serviceOp: c.serviceOp, page: c.page },
      run: () => execute(c),
    })),
    [custom, execute, t],
  );

  const commands = useMemo(() => [...terminalCommands, ...customCommands, ...builtins], [terminalCommands, customCommands, builtins]);

  const launchAgent = useCallback((project: string, agentId: "claude" | "codex" | "kimi" | "opencode", label: string) => {
    ctxRef.current.launchAgent?.(project, agentId, label);
  }, []);

  return { commands, customCommands, paletteHotkey, reload, runIntent, projects, sshHosts, agentProviders, getProjectCommands, ensureProjectCommands, launchAgent };
}
