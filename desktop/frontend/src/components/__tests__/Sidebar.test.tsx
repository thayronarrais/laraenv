// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, expect, it, vi } from 'vitest';
import Sidebar from '../Sidebar';
vi.mock('../../../wailsjs/go/main/App', () => ({ AppVersion: async () => '1', OpenURL: vi.fn() }));
vi.mock('../../lib/i18n', () => ({ useT: () => (key: string) => key }));
afterEach(cleanup);
it('primary rail keeps labels account and update access at 56 px', () => {
  const change = vi.fn(), account = vi.fn(), toggle = vi.fn();
  render(<Sidebar current="terminal" onChange={change} collapsed onToggleCollapsed={toggle} updateAvailable status={{ enabled: true, loggedIn: true, name: 'Alex', plan: 'free' } as any} onGoToPlan={account} />);
  expect(screen.getByRole('complementary').style.width).toBe('56px');
  fireEvent.click(screen.getByRole('button', { name: 'sidebar.nav.settings' })); expect(change).toHaveBeenCalledWith('settings');
  expect(screen.getByRole('status', { name: 'sidebar.updateAvailable' })).toBeTruthy();
  fireEvent.click(screen.getByRole('button', { name: 'cloudOptIn.title' })); expect(account).toHaveBeenCalledOnce();
  fireEvent.click(screen.getByRole('button', { name: 'sidebar.expand' })); expect(toggle).toHaveBeenCalledOnce();
  expect(screen.getByRole('button', { name: 'sidebar.footer.donate' })).toBeTruthy();
});
it('does not expose saved account avatars or plan badges before opt-in', () => {
  render(<Sidebar current="deployments" onChange={vi.fn()} status={{ enabled: false, loggedIn: true, name: 'Saved', githubId: '123', plan: 'pro' } as any} />);
  expect(screen.queryByText('Saved')).toBeNull();
  expect(document.querySelector('img[src*="avatars.githubusercontent.com"]')).toBeNull();
  expect(screen.queryByText('sidebar.account.pro')).toBeNull();
  expect(screen.getByRole('button', { name: 'sidebar.nav.deployments' })).toBeTruthy();
});
it('defaults to 192 px with no collapse control for existing callers', () => {
  render(<Sidebar current="terminal" onChange={vi.fn()} />);
  expect(screen.getByRole('complementary').style.width).toBe('192px');
  expect(screen.queryByRole('button', { name: 'sidebar.collapse' })).toBeNull();
});
