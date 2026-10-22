// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, expect, it, vi } from 'vitest';
import { WorkspaceFilesPanel } from '../WorkspaceFilesPanel';
import { deferred } from './fixtures';
const bridge = vi.hoisted(() => ({ projects: vi.fn(), context: vi.fn(), list: vi.fn() }));
vi.mock('../../../../wailsjs/go/main/App', () => ({ ListProjects: (...args: unknown[]) => bridge.projects(...args) }));
vi.mock('../api', () => ({ workspaceDocumentApi: { context: (...args: unknown[]) => bridge.context(...args), list: (...args: unknown[]) => bridge.list(...args) } }));
vi.mock('../../../lib/i18n', () => ({ useT: () => (key: string) => key }));
afterEach(() => { cleanup(); vi.resetAllMocks(); });
const props = { selection: { kind: 'local' as const, context: null }, context: null, active: true, onSelectProject: vi.fn(), onOpenDocument: vi.fn(), onClaimNavigator: vi.fn() };
const root = (label: string) => ({ code: 'ok', value: { rootKey: label, canonicalRoot: label, label, path: label, canRead: true, canWrite: true } });
it('files panel ignores stale catalogs while SSH cannot use a manual local context', async () => {
  const catalog = deferred<any>(); bridge.projects.mockReturnValue(catalog.promise);
  const view = render(<WorkspaceFilesPanel {...props} />); await waitFor(() => expect(bridge.projects).toHaveBeenCalledOnce());
  view.rerender(<WorkspaceFilesPanel {...props} selection={{ kind: 'ssh', context: null }} context={{ kind: 'project', projectName: 'manual' }} />);
  await act(async () => catalog.resolve([{ name: 'stale' }]));
  expect(screen.queryByRole('combobox')).toBeNull(); expect(screen.getByRole('status').textContent).toContain('workspace.navigation.sshFiles'); expect(bridge.context).not.toHaveBeenCalled();
});
it('pauses inactive catalog queries and rejects their late results', async () => {
  const catalog = deferred<any>(); bridge.projects.mockReturnValueOnce(catalog.promise).mockResolvedValue([{ name: 'fresh' }]);
  const view = render(<WorkspaceFilesPanel {...props} active={false} />); expect(bridge.projects).not.toHaveBeenCalled();
  view.rerender(<WorkspaceFilesPanel {...props} />); await waitFor(() => expect(bridge.projects).toHaveBeenCalledOnce());
  view.rerender(<WorkspaceFilesPanel {...props} active={false} />); await act(async () => catalog.resolve([{ name: 'stale' }]));
  view.rerender(<WorkspaceFilesPanel {...props} />); expect(await screen.findByRole('option', { name: 'fresh' })).toBeTruthy(); expect(screen.queryByRole('option', { name: 'stale' })).toBeNull();
});
it('invalidates hidden tree requests on context change and preserves expansion for the same context', async () => {
  const page = deferred<any>(); bridge.context.mockImplementation(async (context: any) => root(context.projectName));
  bridge.list.mockImplementation(async (location: any) => location.context.projectName === 'old' ? page.promise : { code: 'ok', value: { entries: location.relativePath ? [{ name: 'file.md', relativePath: 'docs/file.md', kind: 'file' }] : [{ name: 'docs', relativePath: 'docs', kind: 'directory' }], nextCursor: '' } });
  const view = render(<WorkspaceFilesPanel {...props} context={{ kind: 'project', projectName: 'old' }} />); await waitFor(() => expect(bridge.list).toHaveBeenCalledOnce());
  view.rerender(<WorkspaceFilesPanel {...props} active={false} context={{ kind: 'project', projectName: 'new' }} />);
  await act(async () => page.resolve({ code: 'ok', value: { entries: [{ name: 'stale.md', relativePath: 'stale.md', kind: 'file' }], nextCursor: '' } }));
  expect(screen.queryByRole('button', { name: /stale.md/ })).toBeNull(); expect(bridge.context).toHaveBeenCalledTimes(1);
  view.rerender(<WorkspaceFilesPanel {...props} context={{ kind: 'project', projectName: 'new' }} />); fireEvent.click(await screen.findByRole('button', { name: /docs/ }));
  await screen.findByRole('button', { name: /file.md/ });
  const input = screen.getByRole('textbox'); fireEvent.change(input, { target: { value: 'readme.md' } }); const calls = bridge.list.mock.calls.length;
  view.rerender(<WorkspaceFilesPanel {...props} active={false} context={{ kind: 'project', projectName: 'new' }} />); view.rerender(<WorkspaceFilesPanel {...props} context={{ kind: 'project', projectName: 'new' }} />);
  expect((screen.getByRole('textbox') as HTMLInputElement).value).toBe('readme.md'); expect(screen.getByRole('button', { name: /docs/ }).textContent).toContain('▾'); expect(bridge.list.mock.calls.length).toBe(calls);
});
it('preserves the completed first page and cursor when a pending continuation is hidden', async () => {
  const latePage = deferred<any>();
  bridge.context.mockResolvedValue(root('demo'));
  bridge.list.mockResolvedValueOnce({ code: 'ok', value: { entries: [{ name: 'first.md', relativePath: 'first.md', kind: 'file' }], nextCursor: 'page-two' } })
    .mockReturnValueOnce(latePage.promise)
    .mockResolvedValueOnce({ code: 'ok', value: { entries: [{ name: 'second.md', relativePath: 'second.md', kind: 'file' }], nextCursor: '' } });
  const context = { kind: 'project' as const, projectName: 'demo' };
  const view = render(<WorkspaceFilesPanel {...props} context={context} />);
  await screen.findByRole('button', { name: /first.md/ });
  fireEvent.click(screen.getByRole('button', { name: 'documents.loadMore' }));
  await waitFor(() => expect(bridge.list).toHaveBeenCalledTimes(2));
  view.rerender(<WorkspaceFilesPanel {...props} context={context} active={false} />);
  expect(screen.getByRole('button', { name: /first.md/ })).toBeTruthy();
  await act(async () => latePage.resolve({ code: 'ok', value: { entries: [{ name: 'late.md', relativePath: 'late.md', kind: 'file' }], nextCursor: '' } }));
  view.rerender(<WorkspaceFilesPanel {...props} context={context} />);
  expect(screen.getByRole('button', { name: /first.md/ })).toBeTruthy();
  expect(screen.queryByRole('button', { name: /late.md/ })).toBeNull();
  expect(bridge.list).toHaveBeenCalledTimes(2);
  fireEvent.click(screen.getByRole('button', { name: 'documents.loadMore' }));
  await screen.findByRole('button', { name: /second.md/ });
  expect(screen.getByRole('button', { name: /first.md/ })).toBeTruthy();
  expect(bridge.list.mock.calls.map(call => call[1])).toEqual(['', 'page-two', 'page-two']);
});
it('restarts a directory after Task 1 reports a stale continuation cursor', async () => {
  bridge.context.mockResolvedValue(root('demo'));
  bridge.list.mockResolvedValueOnce({ code: 'ok', value: { entries: [{ name: 'old.md', relativePath: 'old.md', kind: 'file' }], nextCursor: 'cursor-v2' } })
    .mockResolvedValueOnce({ code: 'stale_cursor', message: 'Directory changed' })
    .mockResolvedValueOnce({ code: 'ok', value: { entries: [{ name: 'fresh.md', relativePath: 'fresh.md', kind: 'file' }], nextCursor: '' } });
  render(<WorkspaceFilesPanel {...props} context={{ kind: 'project', projectName: 'demo' }} />);
  fireEvent.click(await screen.findByRole('button', { name: 'documents.loadMore' }));
  expect(await screen.findByRole('button', { name: /fresh.md/ })).toBeTruthy(); expect(screen.queryByRole('button', { name: /old.md/ })).toBeNull();
  expect(bridge.list.mock.calls.map(call => call[1])).toEqual(['', 'cursor-v2', '']);
});
it.each(['invalid_path', 'stale_cursor'])('reports %s without retrying a fresh directory or an invalid continuation', async code => {
  bridge.context.mockResolvedValue(root('demo'));
  if (code === 'invalid_path') bridge.list.mockResolvedValueOnce({ code: 'ok', value: { entries: [], nextCursor: 'cursor-v2' } });
  bridge.list.mockResolvedValue({ code, message: 'Cannot list' });
  render(<WorkspaceFilesPanel {...props} context={{ kind: 'project', projectName: 'demo' }} />);
  if (code === 'invalid_path') fireEvent.click(await screen.findByRole('button', { name: 'documents.loadMore' }));
  expect((await screen.findByRole('status')).textContent).toContain('documents.problem.invalid_path');
  expect(bridge.list).toHaveBeenCalledTimes(code === 'invalid_path' ? 2 : 1);
});
