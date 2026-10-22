import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { useDialogs } from "../components/dialogs/DialogProvider";
import { Plus, Folder, ExternalLink, Trash2, RefreshCw, Globe, ChevronDown, Code2, FileCode, Lock, Unlock, TerminalSquare, Clock, Play, Square, Terminal, X, Tag, ListOrdered, Stethoscope, Copy } from "lucide-react";
import { createPortal } from "react-dom";
import PortalMenu from "../components/PortalMenu";
import VhostEditorModal from "../components/VhostEditorModal";
import {
  ListProjects, OpenProjectFolder, OpenURL,
  GetProjectsConfig, SetProjectsTLD, ListPhpVersions, SetProjectPhp, SetProjectSSL,
  LocalCAStatus, TrustLocalCA, NotifyLocalCAUntrusted,
  ListTerminals, SetDefaultTerminal, SetProjectTerminal,
  ListNodeVersions, SetProjectNode,
  StartDevServer, StopDevServer, GetDevServerStatus,
  StartQueueWorker, StartQueueWorkerWithArgs, StopQueueWorker, RestartQueueWorker,
  GetQueueWorkerStatus, GetLastQueueArgs, GetResolvedPhpExe,
  SetProjectProxyPort, GetProjectDevCommand, SetProjectDevCommand,
  MakeDevCommandWindowsSafe,
  ReloadStack,
  ServiceStatus,
  ListEditors, SetProjectEditor, SetDefaultEditor,
  SetProjectCustomEditor, SetDefaultCustomEditor, OpenProjectInEditor,
  ListAgentProfiles,
} from "../../wailsjs/go/main/App";
import { ClipboardSetText, EventsOn } from "../../wailsjs/runtime";
import TypeBadge from "../components/TypeBadge";
import JSBadge from "../components/JSBadge";
import NewProjectModal from "../components/NewProjectModal";
import DeleteProjectDialog from "../components/DeleteProjectDialog";
import DevServerLogsModal from "../components/DevServerLogsModal";
import ProjectsBrowser from "../components/ProjectsBrowser";
import ProjectTagsModal from "../components/ProjectTagsModal";
import ProjectCommands from "../components/ProjectCommands";
import SiteDoctorModal from "../components/SiteDoctorModal";
import AgentLauncherMenu, { type AgentID, type AgentProfile, type AgentLaunchDraft } from "../components/AgentLauncherMenu";
import { nextProxyPort, showProxyToggle } from "./projectProxy";
import { KIND_LABELS } from "../lib/installKinds";
import { useT } from "../lib/i18n";
import { ProjectRuntimeSelector, type ProjectRuntimeKind, type ProjectRuntimeResolution } from '../components/ProjectRuntimeSelector';
import './Projects.css';

type Project = {
  name: string; url: string; path: string; kind: string; docRoot: string;
  phpVersion: string; nodeVersion: string; ssl: boolean;
  phpRuntime?: ProjectRuntimeResolution; nodeRuntime?: ProjectRuntimeResolution;
  jsFramework: string; packageManager: string; proxyPort: number;
  tags: string[];
  paused?: boolean;
};
type ProjectsCfg = {
  tld: string; root: string; phpVersions: Record<string, string>; ssl: Record<string, boolean>;
  devCommands?: Record<string, string>; defaultDevCommand?: string;
};
type PhpVersion = { version: string; path: string; active: boolean };
type NodeVersion = { version: string; path: string; active: boolean };
type TerminalProfile = { kind: string; label: string; available: boolean; path: string };
type TerminalsInfo = { profiles: TerminalProfile[]; defaultKind: string; projectKinds: Record<string, string> };
type EditorProfile = { kind: string; label: string; available: boolean; path: string };
type EditorsInfo = { profiles: EditorProfile[]; defaultKind: string; defaultCustomCmd: string; projectKinds: Record<string, string>; projectCustomCmds: Record<string, string> };
type DevStatus = { running: boolean; pid: number; port: number; script: string };
type QueueStatus = { running: boolean; pid: number; args: string[] };
type SSLAction = { busy: boolean; error: boolean; message: string };
const COMMON_TLDS = ["test", "local", "dev", "localhost"];

// The services that can serve a project. They share one exclusivity group,
// so at most one of them is ever running.
const WEB_SERVERS = ["nginx", "apache", "caddy"];

export default function Projects({
  flash,
  onOpenLocalHttps,
  onOpenTerminal,
  onRunCommand,
  onOpenAgent,
  onOpenCron,
  onOpenRuntimeDownloads,
  openNewProject,
  agentSessionsEnabled = false,
}: {
  flash: (m: string) => void;
  onOpenLocalHttps: () => void;
  onOpenTerminal: (project: string, kind?: string) => void;
  onRunCommand: (project: string, command: string, kind?: string) => void;
  onOpenAgent: (project: string, agentId: AgentID, agentLabel: string, options: AgentLaunchDraft) => void;
  onOpenCron: (project: string) => void;
  onOpenRuntimeDownloads?: (kind: ProjectRuntimeKind, version?: string) => void;
  openNewProject?: { seq: number; kind?: string };
  agentSessionsEnabled?: boolean;
}) {
  const t = useT();
  const [items, setItems] = useState<Project[]>([]);
  const dialogs = useDialogs();
  const [cfg, setCfg] = useState<ProjectsCfg>({ tld: "test", root: "", phpVersions: {}, ssl: {} });
  const [phpVersions, setPhpVersions] = useState<PhpVersion[]>([]);
  const [doctorSelection, setDoctorSelection] = useState<{ projectName: string | null } | null>(null);
  const [nodeVersions, setNodeVersions] = useState<NodeVersion[]>([]);
  const [tldOpen, setTldOpen] = useState(false);
  const [phpOpen, setPhpOpen] = useState<string | null>(null);
  const [nodeOpen, setNodeOpen] = useState<string | null>(null);
  const [termOpen, setTermOpen] = useState<string | null>(null);
  const [terminals, setTerminals] = useState<TerminalsInfo>({ profiles: [], defaultKind: "", projectKinds: {} });
  const [editorOpen, setEditorOpen] = useState<string | null>(null);
  const [editors, setEditors] = useState<EditorsInfo>({ profiles: [], defaultKind: "", defaultCustomCmd: "", projectKinds: {}, projectCustomCmds: {} });
  const [modalOpen, setModalOpen] = useState(false);
  // Pre-selected project type when the modal is opened from a palette entry
  // (e.g. "New Laravel project"). undefined → modal uses its own default.
  const [newProjKind, setNewProjKind] = useState<string | undefined>(undefined);
  // Opened from the command palette ("New Project"): bump seq → open the modal.
  const lastNewProjSeq = useRef(0);
  useEffect(() => {
    if (openNewProject && openNewProject.seq !== lastNewProjSeq.current) {
      lastNewProjSeq.current = openNewProject.seq;
      setNewProjKind(openNewProject.kind);
      setModalOpen(true);
    }
  }, [openNewProject?.seq]);
  const [editingVhost, setEditingVhost] = useState<string | null>(null);
  const [deleting, setDeleting] = useState<Project | null>(null);
  const [reloading, setReloading] = useState(false);
  const [sslActions, setSslActions] = useState<Record<string, SSLAction>>({});
  const pendingSSL = useRef(new Set<string>());
  const [devStatus, setDevStatus] = useState<Record<string, DevStatus>>({});
  const [queueStatus, setQueueStatus] = useState<Record<string, QueueStatus>>({});
  const [queueMenuOpen, setQueueMenuOpen] = useState<string | null>(null);
  const [paramsModal, setParamsModal] = useState<Project | null>(null);
  const [devMenuOpen, setDevMenuOpen] = useState<string | null>(null);
  const [devCmdModal, setDevCmdModal] = useState<Project | null>(null);
  const [logsFor, setLogsFor] = useState<string | null>(null);
  const [selectedName, setSelectedName] = useState<string | null>(null);
  const selectionFocusRef = useRef<HTMLElement | null>(null);
  const selectProject = useCallback((name: string | null) => {
    if (name !== null) selectionFocusRef.current = document.activeElement instanceof HTMLButtonElement ? document.activeElement : null;
    setSelectedName(name);
  }, []);
  const narrowDetails = useNarrowProjectDetails();
  // Which web server is actually serving, if any. Only one of the three can
  // run at a time (they share the "web" exclusivity group), and a project is
  // reachable through whichever it is — deriving this from nginx alone would
  // grey out every project the moment the user switches to Apache or Caddy.
  const [webServer, setWebServer] = useState<string | null>(null);
  const [tagFilter, setTagFilter] = useState<string>("__all__");
  const [editingTags, setEditingTags] = useState<Project | null>(null);
  const [agents, setAgents] = useState<AgentProfile[]>([]);
  const [agentOpen, setAgentOpen] = useState<string | null>(null);

  const tagIndex = useMemo(() => {
    const set = new Set<string>();
    let hasUntagged = false;
    for (const p of items) {
      const ts = (p.tags ?? []).map((tag) => tag.trim()).filter(Boolean);
      if (ts.length === 0) hasUntagged = true;
      for (const tag of ts) set.add(tag);
    }
    return { sorted: [...set].sort((a, b) => a.localeCompare(b)), hasUntagged };
  }, [items]);

  const selectedProject = items.find(project => project.name === selectedName);
  const listRef = useRef<HTMLDivElement>(null);
  const headerRef = useRef<HTMLElement>(null);

  useEffect(() => {
    setPhpOpen(null); setNodeOpen(null); setTermOpen(null); setEditorOpen(null);
    setDevMenuOpen(null); setQueueMenuOpen(null); setAgentOpen(null); setTldOpen(false);
  }, [selectedName]);

  useEffect(() => {
    const background = [listRef.current, headerRef.current].filter((node): node is HTMLElement => node !== null);
    for (const node of background) node.inert = narrowDetails && !!selectedProject;
    return () => { for (const node of background) node.inert = false; };
  }, [narrowDetails, selectedProject]);

  const refresh = () => ListProjects().then((p: Project[]) => setItems(p ?? []));
  const refreshCfg = () => GetProjectsConfig().then((c: ProjectsCfg) => setCfg(c));
  const refreshPhp = () => ListPhpVersions().then((v: PhpVersion[]) => setPhpVersions(v ?? []));
  const refreshNode = () => ListNodeVersions().then((v: NodeVersion[]) => setNodeVersions(v ?? []));
  const refreshTerminals = () => ListTerminals().then((info: TerminalsInfo) => setTerminals(info));
  const refreshEditors = () => ListEditors().then((e: EditorsInfo) => setEditors(e));
  const refreshDevStatus = async (name: string) => {
    const s: DevStatus = await GetDevServerStatus(name);
    setDevStatus((prev) => ({ ...prev, [name]: s }));
  };
  const refreshQueueStatus = async (name: string) => {
    try {
      const s = (await GetQueueWorkerStatus(name)) as QueueStatus;
      setQueueStatus((prev) => ({ ...prev, [name]: s }));
    } catch { /* ignore */ }
  };

  const handleReload = async () => {
    if (reloading) return;
    setReloading(true);
    try {
      await ReloadStack();
      await Promise.all([refresh(), refreshPhp()]);
      flash(t("projects.flash.reloaded"));
    } catch (e) {
      flash(t("projects.errors.generic", { message: String(e) }));
    } finally {
      setReloading(false);
    }
  };

  useEffect(() => {
    refresh();
    refreshCfg();
    refreshTerminals();
    refreshEditors();
    refreshPhp();
    refreshNode();
    if (agentSessionsEnabled) {
      ListAgentProfiles()
        .then((profiles) => setAgents((profiles ?? []).map((profile) => ({ ...profile, id: profile.id as AgentID }))))
        .catch(() => {});
    } else {
      setAgents([]);
    }

    const onAdded = (name: string) => { flash(t("projects.flash.projectAdded", { name })); refresh(); };
    const onRemoved = (name: string) => { flash(t("projects.flash.projectRemoved", { name })); refresh(); };
    const onTld = () => { refreshCfg(); refresh(); };
    const onProjectError = (msg: string) => flash(t("projects.errors.generic", { message: msg }));

    const onPhpChanged = () => refresh();
    const onSslChanged = () => { refreshCfg(); refresh(); };
    // onInstalled below (the generic install:installed listener) suppresses
    // its own toast for kind === "php" specifically because this handler
    // already flashes one — if this stops flashing (or is removed), drop
    // that suppression too or PHP installs will stop toasting silently.
    const onPhpInstalled = (data: { version?: string }) => {
      refreshPhp();
      refresh();
      if (data?.version) flash(t("projects.flash.phpInstalled", { version: data.version }));
    };

    const onNodeChanged = () => refresh();
    const onProxyChanged = () => refresh();
    const onDevStatus = (name: string) => refreshDevStatus(name);
    const onQueueStatus = (name: string) => refreshQueueStatus(name);
    // Generic counterpart to php:installed: fires for every installed kind
    // (Node, Cmder, Git Bash, …), not just PHP, so the Node dropdown and the
    // terminal-kind dropdown stop reading "not installed" the moment an
    // install finishes instead of waiting for a page reload.
    const onInstalled = (data: { kind?: string; version?: string }) => {
      refreshNode();
      refreshTerminals();
      if (data?.kind === 'php' || data?.kind === 'nodejs') refresh();
      // php:installed already flashes its own message — avoid a duplicate.
      if (data?.kind && data?.version && data.kind !== "php") {
        flash(t("projects.flash.kindInstalled", { kind: KIND_LABELS[data.kind] ?? data.kind, version: data.version }));
      }
    };

    const refreshWebServer = async () => {
      for (const name of WEB_SERVERS) {
        try {
          if (await ServiceStatus(name) === "running") {
            setWebServer(name);
            return;
          }
        } catch { /* this server may not be installed */ }
      }
      setWebServer(null);
    };
    refreshWebServer();

    // Per-listener cancelers from EventsOn — a global EventsOff("<name>")
    // removes EVERY listener for that event app-wide, which permanently
    // deafens the other kept-alive pages sharing it (e.g. service:status
    // also drives the Dashboard cards).
    const offs = [
      EventsOn("project:added", onAdded),
      EventsOn("project:removed", onRemoved),
      EventsOn("project:tld-changed", onTld),
      EventsOn("project:error", onProjectError),
      EventsOn("project:php-changed", onPhpChanged),
      EventsOn("project:node-changed", onNodeChanged),
      EventsOn("project:runtime-changed", () => { refresh(); refreshPhp(); refreshNode(); }),
      EventsOn("stack:changed", () => { refresh(); refreshPhp(); refreshNode(); }),
      EventsOn("project:proxy-changed", onProxyChanged),
      EventsOn("project:ssl-changed", onSslChanged),
      EventsOn("project:tags-changed", refresh),
      EventsOn("php:installed", onPhpInstalled),
      EventsOn("install:installed", onInstalled),
      EventsOn("devserver:status", onDevStatus),
      EventsOn("queueworker:status", onQueueStatus),
      // Re-derive from all three rather than trusting one event: switching
      // servers stops one and starts another, and the events can arrive in
      // either order.
      EventsOn("service:status", (name: string) => {
        if (WEB_SERVERS.includes(name)) void refreshWebServer();
      }),
    ];

    return () => offs.forEach((off) => off());
  }, [agentSessionsEnabled]);

  // Process state belongs to the selected project, including projects with
  // package scripts but no recognized framework. Refresh on every selection,
  // rather than using the list length as a proxy for project identity.
  useEffect(() => {
    if (!selectedProject) return;
    let cancelled = false;
    const name = selectedProject.name;
    GetDevServerStatus(name).then((status: DevStatus) => {
      if (!cancelled) setDevStatus(previous => ({ ...previous, [name]: status }));
    }).catch(() => {});
    if (selectedProject.kind === 'laravel') GetQueueWorkerStatus(name).then((status: QueueStatus) => {
      if (!cancelled) setQueueStatus(previous => ({ ...previous, [name]: status }));
    }).catch(() => {});
    return () => { cancelled = true; };
  }, [selectedProject]);

  const setTLD = async (tld: string) => {
    setTldOpen(false);
    try {
      await SetProjectsTLD(tld);
    } catch (e) { flash(t("projects.errors.generic", { message: String(e) })); }
  };

  const setCustomTLD = async () => {
    const v = await dialogs.prompt({ title: t("projects.tld.promptTitle"), label: t("projects.tld.promptLabel"), defaultValue: cfg.tld });
    if (v) setTLD(v);
  };

  const toggleSSL = async (p: Project) => {
    // A ref also blocks a second activation before React renders disabled.
    if (pendingSSL.current.has(p.name)) return;
    pendingSSL.current.add(p.name);
    const enabling = !p.ssl;
    setSslActions(previous => ({ ...previous, [p.name]: {
      busy: true, error: false, message: t(enabling ? 'projects.https.enabling' : 'projects.https.disabling'),
    } }));
    try {
      await SetProjectSSL(p.name, enabling);
      // A successful call confirms the saved preference and applied vhost.
      // Reflect that result even when the runtime event or a list read fails.
      setItems(previous => previous.map(item => item.name === p.name ? {
        ...item, ssl: enabling, url: item.url.replace(/^https?:\/\//, enabling ? 'https://' : 'http://'),
      } : item));
      setCfg(previous => ({ ...previous, ssl: { ...previous.ssl, [p.name]: enabling } }));
      const message = t(enabling ? "projects.flash.sslEnabled" : "projects.flash.sslDisabled", { name: p.name });
      setSslActions(previous => ({ ...previous, [p.name]: { busy: true, error: false, message } }));
      flash(message);
      if (enabling) await offerLocalCATrust(p);
    } catch (e) {
      const message = t("projects.errors.generic", { message: String(e) });
      setSslActions(previous => ({ ...previous, [p.name]: { busy: true, error: true, message } }));
      flash(message);
    } finally {
      pendingSSL.current.delete(p.name);
      setSslActions(previous => ({ ...previous, [p.name]: { ...previous[p.name], busy: false } }));
    }
  };

  const offerLocalCATrust = async (p: Project) => {
    // An installed copy trusted the CA automatically as part of the call
    // above — nothing more to do. A portable copy never does that on its
    // own (see Settings → Services → Local HTTPS), so ask right here,
    // where the user is, instead of sending them looking for that card.
    // LocalCAStatus is the one call for all of this: whether this copy is
    // portable, whether a CA exists, and whether it's already trusted.
    // Every path out of here that cannot offer the prompt still has to leave
    // the user somewhere. Silence means they enable SSL, hit a browser
    // warning, and have nothing connecting the two.
    const sendToSettings = async (why: string) => {
      setSslActions(previous => ({ ...previous, [p.name]: { busy: true, error: true, message: why } }));
      const go = await dialogs.confirm({
        title: t("projects.sslCertDialog.title"),
        message: `${why}\n\n${t("projects.sslCertDialog.message")}`,
        confirmLabel: t("projects.sslCertDialog.confirmLabel"),
        cancelLabel: t("projects.sslCertDialog.cancelLabel"),
      });
      if (go) onOpenLocalHttps();
    };

    let status;
    try {
      status = await LocalCAStatus();
    } catch {
      await sendToSettings(t("projects.sslCertDialog.checkFailedReason"));
      return;
    }
    if (!status.portable || !status.exists || status.trusted) return;
    if (status.checkError) {
      await sendToSettings(t("projects.sslCertDialog.checkFailedReason"));
      return;
    }

    const host = `${p.name}.${cfg.tld}`;
    setSslActions(previous => ({ ...previous, [p.name]: {
      busy: true, error: false, message: t('projects.https.untrusted'),
    } }));
    const trust = await dialogs.confirm({
      title: t("projects.trustCaDialog.title"),
      message: (
        <>
          {t("projects.trustCaDialog.introBeforeHost")} <code className="text-accent2">{host}</code>{t("projects.trustCaDialog.afterHost")}
        </>
      ),
      confirmLabel: t("projects.trustCaDialog.confirmLabel"),
      cancelLabel: t("projects.trustCaDialog.cancelLabel"),
    });

    if (!trust) {
      NotifyLocalCAUntrusted(p.name).catch(() => {});
      return;
    }

    try {
      await TrustLocalCA();
      const message = t("projects.trustCaDialog.trustedFlash", { host });
      setSslActions(previous => ({ ...previous, [p.name]: { busy: true, error: false, message } }));
      flash(message);
    } catch (e) {
      const message = t("projects.trustCaDialog.trustFailedFlash", { message: String(e) });
      setSslActions(previous => ({ ...previous, [p.name]: { busy: true, error: true, message } }));
      flash(message);
      NotifyLocalCAUntrusted(p.name).catch(() => {});
    }
  };

  const setPhp = async (project: string, version: string) => {
    setPhpOpen(null);
    try {
      await SetProjectPhp(project, version);
      flash(t("projects.flash.phpVersionSet", { project, version: version || t("projects.runtime.automatic") }));
    } catch (e) { flash(t("projects.errors.generic", { message: String(e) })); }
  };

  const setNode = async (project: string, version: string) => {
    setNodeOpen(null);
    try {
      await SetProjectNode(project, version);
      flash(t("projects.flash.nodeVersionSet", { project, version: version || t("projects.runtime.automatic") }));
    } catch (e) { flash(t("projects.errors.generic", { message: String(e) })); }
  };

  const toggleDev = async (p: Project) => {
    const st = devStatus[p.name];
    try {
      if (st?.running) {
        await StopDevServer(p.name);
        flash(t("projects.flash.devServerStopped", { name: p.name }));
      } else {
        await StartDevServer(p.name);
        flash(t("projects.flash.devServerStarting", { name: p.name }));
        setLogsFor(p.name);
      }
    } catch (e) { flash(t("projects.errors.generic", { message: String(e) })); }
  };

  const toggleQueue = async (p: Project) => {
    const st = queueStatus[p.name];
    try {
      if (st?.running) {
        await StopQueueWorker(p.name);
        flash(t("projects.flash.queueWorkerStopped", { name: p.name }));
      } else {
        await StartQueueWorker(p.name);
        flash(t("projects.flash.queueWorkerStarting", { name: p.name }));
      }
    } catch (e) { flash(t("projects.errors.generic", { message: String(e) })); }
  };

  const restartQueue = async (p: Project) => {
    setQueueMenuOpen(null);
    try {
      await RestartQueueWorker(p.name);
      flash(t("projects.flash.queueWorkerRestarting", { name: p.name }));
    } catch (e) { flash(t("projects.errors.generic", { message: String(e) })); }
  };

  const openQueueParams = (p: Project) => {
    setQueueMenuOpen(null);
    setParamsModal(p);
  };

  const startQueueWithArgs = async (p: Project, extraArgs: string) => {
    try {
      await StartQueueWorkerWithArgs(p.name, extraArgs);
      flash(t("projects.flash.queueWorkerStarting", { name: p.name }));
      setParamsModal(null);
    } catch (e) { flash(t("projects.errors.generic", { message: String(e) })); }
  };

  const openDevCommand = (p: Project) => {
    setDevMenuOpen(null);
    setDevCmdModal(p);
  };

  const saveDevCommand = async (p: Project, cmd: string) => {
    try {
      await SetProjectDevCommand(p.name, cmd);
      flash(t(cmd ? "projects.flash.devCommandUpdated" : "projects.flash.devCommandReset", { name: p.name }));
      setDevCmdModal(null);
      refreshCfg();
    } catch (e) { flash(t("projects.errors.generic", { message: String(e) })); }
  };

  const wireProxy = async (p: Project) => {
    const next = nextProxyPort(p.proxyPort, devStatus[p.name]?.port);
    if (next === null) {
      flash(t("projects.flash.noPortDetected", { name: p.name }));
      return;
    }
    try {
      await SetProjectProxyPort(p.name, next);
      flash(next > 0
        ? t("projects.flash.proxyWired", { name: p.name, port: next })
        : t("projects.flash.proxyDisabled", { name: p.name }));
    } catch (e) { flash(t("projects.errors.generic", { message: String(e) })); }
  };

  const defaultPhp = phpVersions.find((v) => v.active)?.version ?? t('projects.common.defaultLabel');
  const defaultNode = nodeVersions.find((v) => v.active)?.version ?? t('projects.common.defaultLabel');

  const detailFor = (p: Project) => (
    <ProjectDetails
      p={p}
      onClose={() => setSelectedName(null)}
      devStatus={devStatus[p.name]}
      phpVersions={phpVersions}
      nodeVersions={nodeVersions}
      terminals={terminals}
      agents={agents}
      agentSessionsEnabled={agentSessionsEnabled}
      flash={flash}
      agentOpen={agentOpen === p.name}
      setAgentOpen={(value) => setAgentOpen(value ? p.name : null)}
      phpOpen={phpOpen === p.name}
      setPhpOpen={(v) => setPhpOpen(v ? p.name : null)}
      nodeOpen={nodeOpen === p.name}
      setNodeOpen={(v) => setNodeOpen(v ? p.name : null)}
      termOpen={termOpen === p.name}
      setTermOpen={(v) => setTermOpen(v ? p.name : null)}
      editors={editors}
      editorOpen={editorOpen === p.name}
      setEditorOpen={(v) => setEditorOpen(v ? p.name : null)}
      defaultPhp={defaultPhp}
      defaultNode={defaultNode}
      onSetPhp={(ver) => setPhp(p.name, ver)}
      onSetNode={(ver) => setNode(p.name, ver)}
      onOpenRuntimeDownloads={onOpenRuntimeDownloads}
      onToggleSSL={() => toggleSSL(p)}
      sslAction={sslActions[p.name]}
      onOpenURL={() => OpenURL(p.url)}
      onOpenFolder={() => OpenProjectFolder(p.name)}
      onOpenCron={() => onOpenCron(p.name)}
      onOpenDoctor={() => setDoctorSelection({ projectName: p.name })}
      onOpenConfig={() => setEditingVhost(p.name)}
      onDelete={() => setDeleting(p)}
      onFilterByTag={(tag) => setTagFilter(tag)}
      onEditTags={() => setEditingTags(p)}
      onPickTerminal={async (kind) => {
        setTermOpen(null);
        try {
          await SetProjectTerminal(p.name, kind);
          onOpenTerminal(p.name, kind);
          refreshTerminals();
        } catch (e) { flash(t("projects.errors.generic", { message: String(e) })); }
      }}
      onSetDefaultTerminal={async (kind) => {
        try {
          await SetDefaultTerminal(kind);
          flash(t("projects.flash.defaultTerminal", { kind }));
          refreshTerminals();
        } catch (e) { flash(t("projects.errors.generic", { message: String(e) })); }
      }}
      onOpenAgent={(agent, options) => onOpenAgent(p.name, agent.id, agent.label, options)}
      onPickEditor={async (kind, customCmd) => {
        setEditorOpen(null);
        try {
          await SetProjectEditor(p.name, kind);
          if (kind === "custom" && customCmd !== undefined) {
            await SetProjectCustomEditor(p.name, customCmd);
          }
          await OpenProjectInEditor(p.name, kind);
          refreshEditors();
        } catch (e) { flash(t("projects.errors.generic", { message: String(e) })); }
      }}
      onSetDefaultEditor={async (kind, customCmd) => {
        try {
          await SetDefaultEditor(kind);
          if (kind === "custom" && customCmd !== undefined) {
            await SetDefaultCustomEditor(customCmd);
          }
          flash(t("projects.flash.defaultEditor", { kind }));
          refreshEditors();
        } catch (e) { flash(t("projects.errors.generic", { message: String(e) })); }
      }}
      onOpenInEditor={async () => {
        try {
          await OpenProjectInEditor(p.name, "");
        } catch (e) { flash(t("projects.errors.generic", { message: String(e) })); }
      }}
      onToggleDev={() => toggleDev(p)}
      onRunCommand={(command) => {
        if (narrowDetails) setSelectedName(null);
        onRunCommand(p.name, command,
          terminals.projectKinds[p.name] || terminals.defaultKind || terminals.profiles.find(profile => profile.available)?.kind);
      }}
      onShowLogs={() => setLogsFor(p.name)}
      onWireProxy={() => wireProxy(p)}
      devCommand={cfg.devCommands?.[p.name]?.trim() || cfg.defaultDevCommand?.trim() || ""}
      devMenuOpen={devMenuOpen === p.name}
      setDevMenuOpen={(v) => setDevMenuOpen(v ? p.name : null)}
      onConfigureDev={() => openDevCommand(p)}
      queueStatus={queueStatus[p.name]}
      queueMenuOpen={queueMenuOpen === p.name}
      setQueueMenuOpen={(v) => setQueueMenuOpen(v ? p.name : null)}
      onToggleQueue={() => toggleQueue(p)}
      onRestartQueue={() => restartQueue(p)}
      onOpenQueueParams={() => openQueueParams(p)}
    />
  );

  return (
    <div className="projects-workspace">
      <header ref={headerRef} className="projects-page-header">
        <div className="projects-page-heading">
          <div className="projects-heading-line"><h1>{t('projects.title')}</h1><span className="projects-count">{t('projects.workspace.projectCount', { count: items.length })}</span></div>
          <p>{t('projects.workspace.subtitle')}</p>
          <div className={`projects-shared-state ${webServer ? 'is-running' : ''}`} role="status" aria-label={t('projects.workspace.sharedServices')} title={t('projects.workspace.sharedHint')}>
            <Globe className="w-3.5 h-3.5" />
            <span>{t(webServer ? 'projects.workspace.webAvailable' : 'projects.workspace.webStopped')}</span>
            {webServer && <span className="projects-web-name">{webServer}</span>}
          </div>
        </div>
        <div className="projects-page-actions">
          <button type="button" onClick={() => setDoctorSelection({ projectName: null })} disabled={items.length === 0} className="projects-header-button">
            <Stethoscope className="w-4 h-4" />{t('projects.doctor.runAll')}
          </button>
          <TldDropdown tld={cfg.tld} open={tldOpen} setOpen={setTldOpen} onPick={setTLD} onCustom={setCustomTLD} />
          <button type="button" onClick={handleReload} disabled={reloading} title={t('projects.reload.title')} className="projects-header-button">
            <RefreshCw className={`w-4 h-4 ${reloading ? 'animate-spin' : ''}`} />{t('projects.reload.label')}
          </button>
          <button type="button" onClick={() => { setNewProjKind(undefined); setModalOpen(true); }} className="projects-header-button projects-primary">
            <Plus className="w-4 h-4" />{t('projects.newProjectButton')}
          </button>
        </div>
      </header>
      <div className={`projects-layout ${selectedProject && !narrowDetails ? 'with-detail' : ''}`}>
        <div ref={listRef} className="projects-list-column">
          <ProjectsBrowser projects={items} selectedName={selectedName} onSelect={selectProject}
            tagFilter={tagFilter} onTagFilterChange={setTagFilter} defaultPhp={defaultPhp} defaultNode={defaultNode}
            onOpenEditor={async name => {
              try { await OpenProjectInEditor(name, ''); }
              catch (cause) { flash(t('projects.errors.generic', { message: String(cause) })); }
            }} onOpenURL={url => { void OpenURL(url); }} onOpenTerminal={name => {
              const kind = terminals.projectKinds[name];
              if (kind) onOpenTerminal(name, kind);
              else onOpenTerminal(name);
            }}
            onOpenFolder={async name => {
              try { await OpenProjectFolder(name); }
              catch (cause) { flash(t('projects.errors.generic', { message: String(cause) })); }
            }} />
          {!selectedProject && items.length > 0 && <p className="projects-list-hint">{t('projects.workspace.selectHint')}</p>}
        </div>
        {selectedProject && <ProjectDetailsSurface key={selectedProject.name} projectName={selectedProject.name} narrow={narrowDetails}
          returnFocusElement={selectionFocusRef.current}
          getFallbackFocus={() => listRef.current?.querySelector<HTMLElement>('input,button') ?? null}
          blocked={!!(doctorSelection || editingVhost || deleting || logsFor || editingTags || paramsModal || devCmdModal || modalOpen)}
          menuOpen={[phpOpen, nodeOpen, termOpen, editorOpen, agentOpen, queueMenuOpen, devMenuOpen].includes(selectedName)}
          onClose={() => setSelectedName(null)}>{detailFor(selectedProject)}</ProjectDetailsSurface>}
      </div>

      <NewProjectModal
        open={modalOpen}
        initialKind={newProjKind}
        onClose={() => setModalOpen(false)}
        flash={flash}
      />
      <VhostEditorModal
        projectName={editingVhost}
        open={editingVhost !== null}
        onClose={() => setEditingVhost(null)}
        flash={flash}
      />
      <SiteDoctorModal
        open={doctorSelection !== null}
        projectName={doctorSelection?.projectName ?? null}
        onClose={() => setDoctorSelection(null)}
        onDone={refresh}
        onOpenRuntimeDownloads={onOpenRuntimeDownloads}
      />
      <DeleteProjectDialog
        projectName={deleting?.name ?? null}
        projectPath={deleting?.path ?? ""}
        open={deleting !== null}
        onClose={() => setDeleting(null)}
        flash={flash}
        onDone={refresh}
      />
      <DevServerLogsModal
        projectName={logsFor}
        open={logsFor !== null}
        onClose={() => setLogsFor(null)}
      />
      <ProjectTagsModal
        project={editingTags}
        open={editingTags !== null}
        onClose={() => setEditingTags(null)}
        flash={flash}
        suggestions={tagIndex.sorted}
      />
      {paramsModal && (
        <QueueParamsModal
          project={paramsModal}
          onCancel={() => setParamsModal(null)}
          onStart={(a) => startQueueWithArgs(paramsModal, a)}
        />
      )}
      {devCmdModal && (
        <DevCommandModal
          project={devCmdModal}
          onCancel={() => setDevCmdModal(null)}
          onSave={(cmd) => saveDevCommand(devCmdModal, cmd)}
          flash={flash}
        />
      )}
    </div>
  );
}

function useNarrowProjectDetails() {
  const [narrow, setNarrow] = useState(() => typeof window.matchMedia === 'function'
    ? window.matchMedia('(max-width: 1099px)').matches : window.innerWidth <= 1099);
  useEffect(() => {
    if (typeof window.matchMedia === 'function') {
      const media = window.matchMedia('(max-width: 1099px)');
      const update = () => setNarrow(media.matches);
      media.addEventListener('change', update);
      update();
      return () => media.removeEventListener('change', update);
    }
    const update = () => setNarrow(window.innerWidth <= 1099);
    window.addEventListener('resize', update);
    return () => window.removeEventListener('resize', update);
  }, []);
  return narrow;
}

function ProjectDetailsSurface({ projectName, narrow, blocked, menuOpen, returnFocusElement, getFallbackFocus, onClose, children }: {
  projectName: string; narrow: boolean; blocked: boolean; menuOpen: boolean; returnFocusElement: HTMLElement | null;
  getFallbackFocus(): HTMLElement | null; onClose(): void; children: ReactNode;
}) {
  const t = useT();
  const panelRef = useRef<HTMLElement>(null);
  const returnFocusRef = useRef<HTMLElement | null>(returnFocusElement);
  const onCloseRef = useRef(onClose); onCloseRef.current = onClose;
  const fallbackFocusRef = useRef(getFallbackFocus); fallbackFocusRef.current = getFallbackFocus;
  const menuOpenRef = useRef(menuOpen); menuOpenRef.current = menuOpen;

  useEffect(() => {
    if (!narrow || blocked || !panelRef.current) return;
    const panel = panelRef.current;
    if (!returnFocusRef.current && document.activeElement instanceof HTMLElement && !panel.contains(document.activeElement)) {
      returnFocusRef.current = document.activeElement;
    }
    panel.querySelector<HTMLButtonElement>('.project-details-close')?.focus();
    const onKey = (event: KeyboardEvent) => {
      // Child dialogs (including confirmations opened through DialogProvider)
      // own their focus and Escape handling while they are above these details.
      if (event.defaultPrevented || document.querySelector('[data-dialog-backdrop]')) return;
      const target = event.target instanceof Element ? event.target : null;
      // Native menu listeners can commit their close before this window
      // listener runs. The event's original menu target remains authoritative.
      if (event.key === 'Escape' && target?.closest('[data-portal-menu]')) return;
      const dialog = target?.closest('[role="dialog"]');
      if (dialog && dialog !== panel && !panel.contains(dialog)) return;
      if (event.key === 'Escape') {
        if (menuOpenRef.current) return;
        event.preventDefault(); onCloseRef.current();
      } else if (event.key === 'Tab') {
        const containers: Element[] = [panel];
        if (menuOpenRef.current) containers.push(...document.querySelectorAll('[data-portal-menu]'));
        const selector = 'button:not([disabled]),a[href],input:not([disabled]),select:not([disabled]),textarea:not([disabled]),[tabindex]:not([tabindex="-1"])';
        const focusable = containers.flatMap(container => Array.from(container.querySelectorAll<HTMLElement>(selector)))
          .filter(element => !element.hidden && getComputedStyle(element).display !== 'none' && getComputedStyle(element).visibility !== 'hidden');
        const first = focusable[0]; const last = focusable[focusable.length - 1];
        if (!first) { event.preventDefault(); panel.focus(); }
        else if (event.shiftKey && (document.activeElement === first || !focusable.includes(document.activeElement as HTMLElement))) {
          event.preventDefault(); last.focus();
        } else if (!event.shiftKey && (document.activeElement === last || !focusable.includes(document.activeElement as HTMLElement))) {
          event.preventDefault(); first.focus();
        }
      }
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [narrow, blocked]);

  useEffect(() => {
    if (!narrow) return;
    return () => { queueMicrotask(() => {
      const target = returnFocusRef.current?.isConnected ? returnFocusRef.current : fallbackFocusRef.current();
      if (target?.isConnected) target.focus();
    }); };
  }, [narrow]);

  const panel = <aside ref={panelRef} role={narrow ? 'dialog' : 'region'} aria-modal={narrow && !blocked ? true : undefined}
    aria-hidden={narrow && blocked ? true : undefined} aria-label={`${t('projects.workspace.details')} · ${projectName}`}
    tabIndex={-1} className={`projects-detail-surface ${narrow ? 'is-overlay' : ''}`}>{children}</aside>;
  return narrow ? createPortal(<div className="projects-details-overlay" onMouseDown={event => {
    if (event.target === event.currentTarget && !blocked) onCloseRef.current();
  }}>{panel}</div>, document.body) : panel;
}

function ProjectDetails(props: {
  p: Project;
  onClose: () => void;
  devStatus?: DevStatus;
  phpVersions: PhpVersion[];
  nodeVersions: NodeVersion[];
  terminals: TerminalsInfo;
  agents: AgentProfile[];
  agentSessionsEnabled: boolean;
  flash: (message: string) => void;
  agentOpen: boolean;
  setAgentOpen: (open: boolean) => void;
  editors: EditorsInfo;
  phpOpen: boolean;
  setPhpOpen: (v: boolean) => void;
  nodeOpen: boolean;
  setNodeOpen: (v: boolean) => void;
  termOpen: boolean;
  setTermOpen: (v: boolean) => void;
  editorOpen: boolean;
  setEditorOpen: (v: boolean) => void;
  defaultPhp: string;
  defaultNode: string;
  onSetPhp: (ver: string) => void;
  onSetNode: (ver: string) => void;
  onOpenRuntimeDownloads?: (kind: ProjectRuntimeKind, version?: string) => void;
  onToggleSSL: () => void;
  sslAction?: SSLAction;
  onOpenURL: () => void;
  onOpenFolder: () => void;
  onOpenCron: () => void;
  onOpenDoctor: () => void;
  onOpenConfig: () => void;
  onDelete: () => void;
  onPickTerminal: (kind: string) => void;
  onSetDefaultTerminal: (kind: string) => void;
  onOpenAgent: (agent: AgentProfile, options: AgentLaunchDraft) => void;
  onPickEditor: (kind: string, customCmd?: string) => void;
  onSetDefaultEditor: (kind: string, customCmd?: string) => void;
  onOpenInEditor: () => void;
  onToggleDev: () => void;
  onRunCommand: (command: string) => void;
  onShowLogs: () => void;
  onWireProxy: () => void;
  devCommand?: string;
  devMenuOpen: boolean;
  setDevMenuOpen: (v: boolean) => void;
  onConfigureDev: () => void;
  onFilterByTag: (tag: string) => void;
  onEditTags: () => void;
  queueStatus?: QueueStatus;
  queueMenuOpen: boolean;
  setQueueMenuOpen: (v: boolean) => void;
  onToggleQueue: () => void;
  onRestartQueue: () => void;
  onOpenQueueParams: () => void;
}) {
  const t = useT();
  const { p, devStatus } = props;
  const showDev = !!p.packageManager || !!p.jsFramework || !!props.devCommand || !!devStatus?.running;
  const devRunning = !!devStatus?.running;
  const domain = p.url.replace(/^https?:\/\//, '').replace(/\/.*$/, '');
  const copy = async (value: string) => {
    try {
      if (await ClipboardSetText(value) === false) return;
      props.flash(t('projects.workspace.copied'));
    } catch (cause) { props.flash(t('projects.errors.generic', { message: String(cause) })); }
  };

  return <div className="project-details">
    <header className="project-details-header" aria-label={t('projects.workspace.selected')}>
      <div className={`project-kind-tile kind-${p.kind}`} aria-hidden="true"><Code2 className="w-7 h-7" /></div>
      <div className="project-details-heading"><h2 title={p.name}>{p.name}</h2>
        <p title={p.path}>{p.path}</p>
      </div>
      <button type="button" onClick={props.onClose} aria-label={t('projects.workspace.closeDetails')} className="project-details-close"><X className="w-4 h-4" /></button>
      <div className="project-details-badges"><TypeBadge kind={p.kind} /><JSBadge framework={p.jsFramework} />
        {p.packageManager && <span className="project-meta-badge">{p.packageManager}</span>}
        {p.paused && <span className="project-meta-badge">{t('projects.workspace.paused')}</span>}
        {showDev && devStatus && <span className={`project-meta-badge ${devRunning ? 'is-active' : ''}`}>{t(devRunning ? 'projects.workspace.devRunning' : 'projects.workspace.devStopped')}</span>}
      </div>
      <div className="project-https-controls" aria-busy={props.sslAction?.busy || undefined}>
        <button type="button" onClick={props.onOpenURL} title={p.url} className="project-domain-link"><Globe className="w-3.5 h-3.5" aria-hidden="true" /><span>{p.url}</span><ExternalLink className="w-3 h-3" aria-hidden="true" /></button>
        <button type="button" onClick={props.onToggleSSL} disabled={props.sslAction?.busy}
          className={`project-https-toggle ${p.ssl ? 'is-active' : ''}`}
          title={t(p.ssl ? 'projects.row.sslButtonOnTooltip' : 'projects.row.sslButtonOffTooltip')}>
          {props.sslAction?.busy ? <RefreshCw className="w-3.5 h-3.5 animate-spin" aria-hidden="true" />
            : p.ssl ? <Lock className="w-3.5 h-3.5" aria-hidden="true" /> : <Unlock className="w-3.5 h-3.5" aria-hidden="true" />}
          {t(p.ssl ? 'projects.https.disable' : 'projects.https.enable')}
        </button>
        {props.sslAction && <p className={`project-https-status ${props.sslAction.error ? 'is-error' : ''}`}
          role={props.sslAction.error ? 'alert' : 'status'}>{props.sslAction.message}</p>}
      </div>
    </header>
    <div className="project-details-scroll">
      <section className="project-detail-section" aria-label={t('projects.workspace.quickLaunch')}>
        <h3>{t('projects.workspace.quickLaunch')}</h3>
        <div className="project-quick-launch">
          <EditorDropdown project={p} editors={props.editors} open={props.editorOpen} setOpen={props.setEditorOpen}
            onPick={props.onPickEditor} onSetDefault={props.onSetDefaultEditor} onOpen={props.onOpenInEditor} />
          <TerminalDropdown project={p} terminals={props.terminals} open={props.termOpen} setOpen={props.setTermOpen}
            onPick={props.onPickTerminal} onSetDefault={props.onSetDefaultTerminal} />
          <RowButton onClick={props.onOpenURL} icon={<Globe className="w-3.5 h-3.5" />}>{t('projects.row.browser')}</RowButton>
          <RowButton onClick={props.onOpenFolder} icon={<Folder className="w-3.5 h-3.5" />}>{t('projects.row.folder')}</RowButton>
          {props.agentSessionsEnabled && <AgentLauncherMenu projectName={p.name} projectPath={p.path} canonicalHost={domain}
            mainProxyPort={p.proxyPort} profiles={props.agents} open={props.agentOpen} setOpen={props.setAgentOpen}
            onLaunch={props.onOpenAgent} notify={props.flash} />}
        </div>
      </section>
      {(showDev || p.kind === 'laravel') && <section className="project-detail-section" aria-label={t('projects.workspace.development')}>
        <h3>{t('projects.workspace.development')}</h3>
        {showDev && <div className="project-development-row">
          <DevServerSplitButton running={devRunning} port={devStatus?.port} customCmd={props.devCommand}
            open={props.devMenuOpen} setOpen={props.setDevMenuOpen} onToggle={props.onToggleDev} onConfigure={props.onConfigureDev} />
          <RowButton onClick={props.onShowLogs} icon={<Terminal className="w-3.5 h-3.5" />}>{t('projects.row.logs')}</RowButton>
          <RowButton onClick={props.onConfigureDev} disabled={devRunning} icon={<TerminalSquare className="w-3.5 h-3.5" />}
            title={devRunning ? t('projects.devServer.configureDisabledTooltip') : undefined}>
            {t('projects.workspace.configureDev')}</RowButton>
          {showProxyToggle(p.proxyPort, devStatus?.port) && <RowButton onClick={props.onWireProxy} tone={p.proxyPort > 0 ? 'warn' : 'default'}
            title={p.proxyPort > 0 ? t(devStatus?.port ? 'projects.row.proxyActiveTitle' : 'projects.row.proxyActiveStoppedTitle', { port: p.proxyPort })
              : t('projects.row.wireProxyTitle', { host: domain, port: devStatus?.port ?? 0 })}>
            {t(p.proxyPort > 0 ? 'projects.row.proxyOnLabel' : 'projects.row.wireProxyLabel')}</RowButton>}
        </div>}
        {p.kind === 'laravel' && <div className="project-queue-controls">
          <h4>{t('projects.workspace.queueHeading')}</h4>
          <div className="project-development-row">
          <QueueWorkerSplitButton status={props.queueStatus} open={props.queueMenuOpen} setOpen={props.setQueueMenuOpen}
            onToggle={props.onToggleQueue} onRestart={props.onRestartQueue} onParams={props.onOpenQueueParams} />
          </div>
        </div>}
      </section>}
      <ProjectCommands key={p.name} projectName={p.name} packageManager={p.packageManager}
        terminalKind={props.terminals.projectKinds[p.name] || props.terminals.defaultKind || props.terminals.profiles.find(profile => profile.available)?.kind}
        onRunCommand={props.onRunCommand} />
      <section className="project-detail-section" aria-label={t('projects.row.groupRuntime')}>
        <h3>{t('projects.row.groupRuntime')}</h3>
        <div className="project-runtime-grid">
          <div><h4>PHP</h4><ProjectRuntimeSelector kind="php" manualVersion={p.phpVersion} runtime={p.phpRuntime}
            versions={props.phpVersions} defaultVersion={props.defaultPhp} open={props.phpOpen} setOpen={props.setPhpOpen}
            onPick={props.onSetPhp} onOpenDownloads={props.onOpenRuntimeDownloads} /></div>
          <div><h4>Node</h4><ProjectRuntimeSelector kind="nodejs" manualVersion={p.nodeVersion} runtime={p.nodeRuntime}
            versions={props.nodeVersions} defaultVersion={props.defaultNode} open={props.nodeOpen} setOpen={props.setNodeOpen}
            onPick={props.onSetNode} onOpenDownloads={props.onOpenRuntimeDownloads} /></div>
        </div>
      </section>
      <section className="project-detail-section" aria-label={t('projects.workspace.projectTools')}>
        <h3>{t('projects.workspace.projectTools')}</h3>
        <div className="project-tools-grid">
          <RowButton onClick={props.onOpenDoctor} icon={<Stethoscope className="w-4 h-4" />}>{t('projects.doctor.button')}</RowButton>
          <RowButton onClick={props.onOpenConfig} icon={<FileCode className="w-4 h-4" />}>{t('projects.row.config')}</RowButton>
          <RowButton onClick={props.onOpenCron} icon={<Clock className="w-4 h-4" />}>{t('projects.row.history')}</RowButton>
        </div>
      </section>
      <section className="project-detail-section project-information" aria-label={t('projects.workspace.projectInformation')}>
        <h3>{t('projects.workspace.projectInformation')}</h3>
        <dl>
          <div><dt>{t('projects.workspace.domain')}</dt><dd><span title={domain}>{domain}</span><button type="button" aria-label={t('projects.workspace.copyDomain')} onClick={() => void copy(domain)}><Copy className="w-3.5 h-3.5" /></button></dd></div>
          <div><dt>{t('projects.workspace.path')}</dt><dd><span title={p.path}>{p.path}</span><button type="button" aria-label={t('projects.workspace.copyPath')} onClick={() => void copy(p.path)}><Copy className="w-3.5 h-3.5" /></button></dd></div>
        </dl>
        <div className="project-client-tags">
          {(p.tags ?? []).map(tag => tag.trim()).filter(Boolean).map(tag => <button type="button" key={tag} onClick={() => props.onFilterByTag(tag)} title={t('projects.workspace.filterByTag', { tag })}><Tag className="w-3 h-3" />{tag}</button>)}
          <button type="button" onClick={props.onEditTags} title={t('projects.row.editTagsTooltip')}><Plus className="w-3 h-3" />{t('projects.row.editTagsLabel')}</button>
        </div>
      </section>
      <div className="project-details-footer"><RowButton onClick={props.onDelete} icon={<Trash2 className="w-3.5 h-3.5" />} tone="danger">{t('projects.common.deleteLabel')}</RowButton></div>
    </div>
  </div>;
}

function RowButton({
  onClick, icon, children, tone = "default", title, disabled = false,
}: {
  onClick: () => void;
  icon?: React.ReactNode;
  children: React.ReactNode;
  tone?: "default" | "active" | "warn" | "danger" | "agent";
  title?: string;
  disabled?: boolean;
}) {
  const cls = {
    default: "bg-white/5 hover:bg-white/10 border-white/10",
    active:  "bg-emerald-500/20 hover:bg-emerald-500/30 border-emerald-400/40 text-emerald-200",
    warn:    "bg-amber-500/20 hover:bg-amber-500/30 border-amber-400/40 text-amber-200",
    danger:  "bg-white/5 hover:bg-red-500/40 border-white/10",
    agent:   "bg-accent2/10 hover:bg-accent2/20 border-accent2/30 text-accent2",
  }[tone];
  return (
    <button
      onClick={onClick}
      title={title}
      disabled={disabled}
      className={`min-h-7 px-2.5 py-1 rounded-md border text-[11px] flex items-center gap-1.5 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent disabled:opacity-35 disabled:cursor-not-allowed ${cls}`}
    >
      {icon}
      {children}
    </button>
  );
}

// ---- helpers (stay here so they share the page's types) ----

function TldDropdown({
  tld, open, setOpen, onPick, onCustom,
}: {
  tld: string; open: boolean; setOpen: (v: boolean) => void;
  onPick: (tld: string) => void; onCustom: () => void;
}) {
  const t = useT();
  const ref = useRef<HTMLButtonElement>(null);
  return (
    <>
      <button
        ref={ref}
        onClick={() => setOpen(!open)}
        className="glass glass-hover px-3 py-1.5 text-xs flex items-center gap-1"
        title={t("projects.tld.dropdownTitle")}
      >
        .{tld} <ChevronDown className="w-3 h-3" />
      </button>
      <PortalMenu open={open} anchorRef={ref} onClose={() => setOpen(false)} align="right" minWidth={160}>
        {COMMON_TLDS.map((tldOption) => (
          <button
            key={tldOption}
            onClick={() => onPick(tldOption)}
            className="w-full text-left px-3 py-1.5 text-xs rounded-md hover:bg-white/10"
          >
            .{tldOption} {tldOption === tld && <span className="opacity-50">{t("projects.common.current")}</span>}
          </button>
        ))}
        <div className="h-px bg-white/10 my-1" />
        <button
          onClick={onCustom}
          className="w-full text-left px-3 py-1.5 text-xs rounded-md hover:bg-white/10"
        >{t("projects.tld.custom")}</button>
      </PortalMenu>
    </>
  );
}

function TerminalDropdown({
  project, terminals, open, setOpen, onPick, onSetDefault,
}: {
  project: Project;
  terminals: TerminalsInfo;
  open: boolean;
  setOpen: (v: boolean) => void;
  onPick: (kind: string) => void;
  onSetDefault: (kind: string) => void;
}) {
  const t = useT();
  const ref = useRef<HTMLButtonElement>(null);
  const projectKind = terminals.projectKinds[project.name] || "";
  const effectiveKind = projectKind || terminals.defaultKind;
  const current = terminals.profiles.find((p) => p.kind === effectiveKind);
  const label = current?.label ?? t("projects.common.terminalFallback");
  const available = terminals.profiles.filter((p) => p.available);
  const unavailable = terminals.profiles.filter((p) => !p.available);

  return (
    <>
      <div className="inline-flex min-w-0 max-w-full">
        <button
          type="button"
          onClick={() => onPick(effectiveKind)}
          title={label}
          className="min-w-0 min-h-7 px-2.5 py-1 rounded-l-md bg-white/5 hover:bg-white/10 border-y border-l border-white/10 text-[11px] flex items-center gap-1.5 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent"
        >
          <TerminalSquare className="w-3 h-3 shrink-0" />
          <span className="truncate">{label}</span>
        </button>
        <button
          type="button"
          ref={ref}
          onClick={() => setOpen(!open)}
          className="min-h-7 px-1.5 py-1 rounded-r-md bg-white/5 hover:bg-white/10 border border-white/10 flex items-center shrink-0 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent"
          title={t("projects.terminalDropdown.title")}
          aria-label={t("projects.terminalDropdown.title")}
          aria-expanded={open}
        >
          <ChevronDown className="w-3 h-3" />
        </button>
      </div>
      <PortalMenu open={open} anchorRef={ref} onClose={() => setOpen(false)} align="right" minWidth={200}>
        <div className="px-3 py-1 text-[10px] uppercase opacity-50">{t("projects.terminalDropdown.header")}</div>
        {available.length === 0 && (
          <div className="px-3 py-1.5 text-[11px] opacity-60">{t("projects.terminalDropdown.none")}</div>
        )}
        {available.map((p) => (
          <div key={p.kind} className="flex items-center gap-1 group">
            <button
              onClick={() => onPick(p.kind)}
              className="flex-1 text-left px-3 py-1.5 text-[11px] rounded-md hover:bg-white/10"
            >
              {p.label}
              {effectiveKind === p.kind && <span className="opacity-50"> {t("projects.common.current")}</span>}
            </button>
            <button
              onClick={(e) => { e.stopPropagation(); onSetDefault(p.kind); }}
              className="px-2 py-1 text-[10px] opacity-0 group-hover:opacity-60 hover:!opacity-100 hover:text-accent"
              title={t("projects.terminalDropdown.setDefaultTooltip")}
            >
              ★
            </button>
          </div>
        ))}
        {unavailable.length > 0 && (
          <>
            <div className="h-px bg-white/10 my-1" />
            <div className="px-3 py-1 text-[10px] uppercase opacity-50">{t("projects.terminalDropdown.notInstalledHeader")}</div>
            {unavailable.map((p) => (
              <div key={p.kind} className="px-3 py-1.5 text-[11px] opacity-40">
                {p.label}
                {p.kind === "cmder" && <span className="ml-1">{t("projects.terminalDropdown.installHint")}</span>}
              </div>
            ))}
          </>
        )}
      </PortalMenu>
    </>
  );
}

function EditorDropdown({
  project, editors, open, setOpen, onPick, onSetDefault, onOpen,
}: {
  project: Project;
  editors: EditorsInfo;
  open: boolean;
  setOpen: (v: boolean) => void;
  onPick: (kind: string, customCmd?: string) => void;
  onSetDefault: (kind: string, customCmd?: string) => void;
  onOpen: () => void;
}) {
  const t = useT();
  const ref = useRef<HTMLButtonElement>(null);
  const [customInput, setCustomInput] = useState("");
  const projectKind = editors.projectKinds[project.name] || "";
  const effectiveKind = projectKind || editors.defaultKind;
  const current = editors.profiles.find((e) => e.kind === effectiveKind);
  const label = effectiveKind === "custom"
    ? (editors.projectCustomCmds[project.name] || editors.defaultCustomCmd || t("projects.common.customFallback"))
    : (current?.label ?? t("projects.common.editorFallback"));
  const available = editors.profiles.filter((e) => e.available && e.kind !== "custom");
  const unavailable = editors.profiles.filter((e) => !e.available && e.kind !== "custom");

  const handleOpen = () => {
    setCustomInput(editors.projectCustomCmds[project.name] || editors.defaultCustomCmd || "");
    setOpen(!open);
  };

  return (
    <>
      <div className="inline-flex min-w-0 max-w-full">
        <button
          type="button"
          onClick={onOpen}
          title={label}
          className="min-w-0 min-h-7 px-2.5 py-1 rounded-l-md bg-white/5 hover:bg-white/10 border-y border-l border-white/10 text-[11px] flex items-center gap-1.5 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent"
        >
          <Code2 className="w-3 h-3 shrink-0" />
          <span className="truncate">{label}</span>
        </button>
        <button
          type="button"
          ref={ref}
          onClick={handleOpen}
          className="min-h-7 px-1.5 py-1 rounded-r-md bg-white/5 hover:bg-white/10 border border-white/10 flex items-center shrink-0 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent"
          title={t("projects.editorDropdown.title")}
          aria-label={t("projects.editorDropdown.title")}
          aria-expanded={open}
        >
          <ChevronDown className="w-3 h-3" />
        </button>
      </div>
      <PortalMenu open={open} anchorRef={ref} onClose={() => setOpen(false)} align="right" minWidth={220}>
        <div className="px-3 py-1 text-[10px] uppercase opacity-50">{t("projects.editorDropdown.header")}</div>
        {available.length === 0 && (
          <div className="px-3 py-1.5 text-[11px] opacity-60">{t("projects.editorDropdown.none")}</div>
        )}
        {available.map((e) => (
          <div key={e.kind} className="flex items-center gap-1 group">
            <button
              onClick={() => onPick(e.kind)}
              className="flex-1 text-left px-3 py-1.5 text-[11px] rounded-md hover:bg-white/10"
            >
              {e.label}
              {effectiveKind === e.kind && <span className="opacity-50"> {t("projects.common.current")}</span>}
            </button>
            <button
              onClick={(ev) => { ev.stopPropagation(); onSetDefault(e.kind); }}
              className="px-2 py-1 text-[10px] opacity-0 group-hover:opacity-60 hover:!opacity-100 hover:text-accent"
              title={t("projects.editorDropdown.setDefaultTooltip")}
            >★</button>
          </div>
        ))}
        {unavailable.length > 0 && (
          <>
            <div className="h-px bg-white/10 my-1" />
            <div className="px-3 py-1 text-[10px] uppercase opacity-50">{t("projects.editorDropdown.notDetectedHeader")}</div>
            {unavailable.map((e) => (
              <div key={e.kind} className="px-3 py-1.5 text-[11px] opacity-40">{e.label}</div>
            ))}
          </>
        )}
        <div className="h-px bg-white/10 my-1" />
        <div className="px-3 py-1 text-[10px] uppercase opacity-50">{t("projects.editorDropdown.customHeader")}</div>
        <div className="px-3 pb-2 flex items-center gap-1">
          <input
            className="flex-1 bg-white/10 border border-white/20 rounded px-2 py-1 text-[11px] outline-none focus:border-white/40"
            placeholder={t("projects.editorDropdown.customPlaceholder")}
            value={customInput}
            onChange={(e) => setCustomInput(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === "Enter" && customInput.trim()) {
                onPick("custom", customInput.trim());
              }
            }}
          />
          <button
            onClick={() => { if (customInput.trim()) onPick("custom", customInput.trim()); }}
            className="px-2 py-1 text-[10px] rounded bg-white/10 hover:bg-white/20"
            title={t("projects.editorDropdown.openButtonTooltip")}
          >{t("projects.editorDropdown.openButton")}</button>
          <button
            onClick={() => { if (customInput.trim()) onSetDefault("custom", customInput.trim()); }}
            className="px-1 py-1 text-[10px] opacity-60 hover:opacity-100 hover:text-accent"
            title={t("projects.editorDropdown.setDefaultCustomTooltip")}
          >★</button>
        </div>
      </PortalMenu>
    </>
  );
}

function QueueWorkerSplitButton({
  status, open, setOpen, onToggle, onRestart, onParams,
}: {
  status?: QueueStatus;
  open: boolean;
  setOpen: (v: boolean) => void;
  onToggle: () => void;
  onRestart: () => void;
  onParams: () => void;
}) {
  const t = useT();
  const chevronRef = useRef<HTMLButtonElement>(null);
  const running = !!status?.running;
  const tone = running
    ? "bg-emerald-500/20 hover:bg-emerald-500/30 border-emerald-400/40 text-emerald-200"
    : "bg-white/5 hover:bg-white/10 border-white/10";
  return (
    <div className="inline-flex">
      <button
        type="button"
        onClick={onToggle}
        title={running
          ? t("projects.queueWorker.runningTitle", { pid: status?.pid })
          : t("projects.queueWorker.startTitle")}
        className={`min-h-7 px-2.5 py-1 text-[11px] border-y border-l rounded-l-md flex items-center gap-1.5 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent ${tone}`}
      >
        {running ? <Square className="w-3 h-3" /> : <ListOrdered className="w-3 h-3" />}
        {t(running ? "projects.workspace.stopQueue" : "projects.workspace.startQueue")}
      </button>
      <button
        type="button"
        ref={chevronRef}
        onClick={() => setOpen(!open)}
        title={t("projects.queueWorker.moreTooltip")}
        className={`min-h-7 px-1.5 py-1 text-[11px] border rounded-r-md flex items-center focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent ${tone}`}
      >
        <ChevronDown className="w-3 h-3" />
      </button>
      <PortalMenu open={open} anchorRef={chevronRef} onClose={() => setOpen(false)} align="right" minWidth={180}>
        <button
          type="button"
          onClick={onRestart}
          disabled={!running}
          className="block w-full text-left px-3 py-1.5 text-[11px] rounded-md hover:bg-white/10 disabled:opacity-40 disabled:cursor-not-allowed"
        >
          {t("projects.queueWorker.restart")}
        </button>
        <button
          type="button"
          onClick={onParams}
          disabled={running}
          className="block w-full text-left px-3 py-1.5 text-[11px] rounded-md hover:bg-white/10 disabled:opacity-40 disabled:cursor-not-allowed"
          title={running ? t("projects.queueWorker.startWithParamsDisabledTooltip") : ""}
        >
          {t("projects.queueWorker.startWithParams")}
        </button>
      </PortalMenu>
    </div>
  );
}

function DevServerSplitButton({
  running, port, customCmd, open, setOpen, onToggle, onConfigure,
}: {
  running: boolean;
  port?: number;
  customCmd?: string;
  open: boolean;
  setOpen: (v: boolean) => void;
  onToggle: () => void;
  onConfigure: () => void;
}) {
  const t = useT();
  const chevronRef = useRef<HTMLButtonElement>(null);
  const tone = running
    ? "bg-emerald-500/20 hover:bg-emerald-500/30 border-emerald-400/40 text-emerald-200"
    : "bg-white/5 hover:bg-white/10 border-white/10";
  const title = running
    ? (port ? t("projects.devServer.runningWithPortTitle", { port }) : t("projects.devServer.runningTitle"))
    : customCmd
      ? t("projects.devServer.startWithCmdTitle", { cmd: customCmd })
      : t("projects.devServer.startDefaultTitle");
  return (
    <div className="inline-flex">
      <button
        type="button"
        onClick={onToggle}
        title={title}
        className={`min-h-7 px-2.5 py-1 text-[11px] border-y border-l rounded-l-md flex items-center gap-1.5 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent ${tone}`}
      >
        {running ? <Square className="w-3 h-3" /> : <Play className="w-3 h-3" />}
        {t(running ? "projects.workspace.stopDev" : "projects.workspace.startDev")}
        {customCmd && !running ? <span className="w-1.5 h-1.5 rounded-full bg-amber-400" title={t("projects.devServer.customCmdDotTitle")} /> : null}
      </button>
      <button
        type="button"
        ref={chevronRef}
        onClick={() => setOpen(!open)}
        title={t("projects.devServer.optionsTooltip")}
        className={`min-h-7 px-1.5 py-1 text-[11px] border rounded-r-md flex items-center focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent ${tone}`}
      >
        <ChevronDown className="w-3 h-3" />
      </button>
      <PortalMenu open={open} anchorRef={chevronRef} onClose={() => setOpen(false)} align="right" minWidth={180}>
        <button
          type="button"
          onClick={onConfigure}
          disabled={running}
          className="block w-full text-left px-3 py-1.5 text-[11px] rounded-md hover:bg-white/10 disabled:opacity-40 disabled:cursor-not-allowed"
          title={running ? t("projects.devServer.configureDisabledTooltip") : ""}
        >
          {t("projects.devServer.configureButton")}
        </button>
      </PortalMenu>
    </div>
  );
}

function QueueParamsModal({
  project, onCancel, onStart,
}: {
  project: Project;
  onCancel: () => void;
  onStart: (args: string) => void;
}) {
  const t = useT();
  const [args, setArgs] = useState("");
  const [phpExe, setPhpExe] = useState("");
  useEffect(() => {
    GetLastQueueArgs(project.name).then((v: string) => setArgs(v ?? "")).catch(() => {});
    GetResolvedPhpExe(project.name).then((v: string) => setPhpExe(v ?? "")).catch(() => {});
  }, [project.name]);
  const cmd = `${phpExe || "<php>"} artisan queue:work${args ? " " + args : ""}`;
  return createPortal(
    <div className="modal-backdrop">
      <div
        onClick={(e) => e.stopPropagation()}
        className="modal-card w-[640px] max-w-[92vw] p-5 flex flex-col"
      >
        <header className="flex items-center justify-between mb-4">
          <h2 className="text-base font-semibold">{t("projects.queueParams.title", { project: project.name })}</h2>
          <button type="button" onClick={onCancel} title={t("common.closeTooltip")} className="p-1 rounded-md hover:bg-white/10">
            <X className="w-4 h-4" />
          </button>
        </header>
        <div className="text-[10px] uppercase tracking-wider opacity-60 mb-1">{t("projects.queueParams.commandLabel")}</div>
        <pre className="text-[11px] bg-black/30 border border-white/10 rounded-md p-2 overflow-x-auto whitespace-pre-wrap break-all font-mono mb-4">
          {cmd}
        </pre>
        <div className="text-[10px] uppercase tracking-wider opacity-60 mb-1">{t("projects.queueParams.argsLabel")}</div>
        <input
          autoFocus
          value={args}
          onChange={(e) => setArgs(e.target.value)}
          placeholder="--queue=high --tries=3 --timeout=60"
          className="w-full px-2 py-1.5 bg-white/5 border border-white/10 rounded-md text-[12px] font-mono focus:outline-none focus:border-accent"
          onKeyDown={(e) => { if (e.key === "Enter") onStart(args); }}
        />
        <div className="text-[10px] opacity-50 mt-2">
          {t("projects.queueParams.hint")}
        </div>
        <div className="flex justify-end gap-2 mt-4">
          <button
            type="button"
            onClick={onCancel}
            className="px-3 py-1.5 text-[11px] rounded-md bg-white/5 hover:bg-white/10 border border-white/10"
          >
            {t("common.cancel")}
          </button>
          <button
            type="button"
            onClick={() => onStart(args)}
            className="px-3 py-1.5 text-[11px] rounded-md bg-emerald-500/30 hover:bg-emerald-500/50 border border-emerald-400/40 text-emerald-100"
          >
            {t("projects.common.startLabel")}
          </button>
        </div>
      </div>
    </div>,
    document.body,
  );
}

function DevCommandModal({
  project, onCancel, onSave, flash,
}: {
  project: Project;
  onCancel: () => void;
  onSave: (cmd: string) => void;
  flash: (msg: string) => void;
}) {
  const t = useT();
  const [cmd, setCmd] = useState("");
  const [fixing, setFixing] = useState(false);
  useEffect(() => {
    GetProjectDevCommand(project.name).then((v: string) => setCmd(v ?? "")).catch(() => {});
  }, [project.name]);

  const makeWindowsSafe = async () => {
    setFixing(true);
    try {
      const msg: string = await MakeDevCommandWindowsSafe(project.name);
      flash(msg);
    } catch (e) {
      flash(t("projects.errors.generic", { message: String(e) }));
    } finally {
      setFixing(false);
    }
  };
  return createPortal(
    <div className="modal-backdrop">
      <div
        onClick={(e) => e.stopPropagation()}
        className="modal-card w-[640px] max-w-[92vw] p-5 flex flex-col"
      >
        <header className="flex items-center justify-between mb-4">
          <h2 className="text-base font-semibold">{t("projects.devCommandModal.title", { project: project.name })}</h2>
          <button type="button" onClick={onCancel} title={t("common.closeTooltip")} className="p-1 rounded-md hover:bg-white/10">
            <X className="w-4 h-4" />
          </button>
        </header>
        <div className="text-[10px] uppercase tracking-wider opacity-60 mb-1">{t("projects.devCommandModal.commandLabel")}</div>
        <input
          autoFocus
          value={cmd}
          onChange={(e) => setCmd(e.target.value)}
          placeholder="npm run dev"
          className="w-full px-2 py-1.5 bg-white/5 border border-white/10 rounded-md text-[12px] font-mono focus:outline-none focus:border-accent"
          onKeyDown={(e) => { if (e.key === "Enter") onSave(cmd); }}
        />

        {/* Windows-safe fix for Laravel's composer run dev (drops Pail) */}
        {cmd.trim() === "composer run dev" && (
          <div className="mt-3 p-2.5 rounded-md bg-amber-500/10 border border-amber-400/20 text-[11px] leading-snug">
            {t("projects.devCommandModal.windowsWarningA")} <span className="font-mono">composer run dev</span> {t("projects.devCommandModal.windowsWarningB")} <span className="font-mono">php artisan pail</span>{t("projects.devCommandModal.windowsWarningC")}{" "}
            <span className="font-mono">pcntl</span> {t("projects.devCommandModal.windowsWarningD")}
            <button
              type="button"
              onClick={makeWindowsSafe}
              disabled={fixing}
              className="mt-2 px-2.5 py-1 rounded-md bg-amber-500/25 hover:bg-amber-500/40 border border-amber-400/40 text-amber-100 disabled:opacity-50"
              title={t("projects.devCommandModal.makeSafeTooltip")}
            >
              {fixing ? t("projects.devCommandModal.patchingLabel") : t("projects.devCommandModal.makeSafeButton")}
            </button>
            <div className="mt-1.5 opacity-60">
              {t("projects.devCommandModal.windowsNotePrefix")} <span className="font-mono">composer.json</span> {t("projects.devCommandModal.windowsNoteSuffix")}
            </div>
          </div>
        )}

        <div className="text-[10px] opacity-60 mt-3 leading-relaxed">
          {t("projects.devCommandModal.hintA")} <span className="font-mono">npm run dev</span>{t("projects.devCommandModal.hintB")} <span className="font-mono">{"{port}"}</span> {t("projects.devCommandModal.hintC")}
          <br />
          {t("projects.devCommandModal.hintD")} <span className="font-mono">composer run dev</span> ·
          <span className="font-mono"> php artisan serve --port={"{port}"}</span>
        </div>
        <div className="flex justify-between gap-2 mt-4">
          <button
            type="button"
            onClick={() => onSave("")}
            className="px-3 py-1.5 text-[11px] rounded-md bg-white/5 hover:bg-white/10 border border-white/10"
            title={t("projects.devCommandModal.resetTooltip")}
          >
            {t("projects.devCommandModal.resetButton")}
          </button>
          <div className="flex gap-2">
            <button
              type="button"
              onClick={onCancel}
              className="px-3 py-1.5 text-[11px] rounded-md bg-white/5 hover:bg-white/10 border border-white/10"
            >
              {t("common.cancel")}
            </button>
            <button
              type="button"
              onClick={() => onSave(cmd)}
              className="px-3 py-1.5 text-[11px] rounded-md bg-emerald-500/30 hover:bg-emerald-500/50 border border-emerald-400/40 text-emerald-100"
            >
              {t("common.save")}
            </button>
          </div>
        </div>
      </div>
    </div>,
    document.body,
  );
}
