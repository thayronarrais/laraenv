// Renders a tab's split tree (Pane = Leaf | Split) using react-resizable-panels.
// Each leaf pane supports focus, close, being a drag source (MIME_PANE) and a
// drop target with edge zones for splitting.

import { useEffect, useRef, useState } from "react";
import { X, GripVertical } from "lucide-react";
import clsx from "clsx";
import { PanelGroup, Panel, PanelResizeHandle } from "react-resizable-panels";
import Terminal, { type TerminalErrorHandler } from "../Terminal";
import SSHConnectingTimeline from "./SSHConnectingTimeline";
import DropZoneOverlay from "./DropZoneOverlay";
import { ctrlBackspaceSequence } from "./keys";
import { MIME_PANE, MIME_TAB, encode, regionForPointer, type DropRegion } from "./dnd";
import type { Leaf, Pane, Tab } from "./types";
import { useT } from "../../lib/i18n";

type FocusIntent = "pointer" | "keyboard" | "passive";

export type PaneDropHandler = (targetLeafId: string, region: DropRegion, e: React.DragEvent) => void;

export default function PaneRenderer({
  tab, groupId, isTabActive, isGroupFocused, focusRequest, onFocus, onClose, onPaneDrop, onError,
}: {
  tab: Tab;
  groupId: string;
  isTabActive: boolean;
  isGroupFocused: boolean;
  focusRequest?: number;
  onFocus: (leafId: string, intent: FocusIntent) => void;
  onClose: (leafId: string) => void;
  onPaneDrop: PaneDropHandler;
  onError?: TerminalErrorHandler;
}) {
  // Focus can be nested inside a keydown handler that cancels Tab afterward.
  // Record its first destination now; claim only after the dispatch has completed.
  const navigation = useRef<{
    key: KeyboardEvent;
    destination?: { leafId: string; element: Element | null; observed: boolean };
  } | null>(null);
  const focusCallback = useRef(onFocus); focusCallback.current = onFocus;
  useEffect(() => {
    let expiry: ReturnType<typeof setTimeout> | undefined;
    const clearNavigation = () => { navigation.current = null; clearTimeout(expiry); };
    const keyDown = (event: KeyboardEvent) => {
      clearNavigation();
      if (event.key !== "Tab" || event.altKey || event.ctrlKey || event.metaKey || event.defaultPrevented) return;
      navigation.current = { key: event };
      expiry = setTimeout(() => {
        const pending = navigation.current;
        navigation.current = null;
        if (pending?.destination && !pending.key.defaultPrevented
          && pending.destination.element?.isConnected && document.activeElement === pending.destination.element) {
          focusCallback.current(pending.destination.leafId, "keyboard");
        }
      }, 0);
    };
    const keyUp = () => {
      // Releasing Tab may precede the scheduled check. Its first destination is
      // already consumed; retain that candidate, but never accept a new one.
      if (!navigation.current?.destination) clearNavigation();
    };
    const focusIn = (event: FocusEvent) => {
      const destination = navigation.current?.destination;
      // React focus capture records the terminal first. Any different/subsequent
      // destination consumes the Tab without authorizing a delayed terminal claim.
      if (destination && !destination.observed && event.target === destination.element) destination.observed = true;
      else clearNavigation();
    };
    document.addEventListener("keydown", keyDown, true);
    document.addEventListener("keyup", keyUp, true);
    document.addEventListener("pointerdown", clearNavigation, true);
    document.addEventListener("focusin", focusIn);
    window.addEventListener("blur", clearNavigation);
    return () => {
      clearNavigation();
      document.removeEventListener("keydown", keyDown, true);
      document.removeEventListener("keyup", keyUp, true);
      document.removeEventListener("pointerdown", clearNavigation, true);
      document.removeEventListener("focusin", focusIn);
      window.removeEventListener("blur", clearNavigation);
    };
  }, []);
  const focus = (leafId: string, intent: FocusIntent) => {
    const pending = navigation.current;
    if (intent === "passive" && pending && !pending.destination) {
      pending.destination = { leafId, element: document.activeElement, observed: false };
    }
    onFocus(leafId, intent);
  };
  return renderNode(tab.tree, tab, groupId, isTabActive, isGroupFocused, focusRequest, focus, onClose, onPaneDrop, onError);
}

function renderNode(
  node: Pane,
  tab: Tab,
  groupId: string,
  isTabActive: boolean,
  isGroupFocused: boolean,
  focusRequest: number | undefined,
  onFocus: (leafId: string, intent: FocusIntent) => void,
  onClose: (leafId: string) => void,
  onPaneDrop: PaneDropHandler,
  onError: TerminalErrorHandler | undefined,
): JSX.Element {
  if (node.type === "leaf") {
    return (
      <PaneLeaf
        key={node.id}
        leaf={node}
        tabId={tab.tabId}
        groupId={groupId}
        isTabActive={isTabActive}
        isFocused={isTabActive && isGroupFocused && tab.activeLeafId === node.id}
        focusRequest={tab.activeLeafId === node.id ? focusRequest : undefined}
        onFocus={intent => onFocus(node.id, intent)}
        onClose={() => onClose(node.id)}
        onPaneDrop={onPaneDrop}
        onError={onError}
      />
    );
  }
  return (
    <PanelGroup direction={node.direction} className="h-full w-full">
      <Panel defaultSize={50} minSize={10}>
        {renderNode(node.children[0], tab, groupId, isTabActive, isGroupFocused, focusRequest, onFocus, onClose, onPaneDrop, onError)}
      </Panel>
      <PanelResizeHandle className={clsx(
        "transition-colors flex items-center justify-center",
        node.direction === "horizontal" ? "w-1.5 cursor-col-resize" : "h-1.5 cursor-row-resize",
        "bg-white/10 hover:bg-accent/60",
      )} />
      <Panel defaultSize={50} minSize={10}>
        {renderNode(node.children[1], tab, groupId, isTabActive, isGroupFocused, focusRequest, onFocus, onClose, onPaneDrop, onError)}
      </Panel>
    </PanelGroup>
  );
}

function PaneLeaf({
  leaf, tabId, groupId, isTabActive, isFocused, focusRequest, onFocus, onClose, onPaneDrop, onError,
}: {
  leaf: Leaf;
  tabId: string;
  groupId: string;
  isTabActive: boolean;
  isFocused: boolean;
  focusRequest?: number;
  onFocus: (intent: FocusIntent) => void;
  onClose: () => void;
  onPaneDrop: PaneDropHandler;
  onError?: TerminalErrorHandler;
}) {
  const t = useT();
  const [region, setRegion] = useState<DropRegion | null>(null);

  const dragKind = (e: React.DragEvent): "tab" | "pane" | null => {
    const t = e.dataTransfer.types;
    if (t.includes(MIME_PANE)) return "pane";
    if (t.includes(MIME_TAB)) return "tab";
    return null;
  };

  return (
    <div
      onPointerDown={() => onFocus("pointer")}
      onMouseDown={() => onFocus("pointer")}
      onDragOver={(e) => {
        if (!dragKind(e)) return;
        e.preventDefault();
        e.dataTransfer.dropEffect = "move";
        const r = e.currentTarget.getBoundingClientRect();
        setRegion(regionForPointer(r.width, r.height, e.clientX - r.left, e.clientY - r.top));
      }}
      onDragLeave={(e) => {
        // Only clear when leaving the pane entirely (not when crossing children).
        if (!e.currentTarget.contains(e.relatedTarget as Node)) setRegion(null);
      }}
      onDrop={(e) => {
        const r = region;
        setRegion(null);
        if (!dragKind(e) || !r) return;
        e.preventDefault();
        e.stopPropagation();
        onPaneDrop(leaf.id, r, e);
      }}
      className={clsx(
        "h-full w-full relative group transition-colors",
        isFocused ? "ring-1 ring-accent/60 ring-inset" : "ring-1 ring-transparent ring-inset",
      )}
    >
      {/* drag grip — start a pane (MIME_PANE) drag */}
      <div
        draggable
        onDragStart={(e) => {
          e.dataTransfer.setData(MIME_PANE, encode({ srcGroupId: groupId, tabId, leafId: leaf.id }));
          e.dataTransfer.effectAllowed = "move";
        }}
        className="absolute top-1 left-1 z-10 opacity-0 group-hover:opacity-60 hover:!opacity-100 bg-black/40 backdrop-blur rounded p-0.5 text-white/80 cursor-grab active:cursor-grabbing"
        title={t("terminal.pane.dragTooltip")}
      >
        <GripVertical className="w-3 h-3" />
      </div>

      <button
        onClick={(e) => { e.stopPropagation(); onClose(); }}
        className="absolute top-1 right-1 z-10 opacity-0 group-hover:opacity-70 hover:!opacity-100 bg-black/40 backdrop-blur rounded p-0.5 text-white/80 hover:text-red-300"
        title={t("terminal.pane.closeTooltip")}
      >
        <X className="w-3 h-3" />
      </button>

      <DropZoneOverlay region={region} />

      {leaf.status === "pending" && leaf.kind === "ssh" && (
        <SSHConnectingTimeline label={leaf.label} stage={leaf.sshStage} />
      )}
      {leaf.status === "pending" && leaf.kind !== "ssh" && (
        <div className="h-full w-full flex items-center justify-center text-xs opacity-60">{leaf.label}</div>
      )}
      {leaf.status === "error" && (
        <div className="h-full w-full flex items-center justify-center p-6">
          <div className="max-w-2xl w-full glass border border-red-400/40 bg-red-500/10 rounded-2xl p-6 flex flex-col gap-3">
            <div className="flex items-center gap-2 text-red-200 text-sm font-semibold">
              <span>⚠</span><span>{t("terminal.pane.connectionFailed")}</span>
            </div>
            <pre className="text-[12px] text-red-100/90 font-mono whitespace-pre-wrap break-all leading-relaxed">
              {leaf.errorMsg || t("terminal.pane.unknownError")}
            </pre>
            <div className="text-[10px] opacity-60">{t("terminal.pane.paneLabelPrefix")} <code>{leaf.label}</code></div>
          </div>
        </div>
      )}
      {(!leaf.status || leaf.status === "ready") && (
        <Terminal
          id={leaf.id}
          visible={isTabActive}
          focused={isFocused}
          focusRequest={focusRequest}
          onFocus={() => onFocus("passive")}
          onError={onError}
          ctrlBackspace={ctrlBackspaceSequence(leaf.kind)}
          // A dead PTY closes its own pane, the same as the X button would:
          // closePane drops the tab when it held the last pane, and the group
          // when it held the last tab.
          onExit={() => onClose()}
        />
      )}
    </div>
  );
}
