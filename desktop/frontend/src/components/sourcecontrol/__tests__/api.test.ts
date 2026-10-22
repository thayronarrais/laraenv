import { beforeEach, expect, it, vi } from 'vitest';
const native = vi.hoisted(() => ({ resolve: vi.fn(), status: vi.fn(), diff: vi.fn(), stage: vi.fn(), unstage: vi.fn(), commit: vi.fn(), push: vi.fn(), operation: vi.fn(), cancel: vi.fn(), events: vi.fn() }));
vi.mock('../../../../wailsjs/go/main/App', () => ({ InspectSourceControl: native.resolve, GetSourceControlStatus: native.status, ReadSourceControlDiff: native.diff, StartSourceControlStage: native.stage, StartSourceControlUnstage: native.unstage, StartSourceControlCommit: native.commit, StartSourceControlPush: native.push, GetSourceControlOperation: native.operation, CancelSourceControlOperation: native.cancel }));
vi.mock('../../../../wailsjs/runtime/runtime', () => ({ EventsOn: native.events }));
import { sourceControlApi } from '../api';
const context = { kind: 'project' as const, projectName: 'app' };
const repository = { context, rootKey: 'root', repositoryKey: 'checkout' };
const dtoRepository = { ...repository, context: { ...context, sessionId: '' } };
const expected = { snapshotRevision: 's', indexRevision: 'i', headOID: 'head', branch: 'main' };
const rawSnapshot = () => ({ repository: dtoRepository, root: { rootKey: 'root', canonicalRoot: '/app', label: 'app', path: '/app', canRead: true, canWrite: true }, ...expected, upstream: '', gitOperation: '', unborn: false, detached: false, countsKnown: true, hasConflicts: false, busy: false, ahead: 0, behind: 0, entries: [], remotes: [{ id: 'remote', name: 'origin', displayURL: 'https://example.test/repo', canPush: true, disabledReason: '' }], operationId: '' });
const rawOperation = () => ({ id: 'op', repository: dtoRepository, kind: 'commit', state: 'succeeded', resultCode: 'ok', output: 'done', message: '', sequence: 3, outputTruncated: false, hasExitCode: true, exitCode: 0, startedAt: '2026-10-08T00:00:00Z', finishedAt: '2026-10-08T00:00:01Z', commitOID: 'newhead', remote: '', remoteBranch: '', mutationObserved: true, remoteUncertain: false, afterSnapshot: rawSnapshot() });
beforeEach(() => vi.resetAllMocks());
it('unwraps native pointers, preserves acronym fields, and returns detached immutable data', async () => {
  const raw = rawSnapshot(); native.resolve.mockResolvedValue({ code: 'ok', message: '', snapshot: raw, operation: null, diff: null, busy: null });
  const result = await sourceControlApi.resolve(context);
  expect(result).toMatchObject({ code: 'ok', value: { headOID: 'head', entries: [], remotes: [{ displayURL: 'https://example.test/repo' }] } });
  expect(native.resolve).toHaveBeenCalledWith({ kind: 'project', projectName: 'app', sessionId: '' });
  raw.remotes[0].displayURL = 'changed';
  if (result.code === 'ok') { expect(result.value.remotes[0].displayURL).toBe('https://example.test/repo'); expect(Object.isFrozen(result.value.entries)).toBe(true); }
});
it('normalizes missing pointers, empty collections and unknown/native failures', async () => {
  native.status.mockResolvedValueOnce({ code: 'ok', snapshot: null }).mockResolvedValueOnce({ code: 'future_code', message: 'details' }).mockResolvedValueOnce({ code: 'stale_snapshot', message: 'changed' }).mockResolvedValueOnce({ code: 'ok', snapshot: { ...rawSnapshot(), entries: null, remotes: null } });
  expect(await sourceControlApi.status(repository)).toMatchObject({ code: 'git_error' });
  expect(await sourceControlApi.status(repository)).toEqual({ code: 'git_error', message: 'details' });
  expect(await sourceControlApi.status(repository)).toEqual({ code: 'stale_snapshot', message: 'changed' });
  expect(await sourceControlApi.status(repository)).toMatchObject({ code: 'ok', value: { entries: [], remotes: [] } });
});
it('maps every request facade and operation dates without leaking Result wrappers', async () => {
  for (const method of ['stage', 'unstage', 'commit', 'push', 'operation', 'cancel'] as const) native[method].mockResolvedValue({ code: 'ok', message: '', operation: rawOperation() });
  const selections = [{ entryId: 'entry', diskRevision: 'disk' }];
  await sourceControlApi.stage(repository, expected, selections); await sourceControlApi.unstage(repository, expected, selections);
  await sourceControlApi.commit(repository, expected, 'message');
  const result = await sourceControlApi.push(repository, expected, { remoteId: 'remote', remoteBranch: 'refs/heads/feature/topic', setUpstream: true });
  expect(native.stage).toHaveBeenCalledWith({ repository: dtoRepository, expected, entries: selections });
  expect(native.unstage).toHaveBeenCalledWith({ repository: dtoRepository, expected, entries: selections });
  expect(native.commit).toHaveBeenCalledWith({ repository: dtoRepository, expected, message: 'message' });
  expect(native.push).toHaveBeenCalledWith({ repository: dtoRepository, expected, remoteId: 'remote', remoteBranch: 'refs/heads/feature/topic', setUpstream: true });
  expect(result).toMatchObject({ code: 'ok', value: { startedAt: '2026-10-08T00:00:00Z', finishedAt: '2026-10-08T00:00:01Z', afterSnapshot: { headOID: 'head' } } });
  await sourceControlApi.operation('op'); await sourceControlApi.cancel('op');
  expect(native.operation).toHaveBeenCalledWith('op'); expect(native.cancel).toHaveBeenCalledWith('op');
  const side = { text: '', revision: '', oid: '', eol: '', size: 0, bom: false };
  native.diff.mockResolvedValue({ code: 'ok', message: '', diff: { id: 'diff', repository: dtoRepository, entryId: 'entry', relativePath: 'a', originalPath: '', layer: 'worktree', kind: 'binary', original: side, modified: side, canOpenWorktree: true, message: 'binary' } });
  expect(await sourceControlApi.diff(repository, expected, 'entry', 'worktree')).toMatchObject({ code: 'ok', value: { kind: 'binary' } });
  expect(native.diff).toHaveBeenCalledWith({ repository: dtoRepository, expected, entryId: 'entry', layer: 'worktree' });
});
it('subscribes to the native operation channel and releases exactly that listener', () => {
  const off = vi.fn(); native.events.mockReturnValue(off); const listener = vi.fn();
  const unsubscribe = sourceControlApi.subscribeOperations(listener);
  const event = { operationId: 'op', repository: dtoRepository, sequence: 2, state: 'running', stream: 'stdout', chunk: 'hello' };
  expect(native.events.mock.calls[0][0]).toBe('sourcecontrol:operation'); native.events.mock.calls[0][1](event);
  expect(listener).toHaveBeenCalledWith(event); unsubscribe(); expect(off).toHaveBeenCalledOnce();
});
