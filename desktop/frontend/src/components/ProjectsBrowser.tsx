import { useEffect, useId, useMemo, useRef, useState } from 'react';
import { ChevronDown, ChevronLeft, ChevronRight, Code2, ExternalLink, Folder, Search, Tag, TerminalSquare, X } from 'lucide-react';
import { useI18n } from '../lib/i18n';
import PortalMenu from './PortalMenu';
import TypeBadge from './TypeBadge';
import JSBadge from './JSBadge';
import { ProjectRuntimeBadge, type ProjectRuntimeResolution } from './ProjectRuntimeSelector';
import { ProjectActionsPicker, useProjectQuickActions } from './ProjectQuickActions';
import './ProjectsBrowser.css';

export type ProjectBrowserItem = {
  name: string; path: string; url: string; kind: string;
  phpVersion: string; nodeVersion: string;
  phpRuntime?: ProjectRuntimeResolution; nodeRuntime?: ProjectRuntimeResolution;
  jsFramework: string; packageManager: string; tags: string[]; paused?: boolean;
};
type ProjectsBrowserProps = {
  projects: ProjectBrowserItem[]; selectedName: string | null; onSelect(name: string | null): void;
  tagFilter: string; onTagFilterChange(tag: string): void;
  onOpenEditor(name: string): void; onOpenURL(url: string): void;
  onOpenTerminal(name: string): void; onOpenFolder(name: string): void;
  defaultPhp: string; defaultNode: string;
};

const naturalNames = new Intl.Collator('en', { numeric: true, sensitivity: 'base' });
const normalizeSearch = (value: string) => value.normalize('NFD').replace(/\p{M}/gu, '').toLowerCase().trim();
const tagKey = (value: string) => value.trim().toLowerCase();
const projectTags = (project: ProjectBrowserItem) => [...new Set((project.tags ?? []).map(tag => tag.trim()).filter(Boolean))];

export default function ProjectsBrowser({ projects, selectedName, onSelect, tagFilter, onTagFilterChange, onOpenEditor, onOpenURL, onOpenTerminal, onOpenFolder, defaultPhp, defaultNode }: ProjectsBrowserProps) {
  const { t, formatNumber } = useI18n();
  const id = useId();
  const [search, setSearch] = useState('');
  const [tagSearch, setTagSearch] = useState('');
  const [tagsOpen, setTagsOpen] = useState(false);
  const [actionsOpen, setActionsOpen] = useState(false);
  const quickActions = useProjectQuickActions();
  const [pagination, setPagination] = useState({ page: 1, size: 10, tag: tagFilter });
  const tagButtonRef = useRef<HTMLButtonElement>(null);
  const listRef = useRef<HTMLDivElement>(null);
  const tags = useMemo(() => {
    const counts = new Map<string, { label: string; count: number }>();
    for (const project of projects) {
      const seen = new Set<string>();
      for (const tag of projectTags(project)) {
        const key = tagKey(tag);
        if (seen.has(key)) continue;
        seen.add(key);
        const entry = counts.get(key);
        if (entry) entry.count++;
        else counts.set(key, { label: tag, count: 1 });
      }
    }
    return [...counts.values()].sort((left, right) => naturalNames.compare(left.label, right.label));
  }, [projects]);
  const untaggedCount = projects.filter(project => projectTags(project).length === 0).length;
  const needle = normalizeSearch(search);
  const filtered = useMemo(() => projects.filter(project => {
    const labels = projectTags(project);
    const matchesTag = tagFilter === '__all__' || (tagFilter === '__untagged__'
      ? labels.length === 0 : labels.some(tag => tagKey(tag) === tagKey(tagFilter)));
    return matchesTag && (!needle || [project.name, project.path, ...labels].some(value => normalizeSearch(value).includes(needle)));
  }).sort((left, right) => naturalNames.compare(left.name, right.name) || (left.name < right.name ? -1 : left.name > right.name ? 1 : 0)), [projects, tagFilter, needle]);
  const pages = Math.max(1, Math.ceil(filtered.length / pagination.size));
  const requestedPage = pagination.tag === tagFilter ? pagination.page : 1;
  const page = Math.max(1, Math.min(requestedPage, pages));
  const offset = (page - 1) * pagination.size;
  const visible = useMemo(() => filtered.slice(offset, offset + pagination.size), [filtered, offset, pagination.size]);
  const selectedTag = tagFilter === '__all__' ? t('projects.workspace.allTags')
    : tagFilter === '__untagged__' ? t('projects.workspace.untagged') : tagFilter;
  const selectedTagCount = tagFilter === '__all__' ? projects.length : tagFilter === '__untagged__'
    ? untaggedCount : tags.find(tag => tagKey(tag.label) === tagKey(tagFilter))?.count ?? 0;
  const searchedTags = tags.filter(tag => normalizeSearch(tag.label).includes(normalizeSearch(tagSearch)));
  const hasFilters = !!search || tagFilter !== '__all__';

  useEffect(() => {
    setPagination(previous => previous.page === page && previous.tag === tagFilter ? previous : { ...previous, page, tag: tagFilter });
  }, [page, tagFilter]);
  useEffect(() => {
    if (selectedName !== null && !visible.some(project => project.name === selectedName)) onSelect(null);
  }, [selectedName, visible, onSelect]);
  useEffect(() => {
    setTagsOpen(false);
    setActionsOpen(false);
  }, [selectedName]);
  useEffect(() => {
    if (listRef.current) listRef.current.scrollTop = 0;
  }, [page, search, tagFilter, pagination.size]);

  const chooseTag = (tag: string) => {
    setPagination(previous => ({ ...previous, page: 1, tag }));
    onTagFilterChange(tag);
    setTagsOpen(false); setTagSearch('');
    tagButtonRef.current?.focus();
  };
  const clearFilters = () => {
    setSearch(''); setTagSearch('');
    setPagination(previous => ({ ...previous, page: 1, tag: '__all__' }));
    onTagFilterChange('__all__');
  };

  return <section className="projects-browser">
    <div className="projects-browser__toolbar">
      <label className="projects-browser__search" htmlFor={`${id}-search`}>
        <Search aria-hidden="true" size={17} />
        <span className="sr-only">{t('projects.workspace.searchLabel')}</span>
        <input id={`${id}-search`} type="text" value={search} placeholder={t('projects.workspace.search')}
          onChange={event => {
            setSearch(event.target.value);
            setPagination(previous => ({ ...previous, page: 1, tag: tagFilter }));
          }} />
      </label>
      <div className="projects-browser__filters">
        <span className="projects-browser__filter-label"><Tag aria-hidden="true" size={13} /> {t('projects.workspace.tags')}</span>
        <button type="button" ref={tagButtonRef} className={`projects-browser__tag-picker${tagFilter !== '__all__' ? ' is-active' : ''}`}
          aria-label={t('projects.workspace.filterTags')} aria-expanded={tagsOpen}
          title={selectedTag} onClick={() => { setActionsOpen(false); setTagSearch(''); setTagsOpen(!tagsOpen); }}>
          <span>{selectedTag}</span><span className="projects-browser__count">{formatNumber(selectedTagCount)}</span><ChevronDown aria-hidden="true" size={13} />
        </button>
        {hasFilters && <button type="button" className="projects-browser__clear" onClick={clearFilters}>
          <X aria-hidden="true" size={13} /> {t('projects.workspace.clearFilters')}
        </button>}
        <ProjectActionsPicker {...quickActions} open={actionsOpen}
          onToggle={() => { setTagsOpen(false); setActionsOpen(!actionsOpen); }} onClose={() => setActionsOpen(false)} />
      </div>
      <PortalMenu open={tagsOpen} anchorRef={tagButtonRef} align="left" minWidth={250}
        onClose={() => { setTagsOpen(false); tagButtonRef.current?.focus(); }}>
        <div className="projects-browser__tag-menu">
          <label className="projects-browser__search projects-browser__tag-search" htmlFor={`${id}-tags`}>
            <Search aria-hidden="true" size={14} /><span className="sr-only">{t('projects.workspace.searchTags')}</span>
            <input id={`${id}-tags`} type="text" autoFocus value={tagSearch} placeholder={t('projects.workspace.searchTags')}
              onChange={event => setTagSearch(event.target.value)} />
          </label>
          <button type="button" aria-label={`${t('projects.workspace.allTags')} ${formatNumber(projects.length)}`}
            aria-pressed={tagFilter === '__all__'} onClick={() => chooseTag('__all__')}>
            <span>{t('projects.workspace.allTags')}</span><span>{formatNumber(projects.length)}</span>
          </button>
          <button type="button" aria-label={`${t('projects.workspace.untagged')} ${formatNumber(untaggedCount)}`}
            aria-pressed={tagFilter === '__untagged__'} onClick={() => chooseTag('__untagged__')}>
            <span>{t('projects.workspace.untagged')}</span><span>{formatNumber(untaggedCount)}</span>
          </button>
          <div className="projects-browser__menu-divider" />
          {searchedTags.map(tag => <button key={tagKey(tag.label)} type="button" title={tag.label}
            aria-label={`${t('projects.workspace.filterByTag', { tag: tag.label })} ${formatNumber(tag.count)}`}
            aria-pressed={tagKey(tagFilter) === tagKey(tag.label)} onClick={() => chooseTag(tag.label)}>
            <span>{tag.label}</span><span>{formatNumber(tag.count)}</span>
          </button>)}
          {searchedTags.length === 0 && <p>{t('projects.workspace.noTags')}</p>}
        </div>
      </PortalMenu>
    </div>

    <div ref={listRef} className="projects-browser__list">
      <table className="projects-browser__table">
        <colgroup><col className="projects-browser__project-col" /><col className="projects-browser__stack-col" /><col className="projects-browser__actions-col" /></colgroup>
        <thead><tr>
          <th scope="col">{t('projects.workspace.project')}</th>
          <th scope="col" className="projects-browser__stack-cell">{t('projects.workspace.stack')}</th>
          <th scope="col" className="projects-browser__actions-heading">{t('projects.workspace.actions')}</th>
        </tr></thead>
        <tbody>{visible.map(project => {
          const labels = projectTags(project);
          return <tr key={project.name} className={`projects-browser__row${selectedName === project.name ? ' is-selected' : ''}`}
            onClick={event => {
              event.currentTarget.querySelector<HTMLButtonElement>('.projects-browser__project-name')?.focus();
              onSelect(project.name);
            }}>
            <td>
              <div className="projects-browser__identity">
                <button type="button" className="projects-browser__project-name" title={project.name}
                  aria-label={t('projects.workspace.selectProject', { project: project.name })} aria-pressed={selectedName === project.name}
                  onClick={event => { event.stopPropagation(); onSelect(project.name); }}>{project.name}</button>
                {project.paused && <span className="projects-browser__paused">{t('projects.workspace.paused')}</span>}
              </div>
              <div className="projects-browser__path" title={project.path}>{project.path}</div>
              <div className="projects-browser__runtime">
                <ProjectRuntimeBadge kind="php" manualVersion={project.phpVersion} defaultVersion={defaultPhp} runtime={project.phpRuntime} />
                <span aria-hidden="true">·</span>
                <ProjectRuntimeBadge kind="nodejs" manualVersion={project.nodeVersion} defaultVersion={defaultNode} runtime={project.nodeRuntime} />
              </div>
            </td>
            <td className="projects-browser__stack-cell">
              <div className="projects-browser__stack" title={[project.kind, project.jsFramework, project.packageManager].filter(Boolean).join(' · ')}>
                <TypeBadge kind={project.kind} /><JSBadge framework={project.jsFramework} />
                {project.packageManager && <span className="projects-browser__package-manager">{project.packageManager}</span>}
              </div>
              <div className="projects-browser__row-tags" title={labels.join(', ')}>
                {labels.slice(0, 2).map(tag => <button type="button" key={tag} title={tag}
                  aria-label={t('projects.workspace.filterByTag', { tag })} onClick={event => { event.stopPropagation(); chooseTag(tag); }}>{tag}</button>)}
                {labels.length > 2 && <span className="projects-browser__more-tags" title={labels.slice(2).join(', ')}>+{formatNumber(labels.length - 2)}</span>}
              </div>
            </td>
            <td>
              <div className="projects-browser__quick-actions">
                {quickActions.actions.map((action, slot) => {
                  if (action === 'none') return null;
                  const options = {
                    editor: { icon: Code2, label: t('projects.common.editorFallback'), title: t('projects.workspace.openEditor', { project: project.name }), run: () => onOpenEditor(project.name) },
                    browser: { icon: ExternalLink, label: t('projects.row.browser'), title: t('projects.workspace.openBrowser', { project: project.name }), run: () => onOpenURL(project.url) },
                    terminal: { icon: TerminalSquare, label: t('projects.workspace.actionTerminal'), title: t('projects.workspace.openTerminal', { project: project.name }), run: () => onOpenTerminal(project.name) },
                    folder: { icon: Folder, label: t('projects.workspace.actionFolder'), title: t('projects.workspace.openFolder', { project: project.name }), run: () => onOpenFolder(project.name) },
                  }[action];
                  const Icon = options.icon;
                  return <button key={slot} type="button" title={options.title} aria-label={options.title} disabled={action === 'browser' && !project.url}
                    onClick={event => { event.stopPropagation(); options.run(); }}>
                    <Icon aria-hidden="true" size={14} /><span>{options.label}</span>
                  </button>;
                })}
              </div>
            </td>
          </tr>;
        })}</tbody>
      </table>
      {visible.length === 0 && <div className="projects-browser__empty"><Search aria-hidden="true" size={22} />
        <p>{t('projects.workspace.empty')}</p>
      </div>}
    </div>

    <footer className="projects-browser__footer">
      <p aria-live="polite">{t('projects.workspace.pagination', {
        from: filtered.length ? offset + 1 : 0, to: Math.min(offset + pagination.size, filtered.length), total: filtered.length,
      })}</p>
      <nav aria-label={t('projects.workspace.page', { page, pages })} className="projects-browser__pagination">
        <button type="button" aria-label={t('projects.workspace.previous')} title={t('projects.workspace.previous')} disabled={page === 1}
          onClick={() => setPagination(previous => ({ ...previous, page: page - 1, tag: tagFilter }))}><ChevronLeft aria-hidden="true" size={16} /></button>
        <span>{t('projects.workspace.page', { page, pages })}</span>
        <button type="button" aria-label={t('projects.workspace.next')} title={t('projects.workspace.next')} disabled={page === pages}
          onClick={() => setPagination(previous => ({ ...previous, page: page + 1, tag: tagFilter }))}><ChevronRight aria-hidden="true" size={16} /></button>
      </nav>
      <label className="projects-browser__page-size" htmlFor={`${id}-size`}>
        <span>{t('projects.workspace.pageSize')}</span>
        <select id={`${id}-size`} value={pagination.size} onChange={event => setPagination({ page: 1, size: Number(event.target.value), tag: tagFilter })}>
          {[10, 20, 50].map(size => <option key={size} value={size}>{formatNumber(size)}</option>)}
        </select>
      </label>
    </footer>
  </section>;
}
