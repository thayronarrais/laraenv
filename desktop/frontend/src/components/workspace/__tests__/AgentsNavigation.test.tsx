// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, expect, it, vi } from 'vitest';
import Agents from '../../../pages/Agents';
import { DialogProvider } from '../../dialogs/DialogProvider';
import { MarkAgentSessionRead } from '../../../../wailsjs/go/main/App';
const translate = (key: string) => key;
vi.mock('../../../lib/i18n', () => ({ useT: () => translate, useI18n: () => ({ t: translate, formatNumber: String }) }));
vi.mock('../../../../wailsjs/runtime', () => ({ EventsOn: () => () => {} }));
vi.mock('../../../../wailsjs/go/main/App', () => ({
  ListAgentSessions: async () => [{ id: 'a1', projectName: 'demo', projectPath: '/demo', agentId: 'codex', launchMode: 'current', title: 'My session', terminalId: 'term-a1', state: 'idle', updatedAt: new Date().toISOString(), unread: true, archived: false }],
  ListAgentProfiles: async () => [], ListProjects: async () => [], GetAgentWorktreeRuntime: async () => ({}),
  GetAgentGitWorkspace: async () => ({}), ListProjectRoutes: async () => [], RestoreCanonicalRoute: vi.fn(), SuggestWorktreePreviewHostname: vi.fn(), SelectAgentContextDirectory: vi.fn(),
  ArchiveAgentSession: vi.fn(), DetachAgentWorktreeRoutes: vi.fn(), MarkAgentSessionRead: vi.fn(), RemoveAgentWorktree: vi.fn(), RenameAgentSession: vi.fn(), StartAgentWorktreeDevServer: vi.fn(), StopAgentSession: vi.fn(), StopAgentWorktreeDevServer: vi.fn(),
}));
afterEach(cleanup);
it('keeps filter and expansion state while closing the real row portal when inactive', async () => {
  const props = { enabled: true, flash: vi.fn(), onEnable: vi.fn(), onOpenMap: vi.fn(), onSelect: vi.fn(), onOpen: vi.fn(), onResume: vi.fn(), onFresh: vi.fn(), onLaunch: vi.fn() };
  const view = render(<DialogProvider><Agents {...props} /></DialogProvider>);
  fireEvent.click(await screen.findByRole('button', { name: 'agents.page.actionsMenu.triggerAria' }));
  expect(screen.getByRole('button', { name: 'agents.page.actionsMenu.rename' })).toBeTruthy();
  const filter = screen.getByRole('textbox'); fireEvent.change(filter, { target: { value: 'My session' } });
  view.rerender(<DialogProvider><Agents {...props} active={false} /></DialogProvider>);
  expect(screen.queryByRole('button', { name: 'agents.page.actionsMenu.rename' })).toBeNull();
  view.rerender(<DialogProvider><Agents {...props} /></DialogProvider>);
  expect((screen.getByRole('textbox') as HTMLInputElement).value).toBe('My session');
  expect(screen.queryByRole('button', { name: 'agents.page.actionsMenu.rename' })).toBeNull();
  fireEvent.click(screen.getByRole('button', { name: /demo/ }));
  expect(screen.queryByRole('button', { name: 'agents.page.actionsMenu.triggerAria' })).toBeNull();
  view.rerender(<DialogProvider><Agents {...props} active={false} /></DialogProvider>); view.rerender(<DialogProvider><Agents {...props} /></DialogProvider>);
  expect(screen.queryByRole('button', { name: 'agents.page.actionsMenu.triggerAria' })).toBeNull();
});

it('opens the row map once without selecting, reading, opening or resuming its principal', async () => {
  vi.mocked(MarkAgentSessionRead).mockClear();
  const props = { enabled: true, flash: vi.fn(), onEnable: vi.fn(), onOpenMap: vi.fn(), onSelect: vi.fn(), onOpen: vi.fn(), onResume: vi.fn(), onFresh: vi.fn(), onLaunch: vi.fn() };
  render(<DialogProvider><Agents {...props} /></DialogProvider>);
  fireEvent.click(await screen.findByRole('button', { name: 'agents.page.actionsMenu.triggerAria' }));
  fireEvent.click(screen.getByRole('button', { name: 'agents.page.actionsMenu.viewMap' }));
  expect(props.onOpenMap).toHaveBeenCalledOnce();
  expect(props.onOpenMap.mock.calls[0][0].id).toBe('a1');
  expect(MarkAgentSessionRead).not.toHaveBeenCalled();
  expect(props.onSelect).not.toHaveBeenCalled(); expect(props.onOpen).not.toHaveBeenCalled(); expect(props.onResume).not.toHaveBeenCalled();
});
