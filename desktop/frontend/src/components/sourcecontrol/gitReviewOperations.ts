import type { SourceControlAPI } from './api';
import type { RootInfo } from '../documents/types';
import { immutableCopy } from './types';
import type { ExpectedRepositoryState, GitDiff, GitLayer, GitProblemCode, RepositoryRef } from './types';
export type GitReviewOrigin = Pick<RootInfo, 'label' | 'path'>;
export type GitReviewTab = { id: string; ref: RepositoryRef; root: GitReviewOrigin; entryId: string; layer: GitLayer; relativePath: string; snapshotRevision: string; status: 'loading' | 'ready' | 'error'; diff: GitDiff | null; problem?: { code: GitProblemCode; message: string } };
type ReviewState = { tabs: GitReviewTab[]; activeTabId: string | null };
export interface GitReviewController extends ReviewState {
  open(ref: RepositoryRef, expected: ExpectedRepositoryState, id: string, layer: GitLayer, root: GitReviewOrigin): Promise<void>;
  select(id: string): void; clearActive(): void; close(id: string): void;
}
let nextReview = 0;
export class GitReviewOperations {
  private state: ReviewState = { tabs: [], activeTabId: null };
  private listeners = new Set<() => void>();
  constructor(private api: Pick<SourceControlAPI, 'diff'>) {}
  subscribe = (listener: () => void) => { this.listeners.add(listener); return () => { this.listeners.delete(listener); }; };
  getSnapshot = () => this.state;
  private publish(state: ReviewState) { this.state = immutableCopy(state); this.listeners.forEach(fn => fn()); }
  select = (id: string) => { if (this.state.tabs.some(tab => tab.id === id)) this.publish({ ...this.state, activeTabId: id }); };
  clearActive = () => this.publish({ ...this.state, activeTabId: null });
  close = (id: string) => { const tabs = this.state.tabs.filter(tab => tab.id !== id); this.publish({ tabs, activeTabId: this.state.activeTabId === id ? tabs[tabs.length - 1]?.id ?? null : this.state.activeTabId }); };
  dispose = () => this.publish({ tabs: [], activeTabId: null });
  open = async (ref: RepositoryRef, expected: ExpectedRepositoryState, entryId: string, layer: GitLayer, root: GitReviewOrigin): Promise<void> => {
    // Status tokens do not identify disk bytes. Every explicit review reads a
    // fresh comparison, capturing its authority and presentation before await.
    ref = immutableCopy(ref); expected = immutableCopy(expected); root = immutableCopy({ label: root.label, path: root.path });
    const id = `review-${++nextReview}`;
    this.publish({ tabs: [...this.state.tabs, { id, ref, root, entryId, layer, snapshotRevision: expected.snapshotRevision, relativePath: '', status: 'loading', diff: null }], activeTabId: id });
    let update: Partial<GitReviewTab>;
    try { const result = await this.api.diff(immutableCopy(ref), immutableCopy(expected), entryId, layer);
      update = result.code === 'ok' ? { status: 'ready', diff: result.value, relativePath: result.value.relativePath } : { status: 'error', problem: { code: result.code, message: result.message } };
    } catch (error) { update = { status: 'error', problem: { code: 'git_error', message: error instanceof Error ? error.message : String(error) } }; }
    if (!this.state.tabs.some(tab => tab.id === id)) return;
    const existing = update.diff && this.state.tabs.find(tab => tab.id !== id && tab.ref.repositoryKey === ref.repositoryKey && tab.ref.rootKey === ref.rootKey && tab.entryId === entryId && tab.layer === layer && tab.diff?.id === update.diff!.id && tab.diff.original.revision === update.diff!.original.revision && tab.diff.modified.revision === update.diff!.modified.revision);
    if (existing) this.publish({ tabs: this.state.tabs.filter(tab => tab.id !== id), activeTabId: this.state.activeTabId === id ? existing.id : this.state.activeTabId });
    else this.publish({ ...this.state, tabs: this.state.tabs.map(tab => tab.id === id ? { ...tab, ...update } : tab) });
  };
}
