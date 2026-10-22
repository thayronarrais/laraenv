// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { ProjectRuntimeBadge, ProjectRuntimeSelector, type ProjectRuntimeResolution } from '../ProjectRuntimeSelector';
import { I18nProvider } from '../../lib/i18n';

vi.mock('../../../wailsjs/go/main/App', () => ({ GetLanguage: () => Promise.resolve('en'), SetLanguage: () => Promise.resolve() }));
vi.mock('../../../wailsjs/runtime', () => ({ EventsOn: () => () => {} }));
afterEach(cleanup);
const detected: ProjectRuntimeResolution = { version: '8.3', fullVersion: '8.3.30', source: 'composer.json', constraint: '^8.2', status: 'resolved' };
function mount(runtime: ProjectRuntimeResolution, manualVersion = '') {
  const onPick = vi.fn(); const onDownloads = vi.fn();
  render(<I18nProvider><ProjectRuntimeSelector kind="php" manualVersion={manualVersion} defaultVersion="8.4" runtime={runtime}
    versions={[{ version: '8.3' }, { version: '8.4' }]} open setOpen={() => {}} onPick={onPick} onOpenDownloads={onDownloads} /></I18nProvider>);
  return { onPick, onDownloads };
}

describe('project runtime selection', () => {
  it('shows detected provenance and lets automatic selection clear the manual override', () => {
    const { onPick } = mount(detected);
    expect(screen.getByRole('button', { name: 'PHP 8.3.30' })).toBeTruthy();
    expect(screen.getByText('Detected')).toBeTruthy();
    expect(screen.getByText('composer.json: ^8.2')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: /Automatic/ }));
    expect(onPick).toHaveBeenCalledWith('');
    fireEvent.click(screen.getByRole('button', { name: /^PHP 8.4$/ }));
    expect(onPick).toHaveBeenLastCalledWith('8.4');
  });

  it('preserves an incompatible manual choice and warns about the project requirement', () => {
    mount({ ...detected, version: '8.1', fullVersion: '8.1.33', source: 'manual', status: 'incompatible' }, '8.1');
    expect(screen.getByRole('button', { name: 'PHP 8.1.33' })).toBeTruthy();
    expect(screen.getByText('Manual')).toBeTruthy();
    expect(screen.getByText('Manual: ^8.2')).toBeTruthy();
    expect(screen.getByRole('status').textContent).toContain('^8.2');
    expect(screen.getByRole('status').textContent).toContain('manual');
  });

  it('marks the installed series selected for a legacy manual PHP patch preference', () => {
    mount({ ...detected, source: 'manual', constraint: '' }, '8.3.15');
    expect(screen.getByRole('button', { name: 'PHP 8.3.30' })).toBeTruthy();
    expect(screen.getByRole('button', { name: 'PHP 8.3 (current)' })).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Automatic' })).toBeTruthy();
  });

  it('identifies a missing manual choice even when no project requirement exists', () => {
    mount({ ...detected, source: 'manual', constraint: '', version: '', fullVersion: '', status: 'missing' }, '8.1');
    expect(screen.getByRole('status').textContent).toContain('Manual PHP version 8.1');
    expect(screen.getByRole('button', { name: 'Open PHP downloads' })).toBeTruthy();
  });

  it('keeps an installed manual runtime effective while warning about an invalid project declaration', () => {
    mount({ ...detected, source: 'manual', constraint: 'not-a-version', status: 'invalid' }, '8.3');
    expect(screen.getByRole('button', { name: 'PHP 8.3.30' })).toBeTruthy();
    expect(screen.getByRole('status').textContent).toContain('not-a-version');
    expect(screen.queryByRole('button', { name: 'PHP Invalid requirement' })).toBeNull();
  });

  it('keeps a missing requirement visible and opens its suggested download instead of a global fallback', () => {
    const { onDownloads } = mount({ ...detected, version: '', fullVersion: '', status: 'missing', installVersion: '8.3.30' });
    expect(screen.queryByRole('button', { name: 'PHP 8.4' })).toBeTruthy(); // available manual option
    expect(screen.getByRole('button', { name: 'PHP Missing' })).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Install PHP 8.3.30' }));
    expect(onDownloads).toHaveBeenCalledWith('php', '8.3.30');
  });

  it('does not offer an invented installation for an invalid version requirement', () => {
    mount({ ...detected, version: '', fullVersion: '', status: 'invalid', constraint: 'not-a-version' });
    expect(screen.getByRole('button', { name: 'PHP Invalid requirement' })).toBeTruthy();
    expect(screen.getByRole('status').textContent).toContain('not-a-version');
    expect(screen.queryByRole('button', { name: /Install PHP/ })).toBeNull();
  });

  it('treats an unknown binding status as unresolved without selecting the global default', () => {
    mount({ ...detected, version: '', fullVersion: '', source: 'future-source', status: 'future-status' });
    expect(screen.getByRole('button', { name: 'PHP Invalid requirement' })).toBeTruthy();
    expect(screen.getByText('future-source: ^8.2')).toBeTruthy();
    expect(screen.getByRole('status').textContent).toContain('^8.2');
    expect(screen.queryByRole('button', { name: /Install PHP/ })).toBeNull();
  });

  it('distinguishes system PATH from a detected managed Node version', () => {
    render(<I18nProvider><ProjectRuntimeBadge kind="nodejs" manualVersion="" defaultVersion="22" runtime={{ version: '', fullVersion: '', source: 'global', constraint: '', status: 'unmanaged' }} /></I18nProvider>);
    expect(screen.getByText('Node System PATH')).toBeTruthy();
    expect(screen.queryByText(/Node 22/)).toBeNull();
    expect(screen.queryByText('Global default')).toBeNull();
  });
});
