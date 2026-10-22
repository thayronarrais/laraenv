import type { DocumentAPI } from './api';
import { documentReducer, initialDocumentState } from './documentReducer';
import { locationOnly, parseLayout } from './serialize';
import type { CloseChoice, CloseDecision, DocumentBuffer, DocumentLocationRef, DocumentState, DocumentTab, DocumentWorkspaceState, Result, Snapshot, WorkspaceDocumentRef } from './types';

const success = <T>(r: Result<T>): r is { code: 'ok' | 'changed'; value: T } => r.code === 'ok' || r.code === 'changed';
const refKey = (r: DocumentLocationRef) => JSON.stringify(locationOnly(r));
const union = (a: DocumentLocationRef[], b: DocumentLocationRef[]) => [...new Map([...a, ...b].map(r => [refKey(r), locationOnly(r)])).values()];
const unavailable = (): Result<Snapshot> => ({ code: 'unavailable', message: 'The document identity is no longer available.' });
async function safely<T>(operation: () => Promise<Result<T>>): Promise<Result<T>> {
  try { return await operation(); } catch (e) { return { code: 'io_error', message: e instanceof Error ? e.message : String(e) }; }
}

let nextBufferGeneration = 0;

/** Owns synchronous state and operation tokens. React renders immutable snapshots of this store. */
export class DocumentOperations {
  state = initialDocumentState();
  private listeners = new Set<() => void>();
  private sequence = new Map<string, number>();
  private operationOrder = 0;
  private snapshotOrder = new Map<string, number>();
  private nextTab = 0;
  private alive = true;
  private hydration: Promise<void> | null = null;
  private saves = new Set<string>();
  private decisionResolver: ((choice: CloseChoice) => void) | null = null;
  private decisionQueue: { decision: CloseDecision; resolve: (choice: CloseChoice) => void }[] = [];
  private closeAll: Promise<boolean> | null = null;
  private pollEpoch = 0;
  private checks = new Set<string>();
  private polling = false;
  constructor(readonly api: DocumentAPI) {}
  subscribe = (fn: () => void) => { this.listeners.add(fn); return () => { this.listeners.delete(fn); }; };
  getSnapshot = () => this.state;
  getBuffers = (): readonly DocumentBuffer[] => this.state.buffers;
  getBuffer = (id: string): DocumentBuffer | undefined => this.state.buffers.find(buffer => buffer.id === id);
  getWorkspaceState = (): DocumentWorkspaceState => ({ ready: this.state.hydrated, dirtyCount: this.state.dirtyCount, stateVersion: this.state.stateVersion });
  private dispatch(action: Parameters<typeof documentReducer>[1]) {
    if (!this.alive) return;
    const next = documentReducer(this.state, action);
    if (next !== this.state) { this.state = next; this.listeners.forEach(fn => fn()); }
  }
  private patch(patch: Partial<DocumentState>) { this.dispatch({ type: 'patch', patch }); }
  private buffer(id: string) { return this.state.buffers.find(b => b.id === id); }
  private candidateReferences(buffer: DocumentBuffer) {
    // Restored tabs retain origins that have not yet been validated for this buffer.
    // The first successful root remains the physical authority for this lifetime.
    // Persisted or joined roots must never replace it merely by matching a pathname.
    return union(buffer.references, this.state.tabs.filter(t => t.bufferId === buffer.id).flatMap(t => t.references))
      .filter(reference => reference.rootKey === buffer.references[0].rootKey);
  }
  private tab(id: string) { return this.state.tabs.find(t => t.id === id); }
  private update(id: string, change: (b: DocumentBuffer) => DocumentBuffer) { this.patch({ buffers: this.state.buffers.map(b => b.id === id ? change(b) : b) }); }
  private token(id: string) { const n = ++this.operationOrder; this.sequence.set(id, n); return n; }
  private current(id: string, token: number) { return this.alive && !this.state.handoffPending && this.sequence.get(id) === token && (this.snapshotOrder.get(id) ?? 0) <= token; }
  activate = () => { this.alive = true; };
  dispose = () => { this.alive = false; this.pollEpoch++; this.resolveDecision('cancel'); };

  hydrate = (): Promise<void> => {
    if (this.hydration) return this.hydration;
    this.hydration = (async () => {
      let raw = ''; try { raw = await this.api.loadLayout(); } catch { /* An unavailable layout never overwrites local buffers. */ }
      if (!this.alive) return;
      const layout = parseLayout(raw);
      if (layout) {
        const restored: DocumentTab[] = layout.tabs.map(t => ({ ...t, id: `document-${++this.nextTab}`, bufferId: null, status: 'loading' }));
        const hadTabs = this.state.tabs.length > 0;
        this.patch({ tabs: [...this.state.tabs, ...restored], ...(hadTabs ? {} : { activeTabId: restored[layout.activeTabIndex]?.id ?? null, visible: layout.visible, panelPercent: layout.panelPercent }) });
        await Promise.all(restored.map(t => this.readTab(t.id, true)));
      }
      this.patch({ hydrated: true });
    })();
    return this.hydration;
  };

  private async readReferences(references: DocumentLocationRef[], expected?: string): Promise<{ result: Result<Snapshot>; location?: DocumentLocationRef }> {
    let result = unavailable();
    for (const location of references) {
      const read = await safely(() => this.api.read(location));
      if (success(read)) {
        if (!expected || read.value.canonicalPath === expected) return { result: read, location };
        result = unavailable();
      } else result = read;
    }
    return { result };
  }

  open = async (ref: WorkspaceDocumentRef): Promise<void> => {
    if (this.state.handoffPending) return;
    const location = locationOnly(ref);
    const existing = this.state.tabs.find(t => t.references.some(r => refKey(r) === refKey(location)));
    if (existing) { this.patch({ activeTabId: existing.id, visible: true }); await this.readTab(existing.id, true); return; }
    const tab: DocumentTab = { id: `document-${++this.nextTab}`, references: [location], mode: ref.mode, bufferId: null, status: 'loading' };
    this.patch({ tabs: [...this.state.tabs, tab], activeTabId: tab.id, visible: true, failedOpen: null });
    await this.readTab(tab.id, false);
  };
  retryTab = async (id: string) => { await this.readTab(id, true); };
  private async readTab(id: string, keepPlaceholder: boolean) {
    const tab = this.tab(id); if (!tab || this.state.handoffPending || (tab.bufferId && this.saves.has(tab.bufferId))) return;
    const token = this.token(id); const bufferToken = tab.bufferId ? this.token(tab.bufferId) : null;
    const attached = tab.bufferId ? this.buffer(tab.bufferId) : undefined;
    const { result, location } = await this.readReferences(attached ? this.candidateReferences(attached) : tab.references, attached?.snapshot.canonicalPath);
    if (!this.current(id, token) || !this.tab(id) || (tab.bufferId && !this.current(tab.bufferId, bufferToken!))) return;
    if (success(result) && location) {
      const snap = result.value;
      let old = attached;
      if (!old) {
        for (const candidate of this.state.buffers.filter(b => b.snapshot.canonicalPath === snap.canonicalPath)) {
          if (candidate.references[0].rootKey === location.rootKey) { old = candidate; break; }
          // A different root can join only while the original physical authority
          // still resolves this file. It never becomes a fallback for that authority.
          const validation = await this.readReferences(this.candidateReferences(candidate), snap.canonicalPath);
          if (!this.current(id, token) || !this.tab(id)) return;
          if (!this.buffer(candidate.id)) continue;
          if (success(validation.result)) { old = this.buffer(candidate.id); break; }
          if (!this.saves.has(candidate.id) && (this.sequence.get(candidate.id) ?? 0) <= token && (this.snapshotOrder.get(candidate.id) ?? 0) <= token)
            this.update(candidate.id, b => ({ ...b, status: 'unavailable', problem: { code: 'unavailable', message: 'The original workspace root is unavailable.' } }));
        }
      }
      // Another alias may have created this generation during validation above.
      if (!attached) old = this.state.buffers.find(b => b.snapshot.canonicalPath === snap.canonicalPath && b.references[0].rootKey === location.rootKey) ?? old;
      if (old) old = this.buffer(old.id);
      const refs = union(old?.references ?? [], [location]);
      const newer = !!old && (this.saves.has(old.id) || (this.snapshotOrder.get(old.id) ?? 0) > token);
      const b: DocumentBuffer = old ? (newer ? { ...old, references: refs } : this.adopt({ ...old, references: refs }, snap)) : { id: `buffer-${++nextBufferGeneration}`, references: refs, snapshot: snap, text: snap.text, dirty: false, editGeneration: 0, viewState: null, pendingDiskSnapshot: null, comparisonVisible: false, status: 'ready' };
      if (!newer) this.snapshotOrder.set(b.id, token);
      const duplicate = this.state.tabs.find(t => t.id !== id && t.bufferId === b.id);
      const target = duplicate?.id ?? id;
      const tabs = this.state.tabs.filter(t => !duplicate || t.id !== id).map(t => t.id === target ? { ...t, references: union(t.references, refs), bufferId: b.id, status: 'ready' as const } : t);
      this.patch({ buffers: old ? this.state.buffers.map(x => x.id === b.id ? b : x) : [...this.state.buffers, b], tabs, activeTabId: this.state.activeTabId === id ? target : this.state.activeTabId });
    } else if ('message' in result) {
      if (tab.bufferId) this.update(tab.bufferId, b => ({ ...b, status: result.code === 'unavailable' ? 'unavailable' : 'error', problem: result }));
      if (!keepPlaceholder && !tab.bufferId) { this.removeTab(id); this.patch({ failedOpen: { location: tab.references[0], code: result.code, message: result.message } }); }
      else this.patch({ tabs: this.state.tabs.map(t => t.id === id ? { ...t, status: result.code === 'unavailable' ? 'unavailable' : 'error' } : t) });
    }
  }
  private adopt(b: DocumentBuffer, snapshot: Snapshot): DocumentBuffer {
    if (b.dirty && snapshot.revision !== b.snapshot.revision) return { ...b, pendingDiskSnapshot: snapshot, status: 'conflict', problem: undefined };
    if (b.dirty) return { ...b, status: b.pendingDiskSnapshot ? 'conflict' : 'ready', problem: undefined };
    return { ...b, snapshot, text: snapshot.text, dirty: false, pendingDiskSnapshot: null, status: 'ready', problem: undefined };
  }
  edit = (id: string, text: string) => { const b = this.buffer(id); if (!b || b.snapshot.readOnly || text === b.text) return; this.update(id, old => ({ ...old, text, dirty: text !== old.snapshot.text, editGeneration: old.editGeneration + 1 })); };
  setViewState = (id: string, viewState: unknown) => this.update(id, b => ({ ...b, viewState }));
  setComparisonVisible = (id: string, comparisonVisible: boolean) => this.update(id, b => ({ ...b, comparisonVisible }));
  setMode = (id: string, mode: DocumentTab['mode']) => this.patch({ tabs: this.state.tabs.map(t => t.id === id ? { ...t, mode } : t) });
  setVisible = (visible: boolean) => this.patch({ visible });
  setPanelPercent = (panelPercent: number) => { if (Number.isFinite(panelPercent)) this.patch({ panelPercent: Math.max(20, Math.min(80, panelPercent)) }); };
  setActive = (activeTabId: string) => { if (this.tab(activeTabId)) this.patch({ activeTabId }); };
  clearFailedOpen = () => this.patch({ failedOpen: null });
  save = (id: string) => this.write(id, false);
  saveMerged = (id: string) => this.write(id, true);
  private async write(id: string, merged: boolean): Promise<boolean> {
    const b = this.buffer(id); if (!b || b.snapshot.readOnly || this.state.handoffPending || this.saves.has(id) || (merged && !b.pendingDiskSnapshot)) return false;
    this.saves.add(id); const token = this.token(id);
    const revision = merged ? b.pendingDiskSnapshot!.revision : b.snapshot.revision;
    try {
      // Revalidate an alias before authorizing a write through that origin.
      const { result: validation, location } = await this.readReferences(this.candidateReferences(b), b.snapshot.canonicalPath);
      if (!this.current(id, token) || !this.buffer(id)) return false;
      if (location) this.update(id, now => ({ ...now, references: union(now.references, [location]) }));
      const result = location ? await safely(() => this.api.save(location, b.text, revision)) : validation;
      if (!this.current(id, token) || !this.buffer(id)) return false;
      if (success(result) && result.value.canonicalPath === b.snapshot.canonicalPath) {
        // Reads issued while this write was pending may have sampled its old bytes.
        // A completed write establishes a barrier after all those read requests.
        this.snapshotOrder.set(id, ++this.operationOrder);
        this.update(id, now => {
          const sameGeneration = now.editGeneration === b.editGeneration;
          return { ...now, snapshot: result.value, text: sameGeneration ? result.value.text : now.text, dirty: !sameGeneration || now.text !== result.value.text, pendingDiskSnapshot: merged && !sameGeneration ? result.value : null, status: merged && !sameGeneration ? 'conflict' : 'ready', problem: undefined };
        });
        return true;
      }
      if ('message' in result) {
        if (result.latest) this.snapshotOrder.set(id, ++this.operationOrder);
        this.update(id, now => ({ ...now, status: result.code === 'conflict' ? 'conflict' : result.code === 'unavailable' ? 'unavailable' : 'error', pendingDiskSnapshot: result.latest ?? now.pendingDiskSnapshot, problem: result }));
      }
      return false;
    } finally { this.saves.delete(id); }
  }
  compare = async (id: string) => { await this.refresh(id, false, true); };
  private async refresh(id: string, replace: boolean, compare: boolean): Promise<boolean> {
    const b = this.buffer(id); if (!b || this.state.handoffPending || this.saves.has(id)) return false;
    const token = this.token(id); const { result, location } = await this.readReferences(this.candidateReferences(b), b.snapshot.canonicalPath);
    if (!this.current(id, token) || !this.buffer(id)) return false;
    if (success(result)) {
      this.snapshotOrder.set(id, token);
      this.update(id, now => {
        const next = replace && now.editGeneration === b.editGeneration ? { ...now, snapshot: result.value, text: result.value.text, dirty: false, pendingDiskSnapshot: null, status: 'ready' as const } : this.adopt(now, result.value);
        return { ...next, references: location ? union(next.references, [location]) : next.references, comparisonVisible: compare || next.comparisonVisible, ...(compare ? { pendingDiskSnapshot: result.value } : {}) };
      }); return !replace || !this.buffer(id)?.dirty;
    }
    if ('message' in result) this.update(id, now => ({ ...now, status: result.code === 'unavailable' ? 'unavailable' : 'error', problem: result }));
    return false;
  }
  reload = async (id: string): Promise<boolean> => {
    while (this.buffer(id)?.dirty) {
      const generation = this.buffer(id)!.editGeneration; const choice = await this.choose({ scope: 'one', names: [this.buffer(id)!.references[0].relativePath] });
      if (choice === 'cancel') return false;
      if (choice === 'save') { if (!await this.save(id)) return false; }
      else if (this.buffer(id)?.editGeneration === generation) break;
    }
    return this.refresh(id, true, false);
  };
  private choose(decision: CloseDecision): Promise<CloseChoice> {
    return new Promise(resolve => {
      if (!this.alive || this.state.handoffPending) { resolve('cancel'); return; }
      if (this.decisionResolver) this.decisionQueue.push({ decision, resolve });
      else { this.decisionResolver = resolve; this.patch({ decision }); }
    });
  }
  resolveDecision = (choice: CloseChoice) => {
    const resolve = this.decisionResolver; this.decisionResolver = null;
    if (resolve) { this.patch({ decision: null }); resolve(choice); }
    const next = this.decisionQueue.shift();
    if (next) { this.decisionResolver = next.resolve; this.patch({ decision: next.decision }); if (!this.alive) this.resolveDecision('cancel'); }
  };
  private discard(ids: string[]) { this.patch({ buffers: this.state.buffers.map(b => ids.includes(b.id) ? { ...b, text: b.snapshot.text, dirty: false, editGeneration: b.editGeneration + 1 } : b) }); }
  private removeTab(id: string) {
    this.token(id); const old = this.tab(id); if (old?.bufferId) this.token(old.bufferId);
    const tabs = this.state.tabs.filter(t => t.id !== id);
    this.patch({ tabs, buffers: this.state.buffers.filter(b => tabs.some(t => t.bufferId === b.id)), activeTabId: this.state.activeTabId === id ? tabs[0]?.id ?? null : this.state.activeTabId });
  }
  closeTab = async (id: string): Promise<boolean> => {
    if (this.state.handoffPending) return false;
    while (true) {
      const tab = this.tab(id); const b = tab?.bufferId ? this.buffer(tab.bufferId) : undefined;
      if (!b?.dirty) { this.removeTab(id); return true; }
      const generation = b.editGeneration;
      const choice = await this.choose({ scope: 'one', names: [tab!.references[0].relativePath] });
      if (choice === 'cancel') return false;
      if (choice === 'save') { if (!await this.save(b.id)) return false; }
      else if (this.buffer(b.id)?.editGeneration === generation) { this.removeTab(id); return true; }
    }
  };
  confirmCloseAll = (): Promise<boolean> => {
    if (this.closeAll) return this.closeAll;
    this.closeAll = this.confirmAll().finally(() => { this.closeAll = null; }); return this.closeAll;
  };
  private async confirmAll(): Promise<boolean> {
    while (this.state.dirtyCount) {
      const dirty = this.state.buffers.filter(b => b.dirty);
      const choice = await this.choose({ scope: 'all', names: dirty.map(b => b.references[0].relativePath) });
      if (choice === 'cancel') return false;
      if (choice === 'discard') this.discard(dirty.filter(b => this.buffer(b.id)?.editGeneration === b.editGeneration).map(b => b.id));
      else for (const b of dirty) { if (this.buffer(b.id)?.dirty && !await this.save(b.id)) return false; }
    }
    return true;
  }
  beginShutdownHandoff = (): DocumentWorkspaceState | null => {
    if (!this.state.hydrated || this.state.dirtyCount || this.state.decision || this.saves.size) return null;
    this.dispatch({ type: 'handoff', pending: true }); return this.getWorkspaceState();
  };
  endShutdownHandoff = () => this.dispatch({ type: 'handoff', pending: false });
  setPolling = (active: boolean) => { this.polling = active; this.pollEpoch++; };
  poll = () => {
    if (!this.polling || this.state.handoffPending) return;
    const epoch = this.pollEpoch;
    const queue = this.state.buffers.map(b => b.id).filter(id => !this.checks.has(id) && !this.saves.has(id));
    const drain = () => {
      while (this.alive && this.polling && epoch === this.pollEpoch && !this.state.handoffPending && this.checks.size < 4 && queue.length) {
        const id = queue.shift()!; if (this.checks.has(id) || this.saves.has(id) || !this.buffer(id)) continue;
        this.checks.add(id); void this.check(id, epoch).finally(() => { this.checks.delete(id); drain(); });
      }
    }; drain();
  };
  private async check(id: string, epoch: number) {
    const b = this.buffer(id)!; const token = this.token(id);
    let result: Result<Snapshot> = unavailable();
    let validatedLocation: DocumentLocationRef | undefined;
    for (const [index, location] of this.candidateReferences(b).entries()) {
      // An unchanged response has no canonical identity. Read fallback origins in full.
      result = await safely(() => index === 0 ? this.api.check(location, b.snapshot.revision) : this.api.read(location));
      if (result.code === 'unchanged') break;
      if (success(result) && result.value.canonicalPath === b.snapshot.canonicalPath) { validatedLocation = location; break; }
      if (success(result)) result = unavailable();
    }
    if (!this.current(id, token) || epoch !== this.pollEpoch || !this.buffer(id)) return;
    if (success(result)) { const snap = result.value; this.snapshotOrder.set(id, token); this.update(id, now => this.adopt({ ...now, references: validatedLocation ? union(now.references, [validatedLocation]) : now.references }, snap)); }
    else if (result.code === 'unchanged' && (b.status === 'unavailable' || b.status === 'error')) this.update(id, now => ({ ...now, status: now.pendingDiskSnapshot ? 'conflict' : 'ready', problem: undefined }));
    else if ('message' in result) { const problem = result; this.update(id, now => ({ ...now, status: problem.code === 'unavailable' ? 'unavailable' : 'error', problem })); }
  }
}
