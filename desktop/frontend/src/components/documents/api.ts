import * as App from '../../../wailsjs/go/main/App';
import type { workspacefiles } from '../../../wailsjs/go/models';
import type { DirectoryPage, DocumentLocationRef, ProblemCode, Result, RootInfo, Snapshot, WorkspaceContextRef, WorkspaceImage } from './types';
export interface DocumentAPI {
  context(context: WorkspaceContextRef): Promise<Result<RootInfo>>;
  list(location: DocumentLocationRef, cursor: string): Promise<Result<DirectoryPage>>;
  read(location: DocumentLocationRef): Promise<Result<Snapshot>>;
  check(location: DocumentLocationRef, revision: string): Promise<Result<Snapshot>>;
  save(location: DocumentLocationRef, content: string, revision: string): Promise<Result<Snapshot>>;
  image(location: DocumentLocationRef): Promise<Result<WorkspaceImage>>;
  openExternal(location: DocumentLocationRef): Promise<Result<void>>;
  loadLayout(): Promise<string>; saveLayout(raw: string): Promise<void>;
}
const problems = new Set<ProblemCode>(['invalid_path', 'stale_cursor', 'unavailable', 'too_large', 'unsupported_format', 'permission_denied', 'conflict', 'io_error']);
export function normalizedResult<T>(raw: workspacefiles.Result, field?: 'context' | 'directory' | 'document' | 'image'): Result<T> {
  if (raw.code === 'unchanged') return { code: 'unchanged' };
  if (raw.code === 'ok' || raw.code === 'changed') {
    if (field && !raw[field]) return { code: 'io_error', message: 'Missing workspace result.' };
    return { code: raw.code, value: (field ? raw[field] : undefined) as T };
  }
  if (problems.has(raw.code as ProblemCode)) return { code: raw.code as ProblemCode, message: raw.message ?? '', ...(raw.document ? { latest: raw.document as Snapshot } : {}) };
  return { code: 'io_error', message: raw.message || 'Invalid workspace result.' };
}
const contextDTO = (ref: WorkspaceContextRef) => ref.kind === 'agent' ? { ...ref, projectName: '' } : { ...ref, sessionId: '' };
const args = (ref: DocumentLocationRef) => [contextDTO(ref.context), ref.rootKey, ref.relativePath] as const;
export const workspaceDocumentApi: DocumentAPI = {
  context: async ref => normalizedResult(await App.GetWorkspaceFileContext(contextDTO(ref)), 'context'),
  list: async (ref, cursor) => normalizedResult(await App.ListWorkspaceDirectory(...args(ref), cursor), 'directory'),
  read: async ref => normalizedResult(await App.ReadWorkspaceDocument(...args(ref)), 'document'),
  check: async (ref, revision) => normalizedResult(await App.CheckWorkspaceDocument(...args(ref), revision), 'document'),
  save: async (ref, content, revision) => normalizedResult(await App.SaveWorkspaceDocument(...args(ref), content, revision), 'document'),
  image: async ref => normalizedResult(await App.ReadWorkspaceImage(...args(ref)), 'image'),
  openExternal: async ref => normalizedResult(await App.OpenWorkspaceDocumentExternally(...args(ref))),
  loadLayout: App.LoadDocumentWorkspace, saveLayout: App.SaveDocumentWorkspace,
};
