import type { WorkspaceContextRef } from '../workspace/types';
import type { RootInfo } from '../documents/types';
export type GitProblemCode = 'unavailable' | 'invalid_path' | 'repository_changed' | 'stale_snapshot' | 'git_missing' | 'busy' | 'conflict' | 'too_large' | 'unsupported_format' | 'permission_denied' | 'git_error' | 'operation_unknown' | 'shutting_down';
export type GitResult<T> = { code: 'ok'; value: T } | { code: GitProblemCode; message: string };
export type GitLayer = 'index' | 'worktree';
export type RepositoryRef = { context: WorkspaceContextRef; rootKey: string; repositoryKey: string };
export type ExpectedRepositoryState = { snapshotRevision: string; indexRevision: string; headOID: string; branch: string };
export type GitRemote = { id: string; name: string; displayURL: string; canPush: boolean; disabledReason: string };
export type GitEntry = {
  id: string; relativePath: string; originalPath: string; displayPath: string; indexCode: string; worktreeCode: string;
  kind: 'tracked' | 'untracked' | 'conflict' | 'gitlink' | 'blocked'; submoduleState: string;
  headOID: string; indexOID: string; headMode: string; indexMode: string; worktreeMode: string;
  canStage: boolean; canUnstage: boolean; disabledReason: string;
};
export type GitStatusSnapshot = ExpectedRepositoryState & {
  repository: RepositoryRef; root: RootInfo; upstream: string; gitOperation: string; unborn: boolean; detached: boolean;
  countsKnown: boolean; hasConflicts: boolean; busy: boolean; ahead: number; behind: number;
  entries: GitEntry[]; remotes: GitRemote[]; operationId: string;
};
export type EntrySelection = { entryId: string; diskRevision: string };
/** Full refs/heads/<branch> at the API boundary. */
export type PushTarget = { remoteId: string; remoteBranch: string; setUpstream: boolean };
export type GitDiffSide = { text: string; revision: string; oid: string; eol: string; size: number; bom: boolean };
export type GitDiff = {
  id: string; repository: RepositoryRef; entryId: string; relativePath: string; originalPath: string; layer: GitLayer;
  kind: 'text' | 'binary' | 'too_large' | 'gitlink' | 'unsupported'; original: GitDiffSide; modified: GitDiffSide;
  canOpenWorktree: boolean; message: string;
};
export type GitOperation = {
  id: string; repository: RepositoryRef; kind: 'stage' | 'unstage' | 'commit' | 'push';
  state: 'running' | 'cancelling' | 'succeeded' | 'failed' | 'cancelled' | 'uncertain';
  resultCode: string; output: string; message: string; sequence: number; outputTruncated: boolean; hasExitCode: boolean; exitCode: number;
  startedAt: string; finishedAt: string; commitOID: string; remote: string; remoteBranch: string;
  mutationObserved: boolean; remoteUncertain: boolean; afterSnapshot: GitStatusSnapshot | null;
};
export type OperationEvent = { operationId: string; repository: RepositoryRef; sequence: number; state: GitOperation['state']; stream: string; chunk: string };
export type StagePermit = { validate(): boolean };
export type StageDirtyChoice = 'save-and-stage' | 'stage-disk' | 'cancel';
export type SourceControlState = {
  snapshot: GitStatusSnapshot | null; loading: boolean; error: { code: GitProblemCode; message: string } | null;
  query: string; message: string; selectedIndexIds: string[]; selectedWorktreeIds: string[]; operations: GitOperation[];
};
export interface SourceControlController extends SourceControlState {
  refresh(): Promise<void>; setQuery(value: string): void; setMessage(value: string): void;
  select(layer: GitLayer, id: string, selected: boolean): void; stage(ids: string[]): Promise<void>; unstage(ids: string[]): Promise<void>;
  commit(): Promise<void>; push(target: PushTarget): Promise<void>; cancelOperation(id: string): Promise<void>;
}

/** Detach native or injected API objects before retaining them in memory. */
export function immutableCopy<T>(value: T): T {
  if (Array.isArray(value)) return Object.freeze(value.map(item => immutableCopy(item))) as T;
  if (value && typeof value === 'object') return Object.freeze(Object.fromEntries(Object.entries(value).map(([key, item]) => [key, immutableCopy(item)]))) as T;
  return value;
}
