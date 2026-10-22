import { useCallback, useEffect, useRef, useState } from 'react';
import { useT } from '../../lib/i18n';
import { workspaceDocumentApi } from './api';
import type { DirectoryEntry, DocumentLocationRef, RootInfo, WorkspaceContextRef, WorkspaceDocumentRef } from './types';
import { resolveWorkspaceURL } from './workspaceUrls';
import { documentProblemKey } from './problemKey';

type PageState = { entries: DirectoryEntry[]; nextCursor: string; loading: boolean; problem: string | null };
export type WorkspaceFileTreeProps = { context: WorkspaceContextRef; active?: boolean; onOpenDocument(ref: WorkspaceDocumentRef): void; onClaimNavigator(): void };
export function WorkspaceFileTree({ context, active = true, onOpenDocument, onClaimNavigator }: WorkspaceFileTreeProps) {
  const t = useT();
  const [rootState, setRoot] = useState<{ identity: string; info: RootInfo } | null>(null);
  const [problem, setProblem] = useState<string | null>(null);
  const [pages, setPages] = useState<Record<string, PageState>>({});
  const [expanded, setExpanded] = useState<Set<string>>(new Set(['']));
  const [path, setPath] = useState('');
  const generation = useRef(0);
  const identity = context.kind === 'project' ? `project:${context.projectName}` : `agent:${context.sessionId}`;
  const root = rootState?.identity === identity ? rootState.info : null;
  useEffect(() => {
    generation.current++; setRoot(null); setPages({}); setExpanded(new Set([''])); setProblem(null); setPath('');
  }, [identity]);
  useEffect(() => {
    generation.current++;
    // Suspend continuations without discarding completed entries or their
    // cursor. An interrupted initial page is retried on activation instead.
    setPages(old => {
      const next: Record<string, PageState> = {};
      for (const [directory, page] of Object.entries(old)) {
        if (!page.loading) next[directory] = page;
        else if (page.nextCursor) next[directory] = { ...page, loading: false };
      }
      return next;
    });
    return () => { generation.current++; };
  }, [identity, active]);
  useEffect(() => {
    if (!active || root) return;
    let current = true;
    void workspaceDocumentApi.context(context).then(result => {
      if (!current) return;
      if (result.code === 'ok' || result.code === 'changed') setRoot({ identity, info: result.value });
      else setProblem('message' in result ? result.code : 'io_error');
    }).catch(() => { if (current) setProblem('io_error'); });
    return () => { current = false; generation.current++; };
  }, [identity, active, root]);
  const load = useCallback(async (directory: string, cursor = '') => {
    if (!active || !root) return;
    const requestedGeneration = generation.current;
    const location: DocumentLocationRef = { context, rootKey: root.rootKey, relativePath: directory };
    setPages(old => ({ ...old, [directory]: { entries: cursor ? old[directory]?.entries ?? [] : [], nextCursor: cursor, loading: true, problem: null } }));
    try {
      const result = await workspaceDocumentApi.list(location, cursor);
      if (requestedGeneration !== generation.current) return;
      if (result.code === 'ok' || result.code === 'changed') setPages(old => ({ ...old, [directory]: { entries: cursor ? [...(old[directory]?.entries ?? []), ...result.value.entries] : result.value.entries, nextCursor: result.value.nextCursor, loading: false, problem: null } }));
      else if (cursor && result.code === 'stale_cursor') void load(directory);
      else setPages(old => ({ ...old, [directory]: { entries: old[directory]?.entries ?? [], nextCursor: '', loading: false, problem: 'message' in result ? result.code : 'io_error' } }));
    } catch { if (requestedGeneration === generation.current) setPages(old => ({ ...old, [directory]: { entries: old[directory]?.entries ?? [], nextCursor: '', loading: false, problem: 'io_error' } })); }
  }, [root, identity, active]);
  useEffect(() => { if (active && root) for (const directory of expanded) if (!pages[directory]) void load(directory); }, [root, load, active, expanded, pages]);
  const open = (relativePath: string) => {
    if (!root) return;
    onOpenDocument({ context, rootKey: root.rootKey, relativePath, mode: /\.md(?:own)?$/i.test(relativePath) ? 'preview' : 'source' });
  };
  const rows = (directory: string, depth: number): React.ReactNode => {
    const page = pages[directory];
    return <>
      {page?.entries.map(entry => <div key={entry.relativePath}>
        <button type="button" style={{ paddingLeft: `${depth * 14 + 8}px` }} className="block w-full truncate py-1 text-left text-xs hover:bg-white/10" disabled={entry.kind === 'blocked'}
          onClick={() => { onClaimNavigator(); if (entry.kind === 'directory') { setExpanded(old => { const next = new Set(old); if (next.has(entry.relativePath)) next.delete(entry.relativePath); else next.add(entry.relativePath); return next; }); } else open(entry.relativePath); }}>
          {entry.kind === 'directory' ? expanded.has(entry.relativePath) ? '▾ ' : '▸ ' : '  '}{entry.name}
        </button>
        {entry.kind === 'directory' && expanded.has(entry.relativePath) && rows(entry.relativePath, depth + 1)}
      </div>)}
      {page?.loading && <p className="px-2 text-xs">{t('documents.loading')}</p>}
      {page?.problem && <p role="status" className="px-2 text-xs">{t(documentProblemKey(page.problem))} <button type="button" onClick={() => void load(directory)}>{t('documents.retry')}</button></p>}
      {page?.nextCursor && !page.loading && <button type="button" className="px-2 text-xs" onClick={() => void load(directory, page.nextCursor)}>{t('documents.loadMore')}</button>}
    </>;
  };
  return <section className="flex h-full min-h-0 flex-col border-r border-white/10" aria-label={t('documents.files')} onPointerDownCapture={onClaimNavigator} onFocusCapture={onClaimNavigator}>
    <header className="border-b border-white/10 p-2 text-xs">{root?.label ?? t('documents.files')}{root?.path && <span title={root.path} className="block truncate opacity-60">{root.path}</span>}</header>
    <form className="flex gap-1 p-2" onSubmit={event => { event.preventDefault(); const target = resolveWorkspaceURL(path, 'root.md'); if (target.kind === 'local') open(target.path); else setProblem('invalid_path'); }}>
      <input aria-label={t('documents.relativePath')} value={path} onChange={event => setPath(event.target.value)} className="min-w-0 flex-1 rounded bg-white/10 px-2 py-1 text-xs" />
      <button type="submit" disabled={!root} className="btn-secondary text-xs">{t('documents.open')}</button>
    </form>
    {problem && <p role="status" className="px-2 text-xs">{t(documentProblemKey(problem))}</p>}
    <div className="min-h-0 flex-1 overflow-auto">{rows('', 0)}</div>
  </section>;
}
