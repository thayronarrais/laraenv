import { useEffect, useMemo, useSyncExternalStore } from 'react';
import type { WorkspaceContextRef } from '../workspace/types';
import { problemCode, sourceControlApi, type SourceControlAPI } from './api';
import { isTerminal, SourceControlOperations, type OperationOrigin } from './sourceControlOperations';
import { immutableCopy } from './types';
import type { ExpectedRepositoryState, GitEntry, GitLayer, GitOperation, GitResult, GitStatusSnapshot, PushTarget, RepositoryRef, SourceControlController, SourceControlState, StagePermit } from './types';

export type SourceControlOptions = {
  context: WorkspaceContextRef | null; active: boolean; windowFocused: boolean; api?: SourceControlAPI;
  beforeStage?: (snapshot: GitStatusSnapshot, entries: GitEntry[]) => Promise<StagePermit | null>;
};
type Checkout = {
  snapshot: GitStatusSnapshot; query: string; message: string; messageVersion: number; selectedIndexIds: string[]; selectedWorktreeIds: string[];
  error: SourceControlState['error']; generation: number; snapshotGeneration: number; actionPending: boolean;
};
const emptyState: SourceControlState = { snapshot: null, loading: false, error: null, query: '', message: '', selectedIndexIds: [], selectedWorktreeIds: [], operations: [] };
const expected = (s: GitStatusSnapshot): ExpectedRepositoryState => ({ snapshotRevision: s.snapshotRevision, indexRevision: s.indexRevision, headOID: s.headOID, branch: s.branch });
const sameRepository = (a: RepositoryRef, b: RepositoryRef) => a.repositoryKey === b.repositoryKey && a.rootKey === b.rootKey;
const sameContext = (a: WorkspaceContextRef, b: WorkspaceContextRef | null) => a.kind === 'agent'
  ? b?.kind === 'agent' && a.sessionId === b.sessionId
  : b?.kind === 'project' && a.projectName === b.projectName;
const failure = (error: unknown) => ({ code: 'git_error' as const, message: error instanceof Error ? error.message : String(error) });

/** One mounted workspace owns the in-memory checkout cache and operation subscription. */
class SourceControlStore {
  private cache = new Map<string, Checkout>();
  private listeners = new Set<() => void>();
  private state: SourceControlState = emptyState;
  private current: string | null = null;
  private context: WorkspaceContextRef | null = null;
  private contextKey = '';
  private contextGeneration = 0;
  private mutationGeneration = 0;
  private resolving = new Map<number, Promise<void>>();
  private queries = new Map<string, Promise<GitResult<GitStatusSnapshot>>>();
  private active = false;
  private focused = false;
  private connected = false;
  private timer: ReturnType<typeof setInterval> | undefined;
  private beforeStage: SourceControlOptions['beforeStage'];
  private operations: SourceControlOperations;
  constructor(private api: SourceControlAPI) {
    this.operations = new SourceControlOperations(api, () => this.publish(), (operation, origin) => this.completed(operation, origin));
  }
  subscribe = (listener: () => void) => { this.listeners.add(listener); return () => { this.listeners.delete(listener); }; };
  getState = () => this.state;
  private publish(override: Partial<SourceControlState> = {}) {
    const checkout = this.current ? this.cache.get(this.current) : undefined;
    this.state = immutableCopy({ ...emptyState, ...(checkout ? { snapshot: checkout.snapshot, query: checkout.query, message: checkout.message, selectedIndexIds: checkout.selectedIndexIds, selectedWorktreeIds: checkout.selectedWorktreeIds, error: checkout.error } : { error: this.state.error }), loading: this.state.loading, operations: this.operations.values().filter(op => op.repository.repositoryKey === this.current), ...override });
    this.listeners.forEach(listener => listener());
  }
  connect() { this.connected = true; this.operations.connect(); }
  disconnect() { this.connected = false; this.contextGeneration++; clearInterval(this.timer); this.operations.disconnect(); }
  setBeforeStage(callback: SourceControlOptions['beforeStage']) { this.beforeStage = callback; }
  configure(options: SourceControlOptions) {
    const key = JSON.stringify(options.context); const changed = key !== this.contextKey;
    const resumed = options.active && options.windowFocused && !(this.active && this.focused);
    this.active = options.active; this.focused = options.windowFocused;
    if (changed) {
      this.contextKey = key; this.context = options.context; this.contextGeneration++; this.current = null;
      this.publish({ loading: !!options.context, error: null });
    }
    clearInterval(this.timer);
    if (this.active && this.focused && this.context) {
      if (changed || resumed || !this.current) void this.refresh();
      this.timer = setInterval(() => { void this.refresh(); }, 2000);
    }
  }
  private apply(snapshot: GitStatusSnapshot): Checkout {
    const key = snapshot.repository.repositoryKey; let checkout = this.cache.get(key);
    if (!checkout) {
      checkout = { snapshot, message: '', messageVersion: 0, query: '', selectedIndexIds: [], selectedWorktreeIds: [], error: null, generation: 0, snapshotGeneration: 0, actionPending: false };
      this.cache.set(key, checkout);
    }
    checkout.snapshot = immutableCopy(snapshot);
    checkout.selectedIndexIds = checkout.selectedIndexIds.filter(id => snapshot.entries.some(e => e.id === id && e.indexCode !== '.' && e.indexCode !== '?' && e.indexCode !== ''));
    checkout.selectedWorktreeIds = checkout.selectedWorktreeIds.filter(id => snapshot.entries.some(e => e.id === id && (e.kind === 'untracked' || (e.worktreeCode !== '.' && e.worktreeCode !== ''))));
    return checkout;
  }
  refresh = async (): Promise<void> => {
    if (!this.connected || !this.context) return;
    const epoch = this.contextGeneration;
    if (!this.current) {
      const pending = this.resolving.get(epoch); if (pending) return pending;
      const context = this.context;
      const mutation = this.mutationGeneration; let reenter = false;
      const work = (async () => {
        let response: GitResult<GitStatusSnapshot>;
        try { response = await this.api.resolve(context); } catch (error) { response = failure(error); }
        if (epoch !== this.contextGeneration || !this.connected) return;
        if (mutation !== this.mutationGeneration) { reenter = true; return; }
        if (response.code !== 'ok') { this.publish({ loading: false, error: response }); return; }
        const checkout = this.apply(response.value); this.current = response.value.repository.repositoryKey;
        checkout.snapshotGeneration++;
        this.publish({ loading: false, error: checkout.error }); this.recover(checkout);
      })();
      this.resolving.set(epoch, work);
      try { await work; } finally { this.resolving.delete(epoch); }
      if (reenter && epoch === this.contextGeneration && this.active && this.focused) await this.refresh();
      return;
    }
    const checkout = this.cache.get(this.current)!;
    if (checkout.actionPending) return;
    await this.read(checkout.snapshot.repository, checkout.generation);
    this.recover(checkout);
  };
  private recover(checkout: Checkout) {
    const origin = { repository: checkout.snapshot.repository, generation: checkout.generation };
    if (checkout.snapshot.operationId) void this.operations.recover(checkout.snapshot.operationId, origin);
    for (const operation of this.operations.values()) if (!isTerminal(operation) && sameRepository(operation.repository, origin.repository)) void this.operations.recover(operation.id, origin);
  }
  private canPublish(ref: RepositoryRef) {
    return this.current !== ref.repositoryKey || sameContext(ref.context, this.context);
  }
  private async read(ref: RepositoryRef, generation: number, fresh = false): Promise<GitResult<GitStatusSnapshot>> {
    const key = ref.repositoryKey; const pending = this.queries.get(key);
    const contextEpoch = this.contextGeneration;
    const snapshotGeneration = this.cache.get(key)?.snapshotGeneration;
    if (pending) {
      const response = await pending;
      if (!fresh) return response;
      // Mutations cannot consume a query started before their generation.
      if (this.cache.get(key)?.generation !== generation) return { code: 'stale_snapshot', message: 'Source Control changed.' };
      return this.read(ref, generation, true);
    }
    const work = (async (): Promise<GitResult<GitStatusSnapshot>> => {
      let response: GitResult<GitStatusSnapshot>;
      try { response = await this.api.status(ref); } catch (error) { response = failure(error); }
      if (response.code === 'ok' && !sameRepository(response.value.repository, ref)) response = { code: 'repository_changed', message: 'Repository changed.' };
      const checkout = this.cache.get(key);
      if (checkout && checkout.generation === generation && checkout.snapshotGeneration === snapshotGeneration && (fresh || contextEpoch === this.contextGeneration) && this.canPublish(ref)) {
        if (response.code === 'ok') this.apply(response.value);
        else checkout.error = response;
        if (this.current === key) this.publish({ loading: false });
      }
      return response;
    })();
    this.queries.set(key, work);
    try { return await work; } finally { if (this.queries.get(key) === work) this.queries.delete(key); }
  }
  private update(change: (checkout: Checkout) => void) { const checkout = this.current ? this.cache.get(this.current) : undefined; if (checkout) { change(checkout); this.publish(); } }
  setMessage = (message: string) => this.update(checkout => { if (checkout.message !== message) checkout.messageVersion++; checkout.message = message; });
  setQuery = (query: string) => this.update(checkout => { checkout.query = query; });
  select = (layer: GitLayer, id: string, selected: boolean) => this.update(checkout => {
    const field = layer === 'index' ? 'selectedIndexIds' : 'selectedWorktreeIds';
    if (!checkout.snapshot.entries.some(entry => entry.id === id)) return;
    checkout[field] = selected ? [...new Set([...checkout[field], id])] : checkout[field].filter(value => value !== id);
  });
  private capture(): { checkout: Checkout; snapshot: GitStatusSnapshot; origin: OperationOrigin } | null {
    const checkout = this.current ? this.cache.get(this.current) : undefined;
    if (!checkout || checkout.actionPending) return null;
    checkout.actionPending = true; checkout.generation++; checkout.error = null;
    this.mutationGeneration++;
    this.contextGeneration++;
    const snapshot = checkout.snapshot; const origin = { repository: snapshot.repository, generation: checkout.generation };
    this.publish(); return { checkout, snapshot, origin };
  }
  private report(checkout: Checkout, origin: OperationOrigin, error: SourceControlState['error']) {
    if (checkout.generation !== origin.generation || !this.canPublish(origin.repository)) return;
    checkout.error = error; if (this.current === origin.repository.repositoryKey) this.publish();
  }
  private async run(captured: NonNullable<ReturnType<SourceControlStore['capture']>>, request: () => Promise<GitResult<GitOperation>>) {
    const { checkout, origin } = captured;
    origin.generation = ++checkout.generation; this.mutationGeneration++;
    try { const response = await this.operations.start(origin, request); if (response.code !== 'ok') this.report(checkout, origin, response); }
    catch (error) { this.report(checkout, origin, failure(error)); }
    finally { checkout.actionPending = false; }
  }
  stage = async (ids: string[]) => {
    const captured = this.capture(); if (!captured) return;
    const { checkout, snapshot, origin } = captured; const beforeStage = this.beforeStage;
    try {
      const entries = [...new Set(ids)].map(id => snapshot.entries.find(entry => entry.id === id));
      if (!entries.length || entries.some(entry => !entry?.canStage)) { this.report(checkout, origin, { code: 'stale_snapshot', message: 'Selected files cannot be staged.' }); return; }
      const permit = beforeStage ? await beforeStage(snapshot, entries as GitEntry[]) : { validate: () => true };
      if (!permit) return;
      const response = await this.read(origin.repository, origin.generation, true);
      if (response.code !== 'ok') { this.report(checkout, origin, response); return; }
      if (checkout.generation !== origin.generation || !entries.every(selected => response.value.entries.some(entry => entry.id === selected!.id && entry.canStage))) { this.report(checkout, origin, { code: 'stale_snapshot', message: 'Selected files changed.' }); return; }
      // No await between the editor generation check and native dispatch.
      if (!permit.validate()) { this.report(checkout, origin, { code: 'stale_snapshot', message: 'Editor changed before staging.' }); return; }
      await this.run(captured, () => this.api.stage(origin.repository, expected(response.value), (entries as GitEntry[]).map(entry => ({ entryId: entry.id, diskRevision: '' }))));
    } catch (error) { this.report(checkout, origin, failure(error)); }
    finally { checkout.actionPending = false; }
  };
  unstage = async (ids: string[]) => {
    const captured = this.capture(); if (!captured) return;
    await this.run(captured, () => this.api.unstage(captured.origin.repository, expected(captured.snapshot), [...new Set(ids)].map(entryId => ({ entryId, diskRevision: '' }))));
  };
  commit = async () => {
    const captured = this.capture(); if (!captured) return;
    captured.origin.message = captured.checkout.message;
    captured.origin.messageVersion = captured.checkout.messageVersion;
    await this.run(captured, () => this.api.commit(captured.origin.repository, expected(captured.snapshot), captured.origin.message!));
  };
  push = async (target: PushTarget) => {
    const captured = this.capture(); if (!captured) return;
    const selectedTarget = immutableCopy(target);
    await this.run(captured, () => this.api.push(captured.origin.repository, expected(captured.snapshot), selectedTarget));
  };
  cancelOperation = async (id: string) => {
    const operation = this.operations.values().find(item => item.id === id); if (!operation) return;
    const checkout = this.cache.get(operation.repository.repositoryKey); if (!checkout) return;
    const origin = { repository: operation.repository, generation: checkout.generation };
    try { const response = await this.api.cancel(id); if (response.code === 'ok') { this.operations.accept(response.value); await this.operations.recover(id, origin); } else this.report(checkout, origin, response); }
    catch (error) { this.report(checkout, origin, failure(error)); }
  };
  private completed(operation: GitOperation, origin: OperationOrigin) {
    const checkout = this.cache.get(origin.repository.repositoryKey); if (!checkout) return;
    if (operation.kind === 'commit' && operation.state === 'succeeded' && origin.message !== undefined && checkout.message === origin.message && checkout.messageVersion === origin.messageVersion) { checkout.message = ''; checkout.messageVersion++; }
    if (checkout.generation === origin.generation && this.canPublish(origin.repository) && (operation.state === 'failed' || operation.state === 'uncertain')) checkout.error = { code: problemCode(operation.resultCode), message: operation.message };
    // Even a newer rejected action does not make this operation's disk mutation disappear.
    checkout.generation++;
    this.mutationGeneration++;
    if (this.current === origin.repository.repositoryKey) this.publish();
    // Refresh physical data through the currently selected, resolved alias when available.
    // The operation and any already captured mutation retain their original authority.
    const refreshRef = this.current === origin.repository.repositoryKey ? checkout.snapshot.repository : origin.repository;
    void this.read(refreshRef, checkout.generation, true);
  }
}

export function useSourceControl(options: SourceControlOptions): SourceControlController {
  const api = options.api ?? sourceControlApi;
  const store = useMemo(() => new SourceControlStore(api), [api]);
  const state = useSyncExternalStore(store.subscribe, store.getState, store.getState);
  useEffect(() => { store.connect(); return () => store.disconnect(); }, [store]);
  useEffect(() => { store.setBeforeStage(options.beforeStage); }, [store, options.beforeStage]);
  const contextKey = JSON.stringify(options.context);
  useEffect(() => { store.configure(options); }, [store, contextKey, options.active, options.windowFocused]);
  return { ...state, refresh: store.refresh, setMessage: store.setMessage, setQuery: store.setQuery, select: store.select, stage: store.stage, unstage: store.unstage, commit: store.commit, push: store.push, cancelOperation: store.cancelOperation };
}
