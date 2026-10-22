import { expect, it, vi } from 'vitest';
const calls = vi.hoisted(() => ({ context: vi.fn(), save: vi.fn(), list: vi.fn() }));
vi.mock('../../../../wailsjs/go/main/App', () => ({ GetWorkspaceFileContext: calls.context, SaveWorkspaceDocument: calls.save, ListWorkspaceDirectory: calls.list, LoadDocumentWorkspace: vi.fn(), SaveDocumentWorkspace: vi.fn() }));
import { normalizedResult, workspaceDocumentApi } from '../api';
import { ref, snapshot } from './fixtures';
it('normalizes concrete DTO fields and supplies the unused context identifier', async () => {
  calls.context.mockResolvedValue({ code: 'ok', context: { rootKey: 'a'.repeat(64) } });
  expect(await workspaceDocumentApi.context({ kind: 'agent', sessionId: 's' })).toEqual({ code: 'ok', value: { rootKey: 'a'.repeat(64) } });
  expect(calls.context).toHaveBeenCalledWith({ kind: 'agent', sessionId: 's', projectName: '' });
  calls.save.mockResolvedValue({ code: 'conflict', message: 'disk changed', document: snapshot() });
  expect(await workspaceDocumentApi.save(ref(), 'text', 'old')).toEqual({ code: 'conflict', message: 'disk changed', latest: snapshot() });
  expect(calls.save).toHaveBeenCalledWith({ kind: 'project', projectName: 'p', sessionId: '' }, 'a'.repeat(64), 'README.md', 'text', 'old');
  expect(normalizedResult({ code: 'unchanged' } as any, 'document')).toEqual({ code: 'unchanged' });
  expect(normalizedResult({ code: 'changed', document: snapshot() } as any, 'document')).toEqual({ code: 'changed', value: snapshot() });
  expect(normalizedResult({ code: 'ok' } as any)).toEqual({ code: 'ok', value: undefined });
  expect(normalizedResult({ code: 'io_error', message: 'conflict unavailable permission_denied' } as any)).toEqual({ code: 'io_error', message: 'conflict unavailable permission_denied' });
});
it('retains stale_cursor from the concrete local directory API for tree recovery', async () => {
  calls.list.mockResolvedValue({ code: 'stale_cursor', message: 'changed', directory: null });
  expect(await workspaceDocumentApi.list(ref('p', 'docs'), 'cursor-v2')).toEqual({ code: 'stale_cursor', message: 'changed' });
  expect(calls.list).toHaveBeenCalledWith({ kind: 'project', projectName: 'p', sessionId: '' }, 'a'.repeat(64), 'docs', 'cursor-v2');
});
