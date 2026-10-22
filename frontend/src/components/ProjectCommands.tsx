import { useEffect, useId, useState } from 'react';
import { Play, RefreshCw, Terminal } from 'lucide-react';
import { ListProjectScripts } from '../../wailsjs/go/main/App';
import { useT } from '../lib/i18n';
import './ProjectCommands.css';

export type ProjectCommandsProps = {
  projectName: string;
  packageManager: string;
  terminalKind?: string;
  onRunCommand: (command: string) => void;
};

// Manifest keys are arguments, never shell syntax. Cmd expands percent/delayed
// variables even in quotes, so exceptional names use a literal PowerShell arg
// encoded as UTF-16LE instead of being interpolated into that shell.
export function projectScriptCommand(manager: string, script: string, terminalKind = 'powershell') {
  const executable = ['npm', 'pnpm', 'yarn', 'bun'].includes(manager) ? manager : 'npm';
  const prefix = `${executable} run ${script.startsWith('-') ? '-- ' : ''}`;
  if (/^[a-zA-Z0-9_@./:-]+$/.test(script)) return prefix + script;
  const powershell = prefix + `'${script.replace(/'/g, "''")}'`;
  const hasControl = Array.from(script).some(character => character.charCodeAt(0) < 32 || character.charCodeAt(0) === 127);
  if (terminalKind === 'cmd' || terminalKind === 'cmder' || hasControl) {
    let bytes = '';
    for (let index = 0; index < powershell.length; index++) {
      const code = powershell.charCodeAt(index);
      bytes += String.fromCharCode(code & 255, code >>> 8);
    }
    return `powershell.exe -NoProfile -EncodedCommand ${btoa(bytes)}`;
  }
  return terminalKind === 'gitbash' ? prefix + `'${script.replace(/'/g, "'\"'\"'")}'` : powershell;
}

export default function ProjectCommands(props: ProjectCommandsProps) {
  return <ProjectCommandsForProject key={props.projectName} {...props} />;
}

function ProjectCommandsForProject({ projectName, packageManager, terminalKind, onRunCommand }: ProjectCommandsProps) {
  const t = useT();
  const id = useId();
  const [scripts, setScripts] = useState<string[]>([]);
  const [selected, setSelected] = useState('');
  const [customCommand, setCustomCommand] = useState('');
  const [revision, setRevision] = useState(0);
  const [loading, setLoading] = useState(true);
  const [failed, setFailed] = useState(false);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    setFailed(false);
    ListProjectScripts(projectName).then(result => {
      if (cancelled) return;
      const available = result ?? [];
      setScripts(available);
      setSelected(previous => available.includes(previous) ? previous : available.includes('dev') ? 'dev' : available[0] ?? '');
    }).catch(() => {
      if (cancelled) return;
      setScripts([]);
      setSelected('');
      setFailed(true);
    }).finally(() => { if (!cancelled) setLoading(false); });
    return () => { cancelled = true; };
  }, [projectName, revision]);

  const command = selected ? projectScriptCommand(packageManager, selected, terminalKind) : '';
  return <section className="project-detail-section project-commands" aria-label={t('projects.commands.title')}>
    <div className="project-commands-heading">
      <h3>{t('projects.commands.title')}</h3>
      <button type="button" className="project-commands-refresh" disabled={loading} onClick={() => setRevision(value => value + 1)}
        title={t('projects.commands.refresh')} aria-label={t('projects.commands.refresh')}><RefreshCw size={13} /></button>
    </div>
    {loading ? <p role="status" className="project-commands-message">{t('projects.commands.loading')}</p>
      : failed ? <div className="project-commands-error"><p role="alert">{t('projects.commands.loadError')}</p>
        <button type="button" onClick={() => setRevision(value => value + 1)}>{t('projects.commands.retry')}</button></div>
      : !scripts.length ? <p className="project-commands-message">{t('projects.commands.empty')}</p>
      : <div className="project-commands-script">
        <label htmlFor={`${id}-script`}>{t('projects.commands.script')}</label>
        <div className="project-commands-controls">
          <select id={`${id}-script`} value={selected} onChange={event => setSelected(event.target.value)}>
            {scripts.map(script => <option key={script} value={script}>{script}</option>)}
          </select>
          <button type="button" disabled={!command} onClick={() => onRunCommand(command)} aria-label={t('projects.commands.runScript', { command })}>
            <Play size={12} aria-hidden="true" />{t('projects.commands.run')}
          </button>
        </div>
        <code className="project-commands-preview">{command}</code>
      </div>}
    <form className="project-commands-custom" onSubmit={event => { event.preventDefault(); if (customCommand.trim()) onRunCommand(customCommand); }}>
      <label htmlFor={`${id}-custom`}>{t('projects.commands.customLabel')}</label>
      <input id={`${id}-custom`} value={customCommand} onChange={event => setCustomCommand(event.target.value)}
        placeholder={t('projects.commands.customPlaceholder')} autoComplete="off" spellCheck={false} />
      <button type="submit" disabled={!customCommand.trim()}><Terminal size={12} aria-hidden="true" />{t('projects.commands.customRun')}</button>
    </form>
    <p className="project-commands-hint">{t('projects.commands.hint')}</p>
  </section>;
}
