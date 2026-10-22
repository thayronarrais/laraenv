// @vitest-environment jsdom
import { act, StrictMode } from 'react';
import { renderHook } from '@testing-library/react';
import { afterEach, expect, it, vi } from 'vitest';
import { useSourceControl } from '../useSourceControl';
import type { SourceControlAPI } from '../api';
import type { GitEntry, GitOperation, GitStatusSnapshot, OperationEvent, StagePermit } from '../types';
(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;
const context = (name = 'a') => ({ kind: 'project' as const, projectName: name });
const entry = (id = 'one'): GitEntry => ({ id, relativePath: id, originalPath: '', displayPath: id, indexCode: 'M', worktreeCode: 'M', kind: 'tracked', submoduleState: '', headOID: 'head', indexOID: 'index', headMode: '100644', indexMode: '100644', worktreeMode: '100644', canStage: true, canUnstage: true, disabledReason: '' });
const snapshot = (key = 'a', revision = 's1'): GitStatusSnapshot => ({ repository: { context: context(key), rootKey: 'root-' + key, repositoryKey: key }, root: { rootKey: 'root-' + key, canonicalRoot: '/' + key, label: key, path: '/' + key, canRead: true, canWrite: true }, snapshotRevision: revision, indexRevision: 'i1', headOID: 'head', branch: 'main', upstream: '', gitOperation: '', unborn: false, detached: false, countsKnown: true, hasConflicts: false, busy: false, ahead: 0, behind: 0, entries: [entry(), entry('two')], remotes: [], operationId: '' });
const operation = (key = 'a', state: GitOperation['state'] = 'running', kind: GitOperation['kind'] = 'commit'): GitOperation => ({ id: 'op-' + key, repository: snapshot(key).repository, kind, state, resultCode: state === 'failed' ? 'git_error' : 'ok', output: '', message: state === 'failed' ? 'hook rejected' : '', sequence: state === 'running' ? 1 : 3, outputTruncated: false, hasExitCode: state !== 'running', exitCode: state === 'failed' ? 1 : 0, startedAt: '2026-10-08T00:00:00Z', finishedAt: '', commitOID: '', remote: '', remoteBranch: '', mutationObserved: false, remoteUncertain: false, afterSnapshot: null });
const ok = <T,>(value: T) => ({ code: 'ok' as const, value });
function deferred<T>() { let resolve!: (value: T) => void; const promise = new Promise<T>(r => { resolve = r; }); return { promise, resolve }; }
function fakeAPI() {
  let listener: (event: OperationEvent) => void = () => {};
  const api: SourceControlAPI = { resolve: vi.fn(async c => ok(snapshot(c.kind === 'project' ? c.projectName : 'a'))), status: vi.fn(async r => ok(snapshot(r.repositoryKey))), diff: vi.fn(), stage: vi.fn(async r => ok(operation(r.repositoryKey, 'running', 'stage'))), unstage: vi.fn(async r => ok(operation(r.repositoryKey, 'running', 'unstage'))), commit: vi.fn(async r => ok(operation(r.repositoryKey))), push: vi.fn(async r => ok(operation(r.repositoryKey, 'running', 'push'))), operation: vi.fn(async () => ok(operation())), cancel: vi.fn(async () => ok(operation('a', 'cancelled'))), subscribeOperations: vi.fn(l => { listener = l; return () => { listener = () => {}; }; }) };
  return { api, emit: (op: GitOperation) => listener({ operationId: op.id, repository: op.repository, sequence: op.sequence, state: op.state, stream: '', chunk: '' }) };
}
async function mount(fake = fakeAPI(), beforeStage?: (s: GitStatusSnapshot, e: GitEntry[]) => Promise<StagePermit | null>) {
  const props = { context: context(), active: true, windowFocused: true, api: fake.api, beforeStage };
  const hook = renderHook(p => useSourceControl(p), { initialProps: props }); await act(async () => {}); return { ...hook, ...fake, props };
}
afterEach(() => vi.useRealTimers());
it('ignores a late resolve after a context change', async () => {
  const fake = fakeAPI(); const slow = deferred<any>(); vi.mocked(fake.api.resolve).mockImplementation(c => c.kind === 'project' && c.projectName === 'a' ? slow.promise : Promise.resolve(ok(snapshot('b'))));
  const h = await mount(fake); h.rerender({ ...h.props, context: context('b') }); await act(async () => {});
  await act(async () => slow.resolve({ code: 'unavailable', message: 'old context failed' }));
  expect(h.result.current.snapshot?.repository.repositoryKey).toBe('b'); expect(h.result.current.error).toBeNull(); h.unmount();
});
it('polls every 2000ms with one request in flight and pauses while inactive or hidden', async () => {
  vi.useFakeTimers(); const h = await mount(); const slow = deferred<any>(); vi.mocked(h.api.status).mockReturnValue(slow.promise);
  await act(async () => vi.advanceTimersByTime(1999)); expect(h.api.status).not.toHaveBeenCalled();
  await act(async () => vi.advanceTimersByTime(1)); expect(h.api.status).toHaveBeenCalledTimes(1);
  await act(async () => vi.advanceTimersByTime(8000)); expect(h.api.status).toHaveBeenCalledTimes(1);
  h.rerender({ ...h.props, windowFocused: false }); await act(async () => slow.resolve(ok(snapshot())));
  await act(async () => vi.advanceTimersByTime(6000)); expect(h.api.status).toHaveBeenCalledTimes(1);
  vi.mocked(h.api.status).mockResolvedValue(ok(snapshot('a', 'focused'))); h.rerender({ ...h.props }); await act(async () => {});
  expect(h.result.current.snapshot?.snapshotRevision).toBe('focused');
  h.rerender({ ...h.props, active: false }); const count = vi.mocked(h.api.status).mock.calls.length;
  await act(async () => vi.advanceTimersByTime(6000)); expect(h.api.status).toHaveBeenCalledTimes(count); h.unmount();
});
it('shares messages and intersected selections across aliases but isolates worktrees', async () => {
  const h = await mount(); act(() => { h.result.current.setMessage('draft'); h.result.current.setQuery('needle'); h.result.current.select('index', 'one', true); h.result.current.select('worktree', 'two', true); });
  vi.mocked(h.api.resolve).mockImplementation(async c => ok({ ...snapshot(c.kind === 'project' && c.projectName === 'alias' ? 'a' : 'worktree'), repository: { ...snapshot(c.kind === 'project' && c.projectName === 'alias' ? 'a' : 'worktree').repository, context: c }, entries: [entry()] }));
  h.rerender({ ...h.props, context: context('worktree') }); await act(async () => {}); expect(h.result.current.message).toBe(''); expect(h.result.current.selectedIndexIds).toEqual([]);
  act(() => h.result.current.setMessage('other')); h.rerender({ ...h.props, context: context('alias') }); await act(async () => {});
  expect(h.result.current.message).toBe('draft'); expect(h.result.current.query).toBe('needle'); expect(h.result.current.selectedIndexIds).toEqual(['one']); expect(h.result.current.selectedWorktreeIds).toEqual([]); h.unmount();
});
it('rejects a pre-mutation status and refreshes after recovered terminal completion', async () => {
  const h = await mount(); const old = deferred<any>(); vi.mocked(h.api.status).mockReturnValueOnce(old.promise).mockResolvedValue(ok(snapshot('a', 'after')));
  let refreshing!: Promise<void>; act(() => { refreshing = h.result.current.refresh(); }); act(() => h.result.current.setMessage('commit'));
  vi.mocked(h.api.operation).mockResolvedValue(ok(operation('a', 'succeeded')));
  await act(async () => { await h.result.current.commit(); });
  await act(async () => { old.resolve(ok(snapshot('a', 'old'))); await refreshing; });
  expect(h.result.current.snapshot?.snapshotRevision).toBe('after'); expect(h.result.current.message).toBe(''); h.unmount();
});
it.each(['failed', 'succeeded'] as const)('keeps operation origin across context switches (%s)', async state => {
  const h = await mount(); const start = deferred<any>(); vi.mocked(h.api.commit).mockReturnValue(start.promise);
  act(() => h.result.current.setMessage('original')); let committing!: Promise<void>; act(() => { committing = h.result.current.commit(); });
  h.rerender({ ...h.props, context: context('b') }); await act(async () => {}); act(() => h.result.current.setMessage('B draft'));
  vi.mocked(h.api.operation).mockResolvedValue(ok(operation('a', state))); await act(async () => { start.resolve(ok(operation())); await committing; });
  expect(h.result.current.snapshot?.repository.repositoryKey).toBe('b'); expect(h.result.current.message).toBe('B draft'); expect(h.result.current.error).toBeNull();
  h.rerender(h.props); await act(async () => {}); expect(h.result.current.message).toBe(state === 'succeeded' ? '' : 'original'); h.unmount();
});
it('preserves an edited commit message when the captured commit succeeds', async () => {
  const h = await mount(); act(() => h.result.current.setMessage('submitted')); await act(async () => h.result.current.commit()); act(() => h.result.current.setMessage('next commit'));
  vi.mocked(h.api.operation).mockResolvedValue(ok(operation('a', 'succeeded'))); await act(async () => h.emit(operation('a', 'succeeded')));
  expect(h.result.current.message).toBe('next commit'); h.unmount();
});
it('rechecks the stage permit after the captured refresh and never retargets', async () => {
  const permit = deferred<StagePermit | null>(); const h = await mount(fakeAPI(), () => permit.promise); const refreshed = deferred<any>();
  vi.mocked(h.api.status).mockReturnValueOnce(refreshed.promise); let staging!: Promise<void>; act(() => { staging = h.result.current.stage(['one']); });
  h.rerender({ ...h.props, context: context('b') }); await act(async () => {}); let valid = true;
  await act(async () => permit.resolve({ validate: () => valid })); expect(h.api.status).toHaveBeenCalledWith(snapshot().repository);
  valid = false; await act(async () => { refreshed.resolve(ok(snapshot('a', 'new'))); await staging; });
  expect(h.api.stage).not.toHaveBeenCalled(); expect(h.result.current.snapshot?.repository.repositoryKey).toBe('b'); expect(h.result.current.error).toBeNull(); h.unmount();
});
it('stages captured entries with the fresh expectation and permits binary whole-file stage', async () => {
  const beforeStage = vi.fn(async () => ({ validate: () => true })); const h = await mount(fakeAPI(), beforeStage);
  vi.mocked(h.api.status).mockResolvedValue(ok(snapshot('a', 'refreshed'))); await act(async () => h.result.current.stage(['one']));
  expect(beforeStage).toHaveBeenCalledWith(expect.objectContaining({ snapshotRevision: 's1' }), [entry()]);
  expect(h.api.stage).toHaveBeenCalledWith(snapshot().repository, { snapshotRevision: 'refreshed', indexRevision: 'i1', headOID: 'head', branch: 'main' }, [{ entryId: 'one', diskRevision: '' }]); expect(h.api.diff).not.toHaveBeenCalled(); h.unmount();
});
it('preserves selections and message on start errors, dispatches unstage/push and recovers cancellation', async () => {
  const h = await mount(); act(() => { h.result.current.setMessage('draft'); h.result.current.select('index', 'one', true); });
  vi.mocked(h.api.commit).mockResolvedValue({ code: 'busy', message: 'locked' }); await act(async () => h.result.current.commit());
  expect(h.result.current).toMatchObject({ message: 'draft', selectedIndexIds: ['one'], error: { code: 'busy' } });
  await act(async () => h.result.current.unstage(['one'])); expect(h.api.unstage).toHaveBeenCalledWith(snapshot().repository, expect.objectContaining({ headOID: 'head' }), [{ entryId: 'one', diskRevision: '' }]);
  const target = { remoteId: 'remote', remoteBranch: 'refs/heads/topic/nested', setUpstream: true }; await act(async () => h.result.current.push(target)); expect(h.api.push).toHaveBeenCalledWith(snapshot().repository, expect.anything(), target);
  await act(async () => h.result.current.cancelOperation('op-a')); expect(h.result.current.operations).toMatchObject([{ state: 'cancelled' }]); h.unmount();
});
it('rejects an alias resolve started before terminal mutation recovery', async () => {
  const h = await mount(); act(() => h.result.current.setMessage('submitted')); await act(async () => h.result.current.commit());
  const alias = deferred<any>(); vi.mocked(h.api.resolve).mockReturnValueOnce(alias.promise).mockResolvedValue(ok({ ...snapshot('a', 'after'), repository: { ...snapshot().repository, context: context('alias') } }));
  h.rerender({ ...h.props, context: context('alias') }); await act(async () => {});
  vi.mocked(h.api.status).mockResolvedValue(ok(snapshot('a', 'after'))); vi.mocked(h.api.operation).mockResolvedValue(ok(operation('a', 'succeeded')));
  await act(async () => h.emit(operation('a', 'succeeded')));
  await act(async () => alias.resolve(ok(snapshot('a', 'before'))));
  expect(h.result.current.snapshot?.snapshotRevision).toBe('after'); expect(h.result.current.snapshot?.repository.context).toEqual(context('alias')); h.unmount();
});
it('ignores status from an old context visit after revisiting the same checkout', async () => {
  const h = await mount(); const old = deferred<any>(); vi.mocked(h.api.status).mockReturnValueOnce(old.promise); let refreshing!: Promise<void>;
  act(() => { refreshing = h.result.current.refresh(); }); h.rerender({ ...h.props, context: context('b') }); await act(async () => {});
  vi.mocked(h.api.resolve).mockResolvedValue(ok(snapshot('a', 'new-visit'))); h.rerender(h.props); await act(async () => {});
  await act(async () => { old.resolve({ code: 'unavailable', message: 'old error' }); await refreshing; });
  expect(h.result.current.snapshot?.snapshotRevision).toBe('new-visit'); expect(h.result.current.error).toBeNull(); h.unmount();
});
it('keeps captured stage selections if the caller mutates its array while the permit is open', async () => {
  const permit = deferred<StagePermit | null>(); const h = await mount(fakeAPI(), () => permit.promise); const ids = ['one']; let staging!: Promise<void>;
  act(() => { staging = h.result.current.stage(ids); }); ids.splice(0, 1, 'missing');
  await act(async () => { permit.resolve({ validate: () => true }); await staging; });
  expect(h.api.stage).toHaveBeenCalledWith(snapshot().repository, expect.anything(), [{ entryId: 'one', diskRevision: '' }]); h.unmount();
});
it('recovers a running operation by ID on polling when its terminal event was missed', async () => {
  vi.useFakeTimers(); const h = await mount(); act(() => h.result.current.setMessage('submitted')); await act(async () => h.result.current.commit());
  vi.mocked(h.api.operation).mockResolvedValue(ok(operation('a', 'succeeded'))); vi.mocked(h.api.status).mockResolvedValue(ok(snapshot('a', 'terminal')));
  await act(async () => vi.advanceTimersByTime(2000)); expect(h.result.current.message).toBe(''); expect(h.result.current.snapshot?.snapshotRevision).toBe('terminal'); h.unmount();
});
it('does not issue background queries before activation and survives strict effect remounting', async () => {
  const fake = fakeAPI(); const h = renderHook(p => useSourceControl(p), { initialProps: { context: context(), active: false, windowFocused: true, api: fake.api }, wrapper: StrictMode });
  await act(async () => {}); expect(fake.api.resolve).not.toHaveBeenCalled(); h.rerender({ context: context(), active: true, windowFocused: true, api: fake.api }); await act(async () => {});
  expect(h.result.current.snapshot?.repository.repositoryKey).toBe('a'); h.unmount();
});
it('blocks stage when refresh fails or a captured entry disappeared', async () => {
  const h = await mount(); act(() => { h.result.current.setMessage('draft'); h.result.current.select('worktree', 'one', true); });
  vi.mocked(h.api.status).mockResolvedValueOnce({ code: 'repository_changed', message: 'replaced' }); await act(async () => h.result.current.stage(['one']));
  expect(h.api.stage).not.toHaveBeenCalled(); expect(h.result.current).toMatchObject({ message: 'draft', selectedWorktreeIds: ['one'], error: { code: 'repository_changed' } });
  vi.mocked(h.api.status).mockResolvedValueOnce(ok({ ...snapshot(), entries: [entry('two')] })); await act(async () => h.result.current.stage(['one']));
  expect(h.api.stage).not.toHaveBeenCalled(); expect(h.result.current.error?.code).toBe('stale_snapshot'); h.unmount();
});
it('does not clear a message edited and then restored while a commit is running', async () => {
  const h = await mount(); act(() => h.result.current.setMessage('submitted')); await act(async () => h.result.current.commit());
  act(() => { h.result.current.setMessage('replacement'); h.result.current.setMessage('submitted'); });
  vi.mocked(h.api.operation).mockResolvedValue(ok(operation('a', 'succeeded'))); await act(async () => h.emit(operation('a', 'succeeded')));
  expect(h.result.current.message).toBe('submitted'); h.unmount();
});
it('does not replace a newly resolved alias with an older captured terminal refresh', async () => {
  const h = await mount(); const oldRefresh = deferred<any>(); vi.mocked(h.api.status).mockReturnValueOnce(oldRefresh.promise);
  vi.mocked(h.api.operation).mockResolvedValue(ok(operation('a', 'succeeded'))); await act(async () => h.result.current.commit());
  vi.mocked(h.api.resolve).mockResolvedValue(ok({ ...snapshot('a', 'alias-current'), repository: { ...snapshot().repository, context: context('alias') } }));
  h.rerender({ ...h.props, context: context('alias') }); await act(async () => {});
  await act(async () => oldRefresh.resolve(ok(snapshot('a', 'old-origin'))));
  expect(h.result.current.snapshot?.snapshotRevision).toBe('alias-current'); expect(h.result.current.snapshot?.repository.context).toEqual(context('alias')); h.unmount();
});
it('refreshes terminal mutations even after a newer action was rejected as busy', async () => {
  const h = await mount(); await act(async () => h.result.current.commit());
  vi.mocked(h.api.push).mockResolvedValue({ code: 'busy', message: 'newer request busy' });
  await act(async () => h.result.current.push({ remoteId: 'origin', remoteBranch: 'refs/heads/main', setUpstream: false }));
  vi.mocked(h.api.status).mockResolvedValue(ok(snapshot('a', 'committed'))); vi.mocked(h.api.operation).mockResolvedValue(ok(operation('a', 'succeeded')));
  await act(async () => h.emit(operation('a', 'succeeded')));
  expect(h.result.current.snapshot?.snapshotRevision).toBe('committed'); expect(h.result.current.error?.message).toBe('newer request busy'); h.unmount();
});
it.each([false, true])('preserves active alias authority when an older operation completes later (old alias expired: %s)', async expired => {
  const h = await mount(); act(() => h.result.current.setMessage('submitted')); await act(async () => h.result.current.commit());
  const aliasRef = { ...snapshot().repository, context: context('alias') };
  vi.mocked(h.api.resolve).mockResolvedValue(ok({ ...snapshot('a', 'alias-resolved'), repository: aliasRef }));
  h.rerender({ ...h.props, context: context('alias') }); await act(async () => {});
  vi.mocked(h.api.status).mockImplementation(async ref => ref.context.kind === 'project' && ref.context.projectName === 'alias'
    ? ok({ ...snapshot('a', 'alias-refreshed'), repository: aliasRef })
    : expired ? { code: 'unavailable', message: 'old alias expired' } : ok(snapshot('a', 'old-alias')));
  vi.mocked(h.api.operation).mockResolvedValue(ok(operation('a', 'succeeded')));
  await act(async () => h.emit(operation('a', 'succeeded')));
  expect(h.result.current.snapshot?.repository).toEqual(aliasRef);
  expect(h.result.current.snapshot?.snapshotRevision).toBe('alias-refreshed'); expect(h.result.current.error).toBeNull();
  expect(h.result.current.operations[0].repository.context).toEqual(context('a'));
  await act(async () => h.result.current.stage(['one']));
  expect(h.api.stage).toHaveBeenLastCalledWith(aliasRef, expect.objectContaining({ snapshotRevision: 'alias-refreshed' }), [{ entryId: 'one', diskRevision: '' }]); h.unmount();
});
it('polls on the original schedule through inline callback rerenders and uses the latest stage callback', async () => {
  vi.useFakeTimers(); const fake = fakeAPI(); const props = { context: context(), active: true, windowFocused: true, api: fake.api, allowStage: true };
  const h = renderHook(p => useSourceControl({ ...p, beforeStage: async () => p.allowStage ? { validate: () => true } : null }), { initialProps: props }); await act(async () => {});
  for (let elapsed = 500; elapsed <= 4000; elapsed += 500) {
    await act(async () => vi.advanceTimersByTime(500));
    act(() => h.result.current.setMessage(String(elapsed)));
    h.rerender({ ...props, allowStage: elapsed < 4000 });
    expect(fake.api.status).toHaveBeenCalledTimes(Math.floor(elapsed / 2000));
  }
  await act(async () => h.result.current.stage(['one'])); expect(fake.api.stage).not.toHaveBeenCalled(); h.unmount();
});
it.each([false, true])('keeps a captured stage refresh from replacing or erroring the newly active alias (refresh failed: %s)', async failed => {
  const permit = deferred<StagePermit | null>(); const h = await mount(fakeAPI(), () => permit.promise); let staging!: Promise<void>;
  act(() => { staging = h.result.current.stage(['one']); });
  const aliasRef = { ...snapshot().repository, context: context('alias') };
  vi.mocked(h.api.resolve).mockResolvedValue(ok({ ...snapshot('a', 'alias-current'), repository: aliasRef }));
  h.rerender({ ...h.props, context: context('alias') }); await act(async () => {});
  vi.mocked(h.api.status).mockResolvedValue(failed ? { code: 'unavailable', message: 'old alias expired' } : ok(snapshot('a', 'stage-captured')));
  await act(async () => { permit.resolve({ validate: () => true }); await staging; });
  expect(h.result.current.snapshot?.repository).toEqual(aliasRef); expect(h.result.current.error).toBeNull();
  expect(h.api.status).toHaveBeenCalledWith(snapshot().repository);
  if (failed) expect(h.api.stage).not.toHaveBeenCalled();
  else expect(h.api.stage).toHaveBeenCalledWith(snapshot().repository, expect.objectContaining({ snapshotRevision: 'stage-captured' }), [{ entryId: 'one', diskRevision: '' }]);
  h.unmount();
});
