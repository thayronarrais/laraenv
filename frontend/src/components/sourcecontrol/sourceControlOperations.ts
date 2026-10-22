import type { SourceControlAPI } from './api';
import { immutableCopy } from './types';
import type { GitOperation, GitResult, OperationEvent, RepositoryRef } from './types';

export type OperationOrigin = { repository: RepositoryRef; generation: number; message?: string; messageVersion?: number };
export const isTerminal = (operation: GitOperation) => operation.state !== 'running' && operation.state !== 'cancelling';

/** Events are hints: ID recovery supplies complete bounded output and terminal results. */
export class SourceControlOperations {
  private operations = new Map<string, GitOperation>();
  private sequences = new Map<string, number>();
  private origins = new Map<string, OperationOrigin>();
  private finished = new Map<string, OperationOrigin>();
  private pending = new Map<string, Promise<void>>();
  private off: (() => void) | undefined;
  constructor(private api: Pick<SourceControlAPI, 'subscribeOperations' | 'operation'>, private changed: () => void, private terminal: (operation: GitOperation, origin: OperationOrigin) => void) {}
  connect() { this.off ??= this.api.subscribeOperations(event => this.onEvent(event)); }
  disconnect() { this.off?.(); this.off = undefined; }
  values() { return [...this.operations.values()]; }
  private finish(id: string) {
    const operation = this.operations.get(id); const origin = this.origins.get(id);
    if (operation && origin && isTerminal(operation) && this.finished.get(id) !== origin) {
      this.finished.set(id, origin); this.terminal(operation, origin);
    }
  }
  accept(operation: GitOperation) {
    if (operation.sequence < (this.sequences.get(operation.id) ?? -1)) return;
    const old = this.operations.get(operation.id);
    if (old && (old.sequence >= operation.sequence || isTerminal(old))) return;
    this.operations.set(operation.id, immutableCopy(operation));
    this.changed(); this.finish(operation.id);
  }
  private onEvent(event: OperationEvent) {
    const latest = Math.max(this.sequences.get(event.operationId) ?? -1, this.operations.get(event.operationId)?.sequence ?? -1);
    if (event.sequence <= latest) return;
    this.sequences.set(event.operationId, event.sequence);
    void this.recover(event.operationId);
  }
  async recover(id: string, origin?: OperationOrigin): Promise<void> {
    if (origin && !this.origins.has(id)) this.origins.set(id, immutableCopy(origin));
    const existing = this.pending.get(id); if (existing) return existing;
    const work = (async () => {
      // Re-enter only when an event arriving during this query advanced the watermark.
      for (let attempt = 0; attempt < 2; attempt++) {
        const watermark = this.sequences.get(id) ?? -1;
        try { const response = await this.api.operation(id); if (response.code === 'ok') this.accept(response.value); }
        catch { /* Keep the operation; the next event/poll retries ID recovery. */ }
        if ((this.sequences.get(id) ?? -1) <= watermark) break;
      }
      this.finish(id);
    })();
    this.pending.set(id, work);
    try { await work; } finally { if (this.pending.get(id) === work) this.pending.delete(id); }
  }
  async start(origin: OperationOrigin, request: () => Promise<GitResult<GitOperation>>): Promise<GitResult<GitOperation>> {
    this.connect();
    const capturedOrigin = immutableCopy(origin);
    const response = await request();
    if (response.code !== 'ok') return response;
    const id = response.value.id;
    // The terminal event may already have been recovered before the start reply.
    this.origins.set(id, capturedOrigin); this.accept(response.value); this.finish(id);
    await this.recover(id);
    return response;
  }
}
