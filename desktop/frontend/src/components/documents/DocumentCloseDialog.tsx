import { useEffect, useId, useRef } from 'react';
import { createPortal } from 'react-dom';
import { useT } from '../../lib/i18n';
import type { CloseChoice, CloseDecision } from './types';

export type DocumentCloseDialogProps = {
  decision: CloseDecision | null;
  onDecision(choice: CloseChoice): void;
  onClaimDocuments(): void;
};

/** Mount once outside hideable pages. The portal also keeps the decision above pane clipping. */
export function DocumentCloseDialog({ decision, onDecision, onClaimDocuments }: DocumentCloseDialogProps) {
  const t = useT(); const title = useId(); const cancel = useRef<HTMLButtonElement>(null);
  const claim = useRef(onClaimDocuments); claim.current = onClaimDocuments;
  useEffect(() => { if (decision) { claim.current(); cancel.current?.focus(); } }, [decision]);
  if (!decision) return null;
  return createPortal(
    <div className="modal-backdrop" onPointerDownCapture={onClaimDocuments} onFocusCapture={onClaimDocuments}>
      <div role="dialog" aria-modal="true" aria-labelledby={title} className="modal-card w-[460px] max-w-[92vw] p-5 flex flex-col gap-4"
        onKeyDown={event => {
          if (event.key === 'Escape') { event.preventDefault(); event.stopPropagation(); onDecision('cancel'); }
          if (event.key === 'Tab') {
            const buttons = event.currentTarget.querySelectorAll<HTMLButtonElement>('button');
            const first = buttons[0], last = buttons[buttons.length - 1];
            if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last.focus(); }
            else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first.focus(); }
          }
        }}>
        <h2 id={title} className="text-base font-semibold">{t('documents.close.title')}</h2>
        <p>{t('documents.close.description')}</p>
        <ul className="max-h-48 overflow-auto text-sm">{decision.names.map((name, index) => <li key={`${index}:${name}`} className="break-all">{name}</li>)}</ul>
        <div className="flex justify-end gap-2">
          <button className="btn-primary" onClick={() => onDecision('save')}>{t(decision.scope === 'all' ? 'documents.close.saveAll' : 'documents.close.save')}</button>
          <button className="btn-secondary" onClick={() => onDecision('discard')}>{t('documents.close.discard')}</button>
          <button ref={cancel} className="btn-secondary" onClick={() => onDecision('cancel')}>{t('documents.close.cancel')}</button>
        </div>
      </div>
    </div>, document.body);
}
