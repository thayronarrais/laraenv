// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
const bridge = vi.hoisted(() => ({ listener: null as null | ((request: {requestId: string}) => void), order: [] as string[], report: vi.fn(), reply: vi.fn() }));
vi.mock('../../../../wailsjs/runtime', () => ({ EventsOn: (_name: string, fn: typeof bridge.listener) => { bridge.order.push('subscribe'); bridge.listener = fn; return () => { bridge.listener = null; }; } }));
vi.mock('../../../../wailsjs/go/main/App', () => ({ SetDocumentWorkspaceState: (...args: unknown[]) => bridge.report(...args), ResolveDocumentShutdown: (...args: unknown[]) => bridge.reply(...args) }));
import { useDocumentWorkspace } from '../useDocumentWorkspace';
import { useDocumentShutdown } from '../useDocumentShutdown';
import type { DocumentController } from '../types';
import { deferred, fakeAPI, ref, snapshot, ok } from './fixtures';
import type { Result, Snapshot } from '../types';
(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;
let root: Root; let current: DocumentController;
async function mount() { const api = fakeAPI(); function Host() { current = useDocumentWorkspace({ api, pageActive: true, windowFocused: true }); useDocumentShutdown(current); return null; } root = createRoot(document.createElement('div')); await act(async () => root.render(<Host />)); await act(async () => current.open(ref())); return api; }
beforeEach(() => { bridge.order.length = 0; bridge.report.mockImplementation(async () => { bridge.order.push('report'); }); bridge.reply.mockResolvedValue(undefined); });
afterEach(() => { act(() => root?.unmount()); vi.clearAllMocks(); vi.useRealTimers(); });
it('subscribes before ready, ignores repeated requests, and freezes after accepted ack until teardown', async () => {
  await mount(); expect(bridge.order[0]).toBe('subscribe'); const ack = deferred<void>(); bridge.reply.mockReturnValueOnce(ack.promise);
  await act(async () => { bridge.listener?.({ requestId: 'one' }); bridge.listener?.({ requestId: 'one' }); });
  expect(current.handoffPending).toBe(true); expect(bridge.reply).toHaveBeenCalledTimes(1);
  act(() => current.edit(current.buffers[0].id, 'blocked')); expect(current.dirtyCount).toBe(0);
  await act(async () => ack.resolve()); act(() => current.edit(current.buffers[0].id, 'still blocked')); expect(current.dirtyCount).toBe(0); expect(current.handoffPending).toBe(true);
});
it('report or ack errors unlock and never auto authorize a retry', async () => {
  await mount(); bridge.report.mockRejectedValueOnce(new Error('offline'));
  await act(async () => bridge.listener?.({ requestId: 'one' })); expect(current.handoffPending).toBe(false); expect(bridge.reply.mock.calls.filter(([r]) => r.decision === 'allow')).toHaveLength(0);
});
it('retains a one-shot pending id after stale cancel, reports current version and retries cancel', async () => {
  vi.useFakeTimers(); await mount(); act(() => current.edit(current.buffers[0].id, 'dirty'));
  await act(async () => bridge.listener?.({ requestId: 'one' }));
  const stale = deferred<void>(); bridge.reply.mockReturnValueOnce(stale.promise);
  await act(async () => current.resolveDecision('cancel'));
  const oldVersion = bridge.reply.mock.calls[0][0].stateVersion;
  act(() => current.edit(current.buffers[0].id, 'racing edit'));
  await act(async () => { stale.reject(new Error('stale version')); });
  await act(async () => vi.advanceTimersByTime(1000));
  const cancels = bridge.reply.mock.calls.map(([r]) => r).filter(r => r.decision === 'cancel');
  expect(cancels.length).toBeGreaterThanOrEqual(2); expect(cancels[1]).toMatchObject({ requestId: 'one', decision: 'cancel' }); expect(cancels[1].stateVersion).toBeGreaterThan(oldVersion);
  expect(bridge.report.mock.calls.some(([s]) => s.stateVersion === cancels[1].stateVersion)).toBe(true);
  await act(async () => bridge.listener?.({ requestId: 'two' })); expect(current.decision?.scope).toBe('all');
});
it('save all never sends allow while a later edit remains dirty', async () => {
  const api = await mount(); act(() => current.edit(current.buffers[0].id, 'first')); await act(async () => bridge.listener?.({ requestId: 'one' }));
  const write = deferred<Result<Snapshot>>(); vi.mocked(api.save).mockReturnValueOnce(write.promise); await act(async () => current.resolveDecision('save'));
  act(() => current.edit(current.buffers[0].id, 'second')); await act(async () => write.resolve(ok(snapshot('/p/README.md', 'first', 'r2'))));
  expect(current.dirtyCount).toBe(1); expect(current.decision?.scope).toBe('all'); expect(bridge.reply.mock.calls.filter(([r]) => r.decision === 'allow')).toHaveLength(0);
  await act(async () => current.resolveDecision('cancel')); expect(bridge.reply).toHaveBeenLastCalledWith(expect.objectContaining({ requestId: 'one', decision: 'cancel' }));
});
it('lost acknowledgement unlocks and allows a fresh server request without replaying old allow', async () => {
  vi.useFakeTimers(); await mount(); bridge.reply.mockRejectedValueOnce(new Error('apply failed')).mockRejectedValueOnce(new Error('no pending request'));
  await act(async () => bridge.listener?.({ requestId: 'one' })); expect(current.handoffPending).toBe(false);
  act(() => current.edit(current.buffers[0].id, 'editable again')); await act(async () => bridge.listener?.({ requestId: 'two' })); expect(current.decision?.scope).toBe('all');
  await act(async () => current.resolveDecision('cancel')); await act(async () => bridge.listener?.({ requestId: 'one' }));
  expect(bridge.reply.mock.calls.filter(([r]) => r.decision === 'allow')).toHaveLength(1);
});
it('reports changed states with strictly increasing versions', async () => {
  await mount(); act(() => current.edit(current.buffers[0].id, 'dirty')); await act(async () => {});
  act(() => current.edit(current.buffers[0].id, 'base')); await act(async () => {});
  const states = bridge.report.mock.calls.map(([s]) => s);
  for (let n = 1; n < states.length; n++) { const previous = states[n - 1], next = states[n]; expect(next.stateVersion).toBeGreaterThanOrEqual(previous.stateVersion); if (next.dirtyCount !== previous.dirtyCount || next.ready !== previous.ready) expect(next.stateVersion).toBeGreaterThan(previous.stateVersion); }
});
it.each(['undo', 'discard'] as const)('does not acknowledge temporary cleanliness from %s during an outstanding save', async action => {
  const api = await mount(); act(() => current.edit(current.buffers[0].id, 'A'));
  const pending = deferred<Result<Snapshot>>(); vi.mocked(api.save).mockReturnValueOnce(pending.promise);
  let saved!: Promise<boolean>; act(() => { saved = current.save(current.buffers[0].id); }); await act(async () => {});
  if (action === 'undo') act(() => current.edit(current.buffers[0].id, 'base'));
  await act(async () => bridge.listener?.({ requestId: 'one' }));
  if (action === 'discard') await act(async () => current.resolveDecision('discard'));
  expect(current.dirtyCount).toBe(0); expect(current.handoffPending).toBe(false);
  expect(bridge.reply.mock.calls.filter(([r]) => r.decision === 'allow')).toHaveLength(0);
  expect(bridge.reply).toHaveBeenLastCalledWith(expect.objectContaining({ requestId: 'one', decision: 'cancel' }));
  await act(async () => { pending.resolve(ok(snapshot('/p/README.md', 'A', 'r2'))); expect(await saved).toBe(true); });
  expect(current.buffers[0]).toMatchObject({ text: 'base', dirty: true, snapshot: { text: 'A' } });
});

it('released continuation failure stops retries and unlocks edits', async () => {
  vi.useFakeTimers(); await mount(); bridge.reply.mockResolvedValueOnce({ released: true });
  await act(async () => bridge.listener?.({ requestId: 'released' }));
  expect(current.handoffPending).toBe(false);
  act(() => current.edit(current.buffers[0].id, 'editable'));
  await act(async () => {}); const replies = bridge.reply.mock.calls.length; const reports = bridge.report.mock.calls.length;
  await act(async () => vi.advanceTimersByTime(10000));
  expect(current.buffers[0].text).toBe('editable'); expect(bridge.reply).toHaveBeenCalledTimes(replies); expect(bridge.report).toHaveBeenCalledTimes(reports);
});
