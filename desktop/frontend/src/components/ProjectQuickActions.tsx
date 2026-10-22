import { useId, useRef, useState } from 'react';
import { SlidersHorizontal } from 'lucide-react';
import { useT } from '../lib/i18n';
import PortalMenu from './PortalMenu';
import './ProjectQuickActions.css';

export type ProjectQuickAction = 'editor' | 'browser' | 'terminal' | 'folder' | 'none';
type ShortcutSlots = [ProjectQuickAction, ProjectQuickAction];
const storageKey = 'laraenv.projectsQuickActions';
const defaults: ShortcutSlots = ['editor', 'browser'];
const choices: ProjectQuickAction[] = ['editor', 'browser', 'terminal', 'folder', 'none'];

function readShortcuts(): ShortcutSlots {
  try {
    const saved = JSON.parse(localStorage.getItem(storageKey) ?? 'null');
    if (saved?.version === 1 && Array.isArray(saved.actions) && saved.actions.length === 2 &&
      saved.actions.every((action: unknown) => choices.includes(action as ProjectQuickAction)) &&
      (saved.actions[0] !== saved.actions[1] || saved.actions[0] === 'none')) return [...saved.actions] as ShortcutSlots;
  } catch { /* Use defaults if storage is unavailable or malformed. */ }
  return [...defaults];
}

export function useProjectQuickActions() {
  const [actions, setActions] = useState<ShortcutSlots>(readShortcuts);
  const [saveFailed, setSaveFailed] = useState(false);
  const save = (next: ShortcutSlots) => {
    setActions(next);
    try {
      localStorage.setItem(storageKey, JSON.stringify({ version: 1, actions: next }));
      setSaveFailed(false);
    } catch { setSaveFailed(true); }
  };
  const selectShortcut = (slot: 0 | 1, action: ProjectQuickAction) => {
    const next: ShortcutSlots = [...actions];
    const other = slot === 0 ? 1 : 0;
    if (action !== 'none' && next[other] === action) next[other] = next[slot];
    next[slot] = action;
    save(next);
  };
  return { actions, saveFailed, selectShortcut, restoreDefaults: () => save([...defaults]) };
}

export function ProjectActionsPicker({ open, onToggle, onClose, actions, saveFailed, selectShortcut, restoreDefaults }: {
  open: boolean; onToggle(): void; onClose(): void; actions: ShortcutSlots; saveFailed: boolean;
  selectShortcut(slot: 0 | 1, action: ProjectQuickAction): void; restoreDefaults(): void;
}) {
  const t = useT();
  const id = useId();
  const buttonRef = useRef<HTMLButtonElement>(null);
  const labels: Record<ProjectQuickAction, string> = {
    editor: t('projects.common.editorFallback'), browser: t('projects.row.browser'),
    terminal: t('projects.workspace.actionTerminal'), folder: t('projects.workspace.actionFolder'), none: t('projects.workspace.actionNone'),
  };
  const close = () => { onClose(); buttonRef.current?.focus(); };
  return <>
    <button ref={buttonRef} type="button" className="projects-actions-customize" aria-expanded={open}
      aria-label={t('projects.workspace.customizeActions')} title={t('projects.workspace.customizeActions')} onClick={onToggle}>
      <SlidersHorizontal aria-hidden="true" size={14} /><span>{t('projects.workspace.customizeActions')}</span>
    </button>
    <PortalMenu open={open} anchorRef={buttonRef} onClose={close} minWidth={280}>
      <div className="projects-actions-menu" role="group" aria-label={t('projects.workspace.customizeActions')}>
        <p>{t('projects.workspace.actionsHint')}</p>
        {([0, 1] as const).map(slot => <label key={slot} htmlFor={`${id}-slot-${slot}`}>
          <span>{t('projects.workspace.actionSlot', { slot: slot + 1 })}</span>
          <select id={`${id}-slot-${slot}`} autoFocus={slot === 0} value={actions[slot]}
            onChange={event => selectShortcut(slot, event.target.value as ProjectQuickAction)}>
            {choices.map(action => <option key={action} value={action}>{labels[action]}</option>)}
          </select>
        </label>)}
        {saveFailed && <p role="alert">{t('projects.workspace.actionsSaveFailed')}</p>}
        <div className="projects-actions-menu__footer">
          <button type="button" onClick={restoreDefaults}>{t('projects.workspace.resetActions')}</button>
          <button type="button" onClick={close}>{t('projects.workspace.actionsClose')}</button>
        </div>
      </div>
    </PortalMenu>
  </>;
}
