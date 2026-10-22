import type { WorkspaceContextRef } from '../workspace/types';
export type { WorkspaceContextRef, WorkspaceSelection, WorkspaceView, ActiveSurface, NavigatorTab } from '../workspace/types';

export type DocumentMode = 'source' | 'preview';
export type DocumentLocationRef = { context: WorkspaceContextRef; rootKey: string; relativePath: string };
export type WorkspaceDocumentRef = DocumentLocationRef & { mode: DocumentMode };
export type RootInfo = { rootKey: string; canonicalRoot: string; label: string; path: string; canRead: boolean; canWrite: boolean };
export type Snapshot = { canonicalPath: string; text: string; revision: string; size: number; bom: boolean; eol: 'lf' | 'crlf' | 'mixed'; readOnly: boolean };
export type DirectoryEntry = { name: string; relativePath: string; kind: 'file' | 'directory' | 'blocked' };
export type DirectoryPage = { entries: DirectoryEntry[]; nextCursor: string };
export type WorkspaceImage = { dataUrl: string; mime: string; size: number };
export type ProblemCode = 'invalid_path' | 'stale_cursor' | 'unavailable' | 'too_large' | 'unsupported_format' | 'permission_denied' | 'conflict' | 'io_error';
export type Result<T> = { code: 'ok' | 'changed'; value: T } | { code: 'unchanged' } | { code: ProblemCode; message: string; latest?: Snapshot };
export type DocumentBuffer = {
  /** Opaque in-memory generation; never a pathname or persisted filesystem authority. */
  id: string; references: DocumentLocationRef[]; snapshot: Snapshot; text: string; dirty: boolean;
  editGeneration: number; viewState: unknown; pendingDiskSnapshot: Snapshot | null; comparisonVisible: boolean;
  status: 'ready' | 'conflict' | 'unavailable' | 'error'; problem?: { code: ProblemCode; message: string };
};
export type DocumentTab = { id: string; references: DocumentLocationRef[]; mode: DocumentMode; bufferId: string | null; status: 'loading' | 'ready' | 'unavailable' | 'error' };
export type CloseChoice = 'save' | 'discard' | 'cancel';
export type CloseDecision = { scope: 'one' | 'all'; names: string[] };
/** The main-process gate DTO, distinct from the local buffer state. */
export type DocumentWorkspaceState = { ready: boolean; dirtyCount: number; stateVersion: number };
export type DocumentState = {
  buffers: DocumentBuffer[]; tabs: DocumentTab[]; activeTabId: string | null; activeBufferId: string | null;
  visible: boolean; panelPercent: number; dirtyCount: number; stateVersion: number; handoffPending: boolean; hydrated: boolean;
  failedOpen: { location: DocumentLocationRef; code: ProblemCode; message: string } | null; decision: CloseDecision | null;
};
export interface DocumentController extends DocumentState {
  open(ref: WorkspaceDocumentRef): Promise<void>; edit(bufferId: string, text: string): void;
  save(bufferId: string): Promise<boolean>; saveMerged(bufferId: string): Promise<boolean>;
  closeTab(tabId: string): Promise<boolean>; retryTab(tabId: string): Promise<void>; reload(bufferId: string): Promise<boolean>;
  compare(bufferId: string): Promise<void>; setComparisonVisible(bufferId: string, visible: boolean): void;
  confirmCloseAll(): Promise<boolean>; setMode(tabId: string, mode: DocumentMode): void; setVisible(visible: boolean): void;
  setPanelPercent(percent: number): void; setActive(tabId: string): void; setViewState(bufferId: string, state: unknown): void;
  clearFailedOpen(): void; beginShutdownHandoff(): DocumentWorkspaceState | null; endShutdownHandoff(): void;
  resolveDecision(choice: CloseChoice): void;
  /** Synchronous snapshot: async close/shutdown work must not read a render-time closure. */
  getWorkspaceState(): DocumentWorkspaceState;
  getBuffers(): readonly DocumentBuffer[];
  getBuffer(id: string): DocumentBuffer | undefined;
}
