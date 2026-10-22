import type { DocumentBuffer, DocumentController } from '../documents/types';
import { immutableCopy } from './types';
import type { GitEntry, GitStatusSnapshot, RepositoryRef, StageDirtyChoice, StagePermit } from './types';

export type StageDirtyDecision = { id: string; repository: RepositoryRef; label: string; path: string; names: string[] };
type Documents = Pick<DocumentController, 'getBuffers' | 'getBuffer' | 'save'>;
let nextDecision = 0;
/** A pending choice belongs to its initiating checkout, independently of visible navigation. */
export class StageDocuments {
  private decision: StageDirtyDecision | null = null;
  private resolve: ((choice: StageDirtyChoice) => void) | null = null;
  private listeners = new Set<() => void>();
  constructor(private documents: Documents) {}
  subscribe = (listener: () => void) => { this.listeners.add(listener); return () => { this.listeners.delete(listener); }; };
  getDecision = () => this.decision;
  private publish(decision: StageDirtyDecision | null) { this.decision = decision; this.listeners.forEach(fn => fn()); }
  choose = (choice: StageDirtyChoice) => { const resolve = this.resolve; this.resolve = null; this.publish(null); resolve?.(choice); };
  dispose = () => this.choose('cancel');
  beforeStage = async (snapshot: GitStatusSnapshot, entries: GitEntry[]): Promise<StagePermit | null> => {
    if (this.resolve) return null;
    const paths = new Set(entries.flatMap(entry => [entry.relativePath, entry.originalPath].filter(Boolean)));
    const root = snapshot.repository.rootKey;
    // Only references validated into a live buffer confer authority. Restored tab paths do not.
    const matches = (buffer: DocumentBuffer) => buffer.references.some(ref => ref.rootKey === root && paths.has(ref.relativePath));
    const affected = this.documents.getBuffers().filter(matches).map(buffer => ({ id: buffer.id, generation: buffer.editGeneration, dirty: buffer.dirty }));
    const identityValid = () => {
      const current = this.documents.getBuffers().filter(matches);
      return current.length === affected.length && affected.every(captured => {
        const buffer = this.documents.getBuffer(captured.id);
        return !!buffer && matches(buffer) && buffer.editGeneration === captured.generation;
      });
    };
    const clean = (id: string) => { const b = this.documents.getBuffer(id); return !!b && b.status === 'ready' && !b.dirty && !b.pendingDiskSnapshot; };
    const permit = { validate: () => identityValid() && affected.every(b => clean(b.id)) };
    if (!affected.some(buffer => buffer.dirty)) return permit;
    const choice = await new Promise<StageDirtyChoice>(resolve => {
      this.resolve = resolve;
      this.publish(immutableCopy({ id: `stage-${++nextDecision}`, repository: snapshot.repository, label: snapshot.root.label, path: snapshot.root.path,
        names: this.documents.getBuffers().filter(buffer => matches(buffer) && buffer.dirty).map(buffer => buffer.references.find(ref => ref.rootKey === root && paths.has(ref.relativePath))!.relativePath) }));
    });
    if (choice === 'cancel') return null;
    // This explicitly stages disk bytes; unsaved buffer generations are deliberately irrelevant.
    if (choice === 'stage-disk') return { validate: () => true };
    const saved = new Set<string>();
    try {
      for (const captured of affected) {
        if (!identityValid() || ![...saved].every(clean)) return null;
        if (captured.dirty) {
          if (!await this.documents.save(captured.id)) return null;
          saved.add(captured.id);
          if (!identityValid() || ![...saved].every(clean)) return null;
        }
      }
    } catch { return null; }
    return permit.validate() ? permit : null;
  };
}
