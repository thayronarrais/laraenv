import type { DocumentState } from './types';
export const initialDocumentState = (): DocumentState => ({ buffers: [], tabs: [], activeTabId: null, activeBufferId: null, visible: false, panelPercent: 45, dirtyCount: 0, stateVersion: 1, handoffPending: false, hydrated: false, failedOpen: null, decision: null });
export type DocumentAction = { type: 'patch'; patch: Partial<DocumentState> } | { type: 'handoff'; pending: boolean };
export function documentReducer(state: DocumentState, action: DocumentAction): DocumentState {
  if (action.type === 'patch' && state.handoffPending) return state;
  const patch = action.type === 'handoff' ? { handoffPending: action.pending } : action.patch;
  if (Object.entries(patch).every(([key, value]) => state[key as keyof DocumentState] === value)) return state;
  const next = { ...state, ...patch, stateVersion: state.stateVersion + 1 };
  next.dirtyCount = next.buffers.filter(b => b.dirty).length;
  next.activeBufferId = next.tabs.find(t => t.id === next.activeTabId)?.bufferId ?? null;
  return next;
}
