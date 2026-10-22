// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import SSHHostModal from '../SSHHostModal';
import { I18nProvider } from '../../lib/i18n';
import type { SSHHost } from '../../pages/SSH';

const bridge = vi.hoisted(() => ({ save: vi.fn() }));
vi.mock('../../../wailsjs/go/main/App', () => ({
  SaveSSHHost: bridge.save,
  ListSSHHosts: () => Promise.resolve([]),
  ListSSHKeys: () => Promise.resolve([]),
  ReadSSHPublicKey: () => Promise.resolve(''),
  TestSSHConnection: vi.fn(),
  GetLanguage: () => Promise.resolve('en'),
  SetLanguage: () => Promise.resolve(),
  GetLanguageOverrides: () => Promise.resolve({ dir: '', catalogues: {}, results: [] }),
}));
vi.mock('../../../wailsjs/runtime', () => ({ EventsOn: () => () => {} }));
vi.mock('../SSHKeyGenModal', () => ({ default: () => null }));
vi.mock('../SSHImportKeyModal', () => ({ default: () => null }));

const unavailable = { name: 'production', address: 'fixture.invalid', port: 22, user: 'developer', tags: [], authMode: 'password', passwordUnavailable: true } as SSHHost;
function mount() {
  const onClose = vi.fn();
  render(<I18nProvider><SSHHostModal open editing={unavailable} onClose={onClose} flash={() => {}} /></I18nProvider>);
  return { onClose };
}
beforeEach(() => { bridge.save.mockReset().mockResolvedValue(undefined); });
afterEach(cleanup);

describe('SSH password recovery after moving a portable environment', () => {
  it('explains the unavailable password and prevents replacing it with an empty value', async () => {
    mount();
    await waitFor(() => expect(screen.getByRole('alert').textContent).toMatch(/re-enter/i));
    fireEvent.click(screen.getByRole('button', { name: /^Save/ }));
    expect(bridge.save).not.toHaveBeenCalled();
  });

  it('lets a replacement password be saved for the current Windows account', async () => {
    const { onClose } = mount();
    await screen.findByRole('alert');
    const input = screen.getByLabelText(/^Password/);
    fireEvent.change(input, { target: { value: 'fixture-replacement' } });
    fireEvent.click(screen.getByRole('button', { name: /^Save/ }));
    await waitFor(() => expect(onClose).toHaveBeenCalledOnce());
    expect(bridge.save).toHaveBeenCalledWith(expect.objectContaining({ name: 'production', password: 'fixture-replacement' }));
  });
});
