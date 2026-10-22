import { useEffect, useId, useRef } from 'react';
import { AlertTriangle, CheckCircle2, ChevronLeft, ChevronRight, Search, X } from 'lucide-react';
import { useI18n } from '../lib/i18n';
import type { DoctorReport } from './SiteDoctorModal';
import TypeBadge from './TypeBadge';

export type DoctorOverviewState = {
  search: string;
  filter: 'all' | 'attention' | 'healthy';
  page: number;
  pageSize: 10 | 20 | 50;
};
export const initialDoctorOverviewState: DoctorOverviewState = { search: '', filter: 'all', page: 1, pageSize: 10 };
const filterKeys = {
  all: 'projects.doctor.overview.all',
  attention: 'projects.doctor.overview.attention',
  healthy: 'projects.doctor.overview.healthy',
} as const;

export default function SiteDoctorOverview({ reports, state, onChange, onSelect }: {
  reports: DoctorReport[];
  state: DoctorOverviewState;
  onChange: (next: DoctorOverviewState) => void;
  onSelect: (project: string) => void;
}) {
  const { t, formatNumber } = useI18n();
  const id = useId();
  const listRef = useRef<HTMLUListElement>(null);
  const healthyCount = reports.filter(report => report.healthy).length;
  const counts = { all: reports.length, attention: reports.length - healthyCount, healthy: healthyCount };
  const search = state.search.trim().toLowerCase();
  const filtered = reports.filter(report => {
    const matchesHealth = state.filter === 'all' || (state.filter === 'healthy' ? report.healthy : !report.healthy);
    return matchesHealth && (!search || report.project.toLowerCase().includes(search) || report.path.toLowerCase().includes(search));
  }).sort((left, right) => {
    if (left.healthy !== right.healthy) return left.healthy ? 1 : -1;
    // Fixed lexical comparison keeps ordering independent of the active UI locale.
    const leftName = left.project.toLowerCase();
    const rightName = right.project.toLowerCase();
    if (leftName !== rightName) return leftName < rightName ? -1 : 1;
    return left.project < right.project ? -1 : left.project > right.project ? 1 : 0;
  });
  const pages = Math.max(1, Math.ceil(filtered.length / state.pageSize));
  const page = Math.max(1, Math.min(state.page, pages));
  const offset = (page - 1) * state.pageSize;
  const visible = filtered.slice(offset, offset + state.pageSize);
  const start = filtered.length ? offset + 1 : 0;
  const end = Math.min(offset + state.pageSize, filtered.length);

  useEffect(() => {
    if (state.page !== page) onChange({ ...state, page });
  }, [state, page, onChange]);

  useEffect(() => {
    if (listRef.current) listRef.current.scrollTop = 0;
  }, [page, state.search, state.filter, state.pageSize]);

  const buttonClass = 'rounded-lg border border-white/15 px-3 py-2 text-sm text-white/80 transition hover:bg-white/10 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent disabled:cursor-not-allowed disabled:opacity-40';

  return <div className="flex min-h-[420px] flex-1 flex-col gap-3">
    <div className="shrink-0 space-y-3">
      <div className="grid grid-cols-3 gap-2">
        {(['all', 'attention', 'healthy'] as const).map(filter => <button
          key={filter} type="button" aria-pressed={state.filter === filter}
          onClick={() => onChange({ ...state, filter, page: 1 })}
          className={`rounded-lg border px-3 py-2.5 text-left transition focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent ${state.filter === filter ? 'border-accent/60 bg-accent/15' : 'border-white/10 bg-white/[0.03] hover:bg-white/[0.07]'}`}
        >
          <span className="block text-xs text-white/70">{t(filterKeys[filter])}</span>{' '}
          <span className={`block text-xl font-semibold tabular-nums ${filter === 'attention' ? 'text-amber-200' : filter === 'healthy' ? 'text-emerald-200' : 'text-white'}`}>{formatNumber(counts[filter])}</span>
        </button>)}
      </div>
      <p className="text-xs leading-relaxed text-white/55">{t('projects.doctor.overview.healthHint')}</p>
      <div>
        <label htmlFor={`${id}-search`} className="mb-1.5 block text-xs font-medium text-white/70">{t('projects.doctor.overview.searchLabel')}</label>
        <div className="relative">
          <Search aria-hidden="true" size={16} className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-white/40" />
          <input id={`${id}-search`} type="text" value={state.search}
            onChange={event => onChange({ ...state, search: event.target.value, page: 1 })}
            placeholder={t('projects.doctor.overview.searchPlaceholder')}
            className="w-full rounded-lg border border-white/15 bg-black/20 py-2 pl-9 pr-10 text-sm text-white placeholder:text-white/35 focus:border-accent/60 focus:outline-none focus:ring-1 focus:ring-accent/60"
          />
          <button type="button" aria-label={t('projects.doctor.overview.clearSearch')} disabled={!state.search}
            onClick={() => onChange({ ...state, search: '', page: 1 })}
            className="absolute right-1.5 top-1/2 -translate-y-1/2 rounded-md p-1.5 text-white/60 hover:bg-white/10 hover:text-white focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent disabled:opacity-25"
          ><X aria-hidden="true" size={16} /></button>
        </div>
      </div>
    </div>

    <ul ref={listRef} className="min-h-0 flex-1 space-y-1.5 overflow-y-auto pr-1">
      {visible.map((report, index) => {
        const failures = report.findings.filter(finding => finding.status === 'fail').length;
        const warnings = report.findings.filter(finding => finding.status === 'warning').length;
        const optional = report.findings.filter(finding => finding.status === 'optional').length;
        const skipped = report.findings.filter(finding => finding.status === 'skipped').length;
        const paused = report.findings.some(finding => finding.code === 'project_paused');
        const descriptionID = `${id}-project-${index}`;
        return <li key={report.project}>
          <button type="button" aria-label={t('projects.doctor.overview.openProject', { project: report.project })}
            aria-describedby={descriptionID} onClick={() => onSelect(report.project)}
            className="flex w-full items-center gap-3 rounded-lg border border-white/10 bg-white/[0.03] px-3 py-2 text-left transition hover:border-white/20 hover:bg-white/[0.07] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-accent"
          >
            {report.healthy ? <CheckCircle2 aria-hidden="true" size={18} className="shrink-0 text-emerald-300" /> : <AlertTriangle aria-hidden="true" size={18} className="shrink-0 text-amber-300" />}
            <div className="flex min-w-0 flex-1 flex-col gap-1.5 sm:flex-row sm:items-center sm:gap-4">
              <div className="min-w-0 sm:flex-1">
                <div className="truncate text-sm font-medium text-white" title={report.project}>{report.project}</div>
                <div className="truncate text-[11px] leading-4 text-white/40" title={report.path}>{report.path}</div>
              </div>
              <div id={descriptionID} className="space-y-0.5 text-xs sm:max-w-[65%] sm:shrink-0">
                <div className="flex flex-wrap items-center gap-x-2 gap-y-0.5 sm:justify-end">
                  <TypeBadge kind={report.kind} />
                  <span className={report.healthy ? 'text-emerald-200' : 'text-amber-200'}>{t(report.healthy ? 'projects.doctor.healthy' : 'projects.doctor.attention')}</span>
                  {paused && <span className="rounded border border-white/15 bg-white/5 px-1.5 py-0.5 text-[10px] text-white/65">{t('projects.doctor.overview.paused')}</span>}
                </div>
                <div className="flex flex-wrap items-center gap-x-2 gap-y-0.5 sm:justify-end">
                  <span className={failures ? 'text-red-200' : 'text-white/45'}>{t('projects.doctor.overview.failures', { count: failures })}</span>
                  <span className={warnings ? 'text-amber-200' : 'text-white/45'}>{t('projects.doctor.overview.warnings', { count: warnings })}</span>
                  {optional > 0 && <span className="text-sky-200">{t('projects.doctor.overview.optional', { count: optional })}</span>}
                  {skipped > 0 && <span className="rounded border border-white/10 px-1.5 py-0.5 text-[10px] text-white/50">{t('projects.doctor.overview.skipped', { count: skipped })}</span>}
                </div>
              </div>
            </div>
            <ChevronRight aria-hidden="true" size={16} className="shrink-0 text-white/35" />
          </button>
        </li>;
      })}
      {!visible.length && <li className="rounded-lg border border-dashed border-white/15 px-4 py-8 text-center text-sm text-white/60">{reports.length ? t('projects.doctor.overview.noMatch') : t('projects.doctor.overview.noProjects')}</li>}
    </ul>

    <div className="shrink-0 space-y-2 border-t border-white/10 pt-3">
      <div className="flex flex-wrap items-center justify-between gap-2 text-xs text-white/60">
        <span role="status" aria-live="polite">{t('projects.doctor.overview.showing', { start, end, total: filtered.length })}</span>
        <div className="flex items-center gap-2">
          <label htmlFor={`${id}-page-size`}>{t('projects.doctor.overview.pageSize')}</label>
          <select id={`${id}-page-size`} value={state.pageSize}
            onChange={event => onChange({ ...state, pageSize: Number(event.target.value) as DoctorOverviewState['pageSize'], page: 1 })}
            className="rounded-md border border-white/15 bg-[#19152e] px-2 py-1.5 text-xs text-white/80 focus:outline-none focus:ring-2 focus:ring-accent"
          >{([10, 20, 50] as const).map(count => <option key={count} value={count}>{t('projects.doctor.overview.perPage', { count })}</option>)}</select>
        </div>
      </div>
      <nav aria-label={t('projects.doctor.overview.pagination')} className="flex items-center justify-between gap-2">
        <button type="button" disabled={page <= 1} onClick={() => onChange({ ...state, page: page - 1 })} className={`${buttonClass} flex items-center gap-1.5`}>
          <ChevronLeft aria-hidden="true" size={15} />{t('projects.doctor.overview.previous')}
        </button>
        <span className="text-xs text-white/60">{t('projects.doctor.overview.page', { page, pages })}</span>
        <button type="button" disabled={page >= pages} onClick={() => onChange({ ...state, page: page + 1 })} className={`${buttonClass} flex items-center gap-1.5`}>
          {t('projects.doctor.overview.next')}<ChevronRight aria-hidden="true" size={15} />
        </button>
      </nav>
    </div>
  </div>;
}
