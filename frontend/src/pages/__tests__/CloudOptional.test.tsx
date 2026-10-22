// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import Account from '../Account';
import Settings from '../Settings';
import Dashboard from '../Dashboard';

const bridge = vi.hoisted(() => ({
  CloudStatus: vi.fn(), CloudMe: vi.fn(), CloudSyncStatus: vi.fn(), CloudSetEnabled: vi.fn(), CloudRandomQuote: vi.fn(),
  events: new Map<string, (snapshot: unknown) => void>(),
}));
vi.mock('../../../wailsjs/go/main/App', async importOriginal => {
  const actual = await importOriginal<Record<string, unknown>>();
  return { ...Object.fromEntries(Object.keys(actual).map(key => [key, async () => []])), ...bridge };
});
vi.mock('../../../wailsjs/runtime', () => ({ EventsOn: (name: string, callback: (snapshot: unknown) => void) => {
  bridge.events.set(name, callback);
  return () => { if (bridge.events.get(name) === callback) bridge.events.delete(name); };
} }));
vi.mock('../../lib/i18n', () => ({ useT: () => (key: string) => key, useI18n: () => ({ t: (key: string) => key, formatNumber: (n: number) => String(n), locales: [], overrideResults: [] }) }));

const saved = { enabled: false, loggedIn: true, baseUrl: 'https://saved.invalid', name: 'Saved', email: 'saved@example.invalid', plan: 'pro', githubId: '123', entitlements: { sync: true, deployments: true } };
beforeEach(() => {
  vi.clearAllMocks();
  bridge.events.clear();
  bridge.CloudStatus.mockResolvedValue({ ...saved });
  bridge.CloudMe.mockResolvedValue({});
  bridge.CloudRandomQuote.mockResolvedValue(null);
  bridge.CloudSyncStatus.mockResolvedValue({ enabled: true, hasKey: true, reachable: true, isPro: true, resourceCount: 1 });
  bridge.CloudSetEnabled.mockImplementation(async (enabled: boolean) => { bridge.CloudStatus.mockResolvedValue({ ...saved, enabled }); });
});

it('shows cached automatic sync failures from a local event only while enabled', async () => {
  bridge.CloudStatus.mockResolvedValue({ ...saved, enabled: true });
  render(<Account flash={vi.fn()} />);
  await screen.findByText('saved@example.invalid');
  // The cached account can render before the enabled-only subscription effect
  // runs. Emit only once its callback exists; optional chaining loses the event.
  await waitFor(() => expect(bridge.events.has('cloud:sync-status-changed')).toBe(true));
  await act(async () => bridge.events.get('cloud:sync-status-changed')!({ enabled: true, hasKey: true, reachable: false, isPro: true, resourceCount: 1, lastError: 'Cloud is temporarily unavailable' }));
  expect(screen.getByRole('alert').textContent).toContain('Cloud is temporarily unavailable');
  fireEvent.click(screen.getByRole('button', { name: 'cloudOptIn.disable' }));
  await screen.findByRole('button', { name: 'cloudOptIn.enable' });
  expect(screen.queryByText('Cloud is temporarily unavailable')).toBeNull();
  expect(bridge.events.has('cloud:sync-status-changed')).toBe(false);
});

it('keeps a newer local sync failure when the initial cached snapshot resolves later', async () => {
  let finishSnapshot!: (snapshot: unknown) => void;
  const initialSnapshot = new Promise(resolve => { finishSnapshot = resolve; });
  bridge.CloudStatus.mockResolvedValue({ ...saved, enabled: true });
  bridge.CloudSyncStatus.mockReturnValue(initialSnapshot);
  render(<Account flash={vi.fn()} />);
  await waitFor(() => expect(bridge.events.has('cloud:sync-status-changed')).toBe(true));
  await act(async () => bridge.events.get('cloud:sync-status-changed')!({ enabled: true, hasKey: true, reachable: false, isPro: true, resourceCount: 1, lastError: 'Latest local failure' }));
  expect(screen.getByRole('alert').textContent).toBe('Latest local failure');

  await act(async () => finishSnapshot({ enabled: true, hasKey: true, reachable: true, isPro: true, resourceCount: 1 }));
  expect(screen.getByRole('alert').textContent).toBe('Latest local failure');
});
afterEach(cleanup);

// A saved Pro session must never implicitly opt an old profile into cloud.
it('shows opt-in without mounting account actions or refreshing a saved session', async () => {
  render(<Account flash={vi.fn()} />);
  expect(await screen.findByRole('button', { name: 'cloudOptIn.enable' })).toBeTruthy();
  expect(screen.queryByText('saved@example.invalid')).toBeNull();
  expect(screen.queryByRole('button', { name: 'account.logoutButton' })).toBeNull();
  expect(bridge.CloudMe).not.toHaveBeenCalled();
  expect(bridge.CloudSyncStatus).not.toHaveBeenCalled();
});

it('enables cloud explicitly and stops stale account updates after disabling', async () => {
  let finishMe!: (value: unknown) => void;
  bridge.CloudMe.mockImplementation(() => new Promise(resolve => { finishMe = resolve; }));
  const statusChange = vi.fn();
  render(<Account flash={vi.fn()} onStatusChange={statusChange} />);
  fireEvent.click(await screen.findByRole('button', { name: 'cloudOptIn.enable' }));
  expect(await screen.findByText('saved@example.invalid')).toBeTruthy();
  fireEvent.click(screen.getByRole('button', { name: 'cloudOptIn.disable' }));
  await screen.findByRole('button', { name: 'cloudOptIn.enable' });
  await act(async () => finishMe({ plan: 'pro' }));
  expect(screen.queryByText('saved@example.invalid')).toBeNull();
  expect(statusChange.mock.calls.at(-1)?.[0].enabled).toBe(false);
  expect(bridge.CloudSetEnabled.mock.calls).toEqual([[true], [false]]);
});

it('opens Settings on General without mounting optional account requests', async () => {
  render(<Settings flash={vi.fn()} />);
  expect(screen.getByRole('button', { name: 'cloudOptIn.general' })).toBeTruthy();
  expect(screen.getByText('settings.appearance.themeHint')).toBeTruthy();
  await act(async () => {});
  expect(bridge.CloudStatus).not.toHaveBeenCalled();
  expect(bridge.CloudMe).not.toHaveBeenCalled();
});

it('keeps the Dashboard quote embedded without a cloud request', async () => {
  render(<Dashboard flash={vi.fn()} />);
  await waitFor(() => expect(screen.getByText(/dashboard.hero.quoteFallbackText/)).toBeTruthy());
  expect(bridge.CloudRandomQuote).not.toHaveBeenCalled();
});
