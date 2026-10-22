import { layerLabels } from '../sourcecontrol/labels';
import { useEffect, useRef, useState } from 'react';
import { useT } from '../../lib/i18n';
import { workspaceDocumentApi } from './api';
import { DocumentEditor, disposeDocumentModel } from './DocumentEditor';
import { MarkdownPreview } from './MarkdownPreview';
import { documentProblemKey } from './problemKey';
import type { DocumentController, DocumentLocationRef, ProblemCode, WorkspaceContextRef } from './types';
import { GitReviewEditor } from '../sourcecontrol/GitReviewEditor';
import type { GitReviewController } from '../sourcecontrol/useGitReviews';

function contextLabel(location: DocumentLocationRef | undefined, canonicalPath?: string): string {
  if (!location) return '';
  if (location.context.kind === 'project') return location.context.projectName;
  // Remove the validated relative path to identify the effective root, not the file's parent.
  const rootName = canonicalPath?.replace(/\\/g, '/').split('/').slice(0, -location.relativePath.split('/').length).filter(Boolean).pop();
  return rootName || `Agent ${location.context.sessionId.slice(0, 8)}`;
}

export type DocumentWorkspaceProps = { controller: DocumentController; reviews?: GitReviewController; context: WorkspaceContextRef | null; active?: boolean; onClaimDocuments(): void };
export function DocumentWorkspace({ controller, reviews, context, onClaimDocuments }: DocumentWorkspaceProps): JSX.Element {
  const t = useT();
  const [externalError, setExternalError] = useState<{ scope: string; code: ProblemCode } | null>(null);
  const externalRequest = useRef(0);
  const owned = useRef(new Set<string>());
  useEffect(() => {
    const next = new Set(controller.buffers.map(buffer => buffer.id));
    for (const id of owned.current) if (!next.has(id)) disposeDocumentModel(id);
    owned.current = next;
  }, [controller.buffers]);
  useEffect(() => () => { for (const id of owned.current) disposeDocumentModel(id); owned.current.clear(); }, []);
  const active = controller.tabs.find(tab => tab.id === controller.activeTabId);
  const buffer = controller.buffers.find(item => item.id === active?.bufferId);
  const location = active?.references[0];
  const failed = controller.failedOpen;
  const externalScope = JSON.stringify([context, active?.id, location, failed?.location]);
  const scopeRef = useRef(externalScope); scopeRef.current = externalScope;
  useEffect(() => { externalRequest.current++; setExternalError(null); }, [externalScope]);
  useEffect(() => () => { externalRequest.current++; }, []);
  const launchExternal = async (target: DocumentLocationRef) => {
    setExternalError(null);
    const request = ++externalRequest.current; const scope = externalScope;
    const fail = (code: ProblemCode) => { if (externalRequest.current === request && scopeRef.current === scope) setExternalError({ scope, code }); };
    try {
      const result = await workspaceDocumentApi.openExternal(target);
      if (result.code !== 'ok' && result.code !== 'changed') fail('message' in result ? result.code : 'io_error');
    } catch { fail('io_error'); }
  };
  return <section className="flex h-full min-h-0 flex-col bg-[#12141b] text-white" aria-label={t('documents.title')} onPointerDownCapture={onClaimDocuments} onFocusCapture={onClaimDocuments}>
    <div data-testid="document-tab-strip" className="flex min-w-0 overflow-x-auto border-b border-white/10">

      {controller.tabs.map(tab => <div key={tab.id} className={`flex shrink-0 items-center border-r border-white/10 text-xs ${tab.id === controller.activeTabId && !reviews?.activeTabId ? 'bg-white/10' : ''}`}>
        <button type="button" className="flex max-w-64 items-center gap-2 px-3 py-2" title={controller.buffers.find(b => b.id === tab.bufferId)?.snapshot.canonicalPath ?? `${tab.references[0]?.context.kind === 'project' ? tab.references[0].context.projectName : tab.references[0]?.context.sessionId}: ${tab.references[0]?.relativePath}`} onClick={() => { reviews?.clearActive(); controller.setActive(tab.id); }}>
          <span className="max-w-40 truncate">{tab.references[0]?.relativePath.split('/').pop()}{controller.buffers.find(b => b.id === tab.bufferId)?.dirty ? ' •' : ''}</span>
          <span className="max-w-24 truncate text-white/50">{contextLabel(controller.buffers.find(b => b.id === tab.bufferId)?.references[0] ?? tab.references[0], controller.buffers.find(b => b.id === tab.bufferId)?.snapshot.canonicalPath)}</span>
        </button>
        <button type="button" aria-label={`${t('documents.closeTab')} ${tab.references[0]?.relativePath}`} disabled={controller.handoffPending} className="px-2" onClick={() => void controller.closeTab(tab.id)}>×</button>
      </div>)}
      {reviews?.tabs.map(tab => <div key={tab.id} className={`flex shrink-0 items-center border-r border-white/10 text-xs ${reviews.activeTabId === tab.id ? 'bg-white/10' : ''}`}>
        <button type="button" className="max-w-80 truncate px-3 py-2" aria-label={`${tab.relativePath || t('git.review')} · ${t(layerLabels[tab.layer])} · ${tab.root.label} · ${tab.root.path}`} title={`${tab.root.path.replace(/[\\/]$/, '')}/${tab.relativePath}`} onClick={() => reviews.select(tab.id)}>{tab.relativePath || t('git.review')} · {t(layerLabels[tab.layer])} · {tab.root.label}</button>
        <button type="button" className="px-2" aria-label={`${t('git.closeReview')} ${tab.relativePath} · ${tab.root.label} · ${tab.root.path}`} onClick={() => reviews.close(tab.id)}>×</button>
      </div>)}
    </div>
    <div className="relative flex min-h-0 flex-1">
      <main className="flex min-w-0 flex-1 flex-col">
        <div className="flex min-h-0 flex-1 flex-col" style={{ display: reviews?.activeTabId ? 'none' : undefined }}>
        {active && <div className="flex gap-1 border-b border-white/10 p-2 text-xs">
          <button type="button" className={active.mode === 'preview' ? 'btn-primary' : 'btn-secondary'} disabled={controller.handoffPending} onClick={() => controller.setMode(active.id, 'preview')}>{t('documents.preview')}</button>
          <button type="button" className={active.mode === 'source' ? 'btn-primary' : 'btn-secondary'} disabled={controller.handoffPending} onClick={() => controller.setMode(active.id, 'source')}>{t('documents.source')}</button>
        </div>}
        {active?.status === 'loading' && <p className="p-4 text-xs">{t('documents.loading')}</p>}
        {active && !buffer && (active.status === 'unavailable' || active.status === 'error') && <div className="p-4 text-xs">
          <p>{t('documents.unavailable')}</p><button type="button" onClick={() => void controller.retryTab(active.id)}>{t('documents.retry')}</button>
        </div>}
        {buffer && <div className="flex flex-wrap items-center gap-2 border-b border-white/10 p-2 text-xs">
          <button type="button" className="btn-primary" disabled={controller.handoffPending || buffer.snapshot.readOnly || buffer.snapshot.eol === 'mixed' || !buffer.dirty} onClick={() => void controller.save(buffer.id)}>{t('documents.save')}</button>
          {buffer.status === 'conflict' && <><span role="status">{t('documents.problem.conflict')}</span><button type="button" className="btn-secondary" disabled={controller.handoffPending} onClick={() => { controller.setMode(active!.id, 'source'); void controller.compare(buffer.id); }}>{t('documents.compare')}</button></>}
          {buffer.pendingDiskSnapshot && <button type="button" className="btn-secondary" disabled={controller.handoffPending} onClick={() => { controller.setMode(active!.id, 'source'); controller.setComparisonVisible(buffer.id, !buffer.comparisonVisible); }}>{t(buffer.comparisonVisible ? 'documents.hideComparison' : 'documents.showComparison')}</button>}
          {(buffer.pendingDiskSnapshot || buffer.status === 'unavailable' || buffer.status === 'error') && <button type="button" className="btn-secondary" disabled={controller.handoffPending} onClick={() => void controller.reload(buffer.id)}>{t('documents.reload')}</button>}
          {buffer.snapshot.eol === 'mixed' && <span role="status">{t('documents.mixedEol')}</span>}
          {buffer.status !== 'conflict' && (buffer.problem || buffer.status === 'unavailable') && <span role="status">{t(documentProblemKey(buffer.problem?.code ?? 'unavailable'))}</span>}
        </div>}
        {buffer && location && <div className="min-h-0 flex-1">
          {active?.mode === 'preview' ? <MarkdownPreview location={location} text={buffer.text} onOpenDocument={ref => void controller.open(ref)} onClaimDocuments={onClaimDocuments} /> :
            <DocumentEditor buffer={buffer} onEdit={text => controller.edit(buffer.id, text)} onSave={() => void controller.save(buffer.id)} onSaveMerged={() => void controller.saveMerged(buffer.id)} onViewState={state => controller.setViewState(buffer.id, state)} disabled={controller.handoffPending} />}
          {buffer.snapshot.eol === 'mixed' && <button type="button" className="btn-secondary m-2 text-xs" disabled={controller.handoffPending} onClick={() => void launchExternal(location)}>{t('documents.openExternal')}</button>}
        </div>}
        {failed && <div role="alert" className="p-4 text-xs">
          <p>{t(documentProblemKey(failed.code))}</p>
          {['too_large', 'unsupported_format'].includes(failed.code) && <button type="button" disabled={controller.handoffPending} onClick={() => void launchExternal(failed.location)}>{t('documents.openExternal')}</button>}
          <button type="button" onClick={controller.clearFailedOpen}>{t('documents.dismiss')}</button>
        </div>}
        {externalError?.scope === externalScope && <p role="status" className="p-3 text-xs">{t(documentProblemKey(externalError.code))}</p>}
        </div>
        {reviews?.tabs.map(tab => <div key={tab.id} className="min-h-0 flex-1" style={{ display: reviews.activeTabId === tab.id ? undefined : 'none' }}><GitReviewEditor tab={tab} /></div>)}
      </main>
    </div>
  </section>;
}
