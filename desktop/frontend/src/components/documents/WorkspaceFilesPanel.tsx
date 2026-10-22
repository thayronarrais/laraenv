import { useEffect, useState } from 'react';
import { ListProjects } from '../../../wailsjs/go/main/App';
import { useT } from '../../lib/i18n';
import type { WorkspaceContextRef, WorkspaceDocumentRef, WorkspaceSelection } from './types';
import { WorkspaceFileTree } from './WorkspaceFileTree';
export type WorkspaceFilesPanelProps = { selection: WorkspaceSelection; context: WorkspaceContextRef | null; active: boolean; onSelectProject(name: string): void; onOpenDocument(ref: WorkspaceDocumentRef): void; onClaimNavigator(): void };
export function WorkspaceFilesPanel({ selection, context, active, onSelectProject, onOpenDocument, onClaimNavigator }: WorkspaceFilesPanelProps) {
  const t = useT(); const [projects, setProjects] = useState<string[]>([]); const [catalogError, setCatalogError] = useState(false); const [retry, setRetry] = useState(0);
  const eligible = selection.kind === 'local' ? selection.context ?? context : null;
  const identity = JSON.stringify([selection.kind, eligible]);
  useEffect(() => {
    if (!active || selection.kind !== 'local' || eligible) return;
    let live = true; setCatalogError(false);
    void Promise.resolve().then(() => live ? ListProjects() : []).then(list => { if (live) setProjects((list ?? []).map(project => project.name)); }).catch(() => { if (live) setCatalogError(true); });
    return () => { live = false; };
  }, [identity, active, retry]);
  if (selection.kind === 'ssh') return <p role="status" className="p-3 text-xs">{t('workspace.navigation.sshFiles')}</p>;
  if (eligible) return <WorkspaceFileTree context={eligible} active={active} onOpenDocument={onOpenDocument} onClaimNavigator={onClaimNavigator} />;
  return <div className="p-3 text-xs" onPointerDownCapture={onClaimNavigator} onFocusCapture={onClaimNavigator}>
    <p>{t('documents.selectProject')}</p>
    {catalogError && <p role="status">{t('documents.projectCatalogFailed')} <button type="button" onClick={() => setRetry(value => value + 1)}>{t('documents.retry')}</button></p>}
    <select aria-label={t('documents.selectProject')} defaultValue="" onChange={event => { if (event.target.value) onSelectProject(event.target.value); }} className="mt-2 w-full rounded bg-white/10 p-2">
      <option value="">{t('documents.selectProject')}</option>{projects.map(name => <option key={name} value={name}>{name}</option>)}
    </select>
  </div>;
}
