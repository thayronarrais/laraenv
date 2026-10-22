import { expect, it, vi } from 'vitest';
import { GitReviewOperations } from '../gitReviewOperations';
import { deferred, ok } from '../../documents/__tests__/fixtures';
import { diff, snapshot } from './uiFixtures';
it('keeps layers and snapshots independent and ignores a closed review response', async () => {
  const pending = deferred<any>(); const reviews = new GitReviewOperations({ diff: () => pending.promise }); const s = snapshot();
  const first = reviews.open(s.repository, s, 'one', 'index', s.root); const id = reviews.getSnapshot().tabs[0].id;
  const second = reviews.open(s.repository, s, 'one', 'worktree', s.root); expect(reviews.getSnapshot().tabs).toHaveLength(2);
  reviews.close(id); pending.resolve(ok(diff())); await Promise.all([first, second]);
  expect(reviews.getSnapshot().tabs).toHaveLength(1); expect(reviews.getSnapshot().tabs[0]).toMatchObject({ layer: 'worktree', status: 'ready' });
  reviews.clearActive(); expect(reviews.getSnapshot().activeTabId).toBeNull(); expect(reviews.getSnapshot().tabs).toHaveLength(1);
});
it.each(['tracked-M', 'untracked'])('reads fresh bytes for explicit %s review with unchanged status, reusing only identical comparisons', async entryId => {
  const s = snapshot(); const initial = diff();
  const fresh = { ...diff(), id: 'new-content', modified: { ...diff().modified, text: 'new disk bytes', revision: 'r3' } };
  const api = { diff: vi.fn().mockResolvedValueOnce(ok(initial)).mockResolvedValueOnce(ok(fresh)).mockResolvedValueOnce(ok(fresh)) };
  const reviews = new GitReviewOperations(api);
  await reviews.open(s.repository, s, entryId, 'worktree', s.root);
  const old = reviews.getSnapshot().tabs[0];
  await reviews.open(s.repository, s, entryId, 'worktree', s.root);
  expect(reviews.getSnapshot().tabs).toHaveLength(2);
  expect(reviews.getSnapshot().tabs[1].diff?.modified.text).toBe('new disk bytes');
  expect(old.diff?.modified.text).toBe('after');
  const selected = reviews.getSnapshot().activeTabId;
  await reviews.open(s.repository, s, entryId, 'worktree', s.root);
  expect(api.diff).toHaveBeenCalledTimes(3);
  expect(reviews.getSnapshot().tabs).toHaveLength(2); expect(reviews.getSnapshot().activeTabId).toBe(selected);
});
it('captures origin before await and a late A reply does not select over B', async () => {
  const pending = deferred<any>(); const a = snapshot(); const b = snapshot();
  b.repository = { ...b.repository, repositoryKey: 'B', rootKey: 'B' }; b.root = { ...b.root, label: 'Checkout B', path: '/B' };
  const reviews = new GitReviewOperations({ diff: vi.fn().mockReturnValueOnce(pending.promise).mockResolvedValue(ok({ ...diff(), repository: b.repository })) });
  const request = reviews.open(a.repository, a, 'one', 'worktree', a.root);
  a.root.label = 'mutated'; a.root.path = '/mutated';
  await reviews.open(b.repository, b, 'one', 'worktree', b.root); const selected = reviews.getSnapshot().activeTabId;
  pending.resolve(ok(diff())); await request;
  expect(reviews.getSnapshot().tabs[0]).toMatchObject({ root: { label: 'Checkout P', path: '/p' } });
  expect(reviews.getSnapshot().activeTabId).toBe(selected);
});
