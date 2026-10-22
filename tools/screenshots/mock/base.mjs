// Builds the script Playwright injects before any page script. It must be
// entirely self-contained: it is serialised and evaluated inside the page,
// so it can close over nothing from this module.
import { installAgentActivityMock } from "./agentActivity.mjs";

const THEME_STORAGE_KEY = "l4e:theme"; // frontend/src/theme.ts:49

export function buildInitScript({ state, theme = "midnight" }) {
  return `(() => {
  const S = ${JSON.stringify(state)};
  try { localStorage.setItem(${JSON.stringify(THEME_STORAGE_KEY)}, ${JSON.stringify(theme)}); } catch (e) {}
  for (const [key, value] of Object.entries(S.storage || {})) {
    try { localStorage.setItem(key, JSON.stringify(value)); } catch (e) {}
  }

  const P = (v) => Promise.resolve(v);
  const listeners = new Map();
  const missing = [];
  const unavailable = () => ({ code: "unavailable", message: "Workspace source unavailable" });
  const fileContext = (ctx, key) => S.workspaceFiles && ctx.kind === "project" &&
    ctx.projectName === S.workspaceFiles.project && (!key || key === S.workspaceFiles.root.rootKey);
  const workspaceFile = (ctx, key, relative) => fileContext(ctx, key) &&
    Object.hasOwn(S.workspaceFiles.files || {}, relative) ? S.workspaceFiles.files[relative] : null;
  const sourceContext = (ctx) => S.sourceControl && ctx.kind === "project" &&
    ctx.projectName === S.sourceControl.snapshot.repository.context.projectName;

  const overrides = {
    IsDetachedWindow: () => P(!!S.detached),
    GetDetachedSeed: () => P(S.workspace ? JSON.stringify(S.workspace) : ""),
    // A clip can flip this so App.tsx renders the standalone command bar
    // (CommandBarWindow) instead of the main shell.
    IsCommandBar: () => P(!!S.commandBar),
    AppVersion: () => P(S.version),
    GetLanguage: () => P("en"),
    GetLanguageOverrides: () => P({ dir: "", catalogues: {}, results: [] }),
    CloudStatus: () => P(S.cloud),
    CloudMe: () => P({ user: { name: S.cloud && S.cloud.name, email: S.cloud && S.cloud.email, plan: "pro" } }),
    CloudRandomQuote: () => P(S.quote),
    CloudSyncStatus: () => P({ enabled: true, hasKey: true, reachable: true, isPro: true, resourceCount: 12 }),
    CloudListDeployments: () => P(S.deployments || []),
    CloudGetDeployment: (id) => P((S.deployments || []).find((d) => d.id === id) || null),
    ListDeployments: () => P(S.deployments || []),
    ListDeployPresets: () => P([]),
    ListServices: () => P(S.services || []),
    ServiceStatus: () => P(S.serviceStatus),
    GetEnabledServices: () => P((S.services || []).map((x) => x.name)),
    ListPhpVersions: () => P(S.php || []),
    ListNodeVersions: () => P(S.node || []),
    ActiveNodeVersion: () => P(((S.node || []).find((n) => n.active) || {}).version || ""),
    ListPhpFpmStatus: () => P(S.fpm || []),
    GetSystemMetrics: () => P(S.metrics),
    IsStackBootstrapped: () => P(true),
    ListProjects: () => P(S.projects || []),
    GetDevServerStatus: () => P({ running: false, port: 0, projectName: "" }),
    GetQueueWorkerStatus: () => P({ running: false, projectName: "" }),
    DiagnoseProject: (name) => P((S.doctor || []).find(report => report.project === name) || null),
    DiagnoseAllProjects: () => P(S.doctor || []),
    GetProjectsConfig: () => P({ root: "C:/sites", tld: "test" }),
    ListSSHHosts: () => P(S.sshHosts || []),
    // The agent workspace is off unless a scenario supplies S.agents. These
    // bindings are queried by the app shell and Command Center even then, so
    // they are mocked explicitly instead of letting the missing-binding guard
    // mistake an intentional empty state for a broken screenshot.
    GetAgentSessionsEnabled: () => P(!!(S.agents && S.agents.enabled)),
    ListAgentProfiles: () => P((S.agents && S.agents.profiles) || []),
    ListAgentSessions: (project, includeArchived) => P(((S.agents && S.agents.sessions) || [])
      .filter((s) => (!project || s.projectName === project) && (includeArchived || !s.archived))),
    ListAgentUsage: () => P((S.agents && S.agents.usage) || []),
    RefreshAgentUsage: () => P((S.agents && S.agents.usage) || []),
    GetAgentWorktreeRuntime: () => P(null),
    SetFocusedAgentSession: (id) => { window.__agentActivity.focusedSessions.push(id); return P(null); },
    MarkAgentSessionRead: () => P(null),
    ListProjectRoutes: () => P([]),
    ListNotifications: () => P([]),
    UnreadNotificationCount: () => P(0),
    NotificationPreferences: () => P({}),
    // Restoring a persisted agent tab asks the backend to resume the session;
    // the mock hands back the session so the tab binds to its terminalId.
    ResumeAgentSession: (id) => P(((S.agents && S.agents.sessions) || []).find((s) => s.id === id) || null),
    ListCrons: () => P(S.crons || []),
    ListSources: () => P([]),
    ListTerminals: () => P(S.terminals),
    ListEditors: () => P({ profiles: [], defaultKind: "", defaultCustomCmd: "", projectKinds: {}, projectCustomCmds: {} }),
    GetCommandCenter: () => P(S.commandCenter || { paletteHotkey: "Ctrl+K", globalHotkey: "", commands: [] }),
    // Per project, so a clip can show the suggestions coming from reading that
    // project (artisan list + package.json scripts) rather than from a fixed
    // list. useCommandCenter maps npmScripts -> npm.
    ListProjectCommands: (name) => P((S.projectCommands || {})[name]
      || { artisan: [], npmScripts: [] }),
    ListProjectScripts: (name) => P(((S.projectCommands || {})[name] || {}).npmScripts || []),
    DrainTerminalAttach: () => P(null),
    // A scenario can hand the workspace a persisted layout (serialize.ts
    // PersistedWorkspace) so tabs — agent tabs included — exist on mount.
    LoadTerminalWorkspace: () => P(S.workspace ? JSON.stringify(S.workspace) : null),
    SaveTerminalWorkspace: () => P(null),
    LoadDocumentWorkspace: () => P(S.documents ? JSON.stringify(S.documents) : JSON.stringify({ version: 1, tabs: [], activeTabIndex: -1, visible: false, panelPercent: 45 })),
    SaveDocumentWorkspace: () => P(null),
    SetDocumentWorkspaceState: (state) => { window.__agentActivity.documentReports.push({ ...state }); return P(null); },
    ResolveDocumentShutdown: (reply) => { window.__agentActivity.documentReplies.push({ ...reply }); return P({ released: false }); },
    InspectSourceControl: (ctx) => P(sourceContext(ctx) ? { code: "ok", snapshot: S.sourceControl.snapshot } : unavailable()),
    GetSourceControlStatus: (ref) => P(sourceContext(ref.context) && ref.rootKey === S.sourceControl.snapshot.repository.rootKey ? { code: "ok", snapshot: S.sourceControl.snapshot } : unavailable()),
    ReadSourceControlDiff: (request) => P(sourceContext(request.repository.context) && request.repository.rootKey === S.sourceControl.snapshot.repository.rootKey && S.sourceControl.diffs[request.entryId + ":" + request.layer] ? { code: "ok", diff: S.sourceControl.diffs[request.entryId + ":" + request.layer] } : unavailable()),
    StartSourceControlStage: () => P({ code: "unavailable", message: "Source control unavailable" }),
    StartSourceControlUnstage: () => P({ code: "unavailable", message: "Source control unavailable" }),
    StartSourceControlCommit: () => P({ code: "unavailable", message: "Source control unavailable" }),
    StartSourceControlPush: () => P({ code: "unavailable", message: "Source control unavailable" }),
    GetSourceControlOperation: () => P({ code: "operation_unknown", message: "Operation unavailable" }),
    CancelSourceControlOperation: () => P({ code: "operation_unknown", message: "Operation unavailable" }),
    GetSourceControlBusy: () => P({ busy: false, operationIds: [] }),
    GetWorkspaceFileContext: (ctx) => P(fileContext(ctx) ? { code: "ok", context: S.workspaceFiles.root } : unavailable()),
    ListWorkspaceDirectory: (ctx, key, relative, cursor) => P(fileContext(ctx, key) ? { code: "ok", directory: { entries: !relative && !cursor ? (S.workspaceFiles.entries || []) : [], nextCursor: "" } } : unavailable()),
    ReadWorkspaceDocument: (ctx, key, relative) => P(workspaceFile(ctx, key, relative) ? { code: "ok", document: workspaceFile(ctx, key, relative) } : unavailable()),
    CheckWorkspaceDocument: (ctx, key, relative, revision) => P(workspaceFile(ctx, key, relative) ? workspaceFile(ctx, key, relative).revision === revision ? { code: "unchanged" } : { code: "changed", document: workspaceFile(ctx, key, relative) } : unavailable()),
    SaveWorkspaceDocument: () => P({ code: "unavailable", message: "Workspace source unavailable" }),
    ReadWorkspaceImage: () => P({ code: "unavailable", message: "Workspace source unavailable" }),
    OpenWorkspaceDocumentExternally: () => P({ code: "unavailable", message: "Workspace source unavailable" }),
    StartPtyForAgentSession: () => Promise.reject(new Error("Agent workspace unavailable")),
    ResizePty: () => P(null),
    WritePty: (id, data) => { window.__agentActivity.ptyWrites.push({ id, data }); return P(null); },
    // The terminal workspace stays mounted across pages, so this is reached
    // even on the dashboard. A fixed id lets a scenario script the session by
    // emitting "term:data:<id>".
    StartPtyKind: () => P("harness-pty-1"),
    // Project shells get a predictable id per project so an adopted agent
    // session (S.agents.sessions[].terminalId) can bind to one of them.
    StartPtyForProject: (name) => P("harness-pty-project-" + name),
    StartPtyForProjectCommand: (name) => P("harness-pty-project-" + name),
  };

  (${installAgentActivityMock.toString()})(S, overrides);
  for (const name of Object.keys(overrides).filter(name => name.startsWith("StartPty"))) {
    const start = overrides[name];
    overrides[name] = (...args) => { window.__agentActivity.ptySpawns.push({ method: name, args }); return start(...args); };
  }

  // xterm-addon-webgl does not paint under headless Chrome. When a scenario
  // sets webgl:false the WebGL context is refused, WebglAddon throws, and
  // Terminal.tsx falls back to xterm's DOM renderer — which paints, and whose
  // rows are real text nodes the readiness check can see.
  if (S.webgl === false) {
    const getContext = HTMLCanvasElement.prototype.getContext;
    HTMLCanvasElement.prototype.getContext = function (type, ...rest) {
      if (type === "webgl" || type === "webgl2" || type === "experimental-webgl") return null;
      return getContext.call(this, type, ...rest);
    };
  }

  // Lets the failure tests prove the recorder catches a genuinely missing
  // binding rather than a simulated one.
  if (S.__dropBinding) delete overrides[S.__dropBinding];

  const app = new Proxy({}, {
    get: (_t, name) => {
      const key = String(name);
      if (overrides[key]) return overrides[key];
      return () => {
        if (missing.indexOf(key) === -1) missing.push(key);
        return P(null);
      };
    },
  });
  window.go = { main: { App: app } };

  const noop = () => {};
  const on = (event, cb) => {
    if (!listeners.has(event)) listeners.set(event, new Set());
    listeners.get(event).add(cb);
    return () => listeners.get(event).delete(cb);
  };
  const runtime = {
    EventsOn: on,
    EventsOnMultiple: on,
    EventsOnce: on,
    EventsOff: noop,
    EventsEmit: noop,
    WindowIsMaximised: () => P(false),
    BrowserOpenURL: noop,
    Quit: noop,
  };
  window.runtime = new Proxy(runtime, {
    get: (t, name) => (name in t ? t[name] : noop),
  });

  window.__harness = {
    missing,
    // Lets a scenario wait until a component has actually subscribed before
    // emitting: the terminal pane subscribes in a useEffect, so emitting as
    // soon as its tab is visible fires into nothing.
    hasListener: (event) => listeners.has(event) && listeners.get(event).size > 0,
    emit: (event, ...args) => {
      const set = listeners.get(event);
      if (set) for (const cb of Array.from(set)) cb(...args);
    },
  };
})();`;
}
