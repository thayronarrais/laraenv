import { describe, expect, it } from 'vitest';
import { parseLayout, serializeWorkspace } from '../serialize';
import { initialDocumentState } from '../documentReducer';
import { ref, snapshot } from './fixtures';
describe('document references', () => {
  it('serializes references only and preserves unavailable placeholders', () => {
    const state = { ...initialDocumentState(), tabs: [{ id: 'secret-id', references: [ref()], mode: 'preview' as const, bufferId: null, status: 'unavailable' as const }], activeTabId: 'secret-id' };
    const raw = serializeWorkspace(state);
    expect(raw).not.toMatch(/secret-id|bufferId|unavailable|revision|"text"/);
    expect(parseLayout(raw)?.tabs[0].references[0].relativePath).toBe('README.md');
  });
  it('validates schema bounds and handles old closed layouts', () => {
    expect(parseLayout('')).toBeNull();
    const base = JSON.parse(serializeWorkspace(initialDocumentState()));
    expect(base).toMatchObject({ activeTabIndex: -1, visible: false, panelPercent: 45 });
    for (const mutation of [{ panelPercent: 19 }, { panelPercent: 81 }, { activeTabIndex: 0 }, { version: 2 }, { text: 'secret' }]) expect(parseLayout(JSON.stringify({ ...base, ...mutation }))).toBeNull();
    expect(parseLayout(JSON.stringify({ ...base, tabs: [{ references: [ref()], mode: 'preview' }], activeTabIndex: 0 }))).toBeNull(); // mode belongs to tab, not exact reference
  });
  it('never serializes edited contents, saved revisions, view state or pending disk content', () => {
    const state = initialDocumentState(); state.buffers = [{ id: '/p/file', references: [ref()], snapshot: snapshot('/p/file', 'saved secret', 'revision-secret'), text: 'local secret', dirty: true, editGeneration: 8, viewState: { secret: 'view-secret' }, pendingDiskSnapshot: snapshot('/p/file', 'disk-secret'), comparisonVisible: true, status: 'conflict' }];
    state.tabs = [{ id: 'tab', references: [ref()], mode: 'source', bufferId: '/p/file', status: 'ready' }]; state.activeTabId = 'tab';
    expect(serializeWorkspace(state)).not.toMatch(/secret|conflict|editGeneration|canonicalPath|comparisonVisible|viewState/);
  });
  it('enforces the one MiB raw limit and exact nested location keys', () => {
    const { mode, ...location } = ref(); const base = { version: 1, tabs: [{ references: [location], mode }], activeTabIndex: 0, visible: true, panelPercent: 80 };
    expect(parseLayout(JSON.stringify(base))).not.toBeNull();
    for (const invalid of [{ ...location, rootKey: 'A'.repeat(64) }, { ...location, relativePath: '../escape' }, { ...location, relativePath: 'a\\b' }, { ...location, context: { kind: 'project', projectName: 'p', sessionId: '' } }, { ...location, text: 'secret' }]) expect(parseLayout(JSON.stringify({ ...base, tabs: [{ references: [invalid], mode }] }))).toBeNull();
    expect(parseLayout(' '.repeat(1048576) + JSON.stringify(base))).toBeNull();
  });
});
