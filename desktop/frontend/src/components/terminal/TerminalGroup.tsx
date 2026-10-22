// A single floating group window: title bar (move), tab strip (browser-style
// reorder / tear-off / merge), maximize/close, 8 resize handles, and the active
// tab's split tree. Move/resize/snap use pointer events; tab/pane DnD use HTML5
// drag-and-drop. Geometry is derived from snap state or the group's restore box.

import { useEffect, useRef, useState } from "react";
import {
  Plus, X, TerminalSquare, ChevronDown, SplitSquareHorizontal, SplitSquareVertical, FolderUp, Square, Copy, ExternalLink, ArrowLeftToLine, Boxes, Check,
} from "lucide-react";
import clsx from "clsx";
import PortalMenu from "../PortalMenu";
import type { TerminalErrorHandler } from "../Terminal";
import PaneRenderer from "./PaneRenderer";
import { allLeaves, findLeaf } from "./treeUtils";
import { snapGeometry, snapZoneForPointer } from "./snap";
import { MIME_TAB, encode, decodeTab } from "./dnd";
import { wheelScrollDelta } from "./tabStrip";
import type { Group, RuntimeVersion, SnapState, TerminalFocusRequest, TerminalsInfo } from "./types";
import type { DropRegion } from "./dnd";
import { useT } from "../../lib/i18n";

const MIN_W = 240;
const MIN_H = 160;

export type GroupApi = {
  focus: (groupId: string) => void;
  claimTerminalFocus: () => void;
  openFiles: (groupId: string, tabId: string, leafId: string) => void;
  move: (groupId: string, x: number, y: number) => void;
  resize: (groupId: string, x: number, y: number, w: number, h: number) => void;
  setSnap: (groupId: string, snap: SnapState) => void;
  previewSnap: (snap: SnapState) => void;
  setActiveTab: (groupId: string, tabId: string) => void;
  closeTab: (groupId: string, tabId: string) => void;
  renameTab: (groupId: string, tabId: string, label: string) => void;
  addTab: (groupId: string, kind?: string) => void;
  focusPane: (groupId: string, tabId: string, leafId: string) => void;
  terminalError?: TerminalErrorHandler;
  closePane: (groupId: string, tabId: string, leafId: string) => void;
  splitActive: (groupId: string, tabId: string, direction: "horizontal" | "vertical", kind?: string) => void;
  onPaneDrop: (destGroupId: string, destTabId: string, targetLeafId: string, region: DropRegion, e: React.DragEvent) => void;
  onTabDropOnTab: (destGroupId: string, targetTabId: string, e: React.DragEvent) => void;
  onTabDropOnStrip: (destGroupId: string, e: React.DragEvent) => void;
  openSftp: (termId: string, label: string) => void;
  setDefaultTerminal: (kind: string) => void;
  resolveDefaultKind: () => string;
  // detach to an independent OS window (second process)
  detachGroup: (groupId: string) => void;
  onTabDragEnd: (srcGroupId: string, tabId: string, screenX: number, screenY: number) => void;
  isOutsideWindow: (screenX: number, screenY: number) => boolean;
  // present only inside a detached window: bring this group back into LaraEnv
  attachToMain?: (groupId: string) => void;
  // per-terminal PHP/Node runtime (local terminals only)
  setLeafRuntime: (groupId: string, tabId: string, leafId: string, runtime: { php?: string; node?: string }) => void;
  phpVersions: RuntimeVersion[];
  nodeVersions: RuntimeVersion[];
};

export default function TerminalGroup({
  group, isFocused, terminalOwnsFocus = true, active = true, focusAgentRequest, focusTerminalRequest, canvasW, canvasH, getCanvasRect, terminals, api,
}: {
  group: Group;
  isFocused: boolean;
  terminalOwnsFocus?: boolean;
  active?: boolean;
  focusAgentRequest?: { sessionId: string; seq: number } | null;
  focusTerminalRequest?: TerminalFocusRequest | null;
  canvasW: number;
  canvasH: number;
  getCanvasRect: () => DOMRect | null;
  terminals: TerminalsInfo;
  api: GroupApi;
}) {
  // Named `tt`, not `t` — this file's pervasive convention names a single Tab
  // object `t` (group.tabs.map((t) => ...), etc.), which would shadow useT()'s
  // return value everywhere it matters.
  const tt = useT();
  const [editingTabId, setEditingTabId] = useState<string | null>(null);
  const [pickerOpen, setPickerOpen] = useState(false);
  const [runtimeOpen, setRuntimeOpen] = useState(false);
  const editRef = useRef<HTMLInputElement>(null);
  const pickerRef = useRef<HTMLButtonElement>(null);
  const runtimeRef = useRef<HTMLButtonElement>(null);
  const handledFocus = useRef({ terminal: 0, agent: 0 });
  const dragState = useRef<{ kind: "move" | "resize"; edge?: string; sx: number; sy: number; ox: number; oy: number; ow: number; oh: number } | null>(null);

  // An explicit switch takes precedence over a menu left open in its target
  // group. Ordinary workspace updates and repeated request objects do not.
  useEffect(() => {
    let closeMenus = false;
    if (focusTerminalRequest && focusTerminalRequest.seq !== handledFocus.current.terminal) {
      handledFocus.current.terminal = focusTerminalRequest.seq;
      closeMenus = group.tabs.some((tab) => allLeaves(tab.tree).some((leaf) => leaf.id === focusTerminalRequest.terminalId));
    }
    if (focusAgentRequest && focusAgentRequest.seq !== handledFocus.current.agent) {
      handledFocus.current.agent = focusAgentRequest.seq;
      closeMenus ||= group.tabs.some((tab) => allLeaves(tab.tree).some((leaf) => leaf.spawn.laraSessionId === focusAgentRequest.sessionId));
    }
    if (closeMenus) {
      setPickerOpen(false);
      setRuntimeOpen(false);
    }
  }, [focusTerminalRequest, focusAgentRequest, group.tabs]);

  useEffect(() => { if (!active) { setPickerOpen(false); setRuntimeOpen(false); setEditingTabId(null); } }, [active]);

  const rect = snapGeometry(canvasW, canvasH, group.snap) ?? { x: group.x, y: group.y, w: group.w, h: group.h };
  const activeTab = group.tabs.find((t) => t.tabId === group.activeTabId) ?? group.tabs[0];

  // The menu's actions are specific to the pane that opened it.
  useEffect(() => { setRuntimeOpen(false); }, [activeTab?.tabId, activeTab?.activeLeafId]);

  // ----- move (title bar) -----
  const onTitlePointerDown = (e: React.PointerEvent) => {
    if (e.button !== 0) return;
    api.claimTerminalFocus();
    api.focus(group.id);
    (e.target as Element).setPointerCapture?.(e.pointerId);
    dragState.current = { kind: "move", sx: e.clientX, sy: e.clientY, ox: rect.x, oy: rect.y, ow: rect.w, oh: rect.h };
  };

  const onResizePointerDown = (edge: string) => (e: React.PointerEvent) => {
    if (e.button !== 0) return;
    e.stopPropagation();
    api.focus(group.id);
    (e.target as Element).setPointerCapture?.(e.pointerId);
    dragState.current = { kind: "resize", edge, sx: e.clientX, sy: e.clientY, ox: rect.x, oy: rect.y, ow: rect.w, oh: rect.h };
  };

  const onPointerMove = (e: React.PointerEvent) => {
    const d = dragState.current;
    if (!d) return;
    const dx = e.clientX - d.sx;
    const dy = e.clientY - d.sy;
    if (d.kind === "move") {
      // While snapped, the first drag restores the window to its float size.
      const w = group.snap ? group.w : d.ow;
      const h = group.snap ? group.h : d.oh;
      let nx = d.ox + dx;
      let ny = d.oy + dy;
      nx = Math.max(-(w - 80), Math.min(nx, canvasW - 40));
      ny = Math.max(0, Math.min(ny, canvasH - 28));
      api.move(group.id, nx, ny);
      api.resize(group.id, nx, ny, w, h); // keep restore size in sync when tearing off a snap
      const cr = getCanvasRect();
      if (cr) {
        const px = e.clientX - cr.left;
        const py = e.clientY - cr.top;
        api.previewSnap(snapZoneForPointer(px, py, canvasW, canvasH));
      }
    } else {
      const edge = d.edge!;
      let { ox: nx, oy: ny, ow: nw, oh: nh } = d;
      if (edge.includes("e")) nw = Math.max(MIN_W, d.ow + dx);
      if (edge.includes("s")) nh = Math.max(MIN_H, d.oh + dy);
      if (edge.includes("w")) { const w = Math.max(MIN_W, d.ow - dx); nx = d.ox + (d.ow - w); nw = w; }
      if (edge.includes("n")) { const h = Math.max(MIN_H, d.oh - dy); ny = d.oy + (d.oh - h); nh = h; }
      api.resize(group.id, nx, ny, nw, nh);
    }
  };

  const endDrag = (e: React.PointerEvent) => {
    const d = dragState.current;
    dragState.current = null;
    if (!d) return;
    if (d.kind === "move") {
      api.previewSnap(null);
      // Dropped outside the LaraEnv window → detach into an independent window.
      if (api.isOutsideWindow(e.screenX, e.screenY)) { api.detachGroup(group.id); return; }
      const cr = getCanvasRect();
      if (cr) {
        const zone = snapZoneForPointer(e.clientX - cr.left, e.clientY - cr.top, canvasW, canvasH);
        if (zone) api.setSnap(group.id, zone);
      }
    }
  };

  const startRename = (id: string) => {
    setEditingTabId(id);
    setTimeout(() => editRef.current?.select(), 0);
  };
  const commitRename = (id: string, v: string) => { api.renameTab(group.id, id, v); setEditingTabId(null); };

  const activeLeaf = activeTab ? findLeaf(activeTab.tree, activeTab.activeLeafId) : null;
  const isSshReady = activeLeaf?.kind === "ssh" && activeLeaf?.status === "ready";

  // Per-terminal runtime picker only applies to local terminals (project/ssh
  // panes resolve their own PHP/Node). "" = follow the global active version.
  const isLocal = activeLeaf?.spawn.kind === "local" && !activeLeaf.spawn.workspaceContext;
  const activePhp = api.phpVersions.find((v) => v.active)?.version ?? "";
  const activeNode = api.nodeVersions.find((v) => v.active)?.version ?? "";
  const leafPhp = activeLeaf?.spawn.phpVersion || "";
  const leafNode = activeLeaf?.spawn.nodeVersion || "";
  const setRuntime = (runtime: { php?: string; node?: string }) => {
    if (activeTab && activeLeaf) api.setLeafRuntime(group.id, activeTab.tabId, activeLeaf.id, runtime);
  };

  return (
    <div
      className={clsx(
        "absolute overflow-hidden flex flex-col bg-[#171717] border border-white/[0.08]",
        // Floating groups are rounded with a drop shadow; a maximized group
        // must stay square & flush so its corners don't inset from the (square,
        // translucent, frameless) window and leak the backdrop through.
        group.snap !== "max" && "rounded-md shadow-[0_12px_48px_rgba(0,0,0,0.55)]",
      )}
      data-testid="terminal-group"
      style={group.snap === "max" ? { inset: 0, zIndex: group.z } : { left: rect.x, top: rect.y, width: rect.w, height: rect.h, zIndex: group.z }}
      onPointerDownCapture={(e) => {
        // Menu controls raise the group on click, together with opening their
        // menu. Raising it on press schedules xterm focus before a held mouse
        // button is released, taking focus away from the control.
        if (pickerRef.current?.contains(e.target as Node) || runtimeRef.current?.contains(e.target as Node)) return;
        api.focus(group.id);
      }}
      onPointerMove={onPointerMove}
      onPointerUp={endDrag}
      onPointerCancel={endDrag}
      // canvas drop bubbling: a tab dropped onto the group chrome shouldn't tear off
      onDragOver={(e) => { if (e.dataTransfer.types.includes(MIME_TAB)) e.preventDefault(); }}
    >
      {/* ----- title / tab strip ----- */}
      <div
        className="flex items-center h-8 bg-[#101010] border-b border-white/[0.08] cursor-move select-none"
        onPointerDown={onTitlePointerDown}
        onDoubleClick={() => api.setSnap(group.id, group.snap === "max" ? null : "max")}
        onDrop={(e) => { if (decodeTab(e.dataTransfer.getData(MIME_TAB))) { e.preventDefault(); e.stopPropagation(); api.onTabDropOnStrip(group.id, e); } }}
      >
        <div
          className="flex items-stretch h-full flex-1 min-w-0 overflow-x-auto no-scrollbar"
          onWheel={(e) => { e.currentTarget.scrollLeft += wheelScrollDelta(e, e.currentTarget); }}
        >
          {group.tabs.map((t) => {
            const isActive = group.activeTabId === t.tabId;
            const isEditing = editingTabId === t.tabId;
            const paneCount = allLeaves(t.tree).length;
            const agentLeaves = allLeaves(t.tree).filter((leaf) => leaf.spawn.kind === "agent");
            const ownsMainRoute = agentLeaves.some((leaf) => leaf.spawn.routeCanonical);
            const previewHost = agentLeaves.find((leaf) => leaf.spawn.previewHostname)?.spawn.previewHostname;
            const unRoutedWorktree = agentLeaves.some((leaf) => leaf.spawn.worktreePath) && !ownsMainRoute && !previewHost;
            return (
              <div
                key={t.tabId}
                draggable={!isEditing}
                onPointerDown={(e) => e.stopPropagation()} // don't start a window-move from a tab
                onDragStart={(e) => {
                  e.dataTransfer.setData(MIME_TAB, encode({ srcGroupId: group.id, tabId: t.tabId }));
                  e.dataTransfer.effectAllowed = "move";
                }}
                onDragEnd={(e) => api.onTabDragEnd(group.id, t.tabId, e.screenX, e.screenY)}
                onDragOver={(e) => { if (e.dataTransfer.types.includes(MIME_TAB)) { e.preventDefault(); e.dataTransfer.dropEffect = "move"; } }}
                onDrop={(e) => { if (decodeTab(e.dataTransfer.getData(MIME_TAB))) { e.preventDefault(); e.stopPropagation(); api.onTabDropOnTab(group.id, t.tabId, e); } }}
                onClick={() => { if (!isEditing) { api.claimTerminalFocus(); api.setActiveTab(group.id, t.tabId); } }}
                onDoubleClick={(e) => { e.stopPropagation(); startRename(t.tabId); }}
                className={clsx(
                  "relative flex items-center gap-1 px-3 text-[11px] cursor-pointer transition-colors shrink-0 border-r border-white/[0.06]",
                  isActive ? "bg-[#171717] text-white" : "text-white/55 hover:bg-white/5",
                )}
                title={findLeaf(t.tree, t.activeLeafId)?.spawn.workspaceContext ? findLeaf(t.tree, t.activeLeafId)?.label : tt("terminal.group.tabTooltip")}
              >
                <TerminalSquare className="w-3 h-3 shrink-0" />
                {isEditing ? (
                  <input
                    ref={editRef}
                    defaultValue={t.label}
                    onBlur={(e) => commitRename(t.tabId, e.target.value)}
                    onKeyDown={(e) => {
                      if (e.key === "Enter") commitRename(t.tabId, (e.target as HTMLInputElement).value);
                      if (e.key === "Escape") setEditingTabId(null);
                    }}
                    onClick={(e) => e.stopPropagation()}
                    className="bg-transparent border-b border-accent outline-none w-20 text-[11px]"
                  />
                ) : (
                  <span className="whitespace-nowrap">{t.label}{paneCount > 1 && <span className="opacity-50"> ×{paneCount}</span>}</span>
                )}
                {!isEditing && ownsMainRoute && <span className="rounded border border-emerald-400/30 bg-emerald-500/10 px-1 py-0.5 font-mono text-[7px] text-emerald-200">{tt("terminal.group.mainBadge")}</span>}
                {!isEditing && previewHost && <span className="max-w-28 truncate rounded border border-sky-400/30 bg-sky-500/10 px-1 py-0.5 font-mono text-[7px] text-sky-200" title={previewHost}>{tt("terminal.group.previewBadge")}</span>}
                {!isEditing && unRoutedWorktree && <span className="rounded border border-white/10 bg-white/5 px-1 py-0.5 font-mono text-[7px] opacity-45">{tt("terminal.group.noRouteBadge")}</span>}
                <button
                  onClick={(e) => { e.stopPropagation(); api.closeTab(group.id, t.tabId); }}
                  className="ml-0.5 opacity-60 hover:opacity-100 hover:text-red-300"
                  title={tt("terminal.group.closeTabTooltip")}
                ><X className="w-3 h-3" /></button>
                {isActive && <span className="pointer-events-none absolute inset-x-0 bottom-0 h-[2px] bg-[#e0e0e0]" />}
              </div>
            );
          })}

          {/* new tab + picker */}
          <div className="flex items-stretch h-full shrink-0" onPointerDown={(e) => e.stopPropagation()}>
            <button onClick={() => api.addTab(group.id)} className="flex items-center px-2 text-[11px] hover:bg-white/5" title={tt("terminal.group.newTabTooltip")}><Plus className="w-3 h-3" /></button>
            <button ref={pickerRef} onClick={() => { api.focus(group.id); setPickerOpen((v) => !v); }} className="px-1.5 hover:bg-white/5 flex items-center" title={tt("terminal.pickTerminalTypeTooltip")}><ChevronDown className="w-3 h-3" /></button>
          </div>
        </div>

        {/* right-side actions */}
        <div className="flex items-center gap-1 shrink-0" onPointerDown={(e) => e.stopPropagation()}>
          {activeLeaf && !api.attachToMain && !isSshReady && (
            <button onClick={() => api.openFiles(group.id, activeTab!.tabId, activeLeaf.id)} className="p-1 rounded hover:bg-white/10 text-white/70" title={tt("workspace.navigation.openFiles")} aria-label={tt("workspace.navigation.openFiles")}><FolderUp className="w-3.5 h-3.5" /></button>
          )}
          {isSshReady && (
            <button onClick={() => api.openSftp(activeLeaf!.id, activeLeaf!.label.replace(/^ssh · /, ""))} className="p-1 rounded hover:bg-white/10 text-white/70" title={tt("terminal.group.sftpTooltip")}><FolderUp className="w-3.5 h-3.5" /></button>
          )}
          {isLocal && (
            <button
              ref={runtimeRef}
              onClick={() => { api.focus(group.id); setRuntimeOpen((v) => !v); }}
              className={clsx(
                "flex items-center gap-1 px-1.5 py-0.5 rounded text-[10px] hover:bg-white/10",
                (leafPhp || leafNode) ? "text-accent" : "text-white/60",
              )}
              title={tt("terminal.group.runtimeTooltip")}
            >
              <Boxes className="w-3 h-3" />
              <span className="whitespace-nowrap">PHP {leafPhp || activePhp || tt("terminal.group.autoBadge")}</span>
              {leafNode && <span className="whitespace-nowrap opacity-80">· Node {leafNode}</span>}
            </button>
          )}
          {activeTab && (
            <>
              <button onClick={() => api.splitActive(group.id, activeTab.tabId, "horizontal")} className="p-1 rounded hover:bg-white/10 text-white/70" title={tt("terminal.group.splitRightTooltip")}><SplitSquareHorizontal className="w-3.5 h-3.5" /></button>
              <button onClick={() => api.splitActive(group.id, activeTab.tabId, "vertical")} className="p-1 rounded hover:bg-white/10 text-white/70" title={tt("terminal.group.splitDownTooltip")}><SplitSquareVertical className="w-3.5 h-3.5" /></button>
            </>
          )}
          {api.attachToMain
            ? <button onClick={() => api.attachToMain!(group.id)} className="p-1 rounded hover:bg-white/10 text-white/70" title={tt("terminal.group.attachTooltip")}><ArrowLeftToLine className="w-3.5 h-3.5" /></button>
            : <button onClick={() => api.detachGroup(group.id)} className="p-1 rounded hover:bg-white/10 text-white/70" title={tt("terminal.group.detachTooltip")}><ExternalLink className="w-3.5 h-3.5" /></button>}
          <button onClick={() => api.setSnap(group.id, group.snap === "max" ? null : "max")} className="p-1 rounded hover:bg-white/10 text-white/70" title={group.snap === "max" ? tt("terminal.group.restoreTooltip") : tt("terminal.group.maximizeTooltip")}>
            {group.snap === "max" ? <Copy className="w-3.5 h-3.5" /> : <Square className="w-3.5 h-3.5" />}
          </button>
          <button onClick={() => group.tabs.forEach((t) => api.closeTab(group.id, t.tabId))} className="p-1 rounded hover:bg-red-500/30 text-white/70 hover:text-red-200" title={tt("terminal.group.closeGroupTooltip")}><X className="w-3.5 h-3.5" /></button>
        </div>
      </div>

      {/* ----- body ----- */}
      <div className="flex-1 min-h-0 relative">
        {group.tabs.map((t) => (
          <div key={t.tabId} className="absolute inset-0" style={{ visibility: group.activeTabId === t.tabId ? "visible" : "hidden" }}>
            <PaneRenderer
              tab={t}
              groupId={group.id}
              isTabActive={group.activeTabId === t.tabId}
              isGroupFocused={isFocused && !editingTabId && !pickerOpen && !runtimeOpen}
              focusRequest={Math.max(
                focusTerminalRequest && t.activeLeafId === focusTerminalRequest.terminalId ? focusTerminalRequest.seq : 0,
                focusAgentRequest && findLeaf(t.tree, t.activeLeafId)?.spawn.laraSessionId === focusAgentRequest.sessionId ? focusAgentRequest.seq : 0,
              ) || undefined}
              onFocus={(leafId, intent) => {
                if (intent === "passive" && !terminalOwnsFocus) return;
                if (intent !== "passive") api.claimTerminalFocus();
                if (!isFocused) api.focus(group.id);
                if (group.activeTabId !== t.tabId) api.setActiveTab(group.id, t.tabId);
                if (t.activeLeafId !== leafId) api.focusPane(group.id, t.tabId, leafId);
              }}
              onError={api.terminalError}
              onClose={(leafId) => api.closePane(group.id, t.tabId, leafId)}
              onPaneDrop={(targetLeafId, region, e) => api.onPaneDrop(group.id, t.tabId, targetLeafId, region, e)}
            />
          </div>
        ))}
      </div>

      {/* ----- resize handles (hidden when maximized) ----- */}
      {group.snap !== "max" && (
        <>
          <Handle cls="top-0 left-2 right-2 h-1 cursor-n-resize" on={onResizePointerDown("n")} />
          <Handle cls="bottom-0 left-2 right-2 h-1 cursor-s-resize" on={onResizePointerDown("s")} />
          <Handle cls="left-0 top-2 bottom-2 w-1 cursor-w-resize" on={onResizePointerDown("w")} />
          <Handle cls="right-0 top-2 bottom-2 w-1 cursor-e-resize" on={onResizePointerDown("e")} />
          <Handle cls="top-0 left-0 w-2 h-2 cursor-nw-resize" on={onResizePointerDown("nw")} />
          <Handle cls="top-0 right-0 w-2 h-2 cursor-ne-resize" on={onResizePointerDown("ne")} />
          <Handle cls="bottom-0 left-0 w-2 h-2 cursor-sw-resize" on={onResizePointerDown("sw")} />
          <Handle cls="bottom-0 right-0 w-2 h-2 cursor-se-resize" on={onResizePointerDown("se")} />
        </>
      )}

      <PortalMenu open={pickerOpen} anchorRef={pickerRef} onClose={() => setPickerOpen(false)} align="left" minWidth={200}>
        <div className="px-3 py-1 text-[10px] uppercase opacity-50">{tt("terminal.openTerminalHeader")}</div>
        {terminals.profiles.filter((p) => p.available).length === 0 && (
          <div className="px-3 py-1.5 text-[11px] opacity-60">{tt("terminal.noTerminalAvailable")}</div>
        )}
        {terminals.profiles.filter((p) => p.available).map((p) => (
          <div key={p.kind} className="flex items-center gap-1 group">
            <button onClick={() => { setPickerOpen(false); api.addTab(group.id, p.kind); }} className="flex-1 text-left px-3 py-1.5 text-[11px] rounded-md hover:bg-white/10">
              {p.label}{api.resolveDefaultKind() === p.kind && <span className="opacity-50">{tt("terminal.defaultSuffix")}</span>}
            </button>
            <button onClick={(e) => { e.stopPropagation(); api.setDefaultTerminal(p.kind); }} className="px-2 py-1 text-[10px] opacity-0 group-hover:opacity-60 hover:!opacity-100 hover:text-accent" title={tt("terminal.group.setDefaultTooltip")}>★</button>
          </div>
        ))}
        {terminals.profiles.filter((p) => !p.available).length > 0 && (
          <>
            <div className="h-px bg-white/10 my-1" />
            <div className="px-3 py-1 text-[10px] uppercase opacity-50">{tt("terminal.group.notInstalledHeader")}</div>
            {terminals.profiles.filter((p) => !p.available).map((p) => (
              <div key={p.kind} className="px-3 py-1.5 text-[11px] opacity-40">{p.label}{p.kind === "cmder" && <span className="ml-1">{tt("terminal.group.installViaDownloads")}</span>}</div>
            ))}
          </>
        )}
      </PortalMenu>

      {/* per-terminal PHP / Node runtime picker (local terminals) */}
      <PortalMenu open={runtimeOpen} anchorRef={runtimeRef} onClose={() => setRuntimeOpen(false)} align="right" minWidth={200}>
        <div className="px-3 py-1 text-[10px] uppercase opacity-50">{tt("terminal.group.phpVersionHeader")}</div>
        <RuntimeOption
          label={`${tt("terminal.group.autoOption")}${activePhp ? ` (${activePhp})` : ""}`}
          selected={!leafPhp}
          onClick={() => { setRuntimeOpen(false); setRuntime({ php: "" }); }}
        />
        {api.phpVersions.map((v) => (
          <RuntimeOption
            key={v.version}
            label={`PHP ${v.version}`}
            active={v.active}
            activeSuffix={tt("terminal.group.activeSuffix")}
            selected={leafPhp === v.version}
            onClick={() => { setRuntimeOpen(false); setRuntime({ php: v.version }); }}
          />
        ))}
        <div className="h-px bg-white/10 my-1" />
        <div className="px-3 py-1 text-[10px] uppercase opacity-50">{tt("terminal.group.nodeVersionHeader")}</div>
        <RuntimeOption
          label={`${tt("terminal.group.autoOption")}${activeNode ? ` (${activeNode})` : ""}`}
          selected={!leafNode}
          onClick={() => { setRuntimeOpen(false); setRuntime({ node: "" }); }}
        />
        {api.nodeVersions.map((v) => (
          <RuntimeOption
            key={v.version}
            label={`Node ${v.version}`}
            active={v.active}
            activeSuffix={tt("terminal.group.activeSuffix")}
            selected={leafNode === v.version}
            onClick={() => { setRuntimeOpen(false); setRuntime({ node: v.version }); }}
          />
        ))}
        <div className="px-3 py-1.5 text-[10px] opacity-40 leading-snug">{tt("terminal.group.restartNote")}</div>
      </PortalMenu>
    </div>
  );
}

function RuntimeOption({ label, active, activeSuffix, selected, onClick }: { label: string; active?: boolean; activeSuffix?: string; selected: boolean; onClick: () => void }) {
  return (
    <button onClick={onClick} className="w-full flex items-center gap-1.5 text-left px-3 py-1.5 text-[11px] rounded-md hover:bg-white/10">
      <Check className={clsx("w-3 h-3 shrink-0", selected ? "opacity-100 text-accent" : "opacity-0")} />
      <span className="whitespace-nowrap">{label}{active && <span className="opacity-50">{activeSuffix}</span>}</span>
    </button>
  );
}

function Handle({ cls, on }: { cls: string; on: (e: React.PointerEvent) => void }) {
  return <div className={clsx("absolute z-30", cls)} onPointerDown={on} />;
}
