import * as App from '../../../wailsjs/go/main/App';
import { sourcecontrol } from '../../../wailsjs/go/models';
import { EventsOn } from '../../../wailsjs/runtime/runtime';
import type { WorkspaceContextRef } from '../workspace/types';
import { immutableCopy } from './types';
import type { EntrySelection, ExpectedRepositoryState, GitDiff, GitLayer, GitOperation, GitProblemCode, GitResult, GitStatusSnapshot, OperationEvent, PushTarget, RepositoryRef } from './types';

export interface SourceControlAPI {
  resolve(context: WorkspaceContextRef): Promise<GitResult<GitStatusSnapshot>>;
  status(ref: RepositoryRef): Promise<GitResult<GitStatusSnapshot>>;
  diff(ref: RepositoryRef, expected: ExpectedRepositoryState, entryId: string, layer: GitLayer): Promise<GitResult<GitDiff>>;
  stage(ref: RepositoryRef, expected: ExpectedRepositoryState, entries: EntrySelection[]): Promise<GitResult<GitOperation>>;
  unstage(ref: RepositoryRef, expected: ExpectedRepositoryState, entries: EntrySelection[]): Promise<GitResult<GitOperation>>;
  commit(ref: RepositoryRef, expected: ExpectedRepositoryState, message: string): Promise<GitResult<GitOperation>>;
  push(ref: RepositoryRef, expected: ExpectedRepositoryState, target: PushTarget): Promise<GitResult<GitOperation>>;
  operation(id: string): Promise<GitResult<GitOperation>>;
  cancel(id: string): Promise<GitResult<GitOperation>>;
  subscribeOperations(listener: (event: OperationEvent) => void): () => void;
}
const problems = new Set<GitProblemCode>(['unavailable', 'invalid_path', 'repository_changed', 'stale_snapshot', 'git_missing', 'busy', 'conflict', 'too_large', 'unsupported_format', 'permission_denied', 'git_error', 'operation_unknown', 'shutting_down']);
export const problemCode = (code: string): GitProblemCode => problems.has(code as GitProblemCode) ? code as GitProblemCode : 'git_error';
const contextDTO = (context: WorkspaceContextRef) => context.kind === 'agent' ? { ...context, projectName: '' } : { ...context, sessionId: '' };
const repositoryDTO = (ref: RepositoryRef) => new sourcecontrol.RepositoryRef({ ...ref, context: contextDTO(ref.context) });
const snapshotDTO = (raw: sourcecontrol.Snapshot): GitStatusSnapshot => immutableCopy({ ...raw, entries: raw.entries ?? [], remotes: raw.remotes ?? [] } as GitStatusSnapshot);
const operationDTO = (raw: sourcecontrol.Operation): GitOperation => immutableCopy({ ...raw, startedAt: raw.startedAt ?? '', finishedAt: raw.finishedAt ?? '', afterSnapshot: raw.afterSnapshot ? snapshotDTO(raw.afterSnapshot) : null } as GitOperation);
async function result<T>(request: () => Promise<sourcecontrol.Result>, field: 'snapshot' | 'diff' | 'operation'): Promise<GitResult<T>> {
  try {
    const raw = await request();
    if (raw.code !== 'ok') return { code: problemCode(raw.code), message: raw.message ?? '' };
    const value = raw[field];
    if (!value) return { code: 'git_error', message: 'Missing Source Control result.' };
    return { code: 'ok', value: (field === 'snapshot' ? snapshotDTO(value as sourcecontrol.Snapshot) : field === 'operation' ? operationDTO(value as sourcecontrol.Operation) : immutableCopy(value)) as T };
  } catch (error) { return { code: 'git_error', message: error instanceof Error ? error.message : String(error) }; }
}
export const sourceControlApi: SourceControlAPI = {
  resolve: context => result(() => App.InspectSourceControl(contextDTO(context)), 'snapshot'),
  status: ref => result(() => App.GetSourceControlStatus(repositoryDTO(ref)), 'snapshot'),
  diff: (ref, expected, entryId, layer) => result(() => App.ReadSourceControlDiff(new sourcecontrol.DiffRequest({ repository: repositoryDTO(ref), expected, entryId, layer })), 'diff'),
  stage: (ref, expected, entries) => result(() => App.StartSourceControlStage(new sourcecontrol.FilesRequest({ repository: repositoryDTO(ref), expected, entries })), 'operation'),
  unstage: (ref, expected, entries) => result(() => App.StartSourceControlUnstage(new sourcecontrol.FilesRequest({ repository: repositoryDTO(ref), expected, entries })), 'operation'),
  commit: (ref, expected, message) => result(() => App.StartSourceControlCommit(new sourcecontrol.CommitRequest({ repository: repositoryDTO(ref), expected, message })), 'operation'),
  push: (ref, expected, target) => result(() => App.StartSourceControlPush(new sourcecontrol.PushRequest({ repository: repositoryDTO(ref), expected, ...target })), 'operation'),
  operation: id => result(() => App.GetSourceControlOperation(id), 'operation'),
  cancel: id => result(() => App.CancelSourceControlOperation(id), 'operation'),
  subscribeOperations: listener => EventsOn('sourcecontrol:operation', (event: OperationEvent) => listener(immutableCopy(event))),
};
