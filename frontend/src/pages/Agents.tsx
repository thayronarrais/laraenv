import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useDialogs } from "../components/dialogs/DialogProvider";
import {
  Archive, ArrowLeftRight, Bot, Boxes, ChevronDown, ChevronRight, GitBranch, MoreHorizontal, Play, Power, RefreshCw,
  RotateCcw, ScrollText, Search, Server, Square, Tag, Trash2, Unlink, Network,
} from "lucide-react";
import AgentLauncherMenu, { type AgentID, type AgentLaunchDraft, type AgentProfile } from "../components/AgentLauncherMenu";
import AgentStatusGlyph, { agentStatusLabel } from "../components/agents/AgentStatusGlyph";
import AgentProviderIcon from "../components/agents/AgentProviderIcon";
import type { AgentSessionView, AgentWorktreeRuntimeView } from "../components/agents/AgentSessionCard";
import { filterAgentSessions, groupAgentSessions, sessionAgeLabel, sessionProjectNames, type AgentSessionScope } from "../components/agents/sessionTree";
import DevServerLogsModal from "../components/DevServerLogsModal";
import PortalMenu from "../components/PortalMenu";
import { useI18n } from "../lib/i18n";
// @ts-ignore
import {
  ArchiveAgentSession, DetachAgentWorktreeRoutes, GetAgentWorktreeRuntime, ListAgentProfiles, ListAgentSessions, ListProjects,
  MarkAgentSessionRead, RemoveAgentWorktree, RenameAgentSession, StartAgentWorktreeDevServer,
  StopAgentSession, StopAgentWorktreeDevServer,
} from "../../wailsjs/go/main/App";
// @ts-ignore
import { EventsOn } from "../../wailsjs/runtime";

type LaunchProject = { name: string; url: string; path: string; proxyPort: number };
const liveStates = new Set(["starting", "working", "needs-input", "done", "idle"]);
// Kept as literal per-key strings (not a template composed at the call site)
// so the catalogue's orphan-key guard, which greps for the literal dotted
// text, can see each key is actually used.
const scopeLabelKeys: Record<AgentSessionScope, string> = {
  active: "agents.page.header.scopeLabels.active",
  recent: "agents.page.header.scopeLabels.recent",
  archived: "agents.page.header.scopeLabels.archived",
};

function SessionActions({
  session, active, canResume, live, runtime, onOpenMap, onOpen, onResume, onFresh, onRename, onStop, onArchive, onMarkRead,
  onStartWorktreeServer, onStopWorktreeServer, onWorktreeLogs, onDetachRoutes, onRemoveWorktree,
}: {
  session: AgentSessionView;
  active: boolean;
  canResume: boolean;
  live: boolean;
  runtime?: AgentWorktreeRuntimeView;
  onOpenMap: () => void;
  onOpen: () => void;
  onResume: () => void;
  onFresh: () => void;
  onRename: () => void;
  onStop: () => void;
  onArchive: () => void;
  onMarkRead: () => void;
  onStartWorktreeServer: () => void;
  onStopWorktreeServer: () => void;
  onWorktreeLogs: () => void;
  onDetachRoutes: () => void;
  onRemoveWorktree: () => void;
}) {
  const { t } = useI18n();
  const [open, setOpen] = useState(false);
  useEffect(() => { if (!active) setOpen(false); }, [active]);
  const anchorRef = useRef<HTMLButtonElement>(null);
  const action = (callback: () => void) => () => { setOpen(false); callback(); };
  const item = "flex min-h-9 w-full items-center gap-2.5 rounded-md px-3 py-2 text-left text-[12px] text-white/75 hover:bg-white/[0.10] hover:text-white";

  return (
    <>
      <button
        ref={anchorRef}
        type="button"
        onClick={(event) => { event.stopPropagation(); setOpen((current) => !current); }}
        className="flex h-6 w-6 shrink-0 items-center justify-center rounded-md text-white/45 opacity-0 transition group-hover/session:opacity-100 hover:bg-white/10 hover:text-white focus:opacity-100 focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-accent2/60"
        aria-label={t("agents.page.actionsMenu.triggerAria", { title: session.title })}
      >
        <MoreHorizontal className="h-3.5 w-3.5" />
      </button>
      <PortalMenu open={active && open} anchorRef={anchorRef} onClose={() => setOpen(false)} align="left" minWidth={210}>
        <div className="px-2.5 pb-1 pt-1.5">
          <div className="truncate text-[12px] font-medium text-white/90">{session.title}</div>
          <div className="mt-1 font-mono text-[10px] uppercase text-white/55">{session.agentId} · {agentStatusLabel(t, session.state)}</div>
        </div>
        <div className="my-1 border-t border-white/[0.07]" />
        {!!session.additionalDirectories?.length && (
          <div className="mb-1 border-b border-white/[0.07] px-3 pb-2 pt-1">
            <div className="mb-1 text-[10px] font-medium text-white/65">{t("agents.page.actionsMenu.foldersHeading")}</div>
            {[session.worktreePath || session.projectPath, ...session.additionalDirectories].map((directory) => (
              <div key={directory} title={directory} className="max-w-[320px] truncate py-0.5 font-mono text-[10px] text-white/50">{directory}</div>
            ))}
          </div>
        )}
        <button type="button" onClick={event => {event.stopPropagation(); action(onOpenMap)();}} className={item}><Network className="h-3 w-3" /> {t("agents.page.actionsMenu.viewMap")}</button>
        {live && <button type="button" onClick={action(onOpen)} className={item}><Play className="h-3 w-3" /> {t("agents.page.actionsMenu.openSession")}</button>}
        {!live && canResume && <button type="button" onClick={action(onResume)} className={item}><RotateCcw className="h-3 w-3" /> {t("agents.page.actionsMenu.resumeSession")}</button>}
        <button type="button" onClick={action(onFresh)} className={item}><Bot className="h-3 w-3" /> {t("agents.page.actionsMenu.newSessionHere")}</button>
        <button type="button" onClick={action(onRename)} className={item}><Tag className="h-3 w-3" /> {t("agents.page.actionsMenu.rename")}</button>
        {session.unread && <button type="button" onClick={action(onMarkRead)} className={item}><RefreshCw className="h-3 w-3" /> {t("agents.page.actionsMenu.markAsRead")}</button>}
        {live && <button type="button" onClick={action(onStop)} className={`${item} text-red-200/70 hover:text-red-100`}><Square className="h-3 w-3" /> {t("agents.page.actionsMenu.stop")}</button>}

        {session.launchMode === "worktree" && runtime && (
          <>
            <div className="my-1 border-t border-white/[0.07]" />
            <div className="px-3 py-1.5 text-[10px] uppercase tracking-widest text-white/50">{t("agents.page.actionsMenu.worktreeHeader")}</div>
            {runtime.needsDevServer && (runtime.running
              ? <button type="button" onClick={action(onStopWorktreeServer)} className={item}><Square className="h-3 w-3" /> {t("agents.page.actionsMenu.stopServer", { port: runtime.port })}</button>
              : <button type="button" onClick={action(onStartWorktreeServer)} className={item}><Server className="h-3 w-3" /> {t("agents.page.actionsMenu.startServer", { port: runtime.configuredPort })}</button>)}
            {runtime.running && <button type="button" onClick={action(onWorktreeLogs)} className={item}><ScrollText className="h-3 w-3" /> {t("agents.page.actionsMenu.serverLogs")}</button>}
            {runtime.routedHostnames.length > 0 && <button type="button" onClick={action(onDetachRoutes)} className={item}><Unlink className="h-3 w-3" /> {t("agents.page.actionsMenu.disconnectUrls")}</button>}
            <button type="button" disabled={!runtime.canRemove} title={runtime.blockers.join(" · ")} onClick={action(onRemoveWorktree)} className={`${item} text-red-200/70 disabled:cursor-not-allowed disabled:opacity-25`}><Trash2 className="h-3 w-3" /> {t("agents.page.actionsMenu.removeWorktree")}</button>
          </>
        )}

        {!live && !session.archived && (
          <>
            <div className="my-1 border-t border-white/[0.07]" />
            <button type="button" onClick={action(onArchive)} className={item}><Archive className="h-3 w-3" /> {t("agents.page.actionsMenu.archive")}</button>
          </>
        )}
      </PortalMenu>
    </>
  );
}

export default function Agents({
  flash, enabled, enabling = false, onEnable, selectedSessionId, onOpenMap, onSelect, onOpen, onResume, onFresh, onLaunch,
  autoSwitch = false, onAutoSwitchChange, autoSwitchWaiting = 0, active = true,
}: {
  flash: (message: string) => void;
  enabled: boolean;
  enabling?: boolean;
  onEnable: () => void;
  selectedSessionId?: string;
  onOpenMap: (session: AgentSessionView) => void;
  onSelect: (session: AgentSessionView) => void;
  onOpen: (session: AgentSessionView) => void;
  onResume: (session: AgentSessionView, providerLabel: string) => void;
  onFresh: (project: string, agentId: AgentID, label: string, additionalDirectories?: string[]) => void;
  onLaunch: (project: string, agentId: AgentID, label: string, options: AgentLaunchDraft) => void;
  autoSwitch?: boolean;
  onAutoSwitchChange?: (enabled: boolean) => void;
  autoSwitchWaiting?: number;
  active?: boolean;
}) {
  const { t, formatNumber } = useI18n();
  const [sessions, setSessions] = useState<AgentSessionView[]>([]);
  const dialogs = useDialogs();
  const [profiles, setProfiles] = useState<AgentProfile[]>([]);
  const [projects, setProjects] = useState<LaunchProject[]>([]);
  const [query, setQuery] = useState("");
  const [scope, setScope] = useState<AgentSessionScope>("active");
  const [projectFilter, setProjectFilter] = useState("");
  const [collapsed, setCollapsed] = useState<Set<string>>(new Set());
  const [launcherProject, setLauncherProject] = useState("");
  const [newSessionOpen, setNewSessionOpen] = useState(false);
  const [newSessionProject, setNewSessionProject] = useState("");
  const [worktreeRuntimes, setWorktreeRuntimes] = useState<Record<string, AgentWorktreeRuntimeView>>({});
  const [logsSession, setLogsSession] = useState<AgentSessionView | null>(null);
  useEffect(() => { if (!active) { setNewSessionOpen(false); setLauncherProject(''); setLogsSession(null); } }, [active]);
  const updateTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const pending = useRef<Map<string, AgentSessionView>>(new Map());
  const sessionsRef = useRef<AgentSessionView[]>([]);
  const flashRef = useRef(flash);
  useEffect(() => { flashRef.current = flash; }, [flash]);
  useEffect(() => { sessionsRef.current = sessions; }, [sessions]);

  const loadWorktreeRuntimes = useCallback(async (source: AgentSessionView[]) => {
    const pairs = await Promise.all(source.filter((session) => session.launchMode === "worktree" && !!session.worktreePath).map(async (session) => {
      try { return [session.id, await GetAgentWorktreeRuntime(session.id)] as const; } catch { return null; }
    }));
    const next: Record<string, AgentWorktreeRuntimeView> = {};
    for (const pair of pairs) if (pair) next[pair[0]] = pair[1] as AgentWorktreeRuntimeView;
    setWorktreeRuntimes(next);
  }, []);

  const refresh = useCallback(async () => {
    try {
      const [nextSessions, nextProfiles, nextProjects] = await Promise.all([ListAgentSessions("", true), ListAgentProfiles(), ListProjects()]);
      const typedSessions = (nextSessions ?? []) as unknown as AgentSessionView[];
      const typedProjects = (nextProjects ?? []) as LaunchProject[];
      setSessions(typedSessions);
      setProfiles((nextProfiles ?? []).map((profile) => ({ ...profile, id: profile.id as AgentID })) as AgentProfile[]);
      setProjects(typedProjects);
      setNewSessionProject((current) => typedProjects.some((project) => project.name === current)
        ? current
        : typedProjects.find((project) => typedSessions.some((session) => session.projectName === project.name))?.name || typedProjects[0]?.name || "");
      await loadWorktreeRuntimes(typedSessions);
    } catch (error) {
      flashRef.current(t("agents.page.errors.loadSessions", { message: String(error) }));
    }
  }, [loadWorktreeRuntimes, t]);

  useEffect(() => { void refresh(); }, [refresh]);
  useEffect(() => {
    const off = EventsOn("agent:session-changed", (session: AgentSessionView) => {
      pending.current.set(session.id, session);
      if (updateTimer.current) return;
      updateTimer.current = setTimeout(() => {
        const updates = [...pending.current.values()];
        pending.current.clear();
        updateTimer.current = null;
        setSessions((current) => {
          const changed = new Set(updates.map((item) => item.id));
          const next = [...updates, ...current.filter((item) => !changed.has(item.id))];
          sessionsRef.current = next;
          return next;
        });
        if (updates.some((item) => item.launchMode === "worktree")) setTimeout(() => void loadWorktreeRuntimes(sessionsRef.current), 0);
      }, 200);
    });
    return () => { off(); if (updateTimer.current) clearTimeout(updateTimer.current); };
  }, [loadWorktreeRuntimes]);
  useEffect(() => {
    const off = EventsOn("devserver:status", (instance: string) => {
      if (String(instance).startsWith("agent-worktree:")) void loadWorktreeRuntimes(sessionsRef.current);
    });
    return () => off();
  }, [loadWorktreeRuntimes]);

  const profileByID = useMemo(() => new Map(profiles.map((profile) => [profile.id, profile])), [profiles]);
  const scopedSessions = useMemo(() => filterAgentSessions(sessions, scope), [sessions, scope]);
  const visibleProjectNames = useMemo(() => sessionProjectNames(scopedSessions), [scopedSessions]);
  const filtered = useMemo(() => filterAgentSessions(sessions, scope, query, projectFilter), [sessions, scope, query, projectFilter]);
  const tree = useMemo(() => groupAgentSessions(filtered), [filtered]);
  const scopeCounts = useMemo(() => ({
    active: filterAgentSessions(sessions, "active").length,
    recent: filterAgentSessions(sessions, "recent").length,
    archived: filterAgentSessions(sessions, "archived").length,
  }), [sessions]);
  const launchProject = projects.find((project) => project.name === newSessionProject) || projects[0];

  useEffect(() => {
    if (projectFilter && !visibleProjectNames.includes(projectFilter)) setProjectFilter("");
  }, [projectFilter, visibleProjectNames]);

  const mutate = async (operation: () => Promise<unknown>, success: string) => {
    try { await operation(); flash(success); } catch (error) { flash(t("agents.page.errors.actionFailed", { message: String(error) })); }
  };
  const mutateRuntime = async (sessionID: string, operation: () => Promise<unknown>, success: string) => {
    try {
      const runtime = await operation() as AgentWorktreeRuntimeView;
      if (runtime?.sessionId) setWorktreeRuntimes((current) => ({ ...current, [sessionID]: runtime }));
      flash(success);
    } catch (error) { flash(t("agents.page.errors.worktreeActionFailed", { message: String(error) })); }
  };
  const toggleProject = (project: string) => setCollapsed((current) => {
    const next = new Set(current);
    if (next.has(project)) next.delete(project); else next.add(project);
    return next;
  });

  if (!enabled) {
    return (
      <aside className="flex h-full min-h-0 w-full flex-col bg-[#111315]/95">
        <header className="border-b border-white/[0.12] px-4 py-4">
          <div className="flex items-center gap-2">
            <Bot className="h-4.5 w-4.5 text-accent2" />
            <span className="text-[14px] font-semibold text-white/95">{t("agents.page.header.title")}</span>
            <span className="ml-auto rounded border border-amber-300/25 bg-amber-300/[0.08] px-2 py-0.5 font-mono text-[10px] uppercase text-amber-100/75">{t("agents.page.header.offBadge")}</span>
          </div>
        </header>
        <div className="min-h-0 flex-1 overflow-y-auto px-4 py-4">
          <div className="rounded-xl border border-accent2/25 bg-accent2/[0.06] p-4">
            <Power className="h-6 w-6 text-accent2/85" />
            <div className="mt-3 text-[13px] font-semibold text-white/90">{t("agents.page.disabled.heading")}</div>
            <p className="mt-2 text-[11px] leading-[1.55] text-white/60">{t("agents.page.disabled.description")}</p>
            <button type="button" disabled={enabling} onClick={onEnable} className="mt-4 flex h-10 w-full items-center justify-center gap-2 rounded-lg bg-accent/55 text-[12px] font-semibold text-white hover:bg-accent/70 disabled:opacity-45">
              <Power className="h-3 w-3" /> {enabling ? t("agents.page.disabled.enablingButton") : t("agents.page.disabled.enableButton")}
            </button>
          </div>
        </div>
        <div className="border-t border-white/[0.10] px-4 py-3 text-[10px] leading-4 text-white/50">{t("agents.page.disabled.footer")}</div>
      </aside>
    );
  }

  return (
    <aside className="flex h-full min-h-0 w-full flex-col bg-[#111315]/95">
      <header className="border-b border-white/[0.12] px-4 pb-4 pt-4">
        <div className="flex items-center justify-between gap-2">
          <div className="flex min-w-0 items-center gap-2">
            <Bot className="h-4.5 w-4.5 shrink-0 text-accent2" />
            <div className="truncate text-[14px] font-semibold text-white/95">{t("agents.page.header.title")}</div>
            <span className="rounded-full bg-white/[0.09] px-2 py-0.5 font-mono text-[10px] text-white/65">{formatNumber(scopeCounts[scope])}</span>
          </div>
          <div className="flex items-center gap-1">
            {launchProject && <AgentLauncherMenu
              projectName={launchProject.name}
              projectPath={launchProject.path}
              canonicalHost={launchProject.url.replace(/^https?:\/\//i, "").replace(/\/.*$/, "")}
              mainProxyPort={launchProject.proxyPort}
              projectOptions={projects}
              onProjectChange={setNewSessionProject}
              profiles={profiles}
              open={active && newSessionOpen}
              setOpen={setNewSessionOpen}
              onLaunch={(agent, options) => onLaunch(launchProject.name, agent.id, agent.label, options)}
              notify={flash}
              buttonLabel={t("agents.page.header.newButtonLabel")}
            />}
            {onAutoSwitchChange && (
            <button
              type="button"
              onClick={() => onAutoSwitchChange(!autoSwitch)}
              aria-pressed={autoSwitch}
              title={autoSwitch && autoSwitchWaiting > 0 ? t("agents.page.autoSwitch.tooltip", { count: autoSwitchWaiting }) : t("agents.page.autoSwitch.tooltipBase")}
              aria-label={autoSwitch && autoSwitchWaiting > 0 ? t("agents.page.autoSwitch.tooltip", { count: autoSwitchWaiting }) : t("agents.page.autoSwitch.tooltipBase")}
              className={`relative flex h-8 w-8 items-center justify-center rounded-md ${autoSwitch ? "bg-accent2/15 text-accent2" : "text-white/55 hover:bg-white/[0.10] hover:text-white"}`}
            >
              <ArrowLeftRight className="h-4 w-4" />
              {autoSwitch && autoSwitchWaiting > 0 && (
                <span className="absolute -right-1 -top-1 min-w-[16px] rounded-full bg-amber-400 px-1 text-center font-mono text-[9px] leading-4 text-black">{formatNumber(autoSwitchWaiting)}</span>
              )}
            </button>
            )}
            <button type="button" onClick={() => void refresh()} className="flex h-8 w-8 items-center justify-center rounded-md text-white/55 hover:bg-white/[0.10] hover:text-white" title={t("agents.page.header.refreshTooltip")}><RefreshCw className="h-4 w-4" /></button>
          </div>
        </div>
        <div className="mt-4 grid grid-cols-3 rounded-lg border border-white/[0.12] bg-black/25 p-1" role="tablist" aria-label={t("agents.page.header.sessionViewAria")}>
          {(["active", "recent", "archived"] as AgentSessionScope[]).map((item) => (
            <button
              key={item}
              type="button"
              role="tab"
              aria-selected={scope === item}
              onClick={() => setScope(item)}
              className={`flex h-8 items-center justify-center gap-1.5 rounded-md text-[11px] font-medium capitalize transition focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-accent2/60 ${scope === item ? "bg-white/[0.12] text-white" : "text-white/55 hover:text-white/80"}`}
            >
              {t(scopeLabelKeys[item])}<span className="font-mono text-[9px] opacity-60">{formatNumber(scopeCounts[item])}</span>
            </button>
          ))}
        </div>
        <div className="mt-2 flex items-center gap-2">
          <label className="flex h-8 min-w-0 flex-1 items-center gap-2 rounded-lg border border-white/[0.12] bg-black/25 px-2.5 focus-within:border-accent2/45">
            <Search className="h-3.5 w-3.5 shrink-0 text-white/45" />
            <input value={query} onChange={(event) => setQuery(event.target.value)} className="min-w-0 flex-1 bg-transparent text-[12px] text-white/85 outline-none placeholder:text-white/45" placeholder={t("agents.page.header.searchPlaceholder")} />
          </label>
          {visibleProjectNames.length > 1 && (
            <label className="block w-[38%] max-w-[150px] shrink-0">
              <span className="sr-only">{t("agents.page.header.projectFilterSrLabel")}</span>
              <select
                aria-label={t("agents.page.header.projectFilterSrLabel")}
                title={t("agents.page.header.projectFilterSrLabel")}
                value={projectFilter}
                onChange={(event) => setProjectFilter(event.target.value)}
                className="input h-8 w-full truncate px-2 font-mono text-[10px]"
              >
                <option value="">{t("agents.page.header.allProjectsOption")}</option>
                {visibleProjectNames.map((name) => <option key={name} value={name}>{name}</option>)}
              </select>
            </label>
          )}
        </div>
      </header>

      <div className="min-h-0 flex-1 overflow-y-auto px-3 py-3">
        {tree.length === 0 ? (
          <div className="px-4 py-10 text-center">
            <div className="text-[12px] font-semibold text-white/75">
              {query || projectFilter ? t("agents.page.empty.filtered") : scope === "active" ? t("agents.page.empty.active") : scope === "recent" ? t("agents.page.empty.recent") : t("agents.page.empty.archived")}
            </div>
            {!query && !projectFilter && scope === "active" && <p className="mt-2 text-[11px] leading-[1.5] text-white/50">{t("agents.page.empty.activeHint")}</p>}
            {launchProject && scope !== "archived" && (
              <button type="button" onClick={() => setNewSessionOpen(true)} className="mt-4 rounded-lg border border-accent2/35 bg-accent2/[0.10] px-3.5 py-2 text-[11px] font-medium text-accent2 hover:bg-accent2/[0.16]">{t("agents.page.empty.newSessionButton")}</button>
            )}
            {!query && !projectFilter && scope === "active" && scopeCounts.recent > 0 && (
              <button type="button" onClick={() => setScope("recent")} className="ml-2 mt-4 rounded-lg px-3.5 py-2 text-[11px] text-white/60 hover:bg-white/[0.08] hover:text-white/85">{t("agents.page.empty.viewRecentButton")}</button>
            )}
          </div>
        ) : tree.map((projectGroup) => {
          const project = projects.find((candidate) => candidate.name === projectGroup.name);
          const isCollapsed = collapsed.has(projectGroup.name);
          return (
            <section key={projectGroup.name} className="mb-1">
              <div className="group/project flex h-7 items-center gap-1 rounded-lg px-1 hover:bg-white/[0.05]">
                <button type="button" onClick={() => toggleProject(projectGroup.name)} className="flex min-w-0 flex-1 items-center gap-1.5 rounded px-1 text-left focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-accent2/60">
                  {isCollapsed ? <ChevronRight className="h-3 w-3 shrink-0 text-white/50" /> : <ChevronDown className="h-3 w-3 shrink-0 text-white/50" />}
                  <span className="truncate text-[12px] font-semibold text-white/90">{projectGroup.name}</span>
                  <span className="font-mono text-[10px] text-white/50">{formatNumber(projectGroup.sessionCount)}</span>
                </button>
                {project && <AgentLauncherMenu
                  projectName={project.name}
                  projectPath={project.path}
                  canonicalHost={project.url.replace(/^https?:\/\//i, "").replace(/\/.*$/, "")}
                  mainProxyPort={project.proxyPort}
                  profiles={profiles}
                  open={active && launcherProject === project.name}
                  setOpen={(open) => setLauncherProject(open ? project.name : "")}
                  onLaunch={(agent, options) => onLaunch(project.name, agent.id, agent.label, options)}
                  notify={flash}
                  buttonLabel={t("agents.page.projectGroup.newSessionButtonLabel")}
                  compact
                />}
              </div>

              {!isCollapsed && (
                <div className="ml-1.5 border-l border-white/[0.12] pl-2">
                  {projectGroup.locations.map((location) => {
                    const locationText = location.labelKey ? t(location.labelKey) : location.label;
                    return (
                    <div key={location.key} className="pb-0.5">
                      <div className="flex h-[22px] items-center gap-2 px-2 font-mono text-[10px] text-white/55" title={locationText}>
                        {location.kind === "worktree" ? <Boxes className="h-3 w-3 shrink-0" /> : <GitBranch className="h-3 w-3 shrink-0" />}
                        <span className="truncate">{locationText}</span>
                        {location.kind === "worktree" && <span className="ml-auto rounded border border-white/[0.12] px-1 py-0 text-[8px] uppercase leading-4">{t("agents.page.locationWorktreeBadge")}</span>}
                      </div>
                      {location.sessions.map((session) => {
                        const profile = profileByID.get(session.agentId);
                        const providerLabel = profile?.label || session.agentId;
                        const canResume = !!profile?.capabilities.resume && !!session.providerSessionId && !session.archived;
                        const live = liveStates.has(session.state) && !!session.terminalId;
                        const selected = session.id === selectedSessionId;
                        const runtime = worktreeRuntimes[session.id];
                        const sessionTitleAttr = `${session.title} · ${providerLabel} · ${agentStatusLabel(t, session.state)}`
                          + (session.additionalDirectories?.length
                            ? `\n${[session.worktreePath || session.projectPath, ...session.additionalDirectories].join("\n")}`
                            : "");
                        return (
                          <div key={session.id} className={`group/session mb-0.5 flex h-[30px] items-center rounded-lg border transition ${selected ? "border-accent2/35 bg-accent2/[0.11]" : "border-transparent hover:bg-white/[0.06]"}`}>
                            <button
                              type="button"
                              onClick={() => {
                                onSelect(session);
                                if (session.unread) void MarkAgentSessionRead(session.id).catch(() => {});
                                if (live) onOpen(session); else if (canResume) onResume(session, providerLabel);
                              }}
                              className="flex min-w-0 flex-1 items-center gap-2 px-2 py-0 text-left focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-inset focus-visible:ring-accent2/60"
                              title={sessionTitleAttr}
                            >
                              <AgentStatusGlyph state={session.state} unread={session.unread} />
                              <AgentProviderIcon provider={session.agentId} compact />
                              <span className="min-w-0 flex-1 truncate text-[12px] font-medium text-white/90">{session.title}</span>
                              <span className="shrink-0 font-mono text-[10px] text-white/50">{`${agentStatusLabel(t, session.state)} · ${sessionAgeLabel(t, session.updatedAt)}`}</span>
                            </button>
                            <SessionActions
                              active={active}
                              session={session}
                              canResume={canResume}
                              live={live}
                              runtime={runtime}
                              onOpenMap={() => onOpenMap(session)}
                              onOpen={() => onOpen(session)}
                              onResume={() => onResume(session, providerLabel)}
                              onFresh={() => onFresh(session.projectName, session.agentId, providerLabel, session.additionalDirectories)}
                              onRename={async () => {
                                const title = await dialogs.prompt({ title: t("agents.page.renameDialog.title"), label: t("agents.page.renameDialog.label"), defaultValue: session.title });
                                if (title && title !== session.title) void mutate(() => RenameAgentSession(session.id, title), t("agents.page.flash.sessionRenamed"));
                              }}
                              onStop={() => void mutate(() => StopAgentSession(session.id), t("agents.page.flash.agentStopped"))}
                              onArchive={() => void mutate(() => ArchiveAgentSession(session.id), t("agents.page.flash.sessionArchived"))}
                              onMarkRead={() => void mutate(() => MarkAgentSessionRead(session.id), t("agents.page.flash.markedAsRead"))}
                              onStartWorktreeServer={() => void mutateRuntime(session.id, () => StartAgentWorktreeDevServer(session.id), t("agents.page.flash.worktreeServerStarted"))}
                              onStopWorktreeServer={() => void mutateRuntime(session.id, () => StopAgentWorktreeDevServer(session.id), t("agents.page.flash.worktreeServerStopped"))}
                              onWorktreeLogs={() => setLogsSession(session)}
                              onDetachRoutes={async () => {
                                if (await dialogs.confirm({ title: t("agents.page.detachRoutesDialog.title"), message: t("agents.page.detachRoutesDialog.message", { hostnames: runtime?.routedHostnames.join(", ") || t("agents.page.detachRoutesDialog.fallbackSubject") }), confirmLabel: t("agents.page.detachRoutesDialog.confirmLabel") })) void mutateRuntime(session.id, () => DetachAgentWorktreeRoutes(session.id), t("agents.page.flash.worktreeUrlsDisconnected"));
                              }}
                              onRemoveWorktree={async () => {
                                if (await dialogs.confirm({ title: t("agents.page.removeWorktreeDialog.title"), message: t("agents.page.removeWorktreeDialog.message", { path: session.worktreePath ?? "" }), confirmLabel: t("agents.page.removeWorktreeDialog.confirmLabel"), danger: true })) void mutate(async () => { await RemoveAgentWorktree(session.id); await refresh(); }, t("agents.page.flash.worktreeRemoved"));
                              }}
                            />
                          </div>
                        );
                      })}
                    </div>
                    );
                  })}
                </div>
              )}
            </section>
          );
        })}
      </div>
      <div className="border-t border-white/[0.10] px-4 py-2.5 text-[10px] text-white/50">{t("agents.page.footer")}</div>
      <DevServerLogsModal projectName={logsSession?.projectName ?? null} worktreeSessionId={logsSession?.id} open={!!logsSession} onClose={() => setLogsSession(null)} />
    </aside>
  );
}
