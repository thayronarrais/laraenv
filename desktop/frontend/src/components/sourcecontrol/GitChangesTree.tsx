import { ChevronDown, ChevronRight, FileCode2, Folder, FolderOpen, Minus, Plus, ExternalLink } from 'lucide-react';
import { layerLabels, selectionLabels } from './labels';
import { useT } from '../../lib/i18n';
import type { GitEntry, GitLayer } from './types';
export type GitTreeCollapse = Record<string, boolean>;
export type GitChangesTreeProps = { entries: GitEntry[]; layer: GitLayer; selectedIds: string[]; query: string; collapsed: GitTreeCollapse; onToggle(path: string): void; disabled?: boolean; onSelect(id: string, selected: boolean): void; onReview(id: string): void; onAction(ids: string[]): void; onOpenDocument(entry: GitEntry): void };
type FolderNode = { name: string; path: string; folders: Map<string, FolderNode>; files: GitEntry[]; descendants: GitEntry[] };
export function GitChangesTree({ entries, layer, selectedIds, query, collapsed, onToggle, disabled = false, onSelect, onReview, onAction, onOpenDocument }: GitChangesTreeProps) {
  const t = useT(); const display = (entry: GitEntry) => entry.displayPath || entry.relativePath;
  const inLayer = entries.filter(entry => layer === 'index' ? !['', '.', '?'].includes(entry.indexCode) : entry.kind === 'untracked' || !['', '.'].includes(entry.worktreeCode));
  const normalizedQuery = query.trim().toLocaleLowerCase();
  const searching = !!normalizedQuery;
  const filtered = inLayer.filter(entry => `${display(entry)} ${entry.originalPath}`.toLocaleLowerCase().includes(normalizedQuery));
  const root: FolderNode = { name: '', path: '', folders: new Map(), files: [], descendants: [] };
  for (const entry of filtered) {
    const parts = display(entry).split('/'); parts.pop(); let node = root;
    for (const name of parts) {
      const path = node.path ? `${node.path}/${name}` : name;
      if (!node.folders.has(name)) node.folders.set(name, { name, path, folders: new Map(), files: [], descendants: [] });
      node = node.folders.get(name)!; node.descendants.push(entry);
    }
    node.files.push(entry);
  }
  const sort = (a: string, b: string) => a.toLowerCase().localeCompare(b.toLowerCase()) || a.localeCompare(b);
  const eligible = (entry: GitEntry) => layer === 'index' ? entry.canUnstage : entry.canStage;
  // Selection comes from the whole layer, even when search/collapse hides it.
  const selected = inLayer.filter(entry => selectedIds.includes(entry.id) && eligible(entry)).map(entry => entry.id);
  const ActionIcon = layer === 'index' ? Minus : Plus;
  const renderNode = (node: FolderNode, depth: number) => <ul className="git-tree-list">
    {[...node.folders.values()].sort((a,b) => sort(a.name,b.name)).map(folder => {
      const expanded = searching || collapsed[folder.path] !== true;
      const ids = folder.descendants.filter(eligible).map(entry => entry.id);
      const label = `${t(layer === 'index' ? 'git.unstageFolder' : 'git.stageFolder')} ${folder.path} (${ids.length})`;
      const FolderIcon = expanded ? FolderOpen : Folder;
      return <li key={folder.path}>
        <div className="git-tree-row" style={{ paddingLeft: 8 + Math.min(depth, 6) * 12 }}>
          <button type="button" className="git-folder-toggle" disabled={searching} aria-label={folder.path} aria-expanded={expanded} title={folder.path} onClick={() => onToggle(folder.path)}>
            {expanded ? <ChevronDown aria-hidden="true"/> : <ChevronRight aria-hidden="true"/>}<FolderIcon aria-hidden="true"/><span>{folder.name}</span><small>{folder.descendants.length}</small>
          </button>
          <button type="button" className="git-action" aria-label={label} title={label} disabled={disabled || !ids.length} onClick={() => onAction(ids)}><ActionIcon aria-hidden="true"/></button>
        </div>
        {expanded && renderNode(folder, depth + 1)}
      </li>;
    })}
    {node.files.sort((a,b) => sort(display(a),display(b))).map(entry => {
      const path = display(entry); const action = `${t(layer === 'index' ? 'git.unstageFile' : 'git.stageFile')} ${path}`;
      const code = layer === 'index' ? entry.indexCode : entry.worktreeCode;
      const status = entry.disabledReason || t(entry.kind === 'untracked' ? 'git.status.untracked' : 'git.status.changed');
      return <li key={entry.id} className="git-tree-row" style={{ paddingLeft: 8 + Math.min(depth, 6) * 12 }} title={entry.originalPath ? `${entry.originalPath} → ${path}` : path}>
        <input type="checkbox" aria-label={`${t(selectionLabels[layer])} ${path}`} checked={selectedIds.includes(entry.id)} disabled={disabled || !eligible(entry)} onChange={event => onSelect(entry.id, event.target.checked)}/>
        <FileCode2 aria-hidden="true" className="git-file-icon"/>
        <button type="button" className="git-file-name" title={path} aria-label={`${t('git.review')} ${path}`} onClick={() => onReview(entry.id)}>{path.split('/').pop()}</button>
        <span className={`git-status ${eligible(entry) ? 'git-status-modified' : 'git-status-blocked'}`} title={status} aria-label={status}>{code === '?' ? 'U' : code}</span>
        <button type="button" className="git-action" aria-label={`${t('git.openFile')} ${path}`} title={`${t('git.openFile')} ${path}`} disabled={!entry.relativePath} onClick={() => { if (entry.relativePath) onOpenDocument(entry); }}><ExternalLink aria-hidden="true"/></button>
        <button type="button" className="git-action git-stage-action" disabled={disabled || !eligible(entry)} title={entry.disabledReason || action} aria-label={action} onClick={() => onAction([entry.id])}><ActionIcon aria-hidden="true"/></button>
      </li>;
    })}
  </ul>;
  const expanded = searching || collapsed[''] !== true;
  return <section role="group" aria-label={t(layerLabels[layer])} className="git-change-section">
    <div className="git-section-heading"><button type="button" className="git-section-toggle" disabled={searching} aria-label={t(layerLabels[layer])} aria-expanded={expanded} onClick={() => onToggle('')}>
      {expanded ? <ChevronDown aria-hidden="true"/> : <ChevronRight aria-hidden="true"/>}<span>{t(layerLabels[layer])}</span><small>{inLayer.length}</small>
    </button><button type="button" className="git-action" title={t(layer === 'index' ? 'git.unstageSelected' : 'git.stageSelected')} aria-label={t(layer === 'index' ? 'git.unstageSelected' : 'git.stageSelected')} disabled={disabled || !selected.length} onClick={() => onAction(selected)}><ActionIcon aria-hidden="true"/></button></div>
    {expanded && renderNode(root, 0)}
  </section>;
}
