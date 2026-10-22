// Sources carry catalog/session authority, never a frontend absolute path.
export type WorkspaceContextRef =
  | { kind: 'agent'; sessionId: string }
  | { kind: 'project'; projectName: string };
export type WorkspaceSelection = { kind: 'local'; context: WorkspaceContextRef | null } | { kind: 'ssh'; context: null };
export type WorkspaceView = 'terminal' | 'documents';
export type ActiveSurface = WorkspaceView | 'navigator';
export type NavigatorTab = 'agents' | 'files' | 'git';
