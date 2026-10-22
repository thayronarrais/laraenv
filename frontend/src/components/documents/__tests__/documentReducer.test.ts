import { expect, it } from 'vitest';
import { documentReducer, initialDocumentState } from '../documentReducer';
it('increments state version only when state changes and freezes mutations during handoff', () => {
  const initial = initialDocumentState();
  const changed = documentReducer(initial, { type: 'patch', patch: { visible: true } });
  expect(changed.stateVersion).toBeGreaterThan(initial.stateVersion);
  const locked = documentReducer(changed, { type: 'handoff', pending: true });
  expect(documentReducer(locked, { type: 'patch', patch: { visible: false } })).toBe(locked);
  expect(documentReducer(locked, { type: 'handoff', pending: false }).handoffPending).toBe(false);
});
