import { expect, it, vi } from 'vitest';
import { StageDocuments } from '../stageDocuments';
import { DocumentOperations } from '../../documents/documentOperations';
import { fakeAPI, ref, deferred, ok, snapshot as disk } from '../../documents/__tests__/fixtures';
import { entry, snapshot } from './uiFixtures';
async function setup() {
  const api = fakeAPI(); vi.mocked(api.read).mockImplementation(async r => ok(disk('/p/' + r.relativePath))); vi.mocked(api.save).mockImplementation(async (r, text) => ok(disk('/p/' + r.relativePath, text, 'r2'))); const documents = new DocumentOperations(api); await documents.hydrate();
  await documents.open(ref('p', 'src/one.ts')); documents.edit(documents.state.buffers[0].id, 'local');
  return { api, documents, stage: new StageDocuments(documents), id: documents.state.buffers[0].id };
}
it('save true with later edit cannot stage and conserves the later text', async () => {
  const { api, documents, stage, id } = await setup(); const saving = deferred<any>(); vi.mocked(api.save).mockReturnValue(saving.promise);
  const pending = stage.beforeStage(snapshot(), [entry()]); stage.choose('save-and-stage'); await Promise.resolve(); await Promise.resolve();
  documents.edit(id, 'later'); saving.resolve(ok(disk('/p/src/one.ts', 'local', 'r2')));
  expect(await pending).toBeNull(); expect(documents.state.buffers[0]).toMatchObject({ text: 'later', dirty: true });
});
it('edit during post-save refresh invalidates a previously valid permit', async () => {
  const { documents, stage, id } = await setup(); const pending = stage.beforeStage(snapshot(), [entry()]); stage.choose('save-and-stage');
  const permit = await pending; expect(permit?.validate()).toBe(true); documents.edit(id, 'during refresh'); expect(permit?.validate()).toBe(false);
});
it('stage replacement root does not adopt old buffer at the same canonical path', async () => {
  const { documents, stage, api } = await setup(); const replacement = snapshot(); replacement.repository.rootKey = 'b'.repeat(64);
  const permit = await stage.beforeStage(replacement, [entry()]); expect(stage.getDecision()).toBeNull(); expect(permit?.validate()).toBe(true);
  expect(api.save).not.toHaveBeenCalled(); expect(documents.state.buffers[0].dirty).toBe(true);
});
it('rename checks both dirty endpoints and saves only affected buffers', async () => {
  const { documents, stage, api } = await setup(); await documents.open(ref('p', 'old.ts')); documents.edit(documents.state.activeBufferId!, 'old');
  await documents.open(ref('p', 'unrelated.ts')); documents.edit(documents.state.activeBufferId!, 'unrelated');
  const pending = stage.beforeStage(snapshot(), [{ ...entry(), originalPath: 'old.ts' }]);
  expect(stage.getDecision()).toMatchObject({ label: 'Checkout P', path: '/p', names: ['src/one.ts', 'old.ts'] });
  stage.choose('save-and-stage'); expect((await pending)?.validate()).toBe(true); expect(api.save).toHaveBeenCalledTimes(2);
  expect(documents.state.buffers.find(b => b.references[0].relativePath === 'unrelated.ts')?.dirty).toBe(true);
});
it.each(['conflict', 'io_error'] as const)('failed %s save refuses stage', async code => {
  const { api, documents, stage } = await setup(); vi.mocked(api.save).mockResolvedValue({ code, message: 'failed' });
  const pending = stage.beforeStage(snapshot(), [entry()]); stage.choose('save-and-stage'); expect(await pending).toBeNull(); expect(documents.state.buffers[0].text).toBe('local');
});
it('stage-disk deliberately allows unsaved edits; cancel refuses stage', async () => {
  const { documents, stage, id, api } = await setup(); const pending = stage.beforeStage(snapshot(), [entry()]); stage.choose('stage-disk');
  const permit = await pending; documents.edit(id, 'still local'); expect(permit?.validate()).toBe(true); expect(api.save).not.toHaveBeenCalled();
  const cancelled = stage.beforeStage(snapshot(), [entry()]); stage.choose('cancel'); expect(await cancelled).toBeNull();
});
