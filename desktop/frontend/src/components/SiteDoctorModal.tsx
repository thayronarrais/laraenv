import { useEffect, useRef, useState } from 'react';
import { AlertTriangle, ArrowLeft, CheckCircle2, ChevronRight, CircleX, Download, Info, Link2, Loader2, MinusCircle, RefreshCw, Square, Stethoscope, Wrench, X } from 'lucide-react';
import { ApplyDoctorFix, CancelDoctorFix, DiagnoseAllProjects, DiagnoseProject } from '../../wailsjs/go/main/App';
import type { main, sitedoctor } from '../../wailsjs/go/models';
import { EventsOn } from '../../wailsjs/runtime';
import { useDialogs } from './dialogs/DialogProvider';
import { useT } from '../lib/i18n';
import SiteDoctorOverview, { initialDoctorOverviewState, type DoctorOverviewState } from './SiteDoctorOverview';

export type DoctorFinding = sitedoctor.Finding;
export type DoctorReport = Omit<sitedoctor.Report, 'convertValues'>;
type DoctorFixRequest = main.DoctorFixRequest;
const checkKeys: Record<string, string> = {
  env: 'projects.doctor.checks.env', app_key: 'projects.doctor.checks.app_key',
  composer_dependencies: 'projects.doctor.checks.composer_dependencies', node_dependencies: 'projects.doctor.checks.node_dependencies',
  storage_link: 'projects.doctor.checks.storage_link', database: 'projects.doctor.checks.database', migrations: 'projects.doctor.checks.migrations',
  php_runtime: 'projects.doctor.checks.php_runtime', node_runtime: 'projects.doctor.checks.node_runtime', vhost: 'projects.doctor.checks.vhost',
  hosts: 'projects.doctor.checks.hosts', php_fpm: 'projects.doctor.checks.php_fpm', wp_config: 'projects.doctor.checks.wp_config',
  document_root: 'projects.doctor.checks.document_root',
};
const messageKeys: Record<string, string> = {
  document_root_ok: 'projects.doctor.messages.document_root_ok', document_root_missing: 'projects.doctor.messages.document_root_missing',
  document_root_unreadable: 'projects.doctor.messages.document_root_unreadable', env_ok: 'projects.doctor.messages.env_ok',
  env_missing: 'projects.doctor.messages.env_missing', env_unreadable: 'projects.doctor.messages.env_unreadable',
  env_too_large: 'projects.doctor.messages.env_too_large', env_unsupported: 'projects.doctor.messages.env_unsupported',
  app_key_ok: 'projects.doctor.messages.app_key_ok', app_key_missing: 'projects.doctor.messages.app_key_missing',
  app_key_unsupported: 'projects.doctor.messages.app_key_unsupported', composer_dependencies_ok: 'projects.doctor.messages.composer_dependencies_ok',
  composer_dependencies_missing: 'projects.doctor.messages.composer_dependencies_missing', node_dependencies_ok: 'projects.doctor.messages.node_dependencies_ok',
  node_dependencies_missing: 'projects.doctor.messages.node_dependencies_missing', storage_link_ok: 'projects.doctor.messages.storage_link_ok',
  storage_link_missing: 'projects.doctor.messages.storage_link_missing', storage_link_conflict: 'projects.doctor.messages.storage_link_conflict',
  storage_link_unreadable: 'projects.doctor.messages.storage_link_unreadable', database_ok: 'projects.doctor.messages.database_ok',
  database_missing: 'projects.doctor.messages.database_missing', database_config_unsupported: 'projects.doctor.messages.database_config_unsupported',
  database_port_invalid: 'projects.doctor.messages.database_port_invalid',
  database_unreachable: 'projects.doctor.messages.database_unreachable', database_probe_failed: 'projects.doctor.messages.database_probe_failed',
  database_external: 'projects.doctor.messages.database_external', database_unsupported: 'projects.doctor.messages.database_unsupported',
  database_name_unsupported: 'projects.doctor.messages.database_name_unsupported', database_extension_missing: 'projects.doctor.messages.database_extension_missing',
  database_auth_failed: 'projects.doctor.messages.database_auth_failed', migrations_ok: 'projects.doctor.messages.migrations_ok',
  migrations_pending: 'projects.doctor.messages.migrations_pending', migrations_unavailable: 'projects.doctor.messages.migrations_unavailable',
  runtime_ok: 'projects.doctor.messages.runtime_ok', runtime_missing: 'projects.doctor.messages.runtime_missing',
  runtime_invalid: 'projects.doctor.messages.runtime_invalid', runtime_incompatible: 'projects.doctor.messages.runtime_incompatible',
  runtime_unmanaged: 'projects.doctor.messages.runtime_unmanaged', vhost_ok: 'projects.doctor.messages.vhost_ok',
  runtime_path_missing: 'projects.doctor.messages.runtime_path_missing',
  vhost_missing: 'projects.doctor.messages.vhost_missing', vhost_mismatch: 'projects.doctor.messages.vhost_mismatch',
  vhost_unreadable: 'projects.doctor.messages.vhost_unreadable', vhost_unavailable: 'projects.doctor.messages.vhost_unavailable',
  hosts_ok: 'projects.doctor.messages.hosts_ok', hosts_missing: 'projects.doctor.messages.hosts_missing',
  hosts_unreadable: 'projects.doctor.messages.hosts_unreadable', php_fpm_ok: 'projects.doctor.messages.php_fpm_ok',
  php_fpm_stopped: 'projects.doctor.messages.php_fpm_stopped', php_fpm_unavailable: 'projects.doctor.messages.php_fpm_unavailable',
  wp_config_ok: 'projects.doctor.messages.wp_config_ok', wp_config_missing: 'projects.doctor.messages.wp_config_missing',
  wp_config_unreadable: 'projects.doctor.messages.wp_config_unreadable', not_applicable: 'projects.doctor.messages.not_applicable',
  prerequisites_missing: 'projects.doctor.messages.prerequisites_missing', project_paused: 'projects.doctor.messages.project_paused',
  inspection_failed: 'projects.doctor.messages.inspection_failed',
};
const statusKeys: Record<string, string> = {
  pass: 'projects.doctor.status.pass', fail: 'projects.doctor.status.fail', warning: 'projects.doctor.status.warning', optional: 'projects.doctor.status.optional', skipped: 'projects.doctor.status.skipped',
};
const severityKeys: Record<string, string> = {
  info: 'projects.doctor.severity.info', warning: 'projects.doctor.severity.warning', error: 'projects.doctor.severity.error',
};
const statusStyles: Record<string, string> = {
  pass: 'text-emerald-300 bg-emerald-500/10', fail: 'text-red-300 bg-red-500/10', warning: 'text-amber-300 bg-amber-500/10', optional: 'text-sky-300 bg-sky-500/10', skipped: 'text-white/50 bg-white/5',
};
type ActiveFix = DoctorFixRequest & { label: string };
let operationSequence = 0;
const newOperationID = () => `doctor-${globalThis.crypto?.randomUUID?.() ?? `${Date.now()}-${++operationSequence}`}`;
const boundedLines = (lines: string[]) => lines.slice(-1000);

export default function SiteDoctorModal({ open, projectName, onClose, onDone, onOpenRuntimeDownloads }: {
  open: boolean;
  projectName: string | null;
  onClose(): void;
  onDone?(): void;
  onOpenRuntimeDownloads?(kind: 'php' | 'nodejs', version?: string): void;
}) {
  const t = useT(); const dialogs = useDialogs();
  const [reports, setReports] = useState<DoctorReport[]>([]);
  const [selectedProject, setSelectedProject] = useState<string | null>(null);
  const [overviewState, setOverviewState] = useState<DoctorOverviewState>(initialDoctorOverviewState);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');
  const [active, setActive] = useState<ActiveFix | null>(null);
  const [canceling, setCanceling] = useState(false);
  const [lines, setLines] = useState<string[]>([]);
  const [outputTitle, setOutputTitle] = useState('');
  const [truncated, setTruncated] = useState(false);
  const activeRef = useRef<ActiveFix | null>(null);
  const generation = useRef(0);
  const outputRef = useRef<HTMLDivElement>(null);
  const contentRef = useRef<HTMLDivElement>(null);
  const outputLineCount = useRef(0);
  const onCloseRef = useRef(onClose); onCloseRef.current = onClose;

  const load = async (session: number) => {
    setLoading(true); setError('');
    try {
      const next = projectName === null ? await DiagnoseAllProjects() : [await DiagnoseProject(projectName)];
      if (generation.current === session) {
        const incoming = next ?? [];
        setReports(incoming);
        if (projectName === null) {
          setSelectedProject(current => incoming.some(report => report.project === current) ? current : null);
        }
      }
    } catch (cause) {
      if (generation.current === session) setError(String(cause));
    } finally {
      if (generation.current === session) setLoading(false);
    }
  };

  useEffect(() => {
    const session = ++generation.current;
    if (!open) return;
    setReports([]); setLines([]); setOutputTitle(''); setTruncated(false);
    setSelectedProject(null); setOverviewState(initialDoctorOverviewState);
    outputLineCount.current = 0;
    setActive(null); activeRef.current = null; setCanceling(false);
    void load(session);
    const off = EventsOn('doctor:output', (event: { operationID: string; project: string; line: string }) => {
      const current = activeRef.current;
      if (!current || event.operationID !== current.operationID || event.project !== current.project) return;
      if (++outputLineCount.current > 1000) setTruncated(true);
      setLines(previous => boundedLines([...previous, event.line]));
    });
    const onKey = (event: KeyboardEvent) => {
      if (event.key === 'Escape' && !activeRef.current) onCloseRef.current();
    };
    window.addEventListener('keydown', onKey);
    return () => {
      generation.current = session + 1;
      off(); window.removeEventListener('keydown', onKey);
    };
    // The current locale and callbacks must not restart a running diagnosis.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open, projectName]);

  useEffect(() => {
    if (outputRef.current) outputRef.current.scrollTop = outputRef.current.scrollHeight;
  }, [lines]);

  const fix = async (report: DoctorReport, finding: DoctorFinding, label: string) => {
    if (activeRef.current || loading) return;
    if (finding.fixID === 'download') {
      if (finding.runtimeKind !== 'php' && finding.runtimeKind !== 'nodejs') return;
      onOpenRuntimeDownloads?.(finding.runtimeKind, finding.installVersion || undefined);
      onClose();
      return;
    }
    const session = generation.current;
    let confirmed = false;
    if (finding.requiresConfirmation) {
      const migrations = finding.fixID === 'migrations';
      confirmed = await dialogs.confirm({
        title: migrations ? t('projects.doctor.confirmMigrationsTitle') : t('projects.doctor.confirmVhostTitle'),
        message: migrations ? t('projects.doctor.confirmMigrationsMessage', { project: report.project }) : t('projects.doctor.confirmVhostMessage', { project: report.project }),
        confirmLabel: migrations ? t('projects.doctor.confirmMigrationsButton') : t('projects.doctor.confirmVhostButton'),
        danger: true,
      });
      if (!confirmed || generation.current !== session || activeRef.current) return;
    }
    const request = { project: report.project, fixID: finding.fixID, confirmed, operationID: newOperationID() };
    const current = { ...request, label };
    activeRef.current = current; setActive(current); setCanceling(false); setError('');
    outputLineCount.current = 0;
    setLines([]); setTruncated(false); setOutputTitle(t('projects.doctor.outputFor', { project: report.project, check: label }));
    try {
      const result = await ApplyDoctorFix(request);
      if (generation.current !== session) return;
      if (result.output) {
        const resultLines = result.output.replace(/\r\n/g, '\n').replace(/\n$/, '').split('\n');
        setLines(boundedLines(resultLines));
        setTruncated(result.truncated || resultLines.length > 1000);
      } else setTruncated(previous => previous || result.truncated);
      if (!result.success) setError(t('projects.doctor.fixFailed', { code: result.exitCode }));
      setLoading(true); activeRef.current = null; setActive(null); setCanceling(false);
      try {
        const next = await DiagnoseProject(report.project);
        if (generation.current === session) setReports(previous => previous.map(item => item.project === report.project ? next : item));
      } catch (cause) {
        if (generation.current === session) setError(String(cause));
      }
      if (generation.current === session) onDone?.();
    } catch (cause) {
      if (generation.current === session) setError(String(cause));
    } finally {
      if (generation.current === session) {
        activeRef.current = null; setActive(null); setCanceling(false); setLoading(false);
      }
    }
  };

  const cancel = async () => {
    const current = activeRef.current;
    if (!current || canceling) return;
    setCanceling(true);
    try { await CancelDoctorFix(current.operationID); }
    catch (cause) {
      if (activeRef.current?.operationID === current.operationID) { setError(String(cause)); setCanceling(false); }
    }
  };

  const overview = projectName === null && selectedProject === null;
  const detailProject = projectName ?? selectedProject;
  const visibleReports = overview ? [] : reports.filter(report => report.project === detailProject);

  useEffect(() => {
    if (contentRef.current) contentRef.current.scrollTop = 0;
  }, [overview, detailProject]);

  if (!open) return null;
  return <div className="modal-backdrop" onClick={() => { if (!activeRef.current) onClose(); }}>
    <div role="dialog" aria-modal="true" aria-labelledby="site-doctor-title" onClick={event => event.stopPropagation()}
      style={projectName === null ? { height: 'min(760px, 86vh)' } : undefined}
      className="modal-card w-[900px] max-w-[94vw] max-h-[86vh] flex flex-col overflow-hidden">
      <header className="flex items-center justify-between px-5 py-4 border-b border-white/10 gap-3 shrink-0">
        <div className="flex items-center gap-2 min-w-0">
          <Stethoscope className="w-5 h-5 text-accent2 shrink-0" />
          <h2 id="site-doctor-title" className="text-base font-semibold truncate">{t('projects.doctor.title')}{detailProject ? ` · ${detailProject}` : ''}</h2>
        </div>
        <button type="button" onClick={onClose} disabled={!!active} aria-label={t('projects.doctor.close')}
          className="p-1 opacity-60 hover:opacity-100 disabled:opacity-30 disabled:cursor-not-allowed"><X className="w-4 h-4" /></button>
      </header>
      {projectName === null && selectedProject !== null && <div className="px-5 py-2 border-b border-white/10 shrink-0">
        <button type="button" autoFocus onClick={() => setSelectedProject(null)} disabled={!!active}
          className="flex items-center gap-2 text-xs px-2 py-1.5 rounded-md hover:bg-white/10 focus-visible:outline focus-visible:outline-2 focus-visible:outline-accent disabled:opacity-40 disabled:cursor-not-allowed">
          <ArrowLeft className="w-3.5 h-3.5" />{t('projects.doctor.overview.back')}
        </button>
      </div>}
      <div ref={contentRef} className={`flex-1 min-h-0 overflow-y-auto p-5 ${overview ? 'flex flex-col gap-4' : 'space-y-4'}`}>
        {!overview && <p className="text-xs opacity-60">{t('projects.doctor.description')}</p>}
        {error && <div role="alert" className="text-xs text-red-200 bg-red-500/10 border border-red-500/20 rounded-lg px-3 py-2 break-words">{error}</div>}
        {loading && <div role="status" className="flex items-center gap-2 text-xs opacity-70"><Loader2 className="w-4 h-4 animate-spin" />{t('projects.doctor.checking')}</div>}
        {!loading && !error && reports.length === 0 && <p className="text-xs opacity-60">{t('projects.doctor.noProjects')}</p>}
        {overview && reports.length > 0 && <SiteDoctorOverview reports={reports} state={overviewState} onChange={setOverviewState} onSelect={setSelectedProject} />}
        {visibleReports.map(report => <section key={report.project} className="rounded-xl border border-white/10 overflow-hidden">
          <div className="flex items-center justify-between gap-3 px-3 py-3 bg-white/5">
            <div className="min-w-0"><h3 className="text-sm font-semibold">{report.project}</h3><p className="text-[10px] opacity-50 break-all mt-0.5">{report.path}</p></div>
            <span className={`text-[10px] shrink-0 ${report.healthy ? 'text-emerald-300' : 'text-amber-300'}`}>{report.healthy ? t('projects.doctor.healthy') : t('projects.doctor.attention')}</span>
          </div>
          {(report.findings ?? []).map(finding => {
            const label = t(checkKeys[finding.id] ?? 'projects.doctor.checks.unknown');
            const status = statusKeys[finding.status] ? finding.status : 'skipped';
            const Icon = status === 'pass' ? CheckCircle2 : status === 'fail' ? CircleX : status === 'warning' ? AlertTriangle : status === 'optional' ? Info : MinusCircle;
            const optional = status === 'optional';
            const actionable = (status === 'fail' || status === 'warning' || optional) && !!finding.fixID;
            const manualReview = (status === 'fail' || status === 'warning') && !finding.fixID;
            const download = finding.fixID === 'download';
            const validDownload = (finding.runtimeKind === 'php' || finding.runtimeKind === 'nodejs') && !!onOpenRuntimeDownloads;
            const pendingCount = finding.code === 'migrations_pending' && /^\d+$/.test(finding.detail ?? '');
            return <div key={finding.id} role="group" aria-label={label} className="flex gap-3 px-3 py-3 border-t border-white/5">
              <Icon className={`w-4 h-4 shrink-0 mt-0.5 ${statusStyles[status].split(' ')[0]}`} />
              <div className="min-w-0 flex-1">
                <div className="flex items-center gap-2 flex-wrap"><h4 className="text-xs font-medium">{label}</h4>
                  <span className={`text-[9px] rounded px-1.5 py-0.5 ${statusStyles[status]}`}>{t(statusKeys[status])}</span>
                  {finding.severity === 'error' && <span className="text-[9px] opacity-45">{t(severityKeys[finding.severity])}</span>}
                  {manualReview && <span className="text-[9px] text-amber-200/80">{t('projects.doctor.manualReview')}</span>}
                </div>
                {finding.code && <p className="text-[11px] opacity-70 mt-1">{t(messageKeys[finding.code] ?? 'projects.doctor.messages.unknown', { count: pendingCount ? Number(finding.detail) : 0 })}</p>}
                {finding.detail && !pendingCount && <p className="text-[10px] opacity-50 mt-1 whitespace-pre-wrap break-words">{finding.detail}</p>}
                {optional && !finding.fixID && <p className="text-[11px] text-sky-200/70 mt-1">{t('projects.doctor.optionalHint')}</p>}
                {manualReview && <p className="text-[11px] opacity-70 mt-1">{t('projects.doctor.manualReviewHint')}</p>}
              </div>
              {actionable && (!download || validDownload) && <button type="button" onClick={() => void fix(report, finding, label)} disabled={!!active || loading}
                className={`self-start shrink-0 px-2.5 py-1.5 rounded-md border text-[10px] flex items-center gap-1.5 disabled:opacity-40 disabled:cursor-not-allowed focus-visible:outline focus-visible:outline-2 focus-visible:outline-accent ${optional ? 'bg-sky-500/10 border-sky-400/25 text-sky-200 hover:bg-sky-500/20' : 'bg-accent/20 border-accent/40 hover:bg-accent/35'}`}>
                {download ? <Download className="w-3 h-3" /> : optional ? <Link2 className="w-3 h-3" /> : <Wrench className="w-3 h-3" />}
                {download ? t('projects.doctor.downloads', { runtime: finding.runtimeKind === 'php' ? 'PHP' : 'Node' }) : optional ? t('projects.doctor.optionalAction') : t('projects.doctor.fix', { check: label })}
              </button>}
            </div>;
          })}
        </section>)}
        {outputTitle && <details key={overview ? 'overview-output' : 'detail-output'} open={!overview}
          className="group rounded-lg border border-white/10 overflow-hidden shrink-0">
          <summary className="flex items-center gap-2 px-3 py-2 text-[11px] font-medium bg-white/5 cursor-pointer list-none focus-visible:outline focus-visible:outline-2 focus-visible:outline-accent">
            <ChevronRight aria-hidden="true" className="w-3.5 h-3.5 shrink-0 group-open:rotate-90" />
            <span className="shrink-0">{t('projects.doctor.output')}</span>
            <span className="truncate opacity-60">{outputTitle}</span>
          </summary>
          <div ref={outputRef} role="log" aria-label={t('projects.doctor.output')} aria-live="polite" tabIndex={0}
            className={`${overview ? 'max-h-24' : 'max-h-48'} overflow-y-auto bg-black/30 px-3 py-2 font-mono text-[10px] leading-relaxed whitespace-pre-wrap break-words focus-visible:outline focus-visible:outline-1 focus-visible:outline-accent`}>
            {lines.length ? lines.map((line, index) => <div key={index}>{line || '\u00a0'}</div>) : <span className="opacity-50">{t('projects.doctor.waitingOutput')}</span>}
          </div>
          {truncated && <p className="px-3 py-2 text-[10px] text-amber-300 bg-amber-500/5">{t('projects.doctor.outputTruncated')}</p>}
        </details>}
      </div>
      <footer className="flex items-center justify-between gap-3 px-5 py-3 border-t border-white/10 shrink-0">
        {active ? <div role="status" className="text-[11px] opacity-70 flex items-center gap-2 min-w-0"><Loader2 className="w-3.5 h-3.5 animate-spin shrink-0" />
          <span className="truncate">{t('projects.doctor.repairing', { project: active.project, check: active.label })}</span></div> : <span />}
        {active ? <button type="button" onClick={() => void cancel()} disabled={canceling}
          className="shrink-0 text-xs px-3 py-1.5 rounded-md bg-white/5 border border-white/10 hover:bg-white/10 disabled:opacity-50 flex items-center gap-1.5">
          <Square className="w-3 h-3" />{canceling ? t('projects.doctor.canceling') : t('projects.doctor.cancel')}</button> :
          <button type="button" onClick={() => void load(generation.current)} disabled={loading}
            className="text-xs px-3 py-1.5 rounded-md bg-white/5 border border-white/10 hover:bg-white/10 disabled:opacity-50 flex items-center gap-1.5">
            <RefreshCw className={`w-3 h-3 ${loading ? 'animate-spin' : ''}`} />{t('projects.doctor.runAgain')}</button>}
      </footer>
    </div>
  </div>;
}
