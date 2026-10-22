import { useEffect, useMemo, useRef, useState } from "react";
import { Bot, Check, ChevronDown, GitBranch, Loader2, Play, Search } from "lucide-react";
import PortalMenu from "./PortalMenu";
import GitLocationPicker, { type GitMode, type RepositoryInfo } from "./agents/GitLocationPicker";
import DomainRoutingPanel, { type ProjectRouteView } from "./agents/DomainRoutingPanel";
import AgentProviderIcon from "./agents/AgentProviderIcon";
import { searchAgentProjects } from "./agents/agentProjectSearch";
import AgentContextFolders from "./agents/AgentContextFolders";
// @ts-ignore
import { GetAgentGitWorkspace, ListAgentSessions, ListProjectRoutes, RestoreCanonicalRoute, SuggestWorktreePreviewHostname } from "../../wailsjs/go/main/App";
import { useT } from "../lib/i18n";

export type AgentID = "claude" | "codex" | "kimi" | "opencode";
export type AgentProfile = {
  id: AgentID;
  label: string;
  available: boolean;
  version?: string;
  reason?: string;
  capabilities: { launch: boolean; resume: boolean; richStatus: boolean; history: boolean; usage: boolean; additionalDirectories?: boolean };
};
export type AgentLaunchDraft = {
  mode: GitMode;
  branch?: string;
  createBranch?: boolean;
  baseRef?: string;
  worktreeName?: string;
  routeCanonical?: boolean;
  previewHostname?: string;
  routeProxyPort?: number;
  additionalDirectories?: string[];
};
export type AgentLaunchProjectOption = {
  name: string;
  url: string;
  path: string;
  proxyPort: number;
};

export default function AgentLauncherMenu({
  projectName, projectPath, canonicalHost, mainProxyPort, projectOptions, onProjectChange,
  profiles, open, setOpen, onLaunch, notify, buttonLabel, compact = false,
}: {
  projectName: string;
  projectPath: string;
  canonicalHost: string;
  mainProxyPort: number;
  projectOptions?: AgentLaunchProjectOption[];
  onProjectChange?: (projectName: string) => void;
  profiles: AgentProfile[];
  open: boolean;
  setOpen: (open: boolean) => void;
  onLaunch: (agent: AgentProfile, options: AgentLaunchDraft) => void;
  notify: (message: string) => void;
  buttonLabel?: string;
  compact?: boolean;
}) {
  const t = useT();
  const anchorRef = useRef<HTMLButtonElement>(null);
  const available = useMemo(() => profiles.filter((profile) => profile.available && profile.capabilities.launch), [profiles]);
  const [selectedID, setSelectedID] = useState<AgentID | "">("");
  const [mode, setMode] = useState<GitMode>("current");
  const [repo, setRepo] = useState<RepositoryInfo | null>(null);
  const [loadingRepo, setLoadingRepo] = useState(false);
  const [branch, setBranch] = useState("");
  const [createBranch, setCreateBranch] = useState(false);
  const [baseRef, setBaseRef] = useState("");
  const [worktreeName, setWorktreeName] = useState("");
  const [routeCanonical, setRouteCanonical] = useState(false);
  const [previewEnabled, setPreviewEnabled] = useState(false);
  const [previewHostname, setPreviewHostname] = useState("");
  const [previewHostnameEdited, setPreviewHostnameEdited] = useState(false);
  const [routeProxyPort, setRouteProxyPort] = useState("");
  const [routes, setRoutes] = useState<ProjectRouteView[]>([]);
  const [restoringRoute, setRestoringRoute] = useState(false);
  const [liveSessionTitles, setLiveSessionTitles] = useState<string[]>([]);
  const [projectQuery, setProjectQuery] = useState("");
  const [projectPickerOpen, setProjectPickerOpen] = useState(false);
  const [highlightedProject, setHighlightedProject] = useState(0);
  const [additionalDirectories, setAdditionalDirectories] = useState<string[]>([]);
  const matchingProjects = useMemo(() => searchAgentProjects(projectOptions ?? [], projectQuery), [projectOptions, projectQuery]);

  useEffect(() => {
    setAdditionalDirectories([]);
  }, [open, projectName]);

  useEffect(() => {
    if (!open) return;
    setMode("current");
    setCreateBranch(false);
    setRouteCanonical(false);
    setPreviewEnabled(false);
    setPreviewHostname("");
    setPreviewHostnameEdited(false);
    setProjectQuery("");
    setProjectPickerOpen(false);
    setHighlightedProject(0);
    setRouteProxyPort(mainProxyPort > 0 ? String(mainProxyPort + 1) : "");
    setSelectedID((current) => available.some((profile) => profile.id === current) ? current : (available[0]?.id ?? ""));
    setLoadingRepo(true);
    GetAgentGitWorkspace(projectName)
      .then((info: RepositoryInfo) => {
        setRepo(info);
        setBranch(info.currentBranch || info.branches?.find((candidate) => !candidate.remote)?.name || "");
        setBaseRef(info.currentBranch || "");
      })
      .catch(() => setRepo({ repository: false, dirty: false, branches: [], worktrees: [], reason: t("agents.launcher.gitUnavailableReason") }))
      .finally(() => setLoadingRepo(false));
    ListAgentSessions(projectName, false)
      .then((sessions) => setLiveSessionTitles((sessions ?? [])
        .filter((session) => ["starting", "working", "needs-input", "idle"].includes(session.state) && session.launchMode !== "worktree")
        .map((session) => session.title)))
      .catch(() => setLiveSessionTitles([]));
    ListProjectRoutes(projectName)
      .then((projectRoutes: ProjectRouteView[]) => setRoutes(projectRoutes ?? []))
      .catch(() => setRoutes([]));
  }, [open, projectName, mainProxyPort, available]);

  useEffect(() => {
    if (!open || mode !== "worktree" || previewHostnameEdited) return;
    let cancelled = false;
    SuggestWorktreePreviewHostname(projectName, worktreeName)
      .then((hostname: string) => { if (!cancelled) setPreviewHostname(hostname); })
      .catch(() => {});
    return () => { cancelled = true; };
  }, [open, mode, projectName, worktreeName, previewHostnameEdited]);

  const selected = profiles.find((profile) => profile.id === selectedID);
  const parsedProxyPort = Number(routeProxyPort);
  const routingEnabled = mode === "worktree" && (routeCanonical || previewEnabled);
  const proxyPortValid = mainProxyPort <= 0 || !routingEnabled || (Number.isInteger(parsedProxyPort) && parsedProxyPort > 0 && parsedProxyPort <= 65535 && parsedProxyPort !== mainProxyPort);
  const routingValid = (!previewEnabled || !!previewHostname.trim()) && proxyPortValid;
  const supportsAdditionalDirectories = selected?.capabilities.additionalDirectories === true;
  const contextValid = additionalDirectories.length === 0 || supportsAdditionalDirectories;
  const canLaunch = !!selected?.available && !!selected.capabilities.launch && contextValid && routingValid && (mode === "current" || (!!repo?.repository && !!branch.trim() && (mode !== "worktree" || !!worktreeName.trim()) && (!createBranch || !!baseRef.trim())));

  const pickProject = (name: string) => {
    onProjectChange?.(name);
    setProjectQuery("");
    setProjectPickerOpen(false);
    setHighlightedProject(0);
  };

  const launch = () => {
    if (!selected || !canLaunch) return;
    onLaunch(selected, {
      mode,
      branch: mode === "current" ? undefined : branch.trim(),
      createBranch: mode === "current" ? undefined : createBranch,
      baseRef: mode === "current" || !createBranch ? undefined : baseRef.trim(),
      worktreeName: mode === "worktree" ? worktreeName.trim() : undefined,
      routeCanonical: mode === "worktree" && routeCanonical ? true : undefined,
      previewHostname: mode === "worktree" && previewEnabled ? previewHostname.trim() : undefined,
      routeProxyPort: mode === "worktree" && routingEnabled && mainProxyPort > 0 ? parsedProxyPort : undefined,
      additionalDirectories: additionalDirectories.length ? additionalDirectories : undefined,
    });
    setOpen(false);
  };

  const restoreCanonical = async () => {
    setRestoringRoute(true);
    try {
      const projectRoutes = await RestoreCanonicalRoute(projectName) as ProjectRouteView[];
      setRoutes(projectRoutes ?? []);
      notify(t("agents.launcher.restoredFlash", { host: canonicalHost }));
    } catch (error) {
      notify(t("agents.launcher.restoreFailedFlash", { host: canonicalHost, message: String(error) }));
    } finally {
      setRestoringRoute(false);
    }
  };

  return (
    <>
      <button
        ref={anchorRef}
        type="button"
        onClick={() => setOpen(!open)}
        className={compact
          ? "h-7 w-7 rounded-md text-white/45 hover:bg-white/10 hover:text-accent2 flex items-center justify-center"
          : "flex h-8 items-center gap-1.5 rounded-md border border-accent2/35 bg-accent2/10 px-3 text-[12px] font-medium text-accent2 hover:bg-accent2/20"}
        title={t("agents.launcher.triggerTooltip", { project: projectName })}
      >
        <Bot className="w-3 h-3" /> {!compact && <>{buttonLabel ?? t("agents.launcher.defaultButtonLabel")} <ChevronDown className="w-3 h-3 opacity-60" /></>}
      </button>
      <PortalMenu open={open} anchorRef={anchorRef} onClose={() => setOpen(false)} align="right" minWidth={430}>
        <div className="w-[430px] max-h-[min(640px,calc(100vh-80px))] overflow-y-auto p-2.5">
          <div className="flex items-start justify-between gap-3 mb-3">
            <div className="min-w-0 flex-1">
              <div className="text-xs font-semibold">{t("agents.launcher.header")}</div>
              {projectOptions && onProjectChange ? (
                <div
                  className="relative mt-1.5"
                  onBlur={(event) => {
                    if (!event.currentTarget.contains(event.relatedTarget as Node | null)) {
                      setProjectPickerOpen(false);
                      setProjectQuery("");
                    }
                  }}
                >
                  <label className="flex h-8 items-center gap-2 rounded-md border border-white/10 bg-black/20 px-2.5 focus-within:border-accent2/40">
                    <Search className="h-3.5 w-3.5 shrink-0 text-white/30" />
                    <span className="sr-only">{t("agents.launcher.searchProjectSrLabel")}</span>
                    <input
                      role="combobox"
                      aria-label={t("agents.launcher.searchAria")}
                      aria-expanded={projectPickerOpen}
                      aria-controls="agent-project-results"
                      aria-autocomplete="list"
                      value={projectQuery}
                      onFocus={() => setProjectPickerOpen(true)}
                      onChange={(event) => { setProjectQuery(event.target.value); setProjectPickerOpen(true); setHighlightedProject(0); }}
                      onKeyDown={(event) => {
                        if (event.key === "ArrowDown") { event.preventDefault(); setProjectPickerOpen(true); setHighlightedProject((current) => Math.min(current + 1, Math.max(0, matchingProjects.length - 1))); }
                        else if (event.key === "ArrowUp") { event.preventDefault(); setHighlightedProject((current) => Math.max(0, current - 1)); }
                        else if (event.key === "Enter" && projectPickerOpen && matchingProjects[highlightedProject]) { event.preventDefault(); pickProject(matchingProjects[highlightedProject].name); }
                        else if (event.key === "Escape") { event.preventDefault(); setProjectPickerOpen(false); setProjectQuery(""); }
                      }}
                      className="min-w-0 flex-1 bg-transparent font-mono text-[10px] outline-none placeholder:text-white/25"
                      placeholder={t("agents.launcher.searchPlaceholder", { project: projectName })}
                    />
                  </label>
                  {projectPickerOpen && (
                    <div id="agent-project-results" role="listbox" className="absolute left-0 right-0 top-9 z-20 max-h-56 overflow-y-auto rounded-md border border-white/10 bg-[#191919] p-1 shadow-2xl">
                      {matchingProjects.length > 0 ? matchingProjects.map((project, index) => (
                        <button
                          key={project.name}
                          type="button"
                          role="option"
                          aria-selected={project.name === projectName}
                          onMouseDown={(event) => event.preventDefault()}
                          onMouseEnter={() => setHighlightedProject(index)}
                          onClick={() => pickProject(project.name)}
                          className={`flex w-full items-center gap-2 rounded px-2 py-2 text-left ${index === highlightedProject ? "bg-white/[0.09]" : "hover:bg-white/[0.06]"}`}
                        >
                          <span className="min-w-0 flex-1">
                            <span className="block truncate text-[10px] font-medium text-white/80">{project.name}</span>
                            <span className="mt-0.5 block truncate font-mono text-[8px] text-white/30">{project.url || project.path}</span>
                          </span>
                          {project.name === projectName && <Check className="h-3.5 w-3.5 shrink-0 text-accent2" />}
                        </button>
                      )) : <div className="px-2 py-3 text-center text-[9px] text-white/35">{t("agents.launcher.noProjectsFound")}</div>}
                    </div>
                  )}
                  <div className="mt-1 flex items-center gap-1.5 font-mono text-[8px] text-white/35"><Check className="h-3 w-3 text-accent2" /> {projectName}</div>
                </div>
              ) : <div className="mt-0.5 truncate font-mono text-[9px] opacity-45">{projectName}</div>}
            </div>
            {loadingRepo ? <Loader2 className="w-3.5 h-3.5 animate-spin opacity-50" /> : repo?.repository ? (
              <span className="flex items-center gap-1 rounded border border-white/10 bg-white/5 px-1.5 py-0.5 font-mono text-[9px] opacity-65">
                <GitBranch className="w-2.5 h-2.5" /> {repo.currentBranch || t("agents.launcher.detachedLabel")}{repo.dirty ? ` · ${t("agents.launcher.dirtyLabel")}` : ""}
              </span>
            ) : null}
          </div>

          <div className="grid grid-cols-2 gap-1.5 mb-3">
            {profiles.map((profile) => {
              const enabled = profile.available && profile.capabilities.launch;
              const active = selectedID === profile.id;
              return (
                <button
                  key={profile.id}
                  type="button"
                  disabled={!enabled}
                  onClick={() => setSelectedID(profile.id)}
                  title={enabled ? profile.version : profile.reason || t("agents.launcher.cliUnavailable")}
                  className={`flex items-center gap-2.5 rounded-lg border px-2.5 py-2.5 text-left transition-colors disabled:opacity-30 disabled:cursor-not-allowed
                    ${active ? "border-accent/60 bg-accent/20" : "border-white/10 bg-white/[0.025] hover:bg-white/[0.07]"}`}
                >
                  <AgentProviderIcon provider={profile.id} />
                  <span className="min-w-0">
                    <span className="block truncate text-[12px] font-semibold">{profile.label}</span>
                    <span className="mt-0.5 block truncate font-mono text-[9px] opacity-50">{profile.version || profile.reason || t("agents.launcher.notDetected")}</span>
                  </span>
                </button>
              );
            })}
          </div>

          <AgentContextFolders
            key={`${projectName}:${open}`}
            projectPath={projectPath}
            directories={additionalDirectories}
            onChange={setAdditionalDirectories}
            supported={supportsAdditionalDirectories}
            worktree={mode === "worktree"}
            providerLabel={selected?.label || t("agents.launcher.thisAgentFallback")}
          />

          <div className="text-[9px] uppercase tracking-widest opacity-45 mb-1.5">{t("agents.launcher.workingCopyHeading")}</div>
          <GitLocationPicker info={repo} mode={mode} onChange={setMode} />

          {mode !== "current" && (
            <div className="mt-2.5 rounded-lg border border-white/10 bg-black/15 p-2.5 grid gap-2">
              <label className="grid gap-1 text-[9px] uppercase tracking-wider opacity-60">
                {t("agents.launcher.branchLabel")}
                <input value={branch} onChange={(event) => setBranch(event.target.value)} list={`branches-${projectName}`} className="input h-8 font-mono text-[11px] normal-case tracking-normal opacity-100" placeholder="feature/my-change" />
                <datalist id={`branches-${projectName}`}>
                  {(repo?.branches ?? []).filter((candidate) => !candidate.remote).map((candidate) => <option key={candidate.name} value={candidate.name} />)}
                </datalist>
              </label>
              <label className="flex items-center gap-2 text-[10px] opacity-70">
                <input type="checkbox" checked={createBranch} onChange={(event) => setCreateBranch(event.target.checked)} />
                {t("agents.launcher.createBranchLabel")}
              </label>
              {createBranch && (
                <label className="grid gap-1 text-[9px] uppercase tracking-wider opacity-60">
                  {t("agents.launcher.baseRefLabel")}
                  <input value={baseRef} onChange={(event) => setBaseRef(event.target.value)} className="input h-8 font-mono text-[11px] normal-case tracking-normal opacity-100" placeholder="main" />
                </label>
              )}
              {mode === "worktree" && (
                <label className="grid gap-1 text-[9px] uppercase tracking-wider opacity-60">
                  {t("agents.launcher.worktreeNameLabel")}
                  <input value={worktreeName} onChange={(event) => setWorktreeName(event.target.value)} className="input h-8 font-mono text-[11px] normal-case tracking-normal opacity-100" placeholder="feature-preview" />
                </label>
              )}
              {mode === "branch" && repo?.dirty && <div className="text-[10px] text-amber-200">{t("agents.launcher.dirtyWarning")}</div>}
            </div>
          )}

          {mode === "worktree" && (
            <DomainRoutingPanel
              canonicalHost={canonicalHost}
              canonicalFromWorktree={routeCanonical}
              onCanonicalFromWorktreeChange={setRouteCanonical}
              previewEnabled={previewEnabled}
              onPreviewEnabledChange={setPreviewEnabled}
              previewHostname={previewHostname}
              onPreviewHostnameChange={(hostname) => { setPreviewHostnameEdited(true); setPreviewHostname(hostname); }}
              mainProxyPort={mainProxyPort}
              proxyPort={routeProxyPort}
              onProxyPortChange={setRouteProxyPort}
              routes={routes}
              mainPath={projectPath}
              restoring={restoringRoute}
              onRestoreCanonical={restoreCanonical}
            />
          )}

          {!repo?.repository && !loadingRepo && <div className="mt-2 text-[9px] opacity-45">{repo?.reason}{t("agents.launcher.checkoutStillAvailableSuffix")}</div>}

          {mode === "current" && liveSessionTitles.length > 0 && (
            <div className="mt-2 rounded-lg border border-amber-400/30 bg-amber-500/10 px-2.5 py-2 text-[10px] leading-4 text-amber-100">
              {t("agents.launcher.sharedCheckoutNotice", { titles: liveSessionTitles.join(", ") })}
            </div>
          )}

          <button
            type="button"
            disabled={!canLaunch || (mode === "branch" && !!repo?.dirty)}
            onClick={launch}
            className="mt-3 w-full h-9 rounded-lg bg-accent/80 hover:bg-accent text-white text-[11px] font-medium flex items-center justify-center gap-2 disabled:opacity-35 disabled:cursor-not-allowed"
          >
            <Play className="w-3 h-3 fill-current" /> {mode === "current" && liveSessionTitles.length > 0 ? t("agents.launcher.continueSharedButton") : t("agents.launcher.startAgentButton", { agent: selected?.label || t("agents.launcher.genericAgentFallback") })}
          </button>
        </div>
      </PortalMenu>
    </>
  );
}
