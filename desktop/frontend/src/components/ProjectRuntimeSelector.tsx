import { useRef } from 'react';
import { Boxes, ChevronDown, Code2 } from 'lucide-react';
import PortalMenu from './PortalMenu';
import { useT } from '../lib/i18n';

export type ProjectRuntimeResolution = {
  version: string;
  fullVersion: string;
  source: string;
  constraint: string;
  status: string;
  installVersion?: string;
  problem?: string;
};
export type ProjectRuntimeKind = 'php' | 'nodejs';
type RuntimeDisplayProps = { kind: ProjectRuntimeKind; manualVersion: string; defaultVersion: string; runtime?: ProjectRuntimeResolution };
const knownRuntimeStatuses = new Set(['resolved', 'missing', 'invalid', 'incompatible', 'unmanaged']);
function useRuntimeDisplay({ kind, manualVersion, defaultVersion, runtime }: RuntimeDisplayProps) {
  const t = useT();
  const name = kind === 'php' ? 'PHP' : 'Node';
  const rawStatus = runtime?.status || 'resolved';
  const unknownStatus = !knownRuntimeStatuses.has(rawStatus);
  const status = unknownStatus ? 'invalid' : rawStatus;
  const invalidWithoutRuntime = status === 'invalid' && (unknownStatus || !(runtime?.source === 'manual' && runtime.version));
  const version = status === 'missing' ? t('projects.runtime.missing')
    : status === 'unmanaged' ? t('projects.runtime.unmanaged')
    : invalidWithoutRuntime ? t('projects.runtime.invalid')
    : runtime?.fullVersion || runtime?.version || manualVersion || defaultVersion;
  const source = runtime?.source || (manualVersion ? 'manual' : 'global');
  const sourceLabel = source === 'manual' ? t('projects.runtime.manual') : source === 'global' ? t('projects.runtime.global') : t('projects.runtime.detected');
  const sourceName = source === 'manual' || source === 'global' ? sourceLabel : source;
  const detail = status === 'unmanaged' ? version : runtime?.constraint
    ? t('projects.runtime.sourceConstraint', { source: sourceName, constraint: runtime.constraint })
    : source === 'manual' ? t('projects.runtime.manualHint') : source === 'global' ? t('projects.runtime.globalHint', { runtime: name }) : t('projects.runtime.detectedHint', { source });
  const warning = status === 'missing' ? source === 'manual'
    ? t('projects.runtime.manualMissingHint', { runtime: name, version: manualVersion })
    : t('projects.runtime.missingHint', { runtime: name, constraint: runtime?.constraint ?? '' })
    : status === 'invalid' ? t('projects.runtime.invalidHint', { runtime: name, constraint: runtime?.constraint ?? '' })
    : status === 'incompatible' ? t('projects.runtime.incompatibleHint', { runtime: name, constraint: runtime?.constraint ?? '' }) : '';
  return { name, version, sourceLabel, detail, warning, status };
}

export function ProjectRuntimeBadge(props: RuntimeDisplayProps) {
  const display = useRuntimeDisplay(props);
  return <span title={[display.detail, display.warning].filter(Boolean).join('\n')} className={display.warning ? 'text-amber-300' : undefined}>
    <span>{display.name} {display.version}</span>{props.runtime && display.status !== 'unmanaged' && <small className="ml-1 opacity-60">{display.sourceLabel}</small>}
  </span>;
}

export function ProjectRuntimeSelector(props: RuntimeDisplayProps & {
  versions: { version: string }[]; open: boolean; setOpen(open: boolean): void; onPick(version: string): void;
  onOpenDownloads?: (kind: ProjectRuntimeKind, version?: string) => void;
}) {
  const t = useT(); const display = useRuntimeDisplay(props); const ref = useRef<HTMLButtonElement>(null);
  const Icon = props.kind === 'php' ? Code2 : Boxes;
  const installVersion = props.runtime?.installVersion;
  const selectionHint = props.kind === 'php' ? t('projects.phpDropdown.title')
    : props.versions.length ? t('projects.nodeDropdown.title') : t('projects.nodeDropdown.noneInstalledTitle');
  return <div className="flex flex-col items-start gap-1 min-w-0">
    <button type="button" ref={ref} onClick={() => props.setOpen(!props.open)} title={`${selectionHint}\n${display.detail}`} aria-expanded={props.open}
      className="min-h-7 max-w-full px-2.5 py-1 rounded-md bg-white/5 hover:bg-white/10 border border-white/10 text-[11px] flex items-center gap-1.5 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent">
      <Icon className="w-3 h-3 shrink-0" /><span className="min-w-0 text-left">{display.name} {display.version}</span><ChevronDown className="w-3 h-3 shrink-0" />
    </button>
    {display.status !== 'unmanaged' && <span className="text-[10px] leading-4 text-white/60">{display.sourceLabel}</span>}
    {props.runtime?.constraint && <span title={display.detail} className="max-w-full text-[11px] leading-4 text-white/60 break-words">{display.detail}</span>}
    {display.warning && <p role="status" className="max-w-full text-[11px] leading-4 text-amber-300 break-words">{display.warning}</p>}
    {display.status === 'missing' && props.onOpenDownloads && <button type="button" className="text-[10px] text-accent2 hover:underline"
      onClick={() => { props.setOpen(false); props.onOpenDownloads?.(props.kind, installVersion || undefined); }}>
      {installVersion ? t('projects.runtime.install', { runtime: display.name, version: installVersion }) : t('projects.runtime.downloads', { runtime: display.name })}
    </button>}
    <PortalMenu open={props.open} anchorRef={ref} onClose={() => props.setOpen(false)} align="right" minWidth={180}>
      <button type="button" onClick={() => props.onPick('')} className="w-full text-left px-3 py-1.5 text-[11px] rounded-md hover:bg-white/10">
        {t('projects.runtime.automatic')} {!props.manualVersion && <span className="opacity-50">{t('projects.common.current')}</span>}
      </button>
      <div className="h-px bg-white/10 my-1" />
      {props.versions.length === 0 && <p className="px-3 py-1.5 text-[11px] opacity-60">{props.kind === 'nodejs' ? t('projects.nodeDropdown.noneInstalledBody') : t('projects.runtime.noneInstalled', { runtime: display.name })}</p>}
      {props.versions.map(version => <button type="button" key={version.version} onClick={() => props.onPick(version.version)}
        className="w-full text-left px-3 py-1.5 text-[11px] rounded-md hover:bg-white/10">
        {display.name} {version.version} {(props.manualVersion === version.version || props.runtime?.source === 'manual' && props.runtime.version === version.version) && <span className="opacity-50">{t('projects.common.current')}</span>}
      </button>)}
    </PortalMenu>
  </div>;
}
