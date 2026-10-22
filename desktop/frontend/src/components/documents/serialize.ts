import type { DocumentLocationRef, DocumentMode, DocumentState } from './types';
export type DocumentLayout = { version: 1; tabs: { references: DocumentLocationRef[]; mode: DocumentMode }[]; activeTabIndex: number; visible: boolean; panelPercent: number };
export const locationOnly = (ref: DocumentLocationRef): DocumentLocationRef => ({ context: ref.context.kind === 'agent' ? { kind: 'agent', sessionId: ref.context.sessionId } : { kind: 'project', projectName: ref.context.projectName }, rootKey: ref.rootKey, relativePath: ref.relativePath });
const exact = (v: any, keys: string[]) => v && typeof v === 'object' && !Array.isArray(v) && Object.keys(v).length === keys.length && keys.every(k => Object.prototype.hasOwnProperty.call(v, k));
const nonblank = (v: unknown) => typeof v === 'string' && v.trim().length > 0;
const validRef = (r: any) => exact(r, ['context', 'rootKey', 'relativePath']) &&
  ((exact(r.context, ['kind', 'sessionId']) && r.context.kind === 'agent' && nonblank(r.context.sessionId)) || (exact(r.context, ['kind', 'projectName']) && r.context.kind === 'project' && nonblank(r.context.projectName))) &&
  typeof r.rootKey === 'string' && /^[0-9a-f]{64}$/.test(r.rootKey) && typeof r.relativePath === 'string' && !/[\\:\0]/.test(r.relativePath) && r.relativePath.split('/').every((p: string) => p !== '' && p !== '.' && p !== '..');
export function parseLayout(raw: string): DocumentLayout | null {
  if (!raw || new TextEncoder().encode(raw).length > 1048576) return null;
  try {
    const v = JSON.parse(raw);
    if (!exact(v, ['version', 'tabs', 'activeTabIndex', 'visible', 'panelPercent']) || v.version !== 1 || !Array.isArray(v.tabs) || typeof v.visible !== 'boolean' || typeof v.panelPercent !== 'number' || !Number.isFinite(v.panelPercent) || v.panelPercent < 20 || v.panelPercent > 80 || !Number.isInteger(v.activeTabIndex)) return null;
    if (v.tabs.length ? v.activeTabIndex < 0 || v.activeTabIndex >= v.tabs.length : v.activeTabIndex !== -1) return null;
    if (!v.tabs.every((t: any) => exact(t, ['references', 'mode']) && (t.mode === 'source' || t.mode === 'preview') && Array.isArray(t.references) && t.references.length > 0 && t.references.every(validRef))) return null;
    return v;
  } catch { return null; }
}
export function serializeWorkspace(state: DocumentState): string {
  const raw = JSON.stringify({ version: 1, tabs: state.tabs.map(t => ({ references: t.references.map(locationOnly), mode: t.mode })), activeTabIndex: state.tabs.length ? Math.max(0, state.tabs.findIndex(t => t.id === state.activeTabId)) : -1, visible: state.visible, panelPercent: state.panelPercent });
  if (!parseLayout(raw)) throw new Error('Invalid or oversized document layout.');
  return raw;
}
