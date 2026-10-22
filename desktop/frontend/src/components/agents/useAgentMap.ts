import { createElement, useCallback, useEffect, useRef, useState } from "react";
import AgentActivityChip from "./AgentActivityChip";
import AgentMapDialog from "./AgentMapDialog";
import { agentActivityApi, getAgentActivityWindowId } from "./agentActivityApi";
import { useAgentActivity } from "./useAgentActivity";

export interface UseAgentMapOptions {
  enabled: boolean;
  selectedSessionId?: string;
  onOpenRoot(sessionId: string): void;
  onSelectSession?(): void;
  isBlocked?(): boolean;
  canOpenRoot?(sessionId: string, terminalId: string): boolean;
}

/** One window owns two independent leases: selection follows the counter,
 * while the dialog stays pinned to the principal that opened it. */
export function useAgentMap(options: UseAgentMapOptions) {
  const {enabled, selectedSessionId = ""} = options;
  const callbacks = useRef(options); callbacks.current = options;
  const [windowId] = useState(getAgentActivityWindowId);
  const [openSessionId, setOpenSessionId] = useState<string | null>(null);
  const restoreFocusRef = useRef(true);
  const openerRef = useRef<HTMLElement | null>(null);
  const counter = useAgentActivity({sessionId: selectedSessionId, windowId, purpose: "counter", enabled});
  const activity = useAgentActivity({sessionId: openSessionId ?? "", windowId, purpose: "dialog", enabled: enabled && !!openSessionId});
  const openMap = useCallback((sessionId: string) => {
    if (callbacks.current.enabled && sessionId && !callbacks.current.isBlocked?.()) {
      restoreFocusRef.current = true;
      // Capture before React makes the background inert (native inert can blur).
      openerRef.current = document.activeElement instanceof HTMLElement ? document.activeElement : null;
      setOpenSessionId(sessionId);
    }
  }, []);
  const closeMap = useCallback((options?: {restoreFocus: boolean}) => {
    restoreFocusRef.current = options?.restoreFocus !== false;
    setOpenSessionId(null);
  }, []);
  useEffect(() => { if (!enabled) closeMap(); }, [enabled, closeMap]);
  useEffect(() => agentActivityApi.onReset(() => closeMap()), [closeMap]);

  // Presentation-only availability: backend terminal IDs may belong to another
  // window/process. The caller must resolve the actual local session and pane.
  const snapshot = activity.snapshot;
  const root = snapshot?.nodes.find(node => node.id === snapshot.rootId);
  const available = !!root?.terminalId && !!options.canOpenRoot?.(openSessionId ?? "", root.terminalId);
  const display = snapshot && !available ? {...snapshot, nodes: snapshot.nodes.map(node => node.id === snapshot.rootId ? {...node, terminalId: undefined} : node)} : snapshot;
  // The footer follows a currently eligible selection. Historical inspection
  // remains available through openMap, with its own pinned snapshot and notice.
  const showCounter = enabled && !!selectedSessionId && counter.snapshot?.messageCode !== "observation-ended";
  const chip = showCounter ? createElement(AgentActivityChip, {...counter,
    onOpen: () => openMap(selectedSessionId),
    onSelectSession: () => { if (!callbacks.current.isBlocked?.()) callbacks.current.onSelectSession?.(); },
  }) : null;
  const dialog = enabled && openSessionId ? createElement(AgentMapDialog, {
    key: openSessionId, sessionId: openSessionId, snapshot: display, loading: activity.loading,
    refreshing: activity.refreshing, error: activity.error, onRefresh: activity.refresh, onClose: closeMap, restoreFocusRef, openerRef,
    onOpenRoot: sessionId => {
      if (!available) return;
      closeMap({restoreFocus: false});
      callbacks.current.onOpenRoot(sessionId);
    },
  }) : null;
  return {openSessionId: enabled ? openSessionId : null, openMap, closeMap, chip, dialog};
}
