import { choiceLabels } from './labels';
import { useEffect, useRef } from 'react';
import { createPortal } from 'react-dom';
import { useT } from '../../lib/i18n';
import type { StageDirtyDecision } from './stageDocuments';
import type { StageDirtyChoice } from './types';
export function StageDirtyDialog({ decision, onChoose }: { decision: StageDirtyDecision | null; onChoose(choice: StageDirtyChoice): void }) {
  const t = useT(); const dialog = useRef<HTMLElement>(null);
  useEffect(() => {
    if (!decision) return;
    const previous = document.activeElement as HTMLElement | null;
    const cancel = dialog.current?.querySelector<HTMLButtonElement>('button:last-child'); cancel?.focus();
    return () => { if (previous?.isConnected) previous.focus(); };
  }, [decision?.id]);
  if (!decision) return null;
  return createPortal(<div className="fixed inset-0 z-[10000] flex items-center justify-center bg-black/60" onKeyDown={event => { if (event.key === 'Escape') { event.preventDefault(); onChoose('cancel'); }
    if (event.key === 'Tab') {
      const buttons = dialog.current?.querySelectorAll<HTMLButtonElement>('button'); if (!buttons?.length) return;
      const first = buttons[0], last = buttons[buttons.length - 1];
      if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last.focus(); }
      else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first.focus(); }
    } }}>
    <section ref={dialog} role="dialog" aria-modal="true" aria-label={t('git.dirtyTitle')} className="max-w-lg rounded border border-white/20 bg-[#171923] p-5 text-white">
      <h2>{t('git.dirtyTitle')}</h2><p>{t('git.dirtyDescription')}</p><p>{decision.label}</p><p className="break-all text-xs">{decision.path}</p>
      <ul className="my-3 max-h-64 overflow-auto text-sm">{decision.names.map(name => <li key={name}>{name}</li>)}</ul>
      <div className="flex flex-wrap gap-2">{(['save-and-stage', 'stage-disk', 'cancel'] as const).map(choice => <button key={choice} autoFocus={choice === 'cancel'} type="button" className="btn-secondary" onClick={() => onChoose(choice)}>{t(choiceLabels[choice])}</button>)}</div>
    </section>
  </div>, document.body);
}
