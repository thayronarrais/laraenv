// @vitest-environment jsdom
import { act } from 'react';
import { createRoot } from 'react-dom/client';
import { expect, it, vi } from 'vitest';
vi.mock('../../../lib/i18n', () => ({ useT: () => (key: string) => key }));
import { DocumentCloseDialog } from '../DocumentCloseDialog';
(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;
it('renders three choices in a portal, claims document focus and cancels on Escape', () => {
  const host = document.createElement('div'); host.hidden = true; document.body.append(host); const root = createRoot(host); const decision = vi.fn(); const claim = vi.fn();
  act(() => root.render(<DocumentCloseDialog decision={{ scope: 'all', names: ['a.md'] }} onDecision={decision} onClaimDocuments={claim} />));
  const dialog = document.querySelector('[role="dialog"]')!;
  expect(host.contains(dialog)).toBe(false); expect(dialog.textContent).toContain('a.md'); expect(dialog.querySelectorAll('button')).toHaveLength(3); expect(claim).toHaveBeenCalled();
  act(() => dialog.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))); expect(decision).toHaveBeenLastCalledWith('cancel');
  for (const button of dialog.querySelectorAll('button')) act(() => (button as HTMLButtonElement).click());
  expect(decision.mock.calls.map(([choice]) => choice)).toEqual(['cancel', 'save', 'discard', 'cancel']);
  act(() => root.unmount()); host.remove();
});
