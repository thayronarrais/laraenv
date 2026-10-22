import { useEffect, useMemo, useSyncExternalStore } from 'react';
import type { DocumentAPI } from './api';
import { DocumentOperations } from './documentOperations';
import { serializeWorkspace } from './serialize';
import type { DocumentController } from './types';

export function useDocumentWorkspace({ pageActive, windowFocused, api }: { pageActive: boolean; windowFocused: boolean; api: DocumentAPI }): DocumentController {
  const operations = useMemo(() => new DocumentOperations(api), [api]);
  const persistence = useMemo(() => ({ lane: Promise.resolve() }), [operations]);
  const state = useSyncExternalStore(operations.subscribe, operations.getSnapshot);
  useEffect(() => { operations.activate(); void operations.hydrate(); return operations.dispose; }, [operations]);
  useEffect(() => {
    const active = pageActive && windowFocused && state.visible;
    operations.setPolling(active);
    if (!active) return;
    const timer = setInterval(operations.poll, 2000);
    return () => { clearInterval(timer); operations.setPolling(false); };
  }, [operations, pageActive, windowFocused, state.visible]);
  useEffect(() => {
    if (!state.hydrated) return;
    const timer = setTimeout(() => {
      persistence.lane = persistence.lane.then(async () => {
        try { await api.saveLayout(serializeWorkspace(operations.state)); } catch { /* Keep the previous valid layout on a failure or oversized references. */ }
      });
    }, 300);
    return () => clearTimeout(timer);
  }, [api, operations, persistence, state.tabs, state.activeTabId, state.visible, state.panelPercent, state.hydrated]);
  return { ...state, open: operations.open, edit: operations.edit, save: operations.save, saveMerged: operations.saveMerged, closeTab: operations.closeTab, retryTab: operations.retryTab, reload: operations.reload, compare: operations.compare, setComparisonVisible: operations.setComparisonVisible, confirmCloseAll: operations.confirmCloseAll, setMode: operations.setMode, setVisible: operations.setVisible, setPanelPercent: operations.setPanelPercent, setActive: operations.setActive, setViewState: operations.setViewState, clearFailedOpen: operations.clearFailedOpen, beginShutdownHandoff: operations.beginShutdownHandoff, endShutdownHandoff: operations.endShutdownHandoff, resolveDecision: operations.resolveDecision, getBuffers: operations.getBuffers, getBuffer: operations.getBuffer, getWorkspaceState: operations.getWorkspaceState };
}
