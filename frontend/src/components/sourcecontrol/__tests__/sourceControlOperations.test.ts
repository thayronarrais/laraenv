import { expect, it, vi } from 'vitest';
import { SourceControlOperations } from '../sourceControlOperations';
import type { GitOperation, OperationEvent } from '../types';
const repository = { context: { kind: 'project' as const, projectName: 'a' }, rootKey: 'root', repositoryKey: 'a' };
const operation = (sequence = 1, state: GitOperation['state'] = 'running'): GitOperation => ({ id: 'op', repository, kind: 'commit', state, resultCode: state === 'succeeded' ? 'ok' : '', output: '', message: '', sequence, outputTruncated: false, hasExitCode: false, exitCode: 0, startedAt: '2026-10-08T00:00:00Z', finishedAt: '', commitOID: '', remote: '', remoteBranch: '', mutationObserved: false, remoteUncertain: false, afterSnapshot: null });
function deferred<T>() { let resolve!: (value: T) => void; const promise = new Promise<T>(r => { resolve = r; }); return { promise, resolve }; }
it('recovers an event before the start reply by ID and preserves the captured origin', async () => {
  let listener!: (event: OperationEvent) => void; const off = vi.fn(); const terminal = vi.fn(); const start = deferred<any>();
  const api = { subscribeOperations: (l: typeof listener) => { listener = l; return off; }, operation: vi.fn(async () => ({ code: 'ok' as const, value: operation(3, 'succeeded') })) };
  const tracker = new SourceControlOperations(api, vi.fn(), terminal); tracker.connect();
  const origin = { repository, generation: 1, message: 'captured' }; const pending = tracker.start(origin, () => start.promise);
  listener({ operationId: 'op', repository, sequence: 3, state: 'succeeded', stream: '', chunk: '' });
  await Promise.resolve(); await Promise.resolve(); start.resolve({ code: 'ok', value: operation() }); await pending;
  expect(api.operation).toHaveBeenCalledWith('op'); expect(tracker.values()).toMatchObject([{ sequence: 3, state: 'succeeded' }]);
  expect(terminal).toHaveBeenCalledOnce(); expect(terminal).toHaveBeenCalledWith(expect.objectContaining({ id: 'op', state: 'succeeded' }), origin);
  tracker.disconnect(); expect(off).toHaveBeenCalledOnce();
});
it('ignores replayed events and older recovery responses without losing a newer terminal event', async () => {
  let listener!: (event: OperationEvent) => void; const first = deferred<any>(); const terminal = vi.fn();
  const query = vi.fn().mockReturnValueOnce(first.promise).mockResolvedValue({ code: 'ok', value: operation(4, 'succeeded') });
  const tracker = new SourceControlOperations({ subscribeOperations: l => { listener = l; return () => {}; }, operation: query }, vi.fn(), terminal); tracker.connect();
  const pending = tracker.start({ repository, generation: 1 }, async () => ({ code: 'ok', value: operation() })); await Promise.resolve();
  listener({ operationId: 'op', repository, sequence: 4, state: 'succeeded', stream: '', chunk: '' });
  listener({ operationId: 'op', repository, sequence: 2, state: 'running', stream: '', chunk: 'old' });
  first.resolve({ code: 'ok', value: operation(2) }); await pending;
  expect(tracker.values()).toMatchObject([{ sequence: 4, state: 'succeeded' }]); expect(terminal).toHaveBeenCalledOnce();
  const calls = query.mock.calls.length; listener({ operationId: 'op', repository, sequence: 4, state: 'succeeded', stream: '', chunk: '' });
  expect(query).toHaveBeenCalledTimes(calls); tracker.disconnect();
});
it('does not apply a terminal recovery older than the latest event sequence', async () => {
  let listener!: (event: OperationEvent) => void; const stale = deferred<any>(); const terminal = vi.fn();
  const query = vi.fn().mockReturnValueOnce(stale.promise).mockResolvedValue({ code: 'ok', value: operation(5, 'succeeded') });
  const tracker = new SourceControlOperations({ subscribeOperations: l => { listener = l; return () => {}; }, operation: query }, vi.fn(), terminal); tracker.connect();
  const start = tracker.start({ repository, generation: 1 }, async () => ({ code: 'ok', value: operation() })); await Promise.resolve();
  listener({ operationId: 'op', repository, sequence: 5, state: 'succeeded', stream: '', chunk: '' });
  stale.resolve({ code: 'ok', value: operation(3, 'failed') }); await start;
  expect(tracker.values()).toMatchObject([{ state: 'succeeded', sequence: 5 }]); expect(terminal).toHaveBeenCalledOnce(); tracker.disconnect();
});
it('attaches captured commit metadata after snapshot recovery discovered the operation first', async () => {
  const terminal = vi.fn(); const start = deferred<any>();
  const tracker = new SourceControlOperations({ subscribeOperations: () => () => {}, operation: async () => ({ code: 'ok', value: operation(3, 'succeeded') }) }, vi.fn(), terminal);
  const origin = { repository, generation: 1, message: 'submitted' }; const pending = tracker.start(origin, () => start.promise);
  await tracker.recover('op', { repository, generation: 1 }); start.resolve({ code: 'ok', value: operation() }); await pending;
  expect(terminal).toHaveBeenLastCalledWith(expect.objectContaining({ state: 'succeeded' }), origin); tracker.disconnect();
});
