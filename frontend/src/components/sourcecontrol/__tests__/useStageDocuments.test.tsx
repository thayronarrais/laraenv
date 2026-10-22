// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, renderHook, screen } from '@testing-library/react';
import { afterEach, expect, it, vi } from 'vitest';
import { useStageDocuments } from '../useStageDocuments';
import { useSourceControl } from '../useSourceControl';
import { useDocumentWorkspace } from '../../documents/useDocumentWorkspace';
import { StageDirtyDialog } from '../StageDirtyDialog';
import { deferred, fakeAPI, ok, ref } from '../../documents/__tests__/fixtures';
import { entry, snapshot } from './uiFixtures';
import type { SourceControlAPI } from '../api';
vi.mock('../../../lib/i18n', () => ({ useT: () => (key: string) => key }));
afterEach(cleanup);
it('keeps the captured decision outside the changed checkout panel and conserves cancel text', async () => {
  let current: any;
  function Harness({ context }: { context: string }) {
    const documents = useDocumentWorkspace({ api, pageActive: false, windowFocused: true });
    const stage = useStageDocuments(documents); current = { documents, stage };
    return <><p>{context}</p><StageDirtyDialog decision={stage.decision} onChoose={stage.choose} /></>;
  }
  const api = fakeAPI(); const view = render(<Harness context="original" />); await act(async () => {});
  await act(async () => current.documents.open(ref('p', 'src/one.ts'))); act(() => current.documents.edit(current.documents.buffers[0].id, 'local'));
  let pending: any; act(() => { pending = current.stage.beforeStage(snapshot(), [entry()]); });
  view.rerender(<Harness context="replacement checkout" />);
  expect(screen.getByRole('dialog').textContent).toContain('Checkout P'); expect(screen.getByRole('dialog').textContent).toContain('/p');
  await act(async () => { fireEvent.click(screen.getByRole('button', { name: 'git.choice.cancel' })); expect(await pending).toBeNull(); });
  expect(current.documents.getBuffers()[0].text).toBe('local'); expect(api.save).not.toHaveBeenCalled();
});
it('blocks the native stage when a new edit arrives during the post-save Git refresh', async () => {
  const documentAPI = fakeAPI(); const status = deferred<any>(); const s = snapshot();
  const api: SourceControlAPI = { resolve: async () => ok(s), status: vi.fn(() => status.promise), diff: vi.fn(), stage: vi.fn(), unstage: vi.fn(), commit: vi.fn(), push: vi.fn(), operation: vi.fn(), cancel: vi.fn(), subscribeOperations: () => () => {} };
  const hook = renderHook(() => {
    const documents = useDocumentWorkspace({ api: documentAPI, pageActive: false, windowFocused: true }); const stage = useStageDocuments(documents);
    const git = useSourceControl({ context: s.repository.context, active: true, windowFocused: true, api, beforeStage: stage.beforeStage });
    return { documents, stage, git };
  });
  await act(async () => {}); await act(async () => hook.result.current.documents.open(ref('p', 'src/one.ts')));
  const id = hook.result.current.documents.buffers[0].id; act(() => hook.result.current.documents.edit(id, 'saved'));
  let pending!: Promise<void>; act(() => { pending = hook.result.current.git.stage(['one']); });
  await act(async () => hook.result.current.stage.choose('save-and-stage')); expect(api.status).toHaveBeenCalledOnce();
  act(() => hook.result.current.documents.edit(id, 'after save'));
  await act(async () => { status.resolve(ok(s)); await pending; });
  expect(api.stage).not.toHaveBeenCalled(); expect(hook.result.current.documents.getBuffer(id)).toMatchObject({ text: 'after save', dirty: true });
  expect(hook.result.current.git.error?.code).toBe('stale_snapshot');
});
