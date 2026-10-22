// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import Downloads from '../Downloads';
import { I18nProvider } from '../../lib/i18n';

const backend = vi.hoisted(() => ({ install: vi.fn(() => Promise.resolve()) }));
const sources = [
  { kind: 'php', version: '8.3.30', url: '', installTo: 'php/8.3', flatten: true, installed: false },
  { kind: 'php', version: '8.4.16', url: '', installTo: 'php/8.4', flatten: true, installed: false },
  { kind: 'nodejs', version: '22.14.0', url: '', installTo: 'nodejs/22', flatten: true, installed: false },
];
vi.mock('../../../wailsjs/go/main/App', () => ({
  GetLanguage: () => Promise.resolve('en'), SetLanguage: () => Promise.resolve(),
  ListSources: () => Promise.resolve(sources), InstallSource: backend.install,
  GetEnabledServices: () => Promise.resolve({}), ServiceStatus: () => Promise.resolve('stopped'),
  OpenSourcePage: vi.fn(), InstallFromFile: vi.fn(), SetServiceEnabled: vi.fn(), OpenURL: vi.fn(),
}));
vi.mock('../../../wailsjs/runtime', () => ({ EventsOn: () => () => {} }));
afterEach(() => { cleanup(); vi.clearAllMocks(); });

describe('project runtime downloads', () => {
  it('focuses the requested PHP build without installing it and can restore the full catalog', async () => {
    render(<I18nProvider><Downloads flash={() => {}} runtimeRequest={{ seq: 1, kind: 'php', version: '8.3.30' }} /></I18nProvider>);
    expect(await screen.findByText('8.3.30')).toBeTruthy();
    expect(screen.queryByText('8.4.16')).toBeNull();
    expect(screen.queryByText('22.14.0')).toBeNull();
    expect(screen.getByText('Suggested for this project')).toBeTruthy();
    expect(backend.install).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole('button', { name: 'Show full catalog' }));
    expect(await screen.findByText('22.14.0')).toBeTruthy();
    expect(screen.getByText('8.4.16')).toBeTruthy();
  });

  it('consumes a later request on the mounted page and keeps unavailable suggestions actionable', async () => {
    const view = render(<I18nProvider><Downloads flash={() => {}} runtimeRequest={{ seq: 1, kind: 'php' }} /></I18nProvider>);
    expect(await screen.findByText('8.3.30')).toBeTruthy();
    expect(screen.queryByText('22.14.0')).toBeNull();
    view.rerender(<I18nProvider><Downloads flash={() => {}} runtimeRequest={{ seq: 2, kind: 'nodejs', version: '99.1.0' }} /></I18nProvider>);
    await waitFor(() => expect(screen.queryByText('8.3.30')).toBeNull());
    expect(screen.getByText('22.14.0')).toBeTruthy();
    expect(screen.getByRole('status').textContent).toContain('99.1.0');
    fireEvent.click(screen.getByRole('button', { name: /^install$/i }));
    await waitFor(() => expect(backend.install).toHaveBeenCalledWith('nodejs', '22.14.0'));
  });
});
