// Terminal canvas: a workspace of floating Groups. Owns the reducer state, the
// async PTY orchestration (spawn/stop happens here; structural changes go
// through the pure reducer), drag-and-drop routing, layout persistence and the
// restore+respawn on startup. Drop-in replacement for the old TerminalTabs:
// same { openRequest, flash } props.

import { useCallback, useEffect, useMemo, useReducer, useRef, useState } from "react";
import { Plus, ChevronDown, TerminalSquare } from "lucide-react";
// @ts-ignore
import {
  StartPtyKind, StartPtyKindIn, StartPtyKindRuntime, StartPtyForProject, StartPtyForProjectCommand, StartPtyForAgentSession, StartPtyForSSH, StopPty, WritePty,
  ListTerminals, ListPhpVersions, ListNodeVersions, SetDefaultTerminal, SaveTerminalWorkspace, LoadTerminalWorkspace,
  DetachWorkspace, GetDetachedSeed, SendTerminalAttach, DrainTerminalAttach,
  LaunchAgentSession, ResumeAgentSession, StopAgentSession, ConfigureWorktreeRouting, ListProjectRoutes, SetFocusedAgentSession,
  ListAgentSessions,
} from "../../../wailsjs/go/main/App";
// @ts-ignore
import { EventsOn, EventsOff, Quit } from "../../../wailsjs/runtime";
import SFTPModal from "../SFTPModal";
import PortalMenu from "../PortalMenu";
import TerminalGroup, { type GroupApi } from "./TerminalGroup";
import { workspaceReducer, emptyWorkspace } from "./workspaceReducer";
import { collectOpenTerminals } from "./openTerminals";
import { serializeWorkspace, hydrateWorkspace, leavesToRespawn, type PersistedWorkspace } from "./serialize";
import { snapGeometry } from "./snap";
import { allLeaves, findLeaf } from "./treeUtils";
import { decodePane, decodeTab, regionToSplit } from "./dnd";
import { isUnavailableAgentHistoryError } from "./agentRestore";
import { isTypingInFormField, terminalHotkey } from "./hotkeys";
import type { AdoptedSessionView } from "./adoptedLeaf";
import type { DropRegion } from "./dnd";
import type { Group, Leaf, OpenProjectRequest, OpenTerminal, RuntimeVersion, SnapState, Tab, TerminalFocusRequest, TerminalsInfo, Workspace } from "./types";
import type { ActiveSurface, WorkspaceContextRef, WorkspaceSelection } from "../workspace/types";
import { useT } from "../../lib/i18n";

export type { OpenProjectRequest } from "./types";

const rand = () => Math.random().toString(36).slice(2, 8);
const newLeafId = () => `leaf-${rand()}`;
const newTabId = () => `tab-${rand()}`;
const newGroupId = () => `grp-${rand()}`;

export default function TerminalWorkspace({
  openRequest,
  focusAgentRequest,
  focusTerminalRequest,
  onOpenTerminalsChange,
  onFocusedAgentSessionChange,
  activeSurface = "terminal",
  onActiveSurfaceChange,
  onWorkspaceSelectionChange,
  openFilesRequest,
  onOpenWorkspaceFiles,
  flash,
  detached = false,
  detachedSeed,
  active = false,
}: {
  openRequest?: OpenProjectRequest | null;
  focusAgentRequest?: { sessionId: string; seq: number } | null;
  focusTerminalRequest?: TerminalFocusRequest | null;
  onOpenTerminalsChange?: (terminals: OpenTerminal[]) => void;
  activeSurface?: ActiveSurface;
  onActiveSurfaceChange?: (surface: ActiveSurface) => void;
  onWorkspaceSelectionChange?: (selection: WorkspaceSelection) => void;
  openFilesRequest?: { seq: number } | null;
  onOpenWorkspaceFiles?: (context: WorkspaceContextRef | null) => void;
  onFocusedAgentSessionChange?: (sessionId: string) => void;
  flash?: (m: string) => void;
  detached?: boolean;
  detachedSeed?: string;
  // Whether the terminal canvas is visible. Starts its saved/default sessions
  // on first use and gates Ctrl+T / Ctrl+W while the page is hidden.
  active?: boolean;
} = {}) {
  // Named `tt`, not `t` — this file pervasively uses `t` as a local Tab
  // variable (closeTab, closePane, setLeafRuntime, splitActive, detachTab),
  // which would shadow useT()'s return value.
  const tt = useT();
  const [ws, dispatch] = useReducer(workspaceReducer, emptyWorkspace);
  const [terminals, setTerminals] = useState<TerminalsInfo>({ profiles: [], defaultKind: "", projectKinds: {} });
  // Installed runtimes for the per-terminal PHP/Node picker (local terminals only).
  const [phpVersions, setPhpVersions] = useState<RuntimeVersion[]>([]);
  const [nodeVersions, setNodeVersions] = useState<RuntimeVersion[]>([]);
  const [preview, setPreview] = useState<SnapState>(null);
  const [sftpFor, setSftpFor] = useState<{ termId: string; label: string } | null>(null);
  const [size, setSize] = useState<{ w: number; h: number }>({ w: 1000, h: 600 });
  const [emptyPickerOpen, setEmptyPickerOpen] = useState(false);
  const [windowFocused, setWindowFocused] = useState(document.hasFocus());
  const [hydrated, setHydrated] = useState(false);
  const [attachSeeds, setAttachSeeds] = useState<string[]>([]);
  const [initializationError, setInitializationError] = useState<string | null>(null);
  const [initializationAttempt, setInitializationAttempt] = useState(0);

  const canvasRef = useRef<HTMLDivElement>(null);
  const emptyPickerRef = useRef<HTMLButtonElement>(null);
  const wsRef = useRef(ws);
  const termRef = useRef(terminals);
  const initializationStarted = useRef(false);
  const needsInitialization = active || detached || !!openRequest || !!focusAgentRequest || !!focusTerminalRequest || attachSeeds.length > 0;
  // Read current requests after asynchronous profile/layout loading. They may
  // arrive while first activation is already restoring the workspace.
  const hasExplicitRequest = useRef(false);
  hasExplicitRequest.current = !!openRequest || !!focusAgentRequest || !!focusTerminalRequest || attachSeeds.length > 0;
  const lastAgentFocusSeq = useRef(0);
  const lastTerminalFocusSeq = useRef(0);
  const reportedAgentFocus = useRef<string | null>(null);
  const surfaceCallback = useRef(onActiveSurfaceChange); surfaceCallback.current = onActiveSurfaceChange;
  const filesCallback = useRef(onOpenWorkspaceFiles); filesCallback.current = onOpenWorkspaceFiles;
  const contextCallback = useRef(onWorkspaceSelectionChange); contextCallback.current = onWorkspaceSelectionChange;
  const claimTerminalFocus = useCallback(() => surfaceCallback.current?.("terminal"), []);
  const flashRef = useRef(flash);
  const openTerminalsCallbackRef = useRef(onOpenTerminalsChange);
  openTerminalsCallbackRef.current = onOpenTerminalsChange;
  const openTerminals = useMemo(() => collectOpenTerminals(ws, tt), [ws, tt]);
  useEffect(() => { openTerminalsCallbackRef.current?.(openTerminals); }, [openTerminals]);
  useEffect(() => { wsRef.current = ws; }, [ws]);
  useEffect(() => { termRef.current = terminals; }, [terminals]);
  useEffect(() => { flashRef.current = flash; }, [flash]);

  useEffect(() => EventsOn("agent:sessions-reset", () => {
    for (const group of wsRef.current.groups) {
      for (const tab of group.tabs) {
        for (const leaf of allLeaves(tab.tree)) {
          if (leaf.spawn.kind === "agent") {
            dispatch({ type: "CLOSE_PANE", groupId: group.id, tabId: tab.tabId, leafId: leaf.id });
          }
        }
      }
    }
  }), []);

  // Agents started by hand inside a LaraEnv terminal: promote the shell leaf
  // while the adopted session is live, revert when it stops.
  useEffect(() => {
    const apply = (session: AdoptedSessionView) => { if (session?.adopted) dispatch({ type: "APPLY_ADOPTED_SESSION", session }); };
    const off = EventsOn("agent:session-changed", apply);
    ListAgentSessions("", false).then((sessions: unknown[]) => (sessions as AdoptedSessionView[]).forEach(apply)).catch(() => {});
    return () => { off(); };
  }, []);

  useEffect(() => {
    const onFocus = () => setWindowFocused(true);
    const onBlur = () => setWindowFocused(false);
    window.addEventListener("focus", onFocus);
    window.addEventListener("blur", onBlur);
    return () => { window.removeEventListener("focus", onFocus); window.removeEventListener("blur", onBlur); };
  }, []);

  // Keep backend focus exact so a visible agent does not generate an OS toast,
  // while hidden/minimized terminals still do. The topmost group's active pane
  // is the one receiving keyboard input.
  useEffect(() => {
    let sessionID = "";
    if (activeSurface === "terminal" && (detached || active) && windowFocused && ws.groups.length) {
      const group = ws.groups.reduce((left, right) => right.z > left.z ? right : left);
      const tab = group.tabs.find((candidate) => candidate.tabId === group.activeTabId);
      const leaf = tab ? findLeaf(tab.tree, tab.activeLeafId) : null;
      sessionID = leaf?.spawn.laraSessionId || "";
    }
    if (reportedAgentFocus.current !== sessionID) {
      reportedAgentFocus.current = sessionID;
      void SetFocusedAgentSession(sessionID);
      if (sessionID) onFocusedAgentSessionChange?.(sessionID);
    }
  }, [active, activeSurface, detached, windowFocused, ws, onFocusedAgentSessionChange]);
  useEffect(() => () => { void SetFocusedAgentSession(""); }, []);

  // Workspace origin follows selection, even while documents own keyboard focus.
  const selectedGroup = ws.groups.reduce<Group | undefined>((top, group) => !top || group.z > top.z ? group : top, undefined);
  const selectedTab = selectedGroup?.tabs.find(tab => tab.tabId === selectedGroup.activeTabId);
  const selectedLeaf = selectedTab ? findLeaf(selectedTab.tree, selectedTab.activeLeafId) : null;
  const selection: WorkspaceSelection = selectedLeaf?.spawn.kind === 'ssh' ? { kind: 'ssh', context: null } : { kind: 'local', context: workspaceContextForLeaf(selectedLeaf) };
  const contextKey = JSON.stringify(selection);
  useEffect(() => { contextCallback.current?.(selection); }, [selectedLeaf?.id, contextKey]);

  const openFilesForLeaf = useCallback((leaf: Leaf | null) => {
    if (leaf?.spawn.kind === "ssh") {
      if (!leaf.status || leaf.status === "ready") setSftpFor({ termId: leaf.id, label: leaf.label.replace(/^ssh \u00b7 /, "") });
      return;
    }
    filesCallback.current?.(workspaceContextForLeaf(leaf));
  }, []);
  const lastFilesSeq = useRef(0);
  useEffect(() => {
    if (!openFilesRequest || openFilesRequest.seq === lastFilesSeq.current) return;
    lastFilesSeq.current = openFilesRequest.seq;
    openFilesForLeaf(selectedLeaf);
  }, [openFilesRequest, selectedLeaf, openFilesForLeaf]);

  useEffect(() => {
    if (!hydrated) return;
    if (!focusAgentRequest || focusAgentRequest.seq === lastAgentFocusSeq.current) return;
    for (const group of wsRef.current.groups) {
      for (const tab of group.tabs) {
        const leaf = allLeaves(tab.tree).find((candidate) => candidate.spawn.laraSessionId === focusAgentRequest.sessionId);
        if (!leaf) continue;
        claimTerminalFocus();
        lastAgentFocusSeq.current = focusAgentRequest.seq;
        dispatch({ type: "FOCUS_GROUP", groupId: group.id });
        dispatch({ type: "SET_ACTIVE_TAB", groupId: group.id, tabId: tab.tabId });
        dispatch({ type: "FOCUS_PANE", groupId: group.id, tabId: tab.tabId, leafId: leaf.id });
        return;
      }
    }
    flashRef.current?.(tt("terminal.workspace.agentNotInWorkspace"));
    lastAgentFocusSeq.current = focusAgentRequest.seq;
  }, [hydrated, focusAgentRequest, tt]);

  useEffect(() => {
    if (!hydrated) return;
    if (!focusTerminalRequest || focusTerminalRequest.seq === lastTerminalFocusSeq.current) return;
    lastTerminalFocusSeq.current = focusTerminalRequest.seq;
    for (const group of wsRef.current.groups) {
      for (const tab of group.tabs) {
        const leaf = allLeaves(tab.tree).find((candidate) => candidate.id === focusTerminalRequest.terminalId);
        if (!leaf) continue;
        claimTerminalFocus();
        dispatch({ type: "FOCUS_GROUP", groupId: group.id });
        dispatch({ type: "SET_ACTIVE_TAB", groupId: group.id, tabId: tab.tabId });
        dispatch({ type: "FOCUS_PANE", groupId: group.id, tabId: tab.tabId, leafId: leaf.id });
        return;
      }
    }
    flashRef.current?.(tt("terminal.workspace.terminalNotInWorkspace"));
  }, [hydrated, focusTerminalRequest, tt]);

  const refreshTerminals = () => ListTerminals().then((t: TerminalsInfo) => setTerminals(t));
  const refreshPhpVersions = () => ListPhpVersions().then((v: RuntimeVersion[]) => setPhpVersions(v ?? [])).catch(() => {});
  const refreshNodeVersions = () => ListNodeVersions().then((v: RuntimeVersion[]) => setNodeVersions(v ?? [])).catch(() => {});

  // Load installed PHP/Node runtimes once for the per-terminal picker, then
  // keep them (and the terminal-kind dropdown, e.g. Cmder/Git Bash) fresh as
  // installs complete — otherwise they read "not installed" until reload.
  useEffect(() => {
    refreshPhpVersions();
    refreshNodeVersions();
    const off = EventsOn("install:installed", () => {
      refreshPhpVersions();
      refreshNodeVersions();
      refreshTerminals();
    });
    return () => { off(); };
  }, []);

  const resolveDefaultKind = useCallback((info?: TerminalsInfo): string => {
    const t = info ?? termRef.current;
    if (t.defaultKind) return t.defaultKind;
    return t.profiles.find((p) => p.available)?.kind ?? "";
  }, []);

  const profileLabel = (k: string) => termRef.current.profiles.find((p) => p.kind === k)?.label ?? k;

  const agentLabel = (id: string) => ({
    claude: "Claude Code", codex: "Codex", kimi: "Kimi Code", opencode: "OpenCode",
  } as Record<string, string>)[id] ?? id;

  const routingForWorktree = (routes: Array<{ canonical: boolean; hostname: string; targetPath: string }>, worktreePath: string) => {
    const normalized = (value: string) => value.replace(/\//g, "\\").replace(/\\+$/, "").toLowerCase();
    const matching = routes.filter((route) => normalized(route.targetPath) === normalized(worktreePath));
    return {
      routeCanonical: matching.some((route) => route.canonical),
      previewHostname: matching.find((route) => !route.canonical)?.hostname,
    };
  };

  // Smallest free "<name> N" number across all tabs in the workspace.
  const nextTabNumber = (): number => {
    const taken = new Set<number>();
    for (const g of wsRef.current.groups)
      for (const t of g.tabs) {
        const m = /^.+ (\d+)$/.exec(t.label);
        if (m) taken.add(parseInt(m[1], 10));
      }
    let n = 1;
    while (taken.has(n)) n++;
    return n;
  };

  // ----- canvas size tracking -----
  useEffect(() => {
    const el = canvasRef.current;
    if (!el) return;
    const update = () => setSize({ w: el.clientWidth, h: el.clientHeight });
    update();
    const ro = new ResizeObserver(update);
    ro.observe(el);
    return () => ro.disconnect();
  }, []);

  const getCanvasRect = useCallback(() => canvasRef.current?.getBoundingClientRect() ?? null, []);

  // ----- geometry helpers -----
  const cascadeGeom = () => {
    const n = wsRef.current.groups.length;
    const w = Math.min(720, Math.max(360, size.w - 60));
    const h = Math.min(460, Math.max(220, size.h - 60));
    const x = Math.min(40 + n * 28, Math.max(0, size.w - w));
    const y = Math.min(28 + n * 28, Math.max(0, size.h - h));
    return { x, y, w, h };
  };

  const focusedGroup = (): Group | null => {
    const gs = wsRef.current.groups;
    if (gs.length === 0) return null;
    return gs.reduce((a, b) => (b.z > a.z ? b : a));
  };

  // ----- spawning a local pane -----
  const makeLeaf = (id: string, label: string, kind: string, spawn: Leaf["spawn"], status: Leaf["status"]): Leaf =>
    ({ type: "leaf", id, label, kind, status, spawn });

  const stopLeaves = async (leaves: Leaf[]) => {
    for (const l of leaves) {
      if (l.status !== "pending" && l.status !== "error") {
        try {
          if (l.spawn.kind === "agent" && l.spawn.laraSessionId) await StopAgentSession(l.spawn.laraSessionId);
          else await StopPty(l.id);
        } catch {}
      }
    }
  };

  // Serialize one group (optionally a subset of its tabs) as a maximized
  // single-group workspace — the seed handed to a detached satellite window.
  const seedFromGroup = (g: Group, tabs: Tab[]): string => {
    const grp: Group = { ...g, id: "g", z: 1, snap: "max", tabs, activeTabId: tabs[0].tabId };
    return JSON.stringify(serializeWorkspace({ groups: [grp], zCounter: 1, nextGroupNum: 2 }));
  };

  // Is a screen-space point outside the LaraEnv window? Used to decide whether a
  // dragged group/tab should detach into its own OS window.
  const isOutsideWindow = (sx: number, sy: number): boolean => {
    const ox = window.screenX, oy = window.screenY;
    const ow = window.outerWidth, oh = window.outerHeight;
    return sx < ox || sx > ox + ow || sy < oy || sy > oy + oh;
  };

  const detachGroup = async (groupId: string) => {
    const g = wsRef.current.groups.find((x) => x.id === groupId);
    if (!g) return;
    const leaves = g.tabs.flatMap((tab) => allLeaves(tab.tree));
    await stopLeaves(leaves.filter((leaf) => leaf.spawn.kind === "agent"));
    try { await DetachWorkspace(seedFromGroup(g, g.tabs)); } catch (e) { console.error(e); flash?.(tt("terminal.workspace.detachFailed", { error: String(e) })); return; }
    await stopLeaves(leaves.filter((leaf) => leaf.spawn.kind !== "agent"));
    dispatch({ type: "CLOSE_GROUP", groupId });
  };

  const detachTab = async (srcGroupId: string, tabId: string) => {
    const g = wsRef.current.groups.find((x) => x.id === srcGroupId);
    const t = g?.tabs.find((x) => x.tabId === tabId);
    if (!g || !t) return;
    const leaves = allLeaves(t.tree);
    await stopLeaves(leaves.filter((leaf) => leaf.spawn.kind === "agent"));
    try { await DetachWorkspace(seedFromGroup(g, [t])); } catch (e) { console.error(e); flash?.(tt("terminal.workspace.detachFailed", { error: String(e) })); return; }
    await stopLeaves(leaves.filter((leaf) => leaf.spawn.kind !== "agent"));
    dispatch({ type: "CLOSE_TAB", groupId: srcGroupId, tabId });
  };

  // ----- re-attach (detached window → back into the main window) -----

  // Detached side: hand a group back to the main window, then drop it here.
  // When the detached window empties, close it.
  const attachGroupToMain = async (groupId: string) => {
    const g = wsRef.current.groups.find((x) => x.id === groupId);
    if (!g) return;
    try { await SendTerminalAttach(seedFromGroup(g, g.tabs)); } catch (e) { console.error(e); return; }
    for (const t of g.tabs) await stopLeaves(allLeaves(t.tree));
    dispatch({ type: "CLOSE_GROUP", groupId });
    if (wsRef.current.groups.length <= 1) { try { Quit(); } catch {} }
  };

  // Main side: a detached window dropped a serialized workspace back in. Add its
  // groups as floating windows (cascaded so they don't all overlap) and respawn.
  const processAttach = async (seed: string) => {
    let pw: PersistedWorkspace;
    try { pw = JSON.parse(seed); } catch { return; }
    if (!pw?.groups?.length) return;
    const hy = hydrateWorkspace(pw);
    const cr = getCanvasRect();
    const cw = cr?.width ?? size.w, ch = cr?.height ?? size.h;
    const base = wsRef.current.groups.length;
    const w = Math.min(720, Math.max(360, cw - 60));
    const h = Math.min(460, Math.max(220, ch - 60));
    const groups: Group[] = hy.groups.map((g, i) => ({
      ...g, snap: null,
      x: Math.min(40 + (base + i) * 28, Math.max(0, cw - w)),
      y: Math.min(28 + (base + i) * 28, Math.max(0, ch - h)),
      w, h,
    }));
    dispatch({ type: "ADD_GROUPS", groups });
    await respawn({ groups, zCounter: groups.length, nextGroupNum: groups.length + 1 });
  };

  // Spawn a new pane that inherits the working context of `src` so a split
  // opens in the same folder you're in: project panes reopen in the project
  // dir, plain shells reuse their spawn cwd. Falls back to a default shell.
  const spawnInherited = async (src: Leaf | null, kindOverride?: string): Promise<Leaf | null> => {
    const sp = src?.spawn;
    try {
      if (sp?.workspaceContext?.kind === "agent" && !sp.workspaceContext.sessionId) {
        throw new Error(tt("terminal.workspace.agentSessionMetadataMissing"));
      }
      const agentSessionId = sp?.workspaceContext?.kind === "agent" ? sp.workspaceContext.sessionId
        : sp?.kind === "agent" ? sp.laraSessionId : undefined;
      if (agentSessionId) {
        const k = kindOverride || sp?.terminalKind || resolveDefaultKind();
        if (!k) return null;
        const result = await StartPtyForAgentSession(agentSessionId, k, 100, 30, sp?.workspaceRootKey || "");
        const label = `${result.label} · ${profileLabel(k).toLowerCase()}`;
        return makeLeaf(result.termId, label, k, {
          kind: "local", terminalKind: k, workspaceContext: { kind: "agent", sessionId: agentSessionId }, workspaceRootKey: result.rootKey,
        }, "ready");
      }
      if (sp?.kind === "agent") throw new Error(tt("terminal.workspace.agentSessionMetadataMissing"));
      if (sp?.kind === "project" && sp.projectName) {
        const k = kindOverride || sp.terminalKind || resolveDefaultKind();
        const id: string = await (kindOverride ? StartPtyForProject : StartPtyForProjectCommand)(sp.projectName, k || "", 100, 30);
        return makeLeaf(id, `${sp.projectName} · ${profileLabel(k).toLowerCase()}`, k || "powershell", { kind: "project", projectName: sp.projectName, terminalKind: k }, "ready");
      }
      if (sp?.kind === "local" && sp.cwd) {
        const k = kindOverride || sp.terminalKind || resolveDefaultKind();
        if (!k) return null;
        const id: string = await StartPtyKindIn(k, sp.cwd, 100, 30);
        return makeLeaf(id, profileLabel(k).toLowerCase(), k, { kind: "local", terminalKind: k, cwd: sp.cwd }, "ready");
      }
      const k = kindOverride || resolveDefaultKind();
      if (!k) return null;
      const id: string = await StartPtyKind(k, 100, 30);
      return makeLeaf(id, profileLabel(k).toLowerCase(), k, { kind: "local", terminalKind: k }, "ready");
    } catch (e) { console.error(e); return null; }
  };

  // Spawn a fresh local terminal in a new maximized group. Used by the empty
  // state (when every group has been closed) and the "+" affordance.
  const createGroup = async (kind?: string) => {
    const k = kind || resolveDefaultKind();
    if (!k) return;
    try {
      const id: string = await StartPtyKind(k, 100, 30);
      const n = nextTabNumber();
      const leaf = makeLeaf(id, `${profileLabel(k).toLowerCase()} ${n}`, k, { kind: "local", terminalKind: k }, "ready");
      const tab: Tab = { tabId: newTabId(), label: leaf.label, tree: leaf, activeLeafId: leaf.id };
      const gid = newGroupId();
      dispatch({ type: "ADD_GROUP_WITH_TAB", groupId: gid, tab, geom: cascadeGeom() });
      dispatch({ type: "SET_SNAP", groupId: gid, snap: "max" });
    } catch (e) { console.error(e); }
  };

  // ----- GroupApi (passed to every TerminalGroup) -----
  const api: GroupApi = useMemo(() => ({
    claimTerminalFocus,
    openFiles: (groupId, tabId, leafId) => {
      const tab = wsRef.current.groups.find(group => group.id === groupId)?.tabs.find(tab => tab.tabId === tabId);
      const leaf = tab ? findLeaf(tab.tree, leafId) : null;
      if (leaf) openFilesForLeaf(leaf);
    },
    focus: (groupId) => dispatch({ type: "FOCUS_GROUP", groupId }),
    move: (groupId, x, y) => dispatch({ type: "MOVE_GROUP", groupId, x, y }),
    resize: (groupId, x, y, w, h) => dispatch({ type: "RESIZE_GROUP", groupId, x, y, w, h }),
    setSnap: (groupId, snap) => dispatch({ type: "SET_SNAP", groupId, snap }),
    previewSnap: (snap) => setPreview(snap),
    setActiveTab: (groupId, tabId) => dispatch({ type: "SET_ACTIVE_TAB", groupId, tabId }),
    renameTab: (groupId, tabId, label) => dispatch({ type: "RENAME_TAB", groupId, tabId, label }),

    closeTab: async (groupId, tabId) => {
      const g = wsRef.current.groups.find((x) => x.id === groupId);
      const t = g?.tabs.find((x) => x.tabId === tabId);
      if (t) await stopLeaves(allLeaves(t.tree));
      dispatch({ type: "CLOSE_TAB", groupId, tabId });
    },

    closePane: async (groupId, tabId, leafId) => {
      const g = wsRef.current.groups.find((x) => x.id === groupId);
      const t = g?.tabs.find((x) => x.tabId === tabId);
      const l = t && findLeaf(t.tree, leafId);
      if (l) await stopLeaves([l]);
      dispatch({ type: "CLOSE_PANE", groupId, tabId, leafId });
    },

    focusPane: (groupId, tabId, leafId) => dispatch({ type: "FOCUS_PANE", groupId, tabId, leafId }),
    terminalError: (operation) => flashRef.current?.(tt({
      copy: "terminal.clipboard.copyFailed",
      paste: "terminal.clipboard.pasteFailed",
      input: "terminal.clipboard.inputFailed",
    }[operation])),

    // Restart a local pane in place with new PHP/Node versions. A live shell's
    // PATH can't be mutated, so we spawn a fresh PTY with the picked runtime and
    // swap the leaf. Only meaningful for local terminals (project/ssh resolve
    // their own runtime). `runtime` fields left undefined keep the current pick.
    setLeafRuntime: async (groupId, tabId, leafId, runtime) => {
      const g = wsRef.current.groups.find((x) => x.id === groupId);
      const t = g?.tabs.find((x) => x.tabId === tabId);
      const l = t && findLeaf(t.tree, leafId);
      if (!g || g.activeTabId !== tabId || !t || t.activeLeafId !== leafId ||
          !l || l.spawn.kind !== "local" || l.spawn.workspaceContext) return;
      const k = l.spawn.terminalKind || resolveDefaultKind();
      const php = runtime.php !== undefined ? runtime.php : (l.spawn.phpVersion || "");
      const node = runtime.node !== undefined ? runtime.node : (l.spawn.nodeVersion || "");
      const newSpawn = { ...l.spawn, phpVersion: php, nodeVersion: node };
      try {
        const id: string = await StartPtyKindRuntime(k, l.spawn.cwd || "", php, node, 100, 30);
        await stopLeaves([l]);
        dispatch({ type: "REPLACE_LEAF", groupId, tabId, leafId, replacement: makeLeaf(id, l.label, k, newSpawn, "ready") });
      } catch (e) { console.error(e); }
    },

    addTab: async (groupId, kind) => {
      const k = kind || resolveDefaultKind();
      if (!k) return;
      try {
        const id: string = await StartPtyKind(k, 100, 30);
        const n = nextTabNumber();
        const leaf = makeLeaf(id, `${profileLabel(k).toLowerCase()} ${n}`, k, { kind: "local", terminalKind: k }, "ready");
        const tab: Tab = { tabId: newTabId(), label: leaf.label, tree: leaf, activeLeafId: leaf.id };
        dispatch({ type: "ADD_TAB", groupId, tab });
      } catch (e) { console.error(e); }
    },

    splitActive: async (groupId, tabId, direction, kind) => {
      const g = wsRef.current.groups.find((x) => x.id === groupId);
      const t = g?.tabs.find((x) => x.tabId === tabId);
      if (!t) return;
      const source = findLeaf(t.tree, t.activeLeafId);
      if (source?.spawn.kind === "ssh") {
        const host = source.spawn.sshHostName;
        if (!host) return;
        const pendingId = newLeafId();
        const pending = makeLeaf(pendingId, `ssh · ${host}`, "ssh", { kind: "ssh", sshHostName: host }, "pending");
        dispatch({ type: "SPLIT_LEAF", groupId, tabId, targetLeafId: t.activeLeafId, newLeaf: pending, direction, side: "after" });
        const reqId = `split-${rand()}`;
        const evt = `ssh:progress:${reqId}`;
        EventsOn(evt, (stage: string) => dispatch({ type: "PATCH_LEAF", groupId, tabId, leafId: pendingId, patch: { sshStage: stage } }));
        try {
          const id = await StartPtyForSSH(host, reqId, 100, 30);
          dispatch({ type: "REPLACE_LEAF", groupId, tabId, leafId: pendingId, replacement: makeLeaf(id, `ssh · ${host}`, "ssh", pending.spawn, "ready") });
        } catch (error) {
          dispatch({ type: "PATCH_LEAF", groupId, tabId, leafId: pendingId, patch: { status: "error", errorMsg: String(error) } });
        } finally { EventsOff(evt); }
        return;
      }
      // Open the split in the same working context as the active pane.
      const leaf = await spawnInherited(source, kind);
      if (!leaf) return;
      dispatch({ type: "SPLIT_LEAF", groupId, tabId, targetLeafId: t.activeLeafId, newLeaf: leaf, direction, side: "after" });
    },

    onPaneDrop: (destGroupId, destTabId, targetLeafId, region, e) => {
      const paneRaw = e.dataTransfer.getData("application/x-lara4ever-pane");
      const pane = paneRaw ? decodePane(paneRaw) : null;
      const tabRaw = e.dataTransfer.getData("application/x-lara4ever-tab");
      const tab = tabRaw ? decodeTab(tabRaw) : null;
      if (region === "center") {
        // merge: move the whole tab into this group as a tab.
        if (tab) dispatch({ type: "MOVE_TAB", srcGroupId: tab.srcGroupId, tabId: tab.tabId, destGroupId });
        return;
      }
      const { direction, side } = regionToSplit(region as Exclude<DropRegion, "center">);
      if (pane) {
        dispatch({ type: "MOVE_PANE", srcGroupId: pane.srcGroupId, srcTabId: pane.tabId, leafId: pane.leafId, destGroupId, destTabId, targetLeafId, direction, side });
      } else if (tab) {
        dispatch({ type: "DROP_TAB_AS_SPLIT", srcGroupId: tab.srcGroupId, tabId: tab.tabId, destGroupId, destTabId, targetLeafId, direction, side });
      }
    },

    onTabDropOnTab: (destGroupId, targetTabId, e) => {
      const p = decodeTab(e.dataTransfer.getData("application/x-lara4ever-tab"));
      if (!p) return;
      if (p.srcGroupId === destGroupId) dispatch({ type: "REORDER_TAB", groupId: destGroupId, srcTabId: p.tabId, targetTabId });
      else dispatch({ type: "MOVE_TAB", srcGroupId: p.srcGroupId, tabId: p.tabId, destGroupId });
    },

    onTabDropOnStrip: (destGroupId, e) => {
      const p = decodeTab(e.dataTransfer.getData("application/x-lara4ever-tab"));
      if (!p || p.srcGroupId === destGroupId) return;
      dispatch({ type: "MOVE_TAB", srcGroupId: p.srcGroupId, tabId: p.tabId, destGroupId });
    },

    openSftp: (termId, label) => setSftpFor({ termId, label }),
    setDefaultTerminal: (kind) => SetDefaultTerminal(kind).then(refreshTerminals).catch(console.error),
    resolveDefaultKind: () => resolveDefaultKind(),
    detachGroup: (groupId) => { void detachGroup(groupId); },
    onTabDragEnd: (srcGroupId, tabId, sx, sy) => { if (isOutsideWindow(sx, sy)) void detachTab(srcGroupId, tabId); },
    isOutsideWindow,
    // Only detached windows expose "attach back"; in the main window this is
    // undefined so TerminalGroup shows the pop-out button instead.
    attachToMain: detached ? (groupId: string) => { void attachGroupToMain(groupId); } : undefined,
    phpVersions,
    nodeVersions,
  }), [resolveDefaultKind, detached, phpVersions, nodeVersions]);

  // ----- terminal hotkeys: Ctrl+T (new tab), Ctrl+W (close tab) -----
  // Only active while the terminal canvas is the visible page (or in a detached
  // satellite window, where the terminal is always the focus).
  useEffect(() => {
    if (!hydrated || (!active && !detached) || activeSurface !== "terminal") return;
    const handler = (e: KeyboardEvent) => {
      const action = terminalHotkey(e, isTypingInFormField(e.target));
      if (!action) return;
      // Both are keys xterm would otherwise write to the PTY: \x14, and \x17
      // which readline shells use as unix-word-rubout. Ctrl+Backspace still
      // sends \x17 to those shells (see keys.ts), so word-delete survives.
      e.preventDefault();
      e.stopPropagation();
      const g = focusedGroup();
      if (action === "new-tab") {
        if (g) void api.addTab(g.id);
        else void createGroup();
      } else if (g) {
        void api.closeTab(g.id, g.activeTabId);
      }
    };
    // App shortcuts capture at window first, so a configured Ctrl+T/Ctrl+W
    // can stop the event before it reaches the workspace. Document capture
    // still runs before xterm cancels these keydowns at its textarea.
    document.addEventListener("keydown", handler, true);
    return () => document.removeEventListener("keydown", handler, true);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [active, activeSurface, detached, api, hydrated]);

  // Main window: receive groups re-attached from detached windows.
  useEffect(() => {
    if (detached) return;
    // Reattachment is explicit terminal use, but must wait for initial HYDRATE
    // so the restored layout cannot replace the returned groups.
    DrainTerminalAttach().then((seeds: string[]) => { if (seeds?.length) setAttachSeeds(pending => [...pending, ...seeds]); }).catch(() => {});
    const handler = (seed: string) => setAttachSeeds(pending => [...pending, seed]);
    EventsOn("terminal:attach", handler);
    return () => EventsOff("terminal:attach");
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  useEffect(() => {
    if (!hydrated || attachSeeds.length === 0) return;
    setAttachSeeds([]);
    void (async () => { for (const seed of attachSeeds) await processAttach(seed); })();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [hydrated, attachSeeds]);

  // ----- canvas-level tear-off: a tab dropped on empty canvas -----
  const onCanvasDrop = (e: React.DragEvent) => {
    const p = decodeTab(e.dataTransfer.getData("application/x-lara4ever-tab"));
    if (!p) return;
    e.preventDefault();
    const cr = getCanvasRect();
    const w = 640, h = 420;
    let x = cr ? e.clientX - cr.left - 60 : 60;
    let y = cr ? e.clientY - cr.top - 14 : 40;
    x = Math.max(0, Math.min(x, size.w - 120));
    y = Math.max(0, Math.min(y, size.h - 40));
    dispatch({ type: "TEAR_OFF_TAB", srcGroupId: p.srcGroupId, tabId: p.tabId, geom: { x, y, w, h } });
  };

  // ----- external open requests (project / ssh / blank / command) -----
  const lastSeq = useRef<number>(0);
  useEffect(() => {
    if (!hydrated) return;
    const req = openRequest;
    if (!req || req.seq === lastSeq.current) return;
    // Shell-backed requests need local terminal profiles. Agent and SSH
    // processes are launched directly by their backend adapters.
    if (req.kind !== "ssh" && req.kind !== "agent" && req.kind !== "agent-resume" && termRef.current.profiles.length === 0) return;
    lastSeq.current = req.seq;
    claimTerminalFocus();

    // The project name this request is scoped to (if any). "project" carries it
    // as `name`; "blank"/"command" carry it as optional `project`.
    const scopedProject =
      req.kind === "project" ? req.name : (req.kind === "agent" || req.kind === "agent-resume") ? req.project : (req.kind === "blank" || req.kind === "command") ? req.project : undefined;
    // Preferred terminal kind requested by the caller (may be empty → default).
    const requestedShell =
      req.kind === "project" ? req.terminalKind : (req.kind === "blank" || req.kind === "command") ? req.shell : undefined;

    const pendingId = `pending-${req.seq}`;
    const placeholderLabel =
      req.kind === "ssh"
        ? tt("terminal.workspace.sshConnectingLabel", { host: req.host })
        : (req.kind === "agent" || req.kind === "agent-resume")
        ? tt("terminal.workspace.agentStartingLabel", { project: req.project, agent: req.agentLabel })
        : scopedProject
        ? tt("terminal.workspace.projectStartingLabel", { project: scopedProject })
        : tt("terminal.workspace.shellStartingLabel", { shell: profileLabel(requestedShell || resolveDefaultKind()).toLowerCase() });
    const spawn: Leaf["spawn"] =
      req.kind === "ssh"
        ? { kind: "ssh", sshHostName: req.host }
        : (req.kind === "agent" || req.kind === "agent-resume")
        ? { kind: "agent", projectName: req.project, agentId: req.agentId }
        : scopedProject
        ? { kind: "project", projectName: scopedProject, terminalKind: requestedShell }
        : { kind: "local", terminalKind: requestedShell };
    const pendingLeaf = makeLeaf(pendingId, placeholderLabel, req.kind === "ssh" ? "ssh" : (req.kind === "agent" || req.kind === "agent-resume") ? `agent:${req.agentId}` : "local", spawn, "pending");
    const tab: Tab = { tabId: newTabId(), label: placeholderLabel, tree: pendingLeaf, activeLeafId: pendingId };

    const target = focusedGroup();
    let groupId: string;
    if (target) { groupId = target.id; dispatch({ type: "ADD_TAB", groupId, tab }); }
    else { groupId = newGroupId(); dispatch({ type: "ADD_GROUP_WITH_TAB", groupId, tab, geom: cascadeGeom() }); }
    const tabId = tab.tabId;

    let unsub: (() => void) | null = null;
    if (req.kind === "ssh") {
      const evt = `ssh:progress:${req.seq}`;
      const handler = (stage: string) => dispatch({ type: "PATCH_LEAF", groupId, tabId, leafId: pendingId, patch: { sshStage: stage } });
      EventsOn(evt, handler);
      unsub = () => EventsOff(evt);
    }

    (async () => {
      try {
        let realId: string; let label: string; let kind: string; let leafSpawn: Leaf["spawn"];
        if (req.kind === "ssh") {
          realId = await StartPtyForSSH(req.host, String(req.seq), 100, 30);
          label = `ssh · ${req.host}`; kind = "ssh"; leafSpawn = spawn;
        } else if (req.kind === "agent-resume") {
          const session = await ResumeAgentSession(req.sessionId);
          if (!session?.terminalId) throw new Error(tt("terminal.workspace.agentNoTerminalError", { agent: req.agentLabel }));
          let routeState: { routeCanonical: boolean; previewHostname?: string } = { routeCanonical: false };
          if (session.worktreePath) {
            try { routeState = routingForWorktree(await ListProjectRoutes(req.project), session.worktreePath); } catch { /* route badges are best-effort */ }
          }
          realId = session.terminalId;
          label = `${req.project} · ${req.agentLabel}`;
          kind = `agent:${req.agentId}`;
          leafSpawn = {
            kind: "agent", projectName: req.project, agentId: req.agentId, laraSessionId: session.id,
            worktreePath: session.worktreePath,
            routeCanonical: routeState.routeCanonical || undefined,
            previewHostname: routeState.previewHostname,
          };
        } else if (req.kind === "agent") {
          const session = await LaunchAgentSession({
            projectName: req.project,
            agentId: req.agentId,
            mode: req.mode,
            branch: req.branch,
            createBranch: req.createBranch,
            baseRef: req.baseRef,
            worktreeName: req.worktreeName,
            additionalDirectories: req.additionalDirectories,
          });
          if (!session?.terminalId) throw new Error(tt("terminal.workspace.agentNoTerminalError", { agent: req.agentLabel }));
          let routeState: { routeCanonical: boolean; previewHostname?: string } = { routeCanonical: false };
          if (req.mode === "worktree" && session.worktreePath && (req.routeCanonical || req.previewHostname)) {
            try {
              const projectRoutes = await ConfigureWorktreeRouting(
                req.project,
                session.worktreePath,
                !!req.routeCanonical,
                req.previewHostname || "",
                req.routeProxyPort || 0,
              );
              routeState = routingForWorktree(projectRoutes, session.worktreePath);
              const routed = [req.routeCanonical ? tt("terminal.workspace.primaryDomain") : "", req.previewHostname || ""].filter(Boolean).join(" + ");
              flash?.(tt("terminal.workspace.routingReady", { project: req.project, routed }));
            } catch (routingError) {
              flash?.(tt("terminal.workspace.routingFailed", { agent: req.agentLabel, error: String(routingError) }));
            }
          }
          if (req.mode === "worktree" && session.worktreePath && !routeState.routeCanonical && !routeState.previewHostname) {
            try {
              routeState = routingForWorktree(await ListProjectRoutes(req.project), session.worktreePath);
            } catch { /* the agent remains usable even if route metadata cannot be read */ }
          }
          realId = session.terminalId;
          label = `${req.project} · ${req.agentLabel}`;
          kind = `agent:${req.agentId}`;
          leafSpawn = {
            kind: "agent", projectName: req.project, agentId: req.agentId, laraSessionId: session.id,
            worktreePath: session.worktreePath,
            routeCanonical: routeState.routeCanonical || undefined,
            previewHostname: routeState.previewHostname,
          };
        } else {
          const k = requestedShell || resolveDefaultKind();
          if (!k) { dispatch({ type: "CLOSE_TAB", groupId, tabId }); return; }
          kind = k;
          if (scopedProject) {
            realId = await (req.kind === 'command' ? StartPtyForProjectCommand : StartPtyForProject)(scopedProject, k, 100, 30);
            label = `${scopedProject} · ${profileLabel(k).toLowerCase()}`;
            leafSpawn = { kind: "project", projectName: scopedProject, terminalKind: k };
          } else {
            realId = await StartPtyKind(k, 100, 30);
            label = `${profileLabel(k).toLowerCase()} ${nextTabNumber()}`;
            leafSpawn = { kind: "local", terminalKind: k };
          }
          // "command" requests run a command line in the fresh terminal.
          if (req.kind === "command" && req.command) {
            try { await WritePty(realId, req.command + "\r"); } catch (e) { console.error(e); }
          }
        }
        dispatch({ type: "REPLACE_LEAF", groupId, tabId, leafId: pendingId, replacement: makeLeaf(realId, label, kind, leafSpawn, "ready") });
        dispatch({ type: "RENAME_TAB", groupId, tabId, label });
      } catch (err) {
        const msg = String(err);
        console.error("[terminal] open failed:", err);
        dispatch({ type: "PATCH_LEAF", groupId, tabId, leafId: pendingId, patch: { status: "error", errorMsg: msg, label: tt("terminal.workspace.failedLabel", { base: placeholderLabel.replace(/ \(.+\)$/, "") }) } });
      } finally { unsub?.(); }
    })();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [hydrated, openRequest?.seq, terminals.profiles.length]);

  // ----- first load: restore + respawn, or open a default group -----
  useEffect(() => {
    // The main workspace stays mounted across page changes to retain live
    // sessions. Mounting it alone must not start shells or resume agents.
    if (!needsInitialization || initializationStarted.current) return;
    initializationStarted.current = true;
    setInitializationError(null);
    (async () => {
      let info: TerminalsInfo;
      try {
        info = await ListTerminals();
      } catch (error) {
        initializationStarted.current = false;
        setInitializationError(String(error));
        return;
      }
      setTerminals(info);
      termRef.current = info;

      let restored: Workspace | null = null;
      try {
        // Detached windows are seeded by their launching window; the main
        // window restores its own persisted layout.
        const raw: string = detached ? (detachedSeed ?? await GetDetachedSeed()) : await LoadTerminalWorkspace();
        if (raw) {
          const pw = JSON.parse(raw) as PersistedWorkspace;
          if (pw && pw.groups && pw.groups.length) restored = hydrateWorkspace(pw);
        }
      } catch (e) { console.error("[terminal] restore failed:", e); }

      if (restored) {
        dispatch({ type: "HYDRATE", workspace: restored });
        await respawn(restored);
      } else if (!hasExplicitRequest.current) {
        const k = resolveDefaultKind(info);
        if (k) {
          try {
            const id: string = await StartPtyKind(k, 100, 30);
            const labelKind = info.profiles.find((p) => p.kind === k)?.label ?? k;
            const leaf = makeLeaf(id, `${labelKind.toLowerCase()} 1`, k, { kind: "local", terminalKind: k }, "ready");
            const tab: Tab = { tabId: newTabId(), label: leaf.label, tree: leaf, activeLeafId: leaf.id };
            // The very first terminal opens maximized so it fills the page —
            // the user can un-maximize, split, tear off or detach from there.
            const gid = newGroupId();
            dispatch({ type: "ADD_GROUP_WITH_TAB", groupId: gid, tab, geom: cascadeGeom() });
            dispatch({ type: "SET_SNAP", groupId: gid, snap: "max" });
          } catch (e) { console.error(e); }
        }
      }
      setHydrated(true);
    })();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [needsInitialization, initializationAttempt]);

  // Recreate a live PTY for every (pending) leaf in a restored workspace.
  const respawn = async (restoredWs: Workspace) => {
    const items = leavesToRespawn(restoredWs);
    for (const { groupId, tabId, leaf } of items) {
      const sp = leaf.spawn;
      try {
        if (sp.workspaceContext?.kind === "agent") {
          const result = await StartPtyForAgentSession(sp.workspaceContext.sessionId, sp.terminalKind || "", 100, 30, sp.workspaceRootKey || "");
          const updated = { ...sp, workspaceRootKey: result.rootKey };
          dispatch({ type: "REPLACE_LEAF", groupId, tabId, leafId: leaf.id, replacement: makeLeaf(result.termId, `${result.label} · ${profileLabel(sp.terminalKind || resolveDefaultKind()).toLowerCase()}`, sp.terminalKind || "powershell", updated, "ready") });
        } else if (sp.kind === "ssh") {
          const reqId = `restore-${rand()}`;
          const evt = `ssh:progress:${reqId}`;
          const handler = (stage: string) => dispatch({ type: "PATCH_LEAF", groupId, tabId, leafId: leaf.id, patch: { sshStage: stage } });
          EventsOn(evt, handler);
          try {
            const id: string = await StartPtyForSSH(sp.sshHostName || "", reqId, 100, 30);
            dispatch({ type: "REPLACE_LEAF", groupId, tabId, leafId: leaf.id, replacement: makeLeaf(id, `ssh · ${sp.sshHostName}`, "ssh", sp, "ready") });
          } finally { EventsOff(evt); }
        } else if (sp.kind === "agent") {
          if (!sp.laraSessionId) throw new Error(tt("terminal.workspace.agentSessionMetadataMissing"));
          const session = await ResumeAgentSession(sp.laraSessionId);
          if (!session?.terminalId) throw new Error(tt("terminal.workspace.agentNoTerminalGeneric"));
          const label = `${sp.projectName || tt("terminal.workspace.projectFallback")} · ${agentLabel(sp.agentId || tt("terminal.workspace.agentFallback"))}`;
          let restoredSpawn = { ...sp, worktreePath: session.worktreePath || sp.worktreePath };
          if (sp.projectName && restoredSpawn.worktreePath) {
            try {
              restoredSpawn = { ...restoredSpawn, ...routingForWorktree(await ListProjectRoutes(sp.projectName), restoredSpawn.worktreePath) };
            } catch { /* retain the last persisted route badge */ }
          }
          dispatch({ type: "REPLACE_LEAF", groupId, tabId, leafId: leaf.id, replacement: makeLeaf(session.terminalId, label, `agent:${sp.agentId}`, restoredSpawn, "ready") });
        } else if (sp.kind === "project") {
          const id: string = await StartPtyForProjectCommand(sp.projectName || "", sp.terminalKind || "", 100, 30);
          dispatch({ type: "REPLACE_LEAF", groupId, tabId, leafId: leaf.id, replacement: makeLeaf(id, leaf.label, sp.terminalKind || "powershell", sp, "ready") });
        } else {
          const k = sp.terminalKind || resolveDefaultKind();
          const id: string = (sp.phpVersion || sp.nodeVersion)
            ? await StartPtyKindRuntime(k, sp.cwd || "", sp.phpVersion || "", sp.nodeVersion || "", 100, 30)
            : sp.cwd ? await StartPtyKindIn(k, sp.cwd, 100, 30) : await StartPtyKind(k, 100, 30);
          dispatch({ type: "REPLACE_LEAF", groupId, tabId, leafId: leaf.id, replacement: makeLeaf(id, leaf.label, k, sp, "ready") });
        }
      } catch (err) {
        const message = String(err);
        if (sp.kind === "agent" && isUnavailableAgentHistoryError(message)) {
          dispatch({ type: "CLOSE_PANE", groupId, tabId, leafId: leaf.id });
          flashRef.current?.(tt("terminal.workspace.agentHistoryUnavailable", { agent: agentLabel(sp.agentId || tt("terminal.workspace.agentFallback")) }));
        } else {
          dispatch({ type: "PATCH_LEAF", groupId, tabId, leafId: leaf.id, patch: { status: "error", errorMsg: message } });
        }
      }
    }
  };

  // ----- debounced persistence -----
  useEffect(() => {
    if (!hydrated || detached) return; // detached windows never persist (would clobber the main layout)
    const handle = setTimeout(() => {
      try { SaveTerminalWorkspace(JSON.stringify(serializeWorkspace(wsRef.current))); } catch {}
    }, 500);
    return () => clearTimeout(handle);
  }, [ws, hydrated, detached]);

  // ----- render -----
  const previewRect = preview ? snapGeometry(size.w, size.h, preview) : null;
  const focusedGroupId = ws.groups.reduce<Group | null>((top, group) => !top || group.z > top.z ? group : top, null)?.id;

  return (
    <div className="h-full w-full flex flex-col min-h-0">
      <div
        ref={canvasRef}
        className="flex-1 min-h-0 relative overflow-hidden"
        onDragOver={(e) => { if (e.dataTransfer.types.includes("application/x-lara4ever-tab")) e.preventDefault(); }}
        onDrop={onCanvasDrop}
      >
        {ws.groups.length === 0 && (
          <div className="absolute inset-0 flex flex-col items-center justify-center gap-4">
            <TerminalSquare className="w-10 h-10 opacity-20" />
            <div className="text-xs opacity-40">{tt("terminal.workspace.noTerminalOpen")}</div>
            {initializationError && <div className="flex max-w-lg flex-col items-center gap-2 text-xs">
              <p role="alert" className="break-words text-red-400">{initializationError}</p>
              <button type="button" onClick={() => setInitializationAttempt(attempt => attempt + 1)}
                className="rounded border border-white/10 px-3 py-2 hover:bg-white/10 focus-visible:outline focus-visible:outline-2 focus-visible:outline-accent2">
                {tt("documents.retry")}
              </button>
            </div>}
            <div className="flex items-stretch rounded-lg border border-white/10 bg-white/5 overflow-hidden">
              <button
                disabled={!hydrated}
                onClick={() => createGroup()}
                className="flex items-center gap-1.5 px-4 py-2 text-sm hover:bg-white/10"
                title={tt("terminal.workspace.openNewTerminalTooltip")}
              >
                <Plus className="w-4 h-4" /> {tt("terminal.workspace.newTerminalButton")}
              </button>
              <button
                ref={emptyPickerRef}
                disabled={!hydrated}
                onClick={() => setEmptyPickerOpen((v) => !v)}
                className="px-2 border-l border-white/10 hover:bg-white/10 flex items-center"
                title={tt("terminal.pickTerminalTypeTooltip")}
              >
                <ChevronDown className="w-4 h-4" />
              </button>
            </div>
            <PortalMenu open={emptyPickerOpen} anchorRef={emptyPickerRef} onClose={() => setEmptyPickerOpen(false)} align="left" minWidth={200}>
              <div className="px-3 py-1 text-[10px] uppercase opacity-50">{tt("terminal.openTerminalHeader")}</div>
              {terminals.profiles.filter((p) => p.available).length === 0 && (
                <div className="px-3 py-1.5 text-[11px] opacity-60">{tt("terminal.noTerminalAvailable")}</div>
              )}
              {terminals.profiles.filter((p) => p.available).map((p) => (
                <button
                  key={p.kind}
                  onClick={() => { setEmptyPickerOpen(false); createGroup(p.kind); }}
                  className="block w-full text-left px-3 py-1.5 text-[11px] rounded-md hover:bg-white/10"
                >
                  {p.label}{resolveDefaultKind() === p.kind && <span className="opacity-50">{tt("terminal.defaultSuffix")}</span>}
                </button>
              ))}
            </PortalMenu>
          </div>
        )}

        {previewRect && (
          <div
            className="absolute rounded-xl border-2 border-accent2/70 bg-accent2/15 pointer-events-none z-[9999] transition-all duration-75"
            style={{ left: previewRect.x, top: previewRect.y, width: previewRect.w, height: previewRect.h }}
          />
        )}

        {ws.groups.map((g) => (
          <TerminalGroup
            key={g.id}
            group={g}
            terminalOwnsFocus={activeSurface === "terminal"}
            active={active || detached}
            isFocused={activeSurface === "terminal" && windowFocused && (active || detached) && g.id === focusedGroupId}
            focusAgentRequest={focusAgentRequest}
            focusTerminalRequest={focusTerminalRequest}
            canvasW={size.w}
            canvasH={size.h}
            getCanvasRect={getCanvasRect}
            terminals={terminals}
            api={api}
          />
        ))}
      </div>

      {sftpFor && (
        <SFTPModal open={true} termId={sftpFor.termId} hostLabel={sftpFor.label} onClose={() => setSftpFor(null)} />
      )}
    </div>
  );
}

function workspaceContextForLeaf(leaf: Leaf | null | undefined): WorkspaceContextRef | null {
  if (!leaf || leaf.spawn.kind === "ssh") return null;
  const spawn = leaf.spawn;
  if (spawn.kind === "agent" && spawn.laraSessionId) return { kind: "agent", sessionId: spawn.laraSessionId };
  if (spawn.workspaceContext) return spawn.workspaceContext;
  if (spawn.kind === "project" && spawn.projectName) return { kind: "project", projectName: spawn.projectName };
  return null;
}
