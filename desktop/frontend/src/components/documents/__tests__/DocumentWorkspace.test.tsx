// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, expect, it, vi } from 'vitest';
import { WorkspaceFilesPanel } from '../WorkspaceFilesPanel';
import { DocumentWorkspace } from '../DocumentWorkspace';
import { diff as gitDiff, snapshot as gitSnapshot } from '../../sourcecontrol/__tests__/uiFixtures';
import { disposeDocumentModel } from '../DocumentEditor';
import { deferred, fakeAPI, ref, snapshot } from './fixtures';
import { useDocumentWorkspace } from '../useDocumentWorkspace';
import { useGitReviews, type GitReviewController } from '../../sourcecontrol/useGitReviews';
import type { SourceControlAPI } from '../../sourcecontrol/api';
import type { DocumentController } from '../types';
vi.mock('../DocumentEditor', () => ({ DocumentEditor: ({ buffer }: any) => <span data-testid="editor-text">{buffer.text}</span>, disposeDocumentModel: vi.fn() }));
vi.mock('../MarkdownPreview', () => ({ MarkdownPreview: ({ text }: any) => <span data-testid="preview-text">{text}</span> }));
const listProjects = vi.fn(async (): Promise<{ name: string }[]> => []);
vi.mock('../../../../wailsjs/go/main/App', () => ({ ListProjects: (...args: unknown[]) => listProjects(...args) }));
const openExternal = vi.fn(async () => ({ code: 'ok' }));
const contextApi = vi.fn(async () => ({ code: 'ok', value: { rootKey: location.rootKey, canonicalRoot: '/p', label: 'P', path: '/p', canRead: true, canWrite: true } }));
const listApi = vi.fn(async () => ({ code: 'ok', value: { entries: [{ name: 'docs', relativePath: 'docs', kind: 'directory' }], nextCursor: '' } }));
vi.mock('../api', () => ({ workspaceDocumentApi: { openExternal: (...args: unknown[]) => openExternal(...args), context: (...args: unknown[]) => contextApi(...args), list: (...args: unknown[]) => listApi(...args) } }));
vi.mock('../../../lib/i18n', () => ({ useT: () => (key: string) => key }));
const location = ref('p', 'README.md');
afterEach(() => {
  cleanup(); openExternal.mockReset(); openExternal.mockResolvedValue({ code: 'ok' });
  listProjects.mockReset(); listProjects.mockResolvedValue([]);
  contextApi.mockReset(); contextApi.mockImplementation(async () => ({ code: 'ok', value: { rootKey: location.rootKey, canonicalRoot: '/p', label: 'P', path: '/p', canRead: true, canWrite: true } }));
  listApi.mockReset(); listApi.mockImplementation(async () => ({ code: 'ok', value: { entries: [{ name: 'docs', relativePath: 'docs', kind: 'directory' }], nextCursor: '' } }));
});
const controller = (mode: 'preview' | 'source' = 'preview'): DocumentController => ({
  buffers: [{ id: '/p/README.md', references: [location], snapshot: snapshot(), text: 'unsaved', dirty: true, editGeneration: 1, viewState: null, pendingDiskSnapshot: null, comparisonVisible: false, status: 'ready' }],
  tabs: [{ id: 'tab', references: [location], mode, bufferId: '/p/README.md', status: 'ready' }], activeTabId: 'tab', activeBufferId: '/p/README.md', visible: true, panelPercent: 45, dirtyCount: 1, stateVersion: 1, handoffPending: false, hydrated: true, failedOpen: null, decision: null,
  open: vi.fn(), edit: vi.fn(), save: vi.fn(), saveMerged: vi.fn(), closeTab: vi.fn(), retryTab: vi.fn(), reload: vi.fn(), compare: vi.fn(), setComparisonVisible: vi.fn(), confirmCloseAll: vi.fn(), setMode: vi.fn(), setVisible: vi.fn(), setPanelPercent: vi.fn(), setActive: vi.fn(), setViewState: vi.fn(), clearFailedOpen: vi.fn(), beginShutdownHandoff: vi.fn(), endShutdownHandoff: vi.fn(), resolveDecision: vi.fn(), getWorkspaceState: vi.fn(), getBuffers: vi.fn(), getBuffer: vi.fn(),
});
it('shows external editor for unsupported input', () => {
  const c = controller(); c.failedOpen = { location, code: 'unsupported_format', message: 'binary' };
  render(<DocumentWorkspace controller={c} context={null} onClaimDocuments={vi.fn()} />);
  fireEvent.click(screen.getByRole('button', { name: 'documents.openExternal' }));
  expect(openExternal).toHaveBeenCalledWith(location);
});
it('keeps document content mounted under independent review tabs and clears review on document click', () => {
  const c = controller('source'); const review = { id: 'review', ref: gitSnapshot().repository, root: gitSnapshot().root, entryId: 'one', layer: 'worktree' as const, relativePath: 'one.ts', snapshotRevision: 's1', status: 'ready' as const, diff: gitDiff('binary') };
  const reviews = { tabs: [review], activeTabId: review.id, open: vi.fn(), select: vi.fn(), clearActive: vi.fn(), close: vi.fn() };
  const view = render(<DocumentWorkspace controller={c} reviews={reviews} context={null} onClaimDocuments={vi.fn()} />);
  expect(screen.getByText('git.summary.binary')).toBeTruthy(); expect(screen.getByTestId('editor-text').textContent).toBe('unsaved');
  fireEvent.click(screen.getByRole('button', { name: /README.md.*p/ })); expect(reviews.clearActive).toHaveBeenCalledOnce(); expect(c.setActive).toHaveBeenCalledWith('tab');
  vi.mocked(disposeDocumentModel).mockClear(); view.rerender(<DocumentWorkspace controller={c} reviews={{ ...reviews, tabs: [], activeTabId: null }} context={null} onClaimDocuments={vi.fn()} />);
  expect(disposeDocumentModel).not.toHaveBeenCalled(); expect(screen.getByTestId('editor-text').textContent).toBe('unsaved');
});
it('shows a localized catalogue failure and retries project loading', async () => {
  listProjects.mockRejectedValueOnce(new Error('offline')).mockResolvedValueOnce([{ name: 'recovered' }]);
  render(<WorkspaceFilesPanel selection={{kind:"local",context:null}} context={null} active onSelectProject={vi.fn()} onOpenDocument={vi.fn()} onClaimNavigator={vi.fn()} />);
  expect((await screen.findByRole('status')).textContent).toContain('documents.projectCatalogFailed');
  fireEvent.click(screen.getByRole('button', { name: 'documents.retry' }));
  await waitFor(() => expect(screen.getByRole('option', { name: 'recovered' })).toBeTruthy());
  expect(listProjects).toHaveBeenCalledTimes(2);
});
it('reports a failed external-editor launch for unsupported input', async () => {
  openExternal.mockResolvedValueOnce({ code: 'permission_denied', message: 'private' });
  const c = controller(); c.failedOpen = { location, code: 'unsupported_format', message: 'binary' };
  render(<DocumentWorkspace controller={c} context={null} onClaimDocuments={vi.fn()} />);
  fireEvent.click(screen.getByRole('button', { name: 'documents.openExternal' }));
  expect((await screen.findByRole('status')).textContent).toContain('documents.problem.permission_denied');
});
it('loads directory pages lazily and opens Markdown in Preview', async () => {
  listApi.mockImplementation(async (ref: any, cursor: string) => ({ code: 'ok', value: ref.relativePath === 'docs' ? { entries: [{ name: 'next.md', relativePath: 'docs/next.md', kind: 'file' }], nextCursor: '' } : { entries: [{ name: 'docs', relativePath: 'docs', kind: 'directory' }], nextCursor: 'next' } }));
  const c = controller();
  render(<WorkspaceFilesPanel selection={{kind:"local",context:location.context}} context={location.context} active onSelectProject={vi.fn()} onOpenDocument={c.open} onClaimNavigator={vi.fn()} />);
  fireEvent.click(await screen.findByRole('button', { name: /docs/ }));
  fireEvent.click(await screen.findByRole('button', { name: /next.md/ }));
  expect(c.open).toHaveBeenCalledWith(expect.objectContaining({ relativePath: 'docs/next.md', rootKey: location.rootKey, mode: 'preview' }));
  await waitFor(() => expect(screen.getByRole('button', { name: 'documents.loadMore' })).toBeTruthy());
  expect(listApi).toHaveBeenCalledWith(expect.objectContaining({ relativePath: 'docs' }), '');
});
it('disposes the canonical Monaco model when its final controller buffer closes', () => {
  const c = controller('source');
  const props = { context: null, onSelectProject: vi.fn(), onClaimDocuments: vi.fn() };
  const view = render(<DocumentWorkspace controller={c} {...props} />);
  view.rerender(<DocumentWorkspace controller={{ ...c, tabs: [], buffers: [], activeTabId: null, activeBufferId: null }} {...props} />);
  expect(disposeDocumentModel).toHaveBeenCalledWith('/p/README.md');
});
it('ignores a late directory page from the previously selected project', async () => {
  const oldPage = deferred<any>();
  contextApi.mockImplementation(async (context: any) => ({ code: 'ok', value: { rootKey: context.projectName === 'old' ? 'a'.repeat(64) : 'b'.repeat(64), canonicalRoot: context.projectName, label: context.projectName, path: context.projectName, canRead: true, canWrite: true } }));
  listApi.mockImplementation(async (location: any) => location.context.projectName === 'old' ? oldPage.promise : { code: 'ok', value: { entries: [{ name: 'new.md', relativePath: 'new.md', kind: 'file' }], nextCursor: '' } });
  const c = controller(); const props = { selection: {kind: 'local' as const, context: null}, active:true, onSelectProject:vi.fn(), onOpenDocument:c.open, onClaimNavigator:vi.fn() };
  const view = render(<WorkspaceFilesPanel {...props} context={{ kind: 'project', projectName: 'old' }} />);
  await waitFor(() => expect(listApi).toHaveBeenCalledWith(expect.objectContaining({ context: { kind: 'project', projectName: 'old' } }), ''));
  view.rerender(<WorkspaceFilesPanel {...props} context={{ kind: 'project', projectName: 'new' }} />);
  expect(await screen.findByRole('button', { name: /new.md/ })).toBeTruthy();
  oldPage.resolve({ code: 'ok', value: { entries: [{ name: 'old.md', relativePath: 'old.md', kind: 'file' }], nextCursor: '' } });
  await new Promise(resolve => setTimeout(resolve, 0));
  expect(screen.queryByRole('button', { name: /old.md/ })).toBeNull();
});
it('opens markdown in Preview with the shared unsaved source and switches mode', () => {
  const c = controller(); render(<DocumentWorkspace controller={c} context={null} onClaimDocuments={vi.fn()} />);
  expect(screen.getByTestId('preview-text').textContent).toBe('unsaved');
  fireEvent.click(screen.getByRole('button', { name: 'documents.source' }));
  expect(c.setMode).toHaveBeenCalledWith('tab', 'source');
});

it('reveals Preview conflict and Compare recovery while retaining local text', () => {
  const c = controller(); c.buffers[0].status = 'conflict'; c.buffers[0].pendingDiskSnapshot = snapshot('/p/README.md', 'external', 'r2');
  render(<DocumentWorkspace controller={c} context={location.context} onClaimDocuments={vi.fn()} />);
  expect(screen.getByRole('status').textContent).toContain('documents.problem.conflict');
  expect(screen.getByTestId('preview-text').textContent).toBe('unsaved');
  fireEvent.click(screen.getByRole('button', { name: 'documents.compare' }));
  expect(c.setMode).toHaveBeenCalledWith('tab', 'source'); expect(c.compare).toHaveBeenCalledWith('/p/README.md');
});
it('shows post-open Preview unavailability and delegates Reload to the controller once', () => {
  const c = controller(); c.buffers[0].status = 'unavailable'; c.buffers[0].problem = { code: 'unavailable', message: 'gone' };
  const confirm = vi.spyOn(window, 'confirm');
  render(<DocumentWorkspace controller={c} context={location.context} onClaimDocuments={vi.fn()} />);
  expect(screen.getByRole('status').textContent).toContain('documents.problem.unavailable');
  expect(screen.getByTestId('preview-text').textContent).toBe('unsaved');
  fireEvent.click(screen.getByRole('button', { name: 'documents.reload' }));
  expect(c.reload).toHaveBeenCalledTimes(1); expect(confirm).not.toHaveBeenCalled(); confirm.mockRestore();
});
it('identifies resolved tabs by context and full canonical path', () => {
  const c = controller(); render(<DocumentWorkspace controller={c} context={location.context} onClaimDocuments={vi.fn()} />);
  const button = screen.getByRole('button', { name: /README.md.*p/ });
  expect(button.title).toBe('/p/README.md');
});
it.each(['tab', 'context'])('ignores late external-editor failure after changing %s', async change => {
  const pending = deferred<any>(); openExternal.mockReturnValueOnce(pending.promise);
  const c = controller(); c.buffers[0].snapshot.eol = 'mixed';
  const props = { onSelectProject: vi.fn(), onClaimDocuments: vi.fn() };
  const view = render(<DocumentWorkspace controller={c} context={location.context} {...props} />);
  fireEvent.click(screen.getByRole('button', { name: 'documents.openExternal' }));
  const next = change === 'tab' ? { ...c, activeTabId: null, activeBufferId: null } : c;
  view.rerender(<DocumentWorkspace controller={next} context={{ kind: 'project', projectName: 'other' }} {...props} />);
  await act(async () => pending.resolve({ code: 'permission_denied', message: 'A only' }));
  expect(screen.queryByText('documents.problem.permission_denied')).toBeNull();
});

it('labels nested agent documents by their effective worktree roots and preserves full-path tooltips', () => {
  const c = controller();
  c.buffers = ['worktree-a', 'worktree-b'].map((name, index) => {
    const origin = { context: { kind: 'agent' as const, sessionId: `session-${name}` }, rootKey: String(index).repeat(64), relativePath: 'docs/README.md' };
    return { ...c.buffers[0], id: name, references: [origin], snapshot: snapshot(`C:\\checkouts\\${name}\\docs\\README.md`) };
  });
  c.tabs = c.buffers.map(b => ({ id: b.id, references: b.references, bufferId: b.id, mode: 'preview', status: 'ready' }));
  c.activeTabId = c.tabs[0].id; c.activeBufferId = c.buffers[0].id;
  render(<DocumentWorkspace controller={c} context={null} onClaimDocuments={vi.fn()} />);
  for (const name of ['worktree-a', 'worktree-b']) {
    const button = screen.getByRole('button', { name: new RegExp(`README.md.*${name}`) });
    expect(button.title).toBe(`C:\\checkouts\\${name}\\docs\\README.md`);
  }
});
