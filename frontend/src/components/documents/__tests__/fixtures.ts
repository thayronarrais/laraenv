import { vi } from 'vitest';
import type { DocumentAPI } from '../api';
import type { Snapshot, WorkspaceDocumentRef } from '../types';
export const ref = (name = 'p', path = 'README.md'): WorkspaceDocumentRef => ({ context: { kind: 'project', projectName: name }, rootKey: 'a'.repeat(64), relativePath: path, mode: 'preview' });
export const snapshot = (canonicalPath = '/p/README.md', text = 'base', revision = 'r1'): Snapshot => ({ canonicalPath, text, revision, size: text.length, bom: false, eol: 'lf', readOnly: false });
export const ok = <T>(value: T) => ({ code: 'ok' as const, value });
export const deferred = <T>() => { let resolve!: (value: T) => void; let reject!: (reason: unknown) => void; const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no; }); return { promise, resolve, reject }; };
export function fakeAPI(): DocumentAPI {
  return { context: vi.fn(), list: vi.fn(), read: vi.fn(async () => ok(snapshot())), check: vi.fn(async () => ({ code: 'unchanged' as const })), save: vi.fn(async (_r, text) => ok(snapshot('/p/README.md', text, 'r2'))), image: vi.fn(), openExternal: vi.fn(), loadLayout: vi.fn(async () => ''), saveLayout: vi.fn(async () => {}) };
}
