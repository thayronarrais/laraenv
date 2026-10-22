import { GitBranch, GitCommitHorizontal, RefreshCw, Upload, Search, CheckCircle2, ChevronDown, ChevronRight } from 'lucide-react';
import { operationLabels, stateLabels } from './labels';
import { useEffect, useState } from 'react';
import { useT } from '../../lib/i18n';
import type { WorkspaceDocumentRef } from '../documents/types';
import type { GitReviewOrigin } from './gitReviewOperations';
import { GitChangesTree, type GitTreeCollapse } from './GitChangesTree';
import type { ExpectedRepositoryState, GitLayer, GitOperation, RepositoryRef, SourceControlController } from './types';
export type SourceControlPanelProps = { controller: SourceControlController; onOpenReview(ref: RepositoryRef, expected: ExpectedRepositoryState, entryId: string, layer: GitLayer, root: GitReviewOrigin): void; onOpenDocument(ref: WorkspaceDocumentRef): void; onClaimNavigator(): void };
function branchRef(value: string): string | null {
  const branch = value.startsWith('refs/heads/') ? value.slice(11) : value;
  if (!branch || branch.startsWith('-') || branch.startsWith('+') || branch === '@' || /[\s~^:?*\[\\\x00-\x1f\x7f]/.test(branch) || branch.includes('..') || branch.includes('@{') || branch.startsWith('/') || branch.endsWith('/') || branch.endsWith('.') || branch.includes('//') || branch.split('/').some(part => part.startsWith('.') || part.endsWith('.lock')) || (value.startsWith('refs/') && !value.startsWith('refs/heads/'))) return null;
  return `refs/heads/${branch}`;
}
export function SourceControlPanel({ controller: c, onOpenReview, onOpenDocument, onClaimNavigator }: SourceControlPanelProps) {
  const t = useT(); const s = c.snapshot;
  const [collapseByCheckout, setCollapseByCheckout] = useState<Record<string, GitTreeCollapse>>({});
  const collapseKey = (layer: GitLayer) => JSON.stringify([s?.repository.repositoryKey, s?.repository.rootKey, layer]);
  const toggle = (layer: GitLayer, path: string) => { const key = collapseKey(layer); setCollapseByCheckout(previous => ({ ...previous, [key]: { ...previous[key], [path]: previous[key]?.[path] !== true } })); };
  const [remoteId, setRemoteId] = useState(''); const [branch, setBranch] = useState(''); const [setUpstream, setSetUpstream] = useState(false);
  const [historyOpen, setHistoryOpen] = useState(false);
  useEffect(() => { setHistoryOpen(false); }, [s?.repository.repositoryKey, s?.repository.rootKey]);
  useEffect(() => { setRemoteId(''); setBranch(s?.branch ?? ''); setSetUpstream(false); }, [s?.repository.repositoryKey, s?.repository.rootKey, s?.branch]);
  const running = c.operations.some(op => op.state === 'running' || op.state === 'cancelling'); const busy = !!s?.busy || running || c.loading;
  const remote = s?.remotes.find(item => item.id === remoteId && item.canPush); const target = branchRef(branch);
  const hasIndex = s?.entries.some(entry => !['', '.', '?'].includes(entry.indexCode));
  const completed = c.operations.filter(op => op.state === 'succeeded').sort((a, b) => (Date.parse(a.finishedAt || a.startedAt) || 0) - (Date.parse(b.finishedAt || b.startedAt) || 0));
  const latest = completed[completed.length - 1];
  const renderOperation = (op: GitOperation) => <div key={op.id} className="border-t border-white/10 p-2 text-xs"><p role={op.state === 'failed' || op.state === 'uncertain' ? 'alert' : 'status'}>{t(operationLabels[op.kind])}: {t(stateLabels[op.state])}</p><p>{op.message}</p>{op.output && <pre className="max-h-40 overflow-auto whitespace-pre-wrap break-words">{op.output}</pre>}{op.outputTruncated && <p>{t('git.outputTruncated')}</p>}{(op.state === 'running' || op.state === 'cancelling') && <button type="button" disabled={op.state === 'cancelling'} onClick={() => void c.cancelOperation(op.id)}>{t('git.cancel')}</button>}</div>;
  return <section className="git-panel" aria-label={t('git.title')} onFocusCapture={onClaimNavigator} onPointerDownCapture={onClaimNavigator}>
    <header className="git-panel-heading"><h2><GitBranch aria-hidden="true"/>{t('git.title')}</h2><button type="button" className="workspace-icon-button" aria-label={t('git.refresh')} title={t('git.refresh')} disabled={busy} onClick={() => void c.refresh()}><RefreshCw aria-hidden="true"/></button></header>
    {c.loading && <p role="status">{t('git.loading')}</p>}
    {c.error && <p role="alert" className="break-words p-2 text-xs">{t('git.failed')}: {c.error.message}</p>}
    {!s && !c.loading && <p className="p-2 text-sm">{t('git.unavailable')}</p>}
    {s && <>
      <div className="git-checkout"><p className="git-checkout-name" title={s.root.path}>{s.root.label}</p><p className="git-branch"><GitBranch aria-hidden="true"/>{s.detached ? t('git.detached') : s.branch}</p><p>{s.upstream || t('git.noUpstream')}</p>{s.countsKnown && <p>{t('git.ahead')}: {s.ahead} · {t('git.behind')}: {s.behind}</p>}{s.gitOperation && <p>{s.gitOperation}</p>}</div>
      <label className="git-search"><Search aria-hidden="true"/><span className="sr-only">{t('git.search')}</span><input className="git-input" placeholder={t('git.search')} value={c.query} onChange={event => c.setQuery(event.target.value)} /></label>
      {s.entries.length === 0 && <p role="status" className="p-2 text-sm">{t('git.clean')}</p>}
      <div className="git-form">
        <p>{t('git.wholeIndex')}</p><label className="block">{t('git.message')}<textarea className="git-input" value={c.message} onChange={event => c.setMessage(event.target.value)} /></label>
        <button type="button" className="git-commit" disabled={busy || !hasIndex || !c.message.trim() || s.hasConflicts || !!s.gitOperation} onClick={() => void c.commit()}><GitCommitHorizontal aria-hidden="true"/>{t('git.commit')}</button>
        {s.hasConflicts && <p role="status">{t('git.conflicts')}</p>}
      </div>
      <div className="git-changes-scroll">
      {(['index', 'worktree'] as const).map(layer => <GitChangesTree key={layer} entries={s.entries} layer={layer} query={c.query} collapsed={collapseByCheckout[collapseKey(layer)] ?? {}} onToggle={path => toggle(layer, path)} selectedIds={layer === 'index' ? c.selectedIndexIds : c.selectedWorktreeIds} disabled={busy}
        onSelect={(id, selected) => c.select(layer, id, selected)} onReview={id => onOpenReview(s.repository, { snapshotRevision: s.snapshotRevision, indexRevision: s.indexRevision, headOID: s.headOID, branch: s.branch }, id, layer, { label: s.root.label, path: s.root.path })}
        onAction={ids => void (layer === 'index' ? c.unstage(ids) : c.stage(ids))} onOpenDocument={entry => onOpenDocument({ context: s.repository.context, rootKey: s.repository.rootKey, relativePath: entry.relativePath, mode: 'source' })} />)}
      </div>
      <div className="git-form">
        <label className="block">{t('git.remote')}<select className="git-input" value={remoteId} onChange={event => setRemoteId(event.target.value)}><option value="">{t('git.chooseRemote')}</option>{s.remotes.map(item => <option key={item.id} value={item.id} disabled={!item.canPush}>{item.name} — {item.displayURL}{item.disabledReason ? ` (${item.disabledReason})` : ''}</option>)}</select></label>
        <label className="block">{t('git.remoteBranch')}<input className="git-input" value={branch} onChange={event => setBranch(event.target.value)} /></label>
        <label className="block"><input type="checkbox" checked={setUpstream} onChange={event => setSetUpstream(event.target.checked)} /> {t('git.setUpstream')}</label>
        <button type="button" className="git-push" disabled={busy || !remote || !target || s.detached || s.unborn || !!s.gitOperation} onClick={() => { if (remote && target) void c.push({ remoteId: remote.id, remoteBranch: target, setUpstream }); }}><Upload aria-hidden="true"/>{t('git.push')}</button>
      </div>
    </>}
    {c.operations.filter(op => op.state !== 'succeeded').map(renderOperation)}
    {latest && <div>
      <button type="button" className="git-operation-summary" aria-expanded={historyOpen} aria-controls="git-success-history" aria-label={`${t('git.operationHistory')}: ${t(operationLabels[latest.kind])}: ${t(stateLabels[latest.state])} (${completed.length})`} title={`${t('git.operationHistory')}: ${latest.message || t(stateLabels[latest.state])}`} onClick={() => setHistoryOpen(open => !open)}>
        <CheckCircle2 aria-hidden="true"/><span role="status">{t(operationLabels[latest.kind])}: {t(stateLabels[latest.state])}</span><small>{completed.length}</small>{historyOpen ? <ChevronDown aria-hidden="true"/> : <ChevronRight aria-hidden="true"/>}
      </button>
      <div id="git-success-history" hidden={!historyOpen}>{historyOpen && completed.map(renderOperation)}</div>
    </div>}
  </section>;
}
