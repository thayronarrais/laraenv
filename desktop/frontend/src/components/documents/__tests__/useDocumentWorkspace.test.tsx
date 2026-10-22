// @vitest-environment jsdom
import { act } from 'react';
import { createRoot, type Root } from 'react-dom/client';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { useDocumentWorkspace } from '../useDocumentWorkspace';
import type { DocumentController, Result, Snapshot } from '../types';
import { deferred, fakeAPI, ok, ref, snapshot } from './fixtures';
(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;
let root: Root; let host: HTMLDivElement; let current: DocumentController;
let props: {api: ReturnType<typeof fakeAPI>; pageActive: boolean; windowFocused: boolean};
function Harness() { current = useDocumentWorkspace(props); return null; }
const api = () => fakeAPI();
async function mount(a = api(), pageActive = true, windowFocused = true) {
  if (!root) { host = document.createElement('div'); root = createRoot(host); }
  props = { api: a, pageActive, windowFocused };
  await act(async () => { root.render(<Harness />); }); return a;
}
afterEach(() => { act(() => root?.unmount()); root = undefined as any; vi.useRealTimers(); });
const open = async (r = ref()) => { await act(async () => { await current.open(r); }); };
const edit = (text = 'local') => act(() => current.edit(current.buffers[0].id, text));
async function restoreAliases() {
  const a = api();
  const references = ['primary', 'alias'].map(name => { const { mode, ...location } = ref(name); return location; });
  vi.mocked(a.loadLayout).mockResolvedValue(JSON.stringify({ version: 1, tabs: [{ references, mode: 'preview' }], activeTabIndex: 0, visible: true, panelPercent: 45 }));
  await mount(a);
  expect(current.tabs[0].references).toHaveLength(2);
  expect(current.buffers[0].references).toHaveLength(1);
  vi.mocked(a.read).mockImplementation(async location => location.context.kind === 'project' && location.context.projectName === 'primary' ? { code: 'unavailable', message: 'primary gone' } : ok(snapshot()));
  return a;
}
describe('document lifecycle', () => {
  it('reads fresh buffers through a controller captured before a synchronous edit', async () => {
    await mount(); await open(); const captured = current; const id = current.buffers[0].id;
    act(() => { current.edit(id, 'new text'); expect(captured.getBuffer(id)?.text).toBe('new text'); expect(captured.getBuffers()[0].dirty).toBe(true); });
  });
  it('keeps replaced physical roots independent even with identical path and bytes', async () => {
    const a = await mount(); await open(); edit('old root edits');
    const oldId = current.buffers[0].id;
    vi.mocked(a.read).mockImplementation(async r => r.rootKey === ref().rootKey ? { code: 'unavailable', message: 'root replaced' } : ok(snapshot()));
    await open({ ...ref(), rootKey: 'b'.repeat(64) });
    expect(current.buffers).toHaveLength(2); expect(current.tabs).toHaveLength(2);
    const old = current.buffers.find(b => b.id === oldId)!;
    const fresh = current.buffers.find(b => b.id !== oldId)!;
    expect(old).toMatchObject({ text: 'old root edits', dirty: true, status: 'unavailable' });
    expect(fresh).toMatchObject({ text: 'base', dirty: false });
    expect(old.id).not.toBe(old.snapshot.canonicalPath);
    await act(async () => expect(await current.save(old.id)).toBe(false));
    expect(a.save).not.toHaveBeenCalled();
    act(() => current.edit(fresh.id, 'replacement edits'));
    await act(async () => expect(await current.save(fresh.id)).toBe(true));
    expect(a.save).toHaveBeenCalledWith(expect.objectContaining({ rootKey: 'b'.repeat(64) }), 'replacement edits', 'r1');
    expect(current.buffers.find(b => b.id === oldId)?.text).toBe('old root edits');
  });
  it('does not authorize an unvalidated restored replacement root as fallback', async () => {
    const a = api(); const { mode: originalMode, ...original } = ref(); const { mode: aliasMode, ...alias } = ref('alias'); const replacement = { ...alias, rootKey: 'b'.repeat(64) };
    vi.mocked(a.loadLayout).mockResolvedValue(JSON.stringify({ version: 1, tabs: [{ references: [original, replacement], mode: 'preview' }], activeTabIndex: 0, visible: true, panelPercent: 45 }));
    await mount(a); edit();
    vi.mocked(a.read).mockImplementation(async r => r.rootKey === original.rootKey ? { code: 'unavailable', message: 'gone' } : ok(snapshot()));
    await act(async () => expect(await current.save(current.buffers[0].id)).toBe(false));
    expect(current.buffers[0]).toMatchObject({ text: 'local', dirty: true, status: 'unavailable' }); expect(a.save).not.toHaveBeenCalled();
  });

  it.each([true, false])('restored save fallback validates canonical identity (same identity: %s)', async sameIdentity => {
    const a = await restoreAliases(); edit();
    if (!sameIdentity) vi.mocked(a.read).mockImplementation(async location => location.context.kind === 'project' && location.context.projectName === 'primary' ? { code: 'unavailable', message: 'primary gone' } : ok(snapshot('/another/worktree/README.md')));
    await act(async () => expect(await current.save(current.buffers[0].id)).toBe(sameIdentity));
    if (sameIdentity) {
      expect(a.save).toHaveBeenCalledWith(expect.objectContaining({ context: { kind: 'project', projectName: 'alias' } }), 'local', 'r1');
      expect(current.buffers[0]).toMatchObject({ snapshot: { canonicalPath: '/p/README.md' }, text: 'local', dirty: false });
      expect(current.buffers[0].references).toHaveLength(2);
    } else {
      expect(a.save).not.toHaveBeenCalled();
      expect(current.buffers[0]).toMatchObject({ snapshot: { canonicalPath: '/p/README.md' }, text: 'local', dirty: true, status: 'unavailable' });
      expect(current.buffers[0].references).toHaveLength(1);
    }
  });
  it.each([true, false])('restored polling fallback validates canonical identity (same identity: %s)', async sameIdentity => {
    vi.useFakeTimers(); const a = await restoreAliases(); edit();
    vi.mocked(a.check).mockResolvedValue({ code: 'unavailable', message: 'primary gone' });
    vi.mocked(a.read).mockResolvedValue(ok(snapshot(sameIdentity ? '/p/README.md' : '/another/worktree/README.md', 'disk', 'r2')));
    await act(async () => vi.advanceTimersByTime(2000));
    expect(a.read).toHaveBeenLastCalledWith(expect.objectContaining({ context: { kind: 'project', projectName: 'alias' } }));
    expect(current.buffers[0]).toMatchObject({ snapshot: { canonicalPath: '/p/README.md' }, text: 'local', dirty: true, status: sameIdentity ? 'conflict' : 'unavailable' });
    expect(current.buffers[0].pendingDiskSnapshot?.revision ?? null).toBe(sameIdentity ? 'r2' : null);
    expect(current.buffers[0].references).toHaveLength(sameIdentity ? 2 : 1);
  });
  it('restored compare validates an alternate origin without replacing local contents', async () => {
    const a = await restoreAliases(); edit();
    vi.mocked(a.read).mockImplementation(async location => location.context.kind === 'project' && location.context.projectName === 'primary' ? { code: 'unavailable', message: 'primary gone' } : ok(snapshot('/p/README.md', 'disk', 'r2')));
    await act(async () => current.compare(current.buffers[0].id));
    expect(current.buffers[0]).toMatchObject({ text: 'local', status: 'conflict', comparisonVisible: true, pendingDiskSnapshot: { revision: 'r2' } });
    expect(current.buffers[0].references).toHaveLength(2);
  });
  it('coalesces concurrent replacement aliases after their old-authority validation awaits', async () => {
    const a = await mount(); await open(); edit();
    const validation = deferred<Result<Snapshot>>();
    vi.mocked(a.read).mockImplementation(r => r.rootKey === ref().rootKey ? validation.promise : Promise.resolve(ok(snapshot())));
    let first!: Promise<void>, second!: Promise<void>;
    act(() => { first = current.open({ ...ref('new-one'), rootKey: 'b'.repeat(64) }); second = current.open({ ...ref('new-two'), rootKey: 'b'.repeat(64) }); });
    await act(async () => {});
    await act(async () => { validation.resolve({ code: 'unavailable', message: 'old root gone' }); await Promise.all([first, second]); });
    expect(current.buffers).toHaveLength(2); expect(current.tabs).toHaveLength(2);
    expect(current.buffers.find(b => !b.dirty)?.references).toHaveLength(2);
  });
  it('ignores a late failed authority validation after the original buffer was refreshed', async () => {
    const a = await mount(); await open(); const oldId = current.buffers[0].id;
    const validation = deferred<Result<Snapshot>>();
    vi.mocked(a.read).mockImplementation(r => r.rootKey === ref().rootKey ? validation.promise : Promise.resolve(ok(snapshot())));
    let opened!: Promise<void>; act(() => { opened = current.open({ ...ref('different-root'), rootKey: 'b'.repeat(64) }); });
    await act(async () => {});
    vi.mocked(a.read).mockResolvedValue(ok(snapshot('/p/README.md', 'current', 'r9')));
    await act(async () => current.retryTab(current.tabs[0].id));
    await act(async () => { validation.resolve({ code: 'unavailable', message: 'old response' }); await opened; });
    expect(current.buffers.find(b => b.id === oldId)).toMatchObject({ status: 'ready', text: 'current' });
  });
  it('joins cross-root aliases only while original authority is live and never uses them after it disappears', async () => {
    const a = await mount(); await open();
    await open({ ...ref('nested-alias'), rootKey: 'b'.repeat(64) });
    expect(current.buffers).toHaveLength(1); expect(current.buffers[0].references).toHaveLength(2);
    edit(); vi.mocked(a.read).mockImplementation(async r => r.rootKey === ref().rootKey ? { code: 'unavailable', message: 'original gone' } : ok(snapshot()));
    await act(async () => expect(await current.save(current.buffers[0].id)).toBe(false));
    expect(a.save).not.toHaveBeenCalled(); expect(current.buffers[0].text).toBe('local');
  });
  it('keeps its generation after ordinary atomic replacement within the same root', async () => {
    const a = await mount(); await open(); const id = current.buffers[0].id;
    vi.mocked(a.read).mockResolvedValue(ok(snapshot('/p/README.md', 'replaced bytes', 'new-revision')));
    await act(async () => current.retryTab(current.tabs[0].id));
    expect(current.buffers[0]).toMatchObject({ id, text: 'replaced bytes', status: 'ready' });
  });
  it('late same-identity opens attach aliases without rolling back a newer snapshot', async () => {
    const a = await mount(); const pending = deferred<Result<Snapshot>>(); vi.mocked(a.read).mockReturnValueOnce(pending.promise);
    let first!: Promise<void>; act(() => { first = current.open(ref()); });
    vi.mocked(a.read).mockResolvedValue(ok(snapshot('/p/README.md', 'new disk', 'r9'))); await open(ref('alias'));
    await act(async () => { pending.resolve(ok(snapshot('/p/README.md', 'old disk', 'r1'))); await first; });
    expect(current.buffers).toHaveLength(1); expect(current.buffers[0].text).toBe('new disk'); expect(current.tabs[0].references).toHaveLength(2);
  });
  it('an older check cannot replace a newer alias snapshot', async () => {
    vi.useFakeTimers(); const a = await mount(); await open(); const pending = deferred<Result<Snapshot>>(); vi.mocked(a.check).mockReturnValueOnce(pending.promise);
    await act(async () => vi.advanceTimersByTime(2000)); vi.mocked(a.read).mockResolvedValue(ok(snapshot('/p/README.md', 'latest', 'r9'))); await open(ref('alias'));
    await act(async () => pending.resolve({ code: 'changed', value: snapshot('/p/README.md', 'stale', 'r2') })); expect(current.buffers[0].text).toBe('latest');
  });
  it('a late alias read cannot replace a newer conflict snapshot from save', async () => {
    const a = await mount(); await open(); edit(); const pending = deferred<Result<Snapshot>>(); vi.mocked(a.read).mockReturnValueOnce(pending.promise);
    let opening!: Promise<void>; act(() => { opening = current.open(ref('alias')); });
    vi.mocked(a.save).mockResolvedValueOnce({ code: 'conflict', message: 'changed', latest: snapshot('/p/README.md', 'latest', 'r9') });
    await act(async () => current.save(current.buffers[0].id));
    await act(async () => { pending.resolve(ok(snapshot('/p/README.md', 'stale', 'r2'))); await opening; }); expect(current.buffers[0].pendingDiskSnapshot?.revision).toBe('r9');
  });
  it('pauses polling for hidden pane, inactive page and unfocused window; late checks do not mutate', async () => {
    vi.useFakeTimers(); const a = await mount(); await open(); const pending = deferred<Result<Snapshot>>(); vi.mocked(a.check).mockReturnValueOnce(pending.promise);
    await act(async () => vi.advanceTimersByTime(2000)); expect(a.check).toHaveBeenCalledTimes(1);
    act(() => current.setVisible(false)); await act(async () => { pending.resolve({ code: 'changed', value: snapshot('/p/README.md', 'late', 'r2') }); vi.advanceTimersByTime(4000); });
    expect(current.buffers[0].text).toBe('base'); expect(a.check).toHaveBeenCalledTimes(1);
    act(() => { current.setVisible(true); props.pageActive = false; root.render(<Harness />); }); await act(async () => vi.advanceTimersByTime(4000)); expect(a.check).toHaveBeenCalledTimes(1);
    act(() => { props.pageActive = true; props.windowFocused = false; root.render(<Harness />); }); await act(async () => vi.advanceTimersByTime(4000)); expect(a.check).toHaveBeenCalledTimes(1);
    act(() => { props.windowFocused = true; root.render(<Harness />); }); await act(async () => vi.advanceTimersByTime(2000)); expect(a.check).toHaveBeenCalledTimes(2);
  });
  it('new opens rejected by format have no editable buffer, while restored failures retain references', async () => {
    const a = await mount(); vi.mocked(a.read).mockResolvedValue({ code: 'too_large', message: 'large file' }); await open();
    expect(current.buffers).toHaveLength(0); expect(current.tabs).toHaveLength(0); expect(current.failedOpen?.code).toBe('too_large');
  });
  it('a newer edit while a reload read waits remains a dirty conflict', async () => {
    const a = await mount(); await open(); edit(); let reloaded!: Promise<boolean>;
    act(() => { reloaded = current.reload(current.buffers[0].id); }); act(() => current.resolveDecision('discard'));
    const pending = deferred<Result<Snapshot>>(); vi.mocked(a.read).mockReturnValueOnce(pending.promise); await act(async () => {}); edit('newest');
    await act(async () => { pending.resolve(ok(snapshot('/p/README.md', 'disk', 'r2'))); expect(await reloaded).toBe(false); }); expect(current.buffers[0]).toMatchObject({ text: 'newest', dirty: true, status: 'conflict' });
  });
  it('deduplicates only canonical identity and retries validated aliases', async () => {
    const a = await mount(); await open(); await open(ref('alias'));
    expect(current.buffers).toHaveLength(1); expect(current.tabs).toHaveLength(1); expect(current.buffers[0].references).toHaveLength(2);
    edit(); vi.mocked(a.read).mockImplementation(async r => r.context.kind === 'project' && r.context.projectName === 'p' ? { code: 'unavailable', message: 'gone' } : ok(snapshot()));
    await act(async () => current.retryTab(current.tabs[0].id)); expect(current.buffers[0].text).toBe('local');
    vi.mocked(a.read).mockResolvedValue(ok(snapshot('/other/README.md')));
    await act(async () => current.retryTab(current.tabs[0].id)); expect(current.buffers).toHaveLength(1); expect(current.buffers[0].status).toBe('unavailable');
    await open(ref('worktree')); expect(current.buffers).toHaveLength(2);
  });
  it('ignores stale opens after close', async () => {
    const a = await mount(); const pending = deferred<Result<Snapshot>>(); vi.mocked(a.read).mockReturnValueOnce(pending.promise);
    let opened!: Promise<void>; act(() => { opened = current.open(ref()); }); const id = current.tabs[0].id;
    await act(async () => { await current.closeTab(id); pending.resolve(ok(snapshot())); await opened; });
    expect(current.tabs).toHaveLength(0); expect(current.buffers).toHaveLength(0);
  });
  it('ignores an initial loading read superseded by manual retry', async () => {
    const a = await mount(); const pending = deferred<Result<Snapshot>>(); vi.mocked(a.read).mockReturnValueOnce(pending.promise);
    let first!: Promise<void>; act(() => { first = current.open(ref()); });
    vi.mocked(a.read).mockResolvedValueOnce(ok(snapshot('/p/README.md', 'retry', 'r9'))); await act(async () => current.retryTab(current.tabs[0].id));
    await act(async () => { pending.resolve(ok(snapshot('/p/README.md', 'old', 'r1'))); await first; }); expect(current.buffers[0].text).toBe('retry');
  });
  it('save captures a generation and preserves later edits', async () => {
    const a = await mount(); await open(); edit('first'); const pending = deferred<Result<Snapshot>>(); vi.mocked(a.save).mockReturnValueOnce(pending.promise);
    let saved!: Promise<boolean>; act(() => { saved = current.save(current.buffers[0].id); }); edit('second');
    await act(async () => { pending.resolve(ok(snapshot('/p/README.md', 'first', 'r2'))); await saved; });
    expect(current.buffers[0]).toMatchObject({ text: 'second', dirty: true, snapshot: { revision: 'r2' } });
  });
  it('a retry cannot invalidate the successful revision of an in-flight save', async () => {
    const a = await mount(); await open(); edit('write me'); const pending = deferred<Result<Snapshot>>(); vi.mocked(a.save).mockReturnValueOnce(pending.promise);
    let saved!: Promise<boolean>; act(() => { saved = current.save(current.buffers[0].id); }); await act(async () => {});
    await act(async () => current.retryTab(current.tabs[0].id));
    await act(async () => { pending.resolve(ok(snapshot('/p/README.md', 'write me', 'r2'))); expect(await saved).toBe(true); });
    expect(current.buffers[0]).toMatchObject({ snapshot: { revision: 'r2' }, dirty: false });
  });
  it('an alias read started during save cannot roll back the committed revision', async () => {
    const a = await mount(); await open(); edit('saved'); const write = deferred<Result<Snapshot>>(); vi.mocked(a.save).mockReturnValueOnce(write.promise);
    let saved!: Promise<boolean>; act(() => { saved = current.save(current.buffers[0].id); }); await act(async () => {});
    const read = deferred<Result<Snapshot>>(); vi.mocked(a.read).mockReturnValueOnce(read.promise); let opened!: Promise<void>; act(() => { opened = current.open(ref('alias')); });
    await act(async () => { write.resolve(ok(snapshot('/p/README.md', 'saved', 'r2'))); await saved; });
    await act(async () => { read.resolve(ok(snapshot('/p/README.md', 'base', 'r1'))); await opened; });
    expect(current.buffers[0]).toMatchObject({ text: 'saved', snapshot: { revision: 'r2' }, dirty: false }); expect(current.tabs[0].references).toHaveLength(2);
  });
  it('validates fallback identity before save and retains dirty buffer after deleted root', async () => {
    const a = await mount(); await open(); await open(ref('alias')); edit();
    vi.mocked(a.read).mockImplementation(async r => r.context.kind === 'project' && r.context.projectName === 'p' ? { code: 'unavailable', message: 'root replaced' } : ok(snapshot('/different/file')));
    await act(async () => expect(await current.save(current.buffers[0].id)).toBe(false)); expect(a.save).not.toHaveBeenCalled(); expect(current.buffers[0].text).toBe('local');
    vi.mocked(a.read).mockImplementation(async r => r.context.kind === 'project' && r.context.projectName === 'p' ? { code: 'unavailable', message: 'gone' } : ok(snapshot()));
    await act(async () => expect(await current.save(current.buffers[0].id)).toBe(true)); expect(a.save).toHaveBeenCalledWith(expect.objectContaining({ context: { kind: 'project', projectName: 'alias' } }), 'local', 'r1');
  });
  it('another disk change during explicit merge becomes the next conflict without discarding local edits', async () => {
    const a = await mount(); await open(); edit(); vi.mocked(a.read).mockResolvedValue(ok(snapshot('/p/README.md', 'disk', 'r2')));
    await act(async () => current.compare(current.buffers[0].id)); vi.mocked(a.save).mockResolvedValue({ code: 'conflict', message: 'new disk', latest: snapshot('/p/README.md', 'new disk', 'r3') });
    await act(async () => expect(await current.saveMerged(current.buffers[0].id)).toBe(false)); expect(current.buffers[0]).toMatchObject({ text: 'local', dirty: true, pendingDiskSnapshot: { revision: 'r3' } });
  });
  it('preserves newer edits as a conflict after an explicit merge save', async () => {
    const a = await mount(); await open(); edit(); vi.mocked(a.read).mockResolvedValue(ok(snapshot('/p/README.md', 'disk', 'r2'))); await act(async () => current.compare(current.buffers[0].id));
    const pending = deferred<Result<Snapshot>>(); vi.mocked(a.save).mockReturnValue(pending.promise); let saved!: Promise<boolean>;
    act(() => { saved = current.saveMerged(current.buffers[0].id); }); await act(async () => {}); edit('newer');
    await act(async () => { pending.resolve(ok(snapshot('/p/README.md', 'local', 'r3'))); await saved; });
    expect(current.buffers[0]).toMatchObject({ text: 'newer', dirty: true, status: 'conflict', snapshot: { revision: 'r3' }, pendingDiskSnapshot: { revision: 'r3' } });
  });
  it('reloads clean buffers preserving view and mode, but preserves dirty conflicts', async () => {
    const a = await mount(); await open(); act(() => current.setViewState(current.buffers[0].id, { scroll: 42 }));
    vi.mocked(a.read).mockResolvedValue(ok(snapshot('/p/README.md', 'disk', 'r2')));
    await act(async () => current.retryTab(current.tabs[0].id)); expect(current.buffers[0]).toMatchObject({ text: 'disk', viewState: { scroll: 42 } }); expect(current.tabs[0].mode).toBe('preview');
    edit(); vi.mocked(a.read).mockResolvedValue(ok(snapshot('/p/README.md', 'new disk', 'r3')));
    await act(async () => current.compare(current.buffers[0].id)); expect(current.buffers[0]).toMatchObject({ text: 'local', status: 'conflict', pendingDiskSnapshot: { revision: 'r3' }, comparisonVisible: true });
    await act(async () => current.save(current.buffers[0].id)); expect(a.save).toHaveBeenLastCalledWith(expect.anything(), 'local', 'r2');
  });
  it('explicit merge saves use the current pending disk revision', async () => {
    const a = await mount(); await open(); edit(); vi.mocked(a.read).mockResolvedValue(ok(snapshot('/p/README.md', 'disk', 'r8')));
    await act(async () => current.compare(current.buffers[0].id)); await act(async () => current.saveMerged(current.buffers[0].id));
    expect(a.save).toHaveBeenLastCalledWith(expect.anything(), 'local', 'r8');
  });
  it('close supports cancel discard and save; save all rechecks edits after awaiting', async () => {
    const a = await mount(); await open(); edit(); let closed!: Promise<boolean>;
    act(() => { closed = current.closeTab(current.tabs[0].id); }); act(() => current.resolveDecision('cancel')); await act(async () => expect(await closed).toBe(false));
    const pending = deferred<Result<Snapshot>>(); vi.mocked(a.save).mockReturnValueOnce(pending.promise);
    let all!: Promise<boolean>; act(() => { all = current.confirmCloseAll(); }); act(() => current.resolveDecision('save'));
    await act(async () => {}); edit('newer'); await act(async () => pending.resolve(ok(snapshot('/p/README.md', 'local', 'r2'))));
    expect(current.decision?.scope).toBe('all'); expect(current.beginShutdownHandoff()).toBeNull();
    act(() => current.resolveDecision('discard')); await act(async () => expect(await all).toBe(true));
    act(() => { expect(current.beginShutdownHandoff()).not.toBeNull(); }); edit('blocked'); expect(current.dirtyCount).toBe(0);
    act(() => current.endShutdownHandoff()); edit(); act(() => { closed = current.closeTab(current.tabs[0].id); }); act(() => current.resolveDecision('save')); await act(async () => expect(await closed).toBe(true)); expect(current.tabs).toHaveLength(0);
  });
  it('failed save all declines shutdown and keeps buffers', async () => {
    const a = await mount(); await open(); edit(); vi.mocked(a.save).mockResolvedValue({ code: 'permission_denied', message: 'no' });
    let all!: Promise<boolean>; act(() => { all = current.confirmCloseAll(); }); act(() => current.resolveDecision('save')); await act(async () => expect(await all).toBe(false)); expect(current.dirtyCount).toBe(1);
  });
  it('restores unavailable historical references and persists only after hydration', async () => {
    vi.useFakeTimers(); const a = api(); const pending = deferred<string>(); vi.mocked(a.loadLayout).mockReturnValue(pending.promise); await mount(a);
    await act(async () => vi.advanceTimersByTime(1000)); expect(a.saveLayout).not.toHaveBeenCalled();
    const { mode, ...location } = ref('historical'); vi.mocked(a.read).mockResolvedValue({ code: 'unavailable', message: 'gone' });
    await act(async () => pending.resolve(JSON.stringify({ version: 1, tabs: [{ references: [location], mode }], activeTabIndex: 0, visible: true, panelPercent: 40 })));
    expect(current.tabs[0]).toMatchObject({ bufferId: null, status: 'unavailable', references: [location] });
    await act(async () => vi.advanceTimersByTime(500)); expect(JSON.parse(vi.mocked(a.saveLayout).mock.calls.at(-1)![0]).tabs[0].references).toEqual([location]);
  });
  it('polls at 2 seconds, bounds checks to four, and ignores stale check after newer retry', async () => {
    vi.useFakeTimers(); const a = await mount(); vi.mocked(a.read).mockImplementation(async r => ok(snapshot('/p/' + r.relativePath)));
    for (let n = 0; n < 6; n++) await open(ref('p', `${n}.md`));
    const pending = deferred<Result<Snapshot>>(); vi.mocked(a.check).mockReturnValue(pending.promise);
    await act(async () => vi.advanceTimersByTime(1999)); expect(a.check).not.toHaveBeenCalled();
    await act(async () => vi.advanceTimersByTime(1)); expect(a.check).toHaveBeenCalledTimes(4);
    await act(async () => vi.advanceTimersByTime(6000)); expect(a.check).toHaveBeenCalledTimes(4);
    vi.mocked(a.read).mockResolvedValue(ok(snapshot('/p/0.md', 'newer', 'r9'))); await act(async () => current.retryTab(current.tabs[0].id));
    await act(async () => pending.resolve({ code: 'changed', value: snapshot('/p/0.md', 'old', 'r2') })); expect(current.buffers[0].text).toBe('newer'); expect(a.check).toHaveBeenCalledTimes(6);
  });
  it('polling validates the alternate origin and recovers availability without losing local text', async () => {
    vi.useFakeTimers(); const a = await mount(); await open(); await open(ref('alias')); edit();
    vi.mocked(a.check).mockResolvedValue({ code: 'unavailable', message: 'deleted' });
    vi.mocked(a.read).mockResolvedValue(ok(snapshot('/wrong/file', 'wrong')));
    await act(async () => vi.advanceTimersByTime(2000)); expect(current.buffers[0]).toMatchObject({ status: 'unavailable', text: 'local' });
    vi.mocked(a.check).mockResolvedValue({ code: 'unchanged' }); await act(async () => vi.advanceTimersByTime(2000)); expect(current.buffers[0].status).toBe('ready');
    vi.mocked(a.check).mockResolvedValue({ code: 'unavailable', message: 'deleted' }); vi.mocked(a.read).mockResolvedValue(ok(snapshot('/p/README.md', 'remote', 'r5')));
    await act(async () => vi.advanceTimersByTime(2000)); expect(current.buffers[0]).toMatchObject({ text: 'local', status: 'conflict', pendingDiskSnapshot: { revision: 'r5' } });
  });
  it('serializes layout writes so an old pending save cannot overwrite newer references', async () => {
    vi.useFakeTimers(); const a = await mount(); const pending = deferred<void>(); vi.mocked(a.saveLayout).mockReturnValueOnce(pending.promise);
    await open(); await act(async () => vi.advanceTimersByTime(300)); expect(a.saveLayout).toHaveBeenCalledTimes(1);
    act(() => current.setPanelPercent(60)); await act(async () => vi.advanceTimersByTime(300)); expect(a.saveLayout).toHaveBeenCalledTimes(1);
    await act(async () => pending.resolve()); expect(a.saveLayout).toHaveBeenCalledTimes(2); expect(JSON.parse(vi.mocked(a.saveLayout).mock.calls[1][0]).panelPercent).toBe(60);
  });
});
