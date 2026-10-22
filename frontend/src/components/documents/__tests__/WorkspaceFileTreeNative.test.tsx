// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, expect, it, vi } from 'vitest';
import { WorkspaceFileTree } from '../WorkspaceFileTree';
import { deferred } from './fixtures';
const native = vi.hoisted(() => ({ context: vi.fn(), list: vi.fn() }));
vi.mock('../../../../wailsjs/go/main/App', () => ({ GetWorkspaceFileContext: native.context, ListWorkspaceDirectory: native.list, LoadDocumentWorkspace: vi.fn(), SaveDocumentWorkspace: vi.fn() }));
vi.mock('../../../lib/i18n', () => ({ useT: () => (key: string) => key }));
afterEach(() => { cleanup(); vi.resetAllMocks(); });
const props = { context: { kind: 'project' as const, projectName: 'fixture' }, onOpenDocument: vi.fn(), onClaimNavigator: vi.fn() };
// Exact JSON shape emitted by TestWorkspaceDirectoryFacadeStaleCursor (no value envelope).
const stale = { code: 'stale_cursor', message: 'stale directory cursor' };
const page = (name: string, nextCursor = '') => ({ code: 'ok', directory: { entries: [{ name, relativePath: name, kind: 'file' }], nextCursor } });
function root() { native.context.mockResolvedValue({ code: 'ok', context: { rootKey: 'native-root', canonicalRoot: '/fixture', label: 'fixture', path: '/fixture', canRead: true, canWrite: true } }); }
it('normalizes native continuation failure, replaces page once, and opens only fresh paths', async () => {
  root(); native.list.mockResolvedValueOnce(page('old.txt', 'native-cursor')).mockResolvedValueOnce(stale).mockResolvedValueOnce(page('new.txt'));
  render(<WorkspaceFileTree {...props} />);
  fireEvent.click(await screen.findByRole('button', { name: 'documents.loadMore' }));
  fireEvent.click(await screen.findByRole('button', { name: 'new.txt' }));
  expect(screen.queryByRole('button', { name: 'old.txt' })).toBeNull();
  expect(native.list.mock.calls.map(args => args[3])).toEqual(['', 'native-cursor', '']);
  expect(props.onOpenDocument).toHaveBeenLastCalledWith({ context: props.context, rootKey: 'native-root', relativePath: 'new.txt', mode: 'source' });
});
it('does not loop when the restarted initial page also changes', async () => {
  root(); native.list.mockResolvedValueOnce(page('old.txt', 'native-cursor')).mockResolvedValue(stale);
  render(<WorkspaceFileTree {...props} />); fireEvent.click(await screen.findByRole('button', { name: 'documents.loadMore' }));
  await screen.findByRole('button', { name: 'documents.retry' });
  expect(native.list.mock.calls.map(args => args[3])).toEqual(['', 'native-cursor', '']);
});
it.each(['inactive', 'new-context'])('ignores late native stale continuation after %s', async mode => {
  root(); const pending = deferred<any>();
  native.list.mockResolvedValueOnce(page('old.txt', 'native-cursor')).mockReturnValueOnce(pending.promise).mockResolvedValue(page('B.txt'));
  const view = render(<WorkspaceFileTree {...props} />);
  fireEvent.click(await screen.findByRole('button', { name: 'documents.loadMore' }));
  await waitFor(() => expect(native.list).toHaveBeenCalledTimes(2));
  view.rerender(<WorkspaceFileTree {...props} active={mode !== 'inactive'} context={mode === 'new-context' ? { kind: 'project', projectName: 'B' } : props.context} />);
  if (mode === 'new-context') await screen.findByRole('button', { name: 'B.txt' });
  const count = native.list.mock.calls.length;
  await act(async () => pending.resolve(stale));
  expect(native.list).toHaveBeenCalledTimes(count);
});
