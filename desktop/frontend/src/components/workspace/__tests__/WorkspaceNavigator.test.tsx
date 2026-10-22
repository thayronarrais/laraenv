// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, expect, it, vi } from 'vitest';
import { useState } from 'react';
import { WorkspaceNavigator } from '../WorkspaceNavigator';
vi.mock('../../../lib/i18n', () => ({ useT: () => (key: string) => key }));
afterEach(cleanup);
const preferences = { version: 1 as const, primaryCollapsed: false, sidebarVisible: true, sidebarTab: 'agents' as const, sidebarWidth: 390 };
function Filter() { const [value, setValue] = useState(''); return <input aria-label="filter" value={value} onChange={e => setValue(e.target.value)} />; }
it('keeps inactive navigator panels mounted and retains their filters across tabs and hide', () => {
  const panels = { agents: <Filter />, files: <input aria-label="path" />, git: <p>Git state</p> }; const update = vi.fn(); const claim = vi.fn();
  const props = { preferences, active: true, compact: false, onUpdate: update, onClaimNavigator: claim, panels };
  const view = render(<WorkspaceNavigator {...props} />); const filter = screen.getByRole('textbox', { name: 'filter' });
  fireEvent.change(filter, { target: { value: 'codex' } });
  fireEvent.click(screen.getByRole('tab', { name: 'workspace.navigation.files' })); expect(update).toHaveBeenCalledWith({ sidebarTab: 'files', sidebarVisible: true });
  view.rerender(<WorkspaceNavigator {...props} preferences={{ ...preferences, sidebarTab: 'files', sidebarVisible: false }} />);
  expect(document.querySelector('[aria-label="filter"]')).toBe(filter); expect((filter as HTMLInputElement).value).toBe('codex');
  expect(filter.closest('[role="tabpanel"]')?.getAttribute('data-active')).toBe('false');
  expect(document.querySelector('[aria-label="path"]')).toBeTruthy();
});
it('clamps rendered drawer width without updating the saved width or including its own width in measurement', () => {
  const rect = vi.spyOn(HTMLElement.prototype, 'getBoundingClientRect').mockReturnValue({ width: 350 } as DOMRect);
  const update = vi.fn(); const view = render(<WorkspaceNavigator preferences={preferences} active compact onUpdate={update} onClaimNavigator={vi.fn()} panels={{ agents: 'A', files: 'F', git: 'G' }} />);
  const drawer = screen.getByRole('complementary', { name: 'workspace.navigation.title' });
  expect(drawer.style.width).toBe('280px'); expect(update).not.toHaveBeenCalled();
  view.rerender(<WorkspaceNavigator preferences={{ ...preferences, sidebarVisible: false }} active compact onUpdate={update} onClaimNavigator={vi.fn()} panels={{ agents: 'A', files: 'F', git: 'G' }} />);
  expect(drawer.style.width).toBe('280px'); rect.mockRestore();
});
