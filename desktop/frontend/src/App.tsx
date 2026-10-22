import { useCallback, useEffect, useRef, useState } from "react";
import type { CSSProperties } from "react";
import { createPortal } from "react-dom";
import { Bot, TerminalSquare, ScrollText, Network, PanelLeftClose, PanelLeftOpen } from "lucide-react";
import Sidebar, { Page } from "./components/Sidebar";
import WindowControls from "./components/WindowControls";
import Dashboard from "./pages/Dashboard";
import Downloads from "./pages/Downloads";
import Projects from "./pages/Projects";
import SSH from "./pages/SSH";
import Settings, { Tab as SettingsTab } from "./pages/Settings";
import Cron from "./pages/Cron";
import Deployments from "./pages/Deployments";
import Agents from "./pages/Agents";
import ServiceLogsModal from "./components/ServiceLogsModal";
import PortsModal from "./components/PortsModal";
import TerminalWorkspace, { OpenProjectRequest } from "./components/terminal/TerminalWorkspace";
import type { OpenTerminal, TerminalFocusRequest } from "./components/terminal/types";
import { DocumentWorkspace } from "./components/documents/DocumentWorkspace";
import { DocumentCloseDialog } from "./components/documents/DocumentCloseDialog";
import { useDocumentWorkspace } from "./components/documents/useDocumentWorkspace";
import { useDocumentShutdown } from "./components/documents/useDocumentShutdown";
import { workspaceDocumentApi } from "./components/documents/api";
import type { ActiveSurface, NavigatorTab, WorkspaceContextRef, WorkspaceSelection, WorkspaceView } from "./components/workspace/types";
import { WorkspaceNavigator } from "./components/workspace/WorkspaceNavigator";
import { readNavigationPreferences, updateNavigationPreferences, writeNavigationPreferences, type NavigationPreferencesPatch } from "./components/workspace/navigationPreferences";
import { WorkspaceFilesPanel } from "./components/documents/WorkspaceFilesPanel";
import type { WorkspaceDocumentRef } from "./components/documents/types";
import { useSourceControl } from "./components/sourcecontrol/useSourceControl";
import { sourceControlApi } from "./components/sourcecontrol/api";
import { SourceControlPanel } from "./components/sourcecontrol/SourceControlPanel";
import { useGitReviews } from "./components/sourcecontrol/useGitReviews";
import { useStageDocuments } from "./components/sourcecontrol/useStageDocuments";
import { StageDirtyDialog } from "./components/sourcecontrol/StageDirtyDialog";
import CommandPalette from "./components/CommandPalette";
import { useCommandCenter } from "./commandCenter/useCommandCenter";
import { matchesHotkey } from "./commandCenter/hotkeys";
import type { CommandContext, CommandIntent } from "./commandCenter/types";
import type { Status as CloudStatusType } from "./pages/Account";
import type { AgentID, AgentLaunchDraft } from "./components/AgentLauncherMenu";
import type { AgentSessionView } from "./components/agents/AgentSessionCard";
import AgentUsageBar from "./components/agents/AgentUsageBar";
import { useAgentMap } from "./components/agents/useAgentMap";
import { useAgentAutoSwitch } from "./components/agents/useAgentAutoSwitch";
import NotificationBell from "./components/NotificationBell";
import { useT } from "./lib/i18n";
// @ts-ignore
import { CloudStatus, GetAgentSessionsEnabled, SetAgentSessionsEnabled, GetDetachedSeed, IsDetachedWindow, IsCommandBar, SendCommandIntent, QuitApp } from "../wailsjs/go/main/App";
// @ts-ignore
import { EventsOn, WindowToggleMaximise } from "../wailsjs/runtime";

const AGENT_NAVIGATOR_KEY = "laraenv.agentNavigatorVisible";
function initialAgentNavigatorVisibility() {
  try { return window.localStorage.getItem(AGENT_NAVIGATOR_KEY) !== "false"; } catch { return true; }
}

const AGENT_AUTO_SWITCH_KEY = "laraenv.agentAutoSwitch";
function initialAgentAutoSwitch(): boolean {
  try { return window.localStorage.getItem(AGENT_AUTO_SWITCH_KEY) === "true"; } catch { return false; }
}

export default function App() {
  // Detect which kind of window this process is: the main shell, a satellite
  // "terminal window", or the standalone command-bar. null = still detecting.
  const [mode, setMode] = useState<"main" | "terminal" | "bar" | null>(null);
  useEffect(() => {
    Promise.all([IsDetachedWindow(), IsCommandBar()])
      .then(([term, bar]: boolean[]) => setMode(bar ? "bar" : term ? "terminal" : "main"))
      .catch(() => setMode("main"));
  }, []);
  if (mode === null) return <div className="h-screen w-screen bg-midnight" />;
  if (mode === "bar") return <CommandBarWindow />;
  if (mode === "terminal") return <DetachedTerminalWindow />;
  return <MainApp />;
}

// The standalone command-bar window (a separate process launched by the global
// hotkey). It shows only the palette filling the window; picking a command
// forwards the intent to the main process and quits.
function CommandBarWindow() {
  const t = useT();
  const noop = () => {};
  const ctx: CommandContext = {
    navigate: noop, newProject: noop, openTerminal: noop, runInTerminal: noop, openSSH: noop, flash: noop, t,
  };
  const { commands, projects, sshHosts, agentProviders, getProjectCommands, ensureProjectCommands, launchAgent } = useCommandCenter(ctx);

  const pick = async (intent: CommandIntent | undefined, alt: boolean) => {
    if (intent) {
      // Enter = native external terminal; Shift+Enter (alt) = in-app xterm.js.
      // Ignored by the main process for non-terminal/ssh actions.
      const withTarget = { ...intent, target: alt ? "xterm" : "native" } as CommandIntent;
      // Await so the intent file is fully written to the inbox before we quit —
      // otherwise QuitApp can tear down this process mid-write and the intent is
      // dropped (the first few attempts silently fail).
      try { await SendCommandIntent(JSON.stringify(withTarget)); } catch { /* ignore */ }
    }
    QuitApp();
  };

  // Spotlight-style: dismiss when the bar loses focus — but only after a short
  // grace period, since focus can bounce while the new window grabs foreground.
  useEffect(() => {
    let armed = false;
    const timer = setTimeout(() => { armed = true; }, 700);
    const onBlur = () => { if (armed) QuitApp(); };
    window.addEventListener("blur", onBlur);
    return () => { clearTimeout(timer); window.removeEventListener("blur", onBlur); };
  }, []);

  return (
    <div
      className="h-screen w-screen overflow-hidden"
      // @ts-ignore — let the whole bar be draggable except interactive bits
      style={{ "--wails-draggable": "no-drag" }}
    >
      <CommandPalette
        bar
        open
        commands={commands}
        launcher={{ projects, sshHosts, agentProviders, getProjectCommands, ensureProjectCommands, runInTerminal: noop, openSSH: noop, launchAgent, t }}
        onClose={() => QuitApp()}
        onPick={pick}
      />
    </div>
  );
}

function DetachedTerminalWindow() {
  const t = useT();
  const [seed, setSeed] = useState<string | null>(null);
  const [navigatorVisible, setNavigatorVisible] = useState(initialAgentNavigatorVisibility);
  const [agentSessionsEnabled, setAgentSessionsEnabled] = useState(false);
  const [enabling, setEnabling] = useState(false);
  const [openRequest, setOpenRequest] = useState<OpenProjectRequest | null>(null);
  const [focusRequest, setFocusRequest] = useState<{ sessionId: string; seq: number } | null>(null);
  const [activeSessionID, setActiveSessionID] = useState("");
  const [openTerminals, setOpenTerminals] = useState<OpenTerminal[]>([]);
  const mapBlocked = useRef(false);
  const agentMap = useAgentMap({
    enabled: agentSessionsEnabled, selectedSessionId: openTerminals.some(pane => pane.laraSessionId === activeSessionID) ? activeSessionID : '',
    isBlocked: () => mapBlocked.current,
    onSelectSession: () => setNavigatorVisible(true),
    canOpenRoot: (sessionId, terminalId) => openTerminals.some(pane => pane.terminalId === terminalId && pane.laraSessionId === sessionId),
    onOpenRoot: sessionId => {
      setActiveSessionID(sessionId);
      sequence.current += 1;
      setFocusRequest({sessionId, seq: sequence.current});
    },
  });
  mapBlocked.current = !!agentMap.openSessionId;
  const [toast, setToast] = useState<string | null>(null);
  const sequence = useRef(0);
  const toastTimer = useRef<ReturnType<typeof setTimeout> | null>(null);

  useEffect(() => {
    GetDetachedSeed().then(setSeed).catch(() => setSeed(""));
    GetAgentSessionsEnabled().then(setAgentSessionsEnabled).catch(() => {});
    const off = EventsOn("agent:feature-changed", setAgentSessionsEnabled);
    return () => off();
  }, []);
  useEffect(() => {
    try { window.localStorage.setItem(AGENT_NAVIGATOR_KEY, String(navigatorVisible)); } catch { /* preference is best effort */ }
  }, [navigatorVisible]);

  const flash = (message: string) => {
    setToast(message);
    if (toastTimer.current) clearTimeout(toastTimer.current);
    toastTimer.current = setTimeout(() => setToast(null), Math.min(7000, Math.max(2800, message.length * 55)));
  };
  const requestAgent = (project: string, agentId: AgentID, agentLabel: string, options: AgentLaunchDraft) => {
    if (mapBlocked.current) return;
    sequence.current += 1;
    setOpenRequest({ kind: "agent", project, agentId, agentLabel, ...options, seq: sequence.current });
  };
  const focusAgent = (session: AgentSessionView) => {
    if (mapBlocked.current) return;
    setActiveSessionID(session.id);
    sequence.current += 1;
    setFocusRequest({ sessionId: session.id, seq: sequence.current });
  };
  const resumeAgent = (session: AgentSessionView, providerLabel: string) => {
    if (mapBlocked.current) return;
    setActiveSessionID(session.id);
    sequence.current += 1;
    setOpenRequest({ kind: "agent-resume", sessionId: session.id, project: session.projectName, agentId: session.agentId, agentLabel: providerLabel, seq: sequence.current });
  };
  const enableAgents = async () => {
    if (enabling) return;
    setEnabling(true);
    try { await SetAgentSessionsEnabled(true); setAgentSessionsEnabled(true); } catch (error) { flash(t("terminal.app.flash.enableAgentSessionsFailed", { error: String(error) })); } finally { setEnabling(false); }
  };

  if (seed === null) return <div className="h-screen w-screen bg-[#171717]" />;
  return (
    <div className="h-screen w-screen flex flex-col overflow-hidden bg-[#171717]">
      <div
        {...(agentMap.openSessionId ? {inert: ""} : {})}
        className="h-9 flex items-center justify-between pl-2 flex-shrink-0 bg-[#101214] border-b border-white/[0.12]"
        onDoubleClick={(event) => {
          if ((event.target as HTMLElement).closest("button")) return;
          WindowToggleMaximise();
        }}
        // @ts-ignore — Wails CSS drag hint
        style={{ "--wails-draggable": "drag" }}
      >
        <div className="flex items-center gap-2 text-[12px] text-white/75">
          <button
            type="button"
            onClick={() => { if (!mapBlocked.current) setNavigatorVisible((visible) => !visible); }}
            title={navigatorVisible ? t("terminal.app.hideSessionNavigatorTooltip") : t("terminal.app.showSessionNavigatorTooltip")}
            aria-label={navigatorVisible ? t("terminal.app.hideSessionNavigatorTooltip") : t("terminal.app.showSessionNavigatorTooltip")}
            aria-pressed={navigatorVisible}
            className={`mr-1 flex h-7 items-center gap-1.5 rounded-md px-2.5 font-medium ${navigatorVisible ? "bg-accent2/12 text-accent2" : "text-white/60 hover:bg-white/10 hover:text-white"}`}
            style={{ "--wails-draggable": "no-drag" } as CSSProperties}
          >
            <Bot className="h-3.5 w-3.5" />
            <span>{t("terminal.app.agentsLabel")}</span>
            {navigatorVisible ? <PanelLeftClose className="h-3 w-3 opacity-55" /> : <PanelLeftOpen className="h-3 w-3 opacity-55" />}
          </button>
          <TerminalSquare className="h-3.5 w-3.5" /> LaraEnv · {t("terminal.app.terminalLabel")}
        </div>
        <span className="pr-3 text-[11px] text-white/50">{t("terminal.app.detachedWindowBadge")}</span>
      </div>
      {toast && <div className="pointer-events-none fixed right-3 top-10 z-[100] max-w-[420px] rounded-md border border-accent/50 bg-[#151515] px-4 py-2 text-xs text-accent2 shadow-2xl">{toast}</div>}
      <div className="flex min-h-0 flex-1" {...(agentMap.openSessionId ? {inert: ""} : {})}>
        {navigatorVisible && (
          <div className="w-[372px] min-w-[340px] max-w-[46%] shrink-0 border-r border-white/[0.12]">
            <Agents
              flash={flash}
              enabled={agentSessionsEnabled}
              enabling={enabling}
              onEnable={() => void enableAgents()}
              selectedSessionId={activeSessionID}
              onSelect={(session) => setActiveSessionID(session.id)}
              onOpenMap={session => agentMap.openMap(session.id)}
              onOpen={focusAgent}
              onResume={resumeAgent}
              onFresh={(project, agentId, label, additionalDirectories) => requestAgent(project, agentId, label, { mode: "current", additionalDirectories })}
              onLaunch={requestAgent}
            />
          </div>
        )}
        <div className="min-w-0 flex-1">
          <TerminalWorkspace
            detached
            detachedSeed={seed}
            activeSurface={agentMap.openSessionId ? "navigator" : "terminal"}
            onOpenTerminalsChange={setOpenTerminals}
            active
            openRequest={openRequest}
            focusAgentRequest={focusRequest}
            onFocusedAgentSessionChange={setActiveSessionID}
            flash={flash}
          />
        </div>
      </div>
      {agentSessionsEnabled && <div {...(agentMap.openSessionId ? {inert: ""} : {})}><AgentUsageBar leading={agentMap.chip} /></div>}
      {agentMap.dialog}
    </div>
  );
}

function MainApp() {
  const t = useT();
  const [page, setPage] = useState<Page>("dashboard");
  const [runtimeDownloadRequest, setRuntimeDownloadRequest] = useState<{seq:number;kind:"php"|"nodejs";version?:string}>();
  const openRuntimeDownloads = (kind: "php"|"nodejs", version?: string) => {
    setRuntimeDownloadRequest(previous => ({seq:(previous?.seq ?? 0)+1,kind,version}));
    setPage("downloads");
  };
  const [activeSurface, setActiveSurface] = useState<ActiveSurface>("terminal");
  const [workspaceSelection, setWorkspaceSelection] = useState<WorkspaceSelection>({ kind: 'local', context: null });
  const [manualProjectContext, setManualProjectContext] = useState<WorkspaceContextRef | null>(null);
  const workspaceContext = workspaceSelection.kind === 'local' ? workspaceSelection.context ?? manualProjectContext : null;
  const [openFilesRequest, setOpenFilesRequest] = useState<{ seq: number } | null>(null);
  const [compactView, setCompactView] = useState<WorkspaceView>("terminal");
  const [navigation, setNavigation] = useState(readNavigationPreferences);
  const updateNavigation = useCallback((patch: NavigationPreferencesPatch) => setNavigation(current => updateNavigationPreferences(current, patch)), []);
  useEffect(() => writeNavigationPreferences(navigation), [navigation]);
  const [outerWidth, setOuterWidth] = useState(1200);
  const navigationArea = useRef<HTMLDivElement>(null);
  // Eligibility never depends on the width reserved by the sidebar itself.
  const drawer = outerWidth - navigation.sidebarWidth < 720;
  const panelActive = (tab: NavigatorTab) => page === 'terminal' && navigation.sidebarVisible && navigation.sidebarTab === tab;
  const [availableWidth, setAvailableWidth] = useState(1000);
  const [windowFocused, setWindowFocused] = useState(document.hasFocus());
  const workspaceArea = useRef<HTMLDivElement>(null);
  const documentSurface = useRef<HTMLDivElement>(null);
  const resizePointer = useRef<number | null>(null);
  const compact = availableWidth < 720;
  const documents = useDocumentWorkspace({ pageActive: page === "terminal" && (!compact || compactView === "documents"), windowFocused, api: workspaceDocumentApi });
  const stage = useStageDocuments(documents);
  const stagePending = useRef(false);
  const decisionBlocked = !!documents.decision || !!stage.decision || documents.handoffPending;
  const mapBlocked = useRef(false);
  const dismissMap = useRef<() => void>(() => {});
  const decisionRef = useRef(decisionBlocked); decisionRef.current = decisionBlocked;
  const blocked = useRef(decisionBlocked); blocked.current = decisionBlocked || mapBlocked.current;
  const actionsBlocked = () => blocked.current || stagePending.current;
  // The one shutdown subscriber refuses a competing close while stage owns a decision.
  useDocumentShutdown({ ...documents, confirmCloseAll: () => {
    if (decisionRef.current || stagePending.current) return Promise.resolve(false);
    dismissMap.current();
    return documents.confirmCloseAll();
  } });
  const reviews = useGitReviews(sourceControlApi);
  const sourceControl = useSourceControl({ context: workspaceContext, active: panelActive('git'), windowFocused,
    beforeStage: async (snapshot, entries) => {
      if (actionsBlocked()) return null;
      stagePending.current = true;
      try { return await stage.beforeStage(snapshot, entries); } finally { stagePending.current = false; }
    },
  });
  const claimDocuments = useCallback(() => { if (!blocked.current && !stagePending.current) setActiveSurface("documents"); }, []);
  const claimNavigator = useCallback(() => { if (!blocked.current && !stagePending.current) setActiveSurface("navigator"); }, []);
  const changeSurface = useCallback((surface: ActiveSurface) => {
    if (blocked.current || stagePending.current) return;
    // Explicit terminal intent releases both editor and navigator input focus.
    if (surface === "terminal" && navigationArea.current?.contains(document.activeElement) && !document.activeElement?.matches('.xterm textarea')) {
      (document.activeElement as HTMLElement | null)?.blur();
    }
    setActiveSurface(surface);
    if (surface !== 'navigator') setCompactView(surface);
  }, []);
  const openWorkspaceFiles = useCallback((_context: WorkspaceContextRef | null) => {
    if (blocked.current || stagePending.current) return;
    updateNavigation({ sidebarVisible: true, sidebarTab: 'files' });
    claimNavigator();
  }, [updateNavigation, claimNavigator]);
  const revealEditor = () => {
    if (actionsBlocked()) return false;
    documents.setVisible(true); changeSurface('documents');
    if (drawer) updateNavigation({ sidebarVisible: false });
    return true;
  };
  const openDocument = (ref: WorkspaceDocumentRef) => {
    if (!revealEditor()) return;
    reviews.clearActive(); void documents.open(ref);
  };
  const selectNavigator = (tab: NavigatorTab) => {
    if (actionsBlocked()) return;
    updateNavigation({ sidebarVisible: true, sidebarTab: tab }); claimNavigator();
    if (tab === 'files' && workspaceSelection.kind === 'ssh') {
      seqRef.current += 1; setOpenFilesRequest({ seq: seqRef.current });
    }
  };
  useEffect(() => {
    const onFocus = () => setWindowFocused(document.visibilityState !== "hidden"), onBlur = () => setWindowFocused(false);
    const onVisibility = () => setWindowFocused(document.hasFocus() && document.visibilityState !== "hidden");
    document.addEventListener("visibilitychange", onVisibility);
    window.addEventListener("focus", onFocus); window.addEventListener("blur", onBlur);
    return () => { window.removeEventListener("focus", onFocus); window.removeEventListener("blur", onBlur); document.removeEventListener("visibilitychange", onVisibility); };
  }, []);
  useEffect(() => {
    const area = workspaceArea.current, outer = navigationArea.current;
    if (!area || !outer) return;
    const observer = new ResizeObserver(entries => {
      for (const entry of entries) if (entry.contentRect.width > 0) {
        if (entry.target === outer) setOuterWidth(entry.contentRect.width);
        else setAvailableWidth(entry.contentRect.width);
      }
    });
    observer.observe(area); observer.observe(outer);
    return () => observer.disconnect();
  }, []);
  const [toast, setToast] = useState<string | null>(null);
  const [updateAvail, setUpdateAvail] = useState(false);
  const [openRequest, setOpenRequest] = useState<OpenProjectRequest | null>(null);
  const [agentFocusRequest, setAgentFocusRequest] = useState<{ sessionId: string; seq: number } | null>(null);
  const [terminalFocusRequest, setTerminalFocusRequest] = useState<TerminalFocusRequest | null>(null);
  const [openTerminals, setOpenTerminals] = useState<OpenTerminal[]>([]);
  const [activeAgentSessionId, setActiveAgentSessionId] = useState("");
  const [cronFilter, setCronFilter] = useState<{ project: string; seq: number } | null>(null);
  const [cloudStatus, setCloudStatus] = useState<CloudStatusType | undefined>(undefined);
  const cloudStatusRevision = useRef(0);
  const updateCloudStatus = (next: CloudStatusType | undefined) => {
    cloudStatusRevision.current++;
    setCloudStatus(next);
  };
  const [settingsInitialTab, setSettingsInitialTab] = useState<SettingsTab | undefined>(undefined);
  const [scrollToPlan, setScrollToPlan] = useState(false);
  const [logsOpen, setLogsOpen] = useState(false);
  const [portsOpen, setPortsOpen] = useState(false);
  const [paletteOpen, setPaletteOpen] = useState(false);
  const [newProjectReq, setNewProjectReq] = useState<{ seq: number; kind?: string } | null>(null);
  const [agentSessionsEnabled, setAgentSessionsEnabled] = useState(false);
  const [agentFeatureSaving, setAgentFeatureSaving] = useState(false);
  const [agentAutoSwitch, setAgentAutoSwitch] = useState(initialAgentAutoSwitch);
  const seqRef = useRef(0);

  useEffect(() => {
    const revision = cloudStatusRevision.current;
    let active = true;
    // CloudStatus reads the cached profile locally; opening the desktop
    // does not refresh a remote session or opt a legacy profile in.
    CloudStatus().then((s: CloudStatusType) => {
      if (active && revision === cloudStatusRevision.current) setCloudStatus(s);
    }).catch(() => {});
    GetAgentSessionsEnabled().then(setAgentSessionsEnabled).catch(() => {});
    return () => { active = false; };
  }, []);
  useEffect(() => {
    try { window.localStorage.setItem(AGENT_AUTO_SWITCH_KEY, String(agentAutoSwitch)); } catch { /* preference is best effort */ }
  }, [agentAutoSwitch]);

  // Backend-pushed notices (e.g. startup orphan-port reclaim) surface as toasts.
  useEffect(() => {
    const off = EventsOn("app:notice", (msg: string) => flash(msg));
    const offSharedCheckout = EventsOn("agent:shared-checkout", (msg: string) => flash(msg));
    const offAgentFeature = EventsOn("agent:feature-changed", (enabled: boolean) => setAgentSessionsEnabled(enabled));
    return () => { off(); offSharedCheckout(); offAgentFeature(); };
  }, []);

  // Local HTTPS lives in Settings > Services. Projects needs to send the
  // user there when it cannot offer to trust the CA in place — telling
  // someone a screen exists without taking them to it is how the first
  // version of this flow failed.
  const goToLocalHttps = () => {
    setSettingsInitialTab("services");
    setPage("settings");
  };

  const goToCloud = () => {
    setSettingsInitialTab("account");
    setScrollToPlan(false);
    setPage("settings");
  };

  const toastTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const flash = (msg: string) => {
    setToast(msg);
    if (toastTimer.current) clearTimeout(toastTimer.current);
    // Longer messages (validation errors etc.) need more reading time.
    toastTimer.current = setTimeout(() => setToast(null), Math.min(7000, Math.max(2800, msg.length * 55)));
  };

  // From Projects → open a project-scoped tab on the Terminal page.
  const requestProjectTerminal = (name: string, terminalKind?: string) => {
    if (actionsBlocked()) return;
    seqRef.current += 1;
    setOpenRequest({ kind: "project", name, terminalKind, seq: seqRef.current });
    setPage("terminal");
  };

  const requestProjectCommand = (project: string, command: string, shell?: string) => {
    if (actionsBlocked()) return;
    seqRef.current += 1;
    setOpenRequest({ kind: "command", project, command, shell, seq: seqRef.current });
    changeSurface("terminal");
    setPage("terminal");
  };

  const enableAgentSessions = async () => {
    if (agentFeatureSaving || agentSessionsEnabled) return;
    setAgentFeatureSaving(true);
    try {
      await SetAgentSessionsEnabled(true);
      setAgentSessionsEnabled(true);
      flash(t("terminal.app.flash.agentSessionsEnabled"));
    } catch (error) {
      flash(t("terminal.app.flash.enableAgentSessionsFailed", { error: String(error) }));
    } finally {
      setAgentFeatureSaving(false);
    }
  };

  const requestAgentTerminal = (project: string, agentId: AgentID, agentLabel: string, options: AgentLaunchDraft) => {
    if (actionsBlocked()) return;
    if (!agentSessionsEnabled) {
      setSettingsInitialTab("experimental");
      setPage("settings");
      flash(t("terminal.app.flash.enableAgentSessionsFirst"));
      return;
    }
    seqRef.current += 1;
    setOpenRequest({ kind: "agent", project, agentId, agentLabel, ...options, seq: seqRef.current });
    updateNavigation({ sidebarVisible: true, sidebarTab: "agents" });
    setPage("terminal");
  };

  const focusAgentTerminal = (session: AgentSessionView) => {
    if (actionsBlocked()) return;
    setActiveAgentSessionId(session.id);
    seqRef.current += 1;
    setAgentFocusRequest({ sessionId: session.id, seq: seqRef.current });
    updateNavigation({ sidebarVisible: true, sidebarTab: "agents" });
    setPage("terminal");
  };

  const focusAgentTerminalByID = (sessionId: string) => {
    if (actionsBlocked()) return;
    setActiveAgentSessionId(sessionId);
    seqRef.current += 1;
    setAgentFocusRequest({ sessionId, seq: seqRef.current });
    updateNavigation({ sidebarVisible: true, sidebarTab: "agents" });
    setPage("terminal");
  };

  const agentMap = useAgentMap({
    enabled: agentSessionsEnabled, selectedSessionId: openTerminals.some(pane => pane.laraSessionId === activeAgentSessionId) ? activeAgentSessionId : '',
    isBlocked: () => decisionRef.current || stagePending.current || mapBlocked.current,
    onSelectSession: () => { updateNavigation({sidebarVisible: true, sidebarTab: "agents"}); setPage("terminal"); claimNavigator(); },
    canOpenRoot: (sessionId, terminalId) => openTerminals.some(pane => pane.terminalId === terminalId && pane.laraSessionId === sessionId),
    onOpenRoot: sessionId => {
      // Explicit navigation follows the dialog's close callback in this batch.
      // Check document ownership directly; the render's map guard is still stale.
      if (decisionRef.current || stagePending.current) return;
      setActiveSurface("terminal"); setCompactView("terminal");
      setActiveAgentSessionId(sessionId);
      seqRef.current += 1;
      setAgentFocusRequest({sessionId, seq: seqRef.current});
      updateNavigation({sidebarVisible: true, sidebarTab: "agents"}); setPage("terminal");
    },
  });
  mapBlocked.current = !!agentMap.openSessionId;
  blocked.current = decisionBlocked || mapBlocked.current;
  dismissMap.current = () => agentMap.closeMap({restoreFocus: false});
  useEffect(() => { if (decisionBlocked) dismissMap.current(); }, [decisionBlocked]);

  const { waiting: agentAutoSwitchWaiting } = useAgentAutoSwitch({
    enabled: agentSessionsEnabled && agentAutoSwitch,
    blocked: activeSurface !== "terminal" || decisionBlocked || !!agentMap.openSessionId || page !== "terminal",
    focusSession: focusAgentTerminalByID,
  });

  const resumeAgentTerminal = (session: AgentSessionView, providerLabel: string) => {
    if (actionsBlocked()) return;
    setActiveAgentSessionId(session.id);
    seqRef.current += 1;
    setOpenRequest({ kind: "agent-resume", sessionId: session.id, project: session.projectName, agentId: session.agentId, agentLabel: providerLabel, seq: seqRef.current });
    updateNavigation({ sidebarVisible: true, sidebarTab: "agents" });
    setPage("terminal");
  };

  // From SSH → open a remote SSH session as a tab on the Terminal page.
  const requestSSHTerminal = (host: string) => {
    if (actionsBlocked()) return;
    seqRef.current += 1;
    setOpenRequest({ kind: "ssh", host, seq: seqRef.current });
    setPage("terminal");
  };

  // From Projects → jump to the Cron page with the project filter pre-applied.
  const requestProjectCron = (name: string) => {
    seqRef.current += 1;
    setCronFilter({ project: name, seq: seqRef.current });
    setPage("cron");
  };

  // ----- Command Center wiring -----
  // The capabilities the palette / built-in commands invoke. MainApp owns
  // navigation, terminal open-requests and toasts.
  const cmdCtx: CommandContext = {
    navigate: (p) => {
      if (p === "agents") { updateNavigation({ sidebarVisible: true, sidebarTab: "agents" }); setPage("terminal"); }
      else setPage(p as Page);
    },
    newProject: (kind) => {
      seqRef.current += 1;
      setNewProjectReq({ seq: seqRef.current, kind });
      setPage("projects");
    },
    openTerminal: ({ project, shell }) => {
      seqRef.current += 1;
      setOpenRequest({ kind: "blank", project, shell, seq: seqRef.current });
      setPage("terminal");
    },
    runInTerminal: ({ command, project, shell }) => {
      seqRef.current += 1;
      setOpenRequest({ kind: "command", command, project, shell, seq: seqRef.current });
      setPage("terminal");
    },
    openSSH: (host) => requestSSHTerminal(host),
    flash: (m) => flash(m),
    openTerminals,
    focusTerminal: (terminalId) => {
      seqRef.current += 1;
      setTerminalFocusRequest({ terminalId, seq: seqRef.current });
      setPage("terminal");
    },
    openAgents: () => { updateNavigation({ sidebarVisible: true, sidebarTab: "agents" }); setPage("terminal"); },
    launchAgent: (project, agentId, label) => requestAgentTerminal(project, agentId, label, { mode: "current" }),
    focusAgentSession: focusAgentTerminalByID,
    resumeAgentSession: (sessionId, project, agentId, label) => {
      setActiveAgentSessionId(sessionId);
      seqRef.current += 1;
      setOpenRequest({ kind: "agent-resume", sessionId, project, agentId, agentLabel: label, seq: seqRef.current });
      updateNavigation({ sidebarVisible: true, sidebarTab: "agents" });
      setPage("terminal");
    },
    t,
  };
  const {
    commands, customCommands, paletteHotkey, reload: reloadCommands, runIntent,
    projects, sshHosts, agentProviders, getProjectCommands, ensureProjectCommands, launchAgent,
  } = useCommandCenter(cmdCtx);

  // Global keybindings: the (rebindable) palette key toggles the palette; each
  // custom command's optional hotkey runs it. Custom hotkeys are ignored while
  // the palette is open so typing in the search box stays unaffected.
  const paletteOpenRef = useRef(paletteOpen);
  useEffect(() => { paletteOpenRef.current = paletteOpen; }, [paletteOpen]);
  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      // The shortcut recorder owns the next key, even when it matches an
      // existing app command and its window listener was registered later.
      if (blocked.current || stagePending.current) {
        if (!(e.target instanceof Element) || !e.target.closest('[role="dialog"]')) { e.preventDefault(); e.stopPropagation(); }
        return;
      }
      if (document.querySelector('[data-command-hotkey-capture="true"]')) return;
      if (activeSurface === "documents" && (e.ctrlKey || e.metaKey) && e.key.toLowerCase() === "s") return;
      if (matchesHotkey(e, paletteHotkey)) {
        e.preventDefault();
        e.stopPropagation();
        setPaletteOpen((o) => !o);
        return;
      }
      if (paletteOpenRef.current || activeSurface !== "terminal") return;
      for (const c of customCommands) {
        if (c.hotkey && matchesHotkey(e, c.hotkey)) {
          e.preventDefault();
          e.stopPropagation();
          void c.run();
          return;
        }
      }
    };
    // Capture before xterm's textarea consumes control keys or sends them to
    // the PTY. Recognized app shortcuts must stay with the app.
    window.addEventListener("keydown", handler, true);
    return () => window.removeEventListener("keydown", handler, true);
  }, [paletteHotkey, customCommands, activeSurface]);

  // The OS-wide global hotkey opens a separate command-bar window; when the
  // user picks a command there, the main process forwards it here as an intent
  // for the shared executor (which owns navigation, terminals, services, etc.).
  useEffect(() => {
    const off = EventsOn("commandcenter:intent", (json: string) => {
      if (blocked.current || stagePending.current) return;
      try { runIntent(JSON.parse(json) as CommandIntent); } catch (e) { console.error(e); }
    });
    return () => off();
  }, [runIntent]);

  return (
    <div className="h-screen w-screen flex flex-col overflow-hidden">
      {/* Drag region invisível no topo + controles à direita */}
      <div
        {...(decisionBlocked || agentMap.openSessionId ? {inert: ""} : {})}
        className="h-9 flex items-center justify-end px-2 flex-shrink-0"
        // @ts-ignore — Wails CSS drag hint
        style={{ "--wails-draggable": "drag" }}
      >
        {/* Toast is portaled to <body> and sits ABOVE modal backdrops (z-50):
            flash feedback fired from inside a modal (validation errors, saves)
            must stay readable — the backdrop dims/blurs everything under it. */}
        {toast && createPortal(
          <div
            className="fixed top-10 right-3 z-[100] glass px-4 py-2 text-xs text-accent2
                       border border-accent/50 shadow-[0_8px_30px_rgba(0,0,0,0.6)]
                       max-w-[420px] break-words pointer-events-none"
          >
            {toast}
          </div>,
          document.body,
        )}
        {agentSessionsEnabled && <NotificationBell onOpenSession={focusAgentTerminalByID} onOpenAgents={() => { updateNavigation({ sidebarVisible: true, sidebarTab: "agents" }); setPage("terminal"); }} flash={flash} />}
        <button
          type="button"
          onClick={() => { if (!actionsBlocked()) setPortsOpen(true); }}
          title={t("terminal.app.portsInUseTooltip")}
          className="w-8 h-7 mr-1 rounded-md hover:bg-white/10 flex items-center justify-center text-white/70 hover:text-white transition-colors"
          // @ts-ignore — Wails CSS hint
          style={{ "--wails-draggable": "no-drag" }}
        >
          <Network className="w-4 h-4" />
        </button>
        <button
          type="button"
          onClick={() => { if (!actionsBlocked()) setLogsOpen(true); }}
          title={t("terminal.app.serviceLogsTooltip")}
          className="w-8 h-7 mr-1 rounded-md hover:bg-white/10 flex items-center justify-center text-white/70 hover:text-white transition-colors"
          // @ts-ignore — Wails CSS hint
          style={{ "--wails-draggable": "no-drag" }}
        >
          <ScrollText className="w-4 h-4" />
        </button>
        <WindowControls />
      </div>

      <StageDirtyDialog decision={stage.decision} onChoose={stage.choose} />
      <DocumentCloseDialog decision={documents.decision} onDecision={documents.resolveDecision} onClaimDocuments={claimDocuments} />
      <ServiceLogsModal open={logsOpen} onClose={() => setLogsOpen(false)} />
      <PortsModal open={portsOpen} onClose={() => setPortsOpen(false)} />
      <CommandPalette
        open={paletteOpen}
        commands={commands}
        launcher={{
          projects,
          sshHosts,
          agentProviders,
          getProjectCommands,
          ensureProjectCommands,
          runInTerminal: cmdCtx.runInTerminal,
          openSSH: cmdCtx.openSSH,
          launchAgent,
          t,
        }}
        onClose={() => setPaletteOpen(false)}
      />

      <div className="flex-1 min-h-0 flex gap-2 px-2 pb-2" {...(decisionBlocked || agentMap.openSessionId ? { inert: "" } : {})}>
        <Sidebar
          collapsed={navigation.primaryCollapsed}
          onToggleCollapsed={() => updateNavigation({ primaryCollapsed: !navigation.primaryCollapsed })}
          current={page}
          onChange={page => { if (!actionsBlocked()) setPage(page); }}
          updateAvailable={updateAvail}
          status={cloudStatus}
          onGoToPlan={goToCloud}
        />
        <main className="flex-1 min-w-0 relative">
          <div className="absolute inset-0" style={{ display: page === "dashboard" ? "block" : "none" }}>
            <Dashboard flash={flash} />
          </div>
          <div className="absolute inset-0" style={{ display: page === "projects" ? "block" : "none" }}>
            <Projects flash={flash} onOpenRuntimeDownloads={openRuntimeDownloads} onOpenLocalHttps={goToLocalHttps} onOpenTerminal={requestProjectTerminal} onRunCommand={requestProjectCommand} onOpenAgent={requestAgentTerminal} onOpenCron={requestProjectCron} openNewProject={newProjectReq ?? undefined} agentSessionsEnabled={agentSessionsEnabled} />
          </div>
          <div className="absolute inset-0" style={{ display: page === "ssh" ? "block" : "none" }}>
            <SSH flash={flash} onConnect={requestSSHTerminal} />
          </div>
          <div className="absolute inset-0" style={{ display: page === "cron" ? "block" : "none" }}>
            <Cron flash={flash} filterRequest={cronFilter} />
          </div>
          <div className="absolute inset-0" style={{ display: page === "deployments" ? "block" : "none" }}>
            <Deployments flash={flash} status={cloudStatus} />
          </div>
          <div className="absolute inset-0" style={{ display: page === "downloads" ? "block" : "none" }}>
            <Downloads flash={flash} runtimeRequest={runtimeDownloadRequest} />
          </div>
          <div className="absolute inset-0" style={{ display: page === "settings" ? "block" : "none" }}>
            <Settings
              flash={flash}
              onUpdateAvailabilityChange={setUpdateAvail}
              initialTab={settingsInitialTab}
              scrollToPlan={scrollToPlan}
              onConsumed={() => setScrollToPlan(false)}
              onStatusChange={updateCloudStatus}
              onCommandCenterChange={reloadCommands}
              agentSessionsEnabled={agentSessionsEnabled}
              onAgentSessionsEnabledChange={setAgentSessionsEnabled}
            />
          </div>
          <div className="absolute inset-0" style={{ display: page === "terminal" ? "block" : "none" }}>
            <div ref={navigationArea} data-testid="workspace-navigation-area" className="glass workspace-frame relative flex h-full min-h-0 overflow-hidden p-0">
              <WorkspaceNavigator preferences={navigation} active={page === 'terminal'} compact={drawer}
                onUpdate={patch => { if (patch.sidebarTab) selectNavigator(patch.sidebarTab); else updateNavigation(patch); }} onClaimNavigator={claimNavigator}
                panels={{
                  agents: <Agents active={panelActive('agents')} flash={flash} enabled={agentSessionsEnabled} enabling={agentFeatureSaving}
                    onEnable={() => void enableAgentSessions()} selectedSessionId={activeAgentSessionId} onSelect={session => setActiveAgentSessionId(session.id)}
                    onOpenMap={session => agentMap.openMap(session.id)} onOpen={focusAgentTerminal} onResume={resumeAgentTerminal}
                    onFresh={(project, agentId, label, additionalDirectories) => requestAgentTerminal(project, agentId, label, { mode: 'current', additionalDirectories })}
                    onLaunch={requestAgentTerminal} autoSwitch={agentAutoSwitch} onAutoSwitchChange={setAgentAutoSwitch} autoSwitchWaiting={agentAutoSwitchWaiting} />,
                  files: <WorkspaceFilesPanel selection={workspaceSelection} context={workspaceContext} active={panelActive('files')}
                    onSelectProject={projectName => { setManualProjectContext({ kind: 'project', projectName }); claimNavigator(); }} onOpenDocument={openDocument} onClaimNavigator={claimNavigator} />,
                  git: <SourceControlPanel controller={sourceControl} onClaimNavigator={claimNavigator} onOpenDocument={openDocument}
                    onOpenReview={(ref, expected, entryId, layer, root) => { if (revealEditor()) void reviews.open(ref, expected, entryId, layer, root); }} />,
                }} />
              <section className="flex min-w-0 flex-1 flex-col">
                <header className="flex h-11 shrink-0 items-center gap-1 overflow-x-auto border-b border-white/[0.08] px-2 text-xs text-white/60">
                  {!navigation.sidebarVisible && <button type="button" aria-label={t('workspace.navigation.show')} title={t('workspace.navigation.show')} aria-controls="workspace-navigator" aria-expanded={false}
                    className="workspace-icon-button shrink-0" onClick={() => selectNavigator(navigation.sidebarTab)}><PanelLeftOpen aria-hidden="true" className="h-4 w-4"/></button>}
                  <button type="button" aria-pressed={documents.visible} aria-label={t(documents.visible ? 'workspace.navigation.hideEditor' : 'workspace.navigation.showEditor')}
                    className="ml-auto shrink-0 rounded px-2 py-1 hover:bg-white/10" onClick={() => {
                      if (actionsBlocked()) return;
                      if (documents.visible) { documents.setVisible(false); changeSurface('terminal'); } else revealEditor();
                    }}>{t('workspace.navigation.editor')}</button>
                </header>
                <div ref={workspaceArea} data-testid="workspace-surfaces" className="relative flex min-h-0 flex-1 flex-col">
                  {compact && documents.visible && <div className="flex shrink-0 gap-2 border-b border-white/10 p-1 text-xs" role="tablist">
                    <button role="tab" aria-selected={compactView === "terminal"} onClick={() => changeSurface("terminal")}>{t("terminal.app.terminalLabel")}</button>
                    <button role="tab" aria-selected={compactView === "documents"} onClick={() => changeSurface("documents")}>{t("documents.title")}</button>
                  </div>}
                  <div className="flex min-h-0 flex-1">
                    <div data-testid="terminal-surface" className="min-w-0 flex-1" style={{ display: compact && documents.visible && compactView === "documents" ? "none" : "block" }}>
                      <TerminalWorkspace
                        openRequest={openRequest}
                        focusAgentRequest={agentFocusRequest}
                        focusTerminalRequest={terminalFocusRequest}
                        onOpenTerminalsChange={setOpenTerminals}
                        onFocusedAgentSessionChange={setActiveAgentSessionId}
                        activeSurface={documents.decision ? "documents" : stage.decision || agentMap.openSessionId ? "navigator" : activeSurface}
                        onActiveSurfaceChange={changeSurface}
                        onWorkspaceSelectionChange={setWorkspaceSelection}
                        openFilesRequest={openFilesRequest}
                        onOpenWorkspaceFiles={openWorkspaceFiles}
                        flash={flash}
                        active={page === "terminal" && (!compact || !documents.visible || compactView === "terminal")}
                      />
                    </div>
                    <div role="separator" aria-label={t("documents.title")} aria-orientation="vertical" tabIndex={0}
                      aria-valuemin={20} aria-valuemax={80} aria-valuenow={documents.panelPercent}
                      className="w-1.5 shrink-0 cursor-col-resize bg-white/10 hover:bg-accent/60 focus:bg-accent/60"
                      style={{ display: documents.visible && !compact ? "block" : "none", touchAction: "none" }}
                      onPointerDown={event => { if (event.button !== 0) return; resizePointer.current = event.pointerId; event.currentTarget.setPointerCapture(event.pointerId); claimDocuments(); }}
                      onPointerMove={event => {
                        if (resizePointer.current !== event.pointerId) return;
                        const rect = workspaceArea.current?.getBoundingClientRect();
                        if (rect?.width) documents.setPanelPercent(Math.min(80, Math.max(20, 100 * (rect.right - event.clientX) / rect.width)));
                      }}
                      onPointerUp={() => { resizePointer.current = null; }} onPointerCancel={() => { resizePointer.current = null; }}
                      onKeyDown={event => { if (event.key === "ArrowLeft" || event.key === "ArrowRight") { event.preventDefault(); claimDocuments(); documents.setPanelPercent(Math.min(80, Math.max(20, documents.panelPercent + (event.key === "ArrowLeft" ? 2 : -2)))); } }}
                    />
                    <div ref={documentSurface} data-testid="document-surface" className="min-w-0 shrink-0" style={{ width: compact ? "100%" : `${Math.max(400, availableWidth * documents.panelPercent / 100)}px`, display: documents.visible && (!compact || compactView === "documents") ? "block" : "none" }}>
                      <DocumentWorkspace controller={documents} reviews={reviews} context={workspaceContext} active={page === 'terminal' && documents.visible && (!compact || compactView === 'documents')} onClaimDocuments={claimDocuments} />
                    </div>
                  </div>
                </div>
              </section>
            </div>
          </div>
        </main>
      </div>
      {agentSessionsEnabled && <div {...(decisionBlocked || agentMap.openSessionId ? {inert: ""} : {})}><AgentUsageBar leading={agentMap.chip} /></div>}
      {agentMap.dialog}
    </div>
  );
}
