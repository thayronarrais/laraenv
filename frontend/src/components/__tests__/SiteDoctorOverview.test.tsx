// @vitest-environment jsdom
import { useState } from 'react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import SiteDoctorOverview, { initialDoctorOverviewState } from '../SiteDoctorOverview';
import type { DoctorOverviewState } from '../SiteDoctorOverview';
import type { DoctorFinding, DoctorReport } from '../SiteDoctorModal';
import { I18nProvider } from '../../lib/i18n';

vi.mock('../../../wailsjs/go/main/App', () => ({
  GetLanguage: () => Promise.resolve('en'), SetLanguage: () => Promise.resolve(),
}));
vi.mock('../../../wailsjs/runtime', () => ({ EventsOn: () => () => {} }));

const finding = (status: string, overrides: Partial<DoctorFinding> = {}): DoctorFinding => ({
  id: 'env', status, severity: status === 'fail' ? 'error' : 'info', code: '',
  detail: 'Detailed finding stays in the project diagnosis.', fixID: '',
  requiresConfirmation: false, runtimeKind: '', installVersion: '', ...overrides,
});
const report = (project: string, healthy = false, overrides: Partial<DoctorReport> = {}): DoctorReport => ({
  project, path: `C:\\sites\\${project}`, kind: 'laravel', healthy,
  findings: [finding(healthy ? 'pass' : 'fail')],
  phpRuntime: { version: '8.3', fullVersion: '8.3.30', source: 'composer.json', constraint: '^8.2', status: 'resolved' },
  nodeRuntime: { version: '', fullVersion: '', source: 'global', constraint: '', status: 'unmanaged' },
  ...overrides,
});
const manyReports = (count = 52) => Array.from({ length: count }, (_, index) => report(`site-${String(index + 1).padStart(2, '0')}`));

function Harness({ reports, initial = initialDoctorOverviewState, onSelect = () => {}, onChange = () => {} }: {
  reports: DoctorReport[];
  initial?: DoctorOverviewState;
  onSelect?: (project: string) => void;
  onChange?: (state: DoctorOverviewState) => void;
}) {
  const [state, setState] = useState(initial);
  return <SiteDoctorOverview reports={reports} state={state} onSelect={onSelect} onChange={next => {
    onChange(next); setState(next);
  }} />;
}
function mount(reports: DoctorReport[], options: Omit<Parameters<typeof Harness>[0], 'reports'> = {}) {
  return render(<I18nProvider><Harness reports={reports} {...options} /></I18nProvider>);
}
const projectButtons = () => screen.queryAllByRole('button', { name: /^View diagnosis for / });
afterEach(cleanup);

describe('Site Doctor overview', () => {
  it('summarizes every report, puts attention first, and opens a project without rendering its findings', async () => {
    const onSelect = vi.fn(); const onChange = vi.fn();
    mount([report('alpha', true), report('zulu'), report('Beta'), report('gamma', true)], { onSelect, onChange });
    const all = await screen.findByRole('button', { name: 'All projects 4' });
    expect(all.getAttribute('aria-pressed')).toBe('true');
    expect(screen.getByRole('button', { name: 'Needs attention 2' }).getAttribute('aria-pressed')).toBe('false');
    expect(screen.getByRole('button', { name: 'Healthy 2' })).toBeTruthy();
    expect(projectButtons().map(button => button.getAttribute('aria-label'))).toEqual([
      'View diagnosis for Beta', 'View diagnosis for zulu', 'View diagnosis for alpha', 'View diagnosis for gamma',
    ]);
    expect(screen.queryByText('Detailed finding stays in the project diagnosis.')).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: 'View diagnosis for Beta' }));
    expect(onSelect).toHaveBeenCalledWith('Beta');
    expect(onChange).not.toHaveBeenCalled();
  });

  it('shows compact status, kind, failure and warning counts, with honest skipped and paused badges', async () => {
    mount([
      report('shop', false, { findings: [finding('fail'), finding('fail'), finding('warning'), finding('skipped')] }),
      report('paused', true, { kind: 'wordpress', findings: [finding('pass'), finding('skipped', { code: 'project_paused' }), finding('skipped', { code: 'not_applicable' })] }),
    ]);
    const shop = await screen.findByRole('button', { name: 'View diagnosis for shop' });
    expect(within(shop).getByText('Laravel')).toBeTruthy();
    expect(within(shop).getByText('Needs attention')).toBeTruthy();
    expect(within(shop).getByText('2 failed')).toBeTruthy();
    expect(within(shop).getByText('1 warning')).toBeTruthy();
    expect(within(shop).getByText('1 not checked')).toBeTruthy();
    const paused = screen.getByRole('button', { name: 'View diagnosis for paused' });
    expect(within(paused).getByText('WordPress')).toBeTruthy();
    expect(within(paused).getByText('Healthy')).toBeTruthy();
    expect(within(paused).getByText('Paused')).toBeTruthy();
    expect(within(paused).getByText('2 not checked')).toBeTruthy();
    expect(screen.getByText(/^Healthy means no failed checks or warnings in the checks performed\./)).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Healthy 1' }));
    expect(projectButtons()).toHaveLength(1);
    expect(screen.getByRole('button', { name: 'View diagnosis for paused' })).toBeTruthy();
  });

  it('uses the report health flag for filters and keeps overall summary counts during a search', async () => {
    mount([report('attention', false, { findings: [] }), report('healthy', true)]);
    fireEvent.click(await screen.findByRole('button', { name: 'Needs attention 1' }));
    expect(projectButtons()).toHaveLength(1);
    expect(screen.getByRole('button', { name: 'View diagnosis for attention' })).toBeTruthy();
    fireEvent.change(screen.getByRole('textbox', { name: 'Search projects' }), { target: { value: 'attention' } });
    expect(screen.getByRole('button', { name: 'All projects 2' })).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Healthy 1' })).toBeTruthy();
  });

  it('keeps optional suggestions separate from warnings while a manual database problem still needs attention', async () => {
    mount([
      report('optional-storage', true, { findings: [finding('optional', { id: 'storage_link', code: 'storage_link_missing', fixID: 'storage_link' })] }),
      report('manual-database', false, { findings: [finding('warning', { id: 'database', code: 'database_auth_failed', severity: 'warning' })] }),
    ]);
    const optional = await screen.findByRole('button', { name: 'View diagnosis for optional-storage' });
    expect(within(optional).getByText('Healthy')).toBeTruthy();
    expect(within(optional).getByText('1 optional suggestion')).toBeTruthy();
    expect(within(optional).getByText('0 warnings')).toBeTruthy();
    const manual = screen.getByRole('button', { name: 'View diagnosis for manual-database' });
    expect(within(manual).getByText('Needs attention')).toBeTruthy();
    expect(within(manual).getByText('1 warning')).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Healthy 1' })).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Needs attention 1' })).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Healthy 1' }));
    expect(projectButtons()).toHaveLength(1);
    expect(screen.getByRole('button', { name: 'View diagnosis for optional-storage' })).toBeTruthy();
  });

  it('limits the initial page to ten projects and navigates with accessible boundary controls', async () => {
    mount(manyReports());
    await screen.findByRole('button', { name: 'View diagnosis for site-01' });
    expect(projectButtons()).toHaveLength(10);
    expect(screen.getByText('Showing 1–10 of 52 projects')).toBeTruthy();
    expect(screen.getByText('Page 1 of 6')).toBeTruthy();
    const nav = screen.getByRole('navigation', { name: 'Project pages' });
    expect((within(nav).getByRole('button', { name: 'Previous' }) as HTMLButtonElement).disabled).toBe(true);
    for (let page = 1; page < 6; page++) fireEvent.click(within(nav).getByRole('button', { name: 'Next' }));
    expect(projectButtons()).toHaveLength(2);
    expect(screen.getByRole('button', { name: 'View diagnosis for site-51' })).toBeTruthy();
    expect(screen.getByText('Showing 51–52 of 52 projects')).toBeTruthy();
    expect((within(nav).getByRole('button', { name: 'Next' }) as HTMLButtonElement).disabled).toBe(true);
    fireEvent.click(within(nav).getByRole('button', { name: 'Previous' }));
    expect(screen.getByText('Page 5 of 6')).toBeTruthy();
    expect(projectButtons()).toHaveLength(10);
  });

  it('offers only ten, twenty, or fifty projects per page and resets page when the size changes', async () => {
    mount(manyReports());
    await screen.findByRole('button', { name: 'View diagnosis for site-01' });
    fireEvent.click(screen.getByRole('button', { name: 'Next' }));
    const size = screen.getByRole('combobox', { name: 'Projects per page' });
    expect(within(size).getAllByRole('option').map(option => (option as HTMLOptionElement).value)).toEqual(['10', '20', '50']);
    fireEvent.change(size, { target: { value: '20' } });
    expect(projectButtons()).toHaveLength(20);
    expect(screen.getByText('Page 1 of 3')).toBeTruthy();
    fireEvent.change(size, { target: { value: '50' } });
    expect(projectButtons()).toHaveLength(50);
    expect(screen.getByText('Page 1 of 2')).toBeTruthy();
  });

  it('searches project names or paths without case sensitivity and resets to the first page', async () => {
    const reports = manyReports(); reports[20] = report('site-21', false, { path: 'D:\\Work\\SpecialFolder' });
    mount(reports, { initial: { ...initialDoctorOverviewState, page: 2 } });
    const search = await screen.findByRole('textbox', { name: 'Search projects' });
    fireEvent.change(search, { target: { value: '  SPECIALfolder  ' } });
    expect(projectButtons()).toHaveLength(1);
    expect(screen.getByRole('button', { name: 'View diagnosis for site-21' })).toBeTruthy();
    expect(screen.getByText('Page 1 of 1')).toBeTruthy();
    fireEvent.change(search, { target: { value: 'SITE-03' } });
    expect(screen.getByRole('button', { name: 'View diagnosis for site-03' })).toBeTruthy();
  });

  it('clears the search and resets the page while preserving the selected health filter', async () => {
    const onChange = vi.fn();
    mount([...manyReports(), report('healthy', true)], { initial: { search: 'site', filter: 'attention', page: 2, pageSize: 10 }, onChange });
    fireEvent.click(await screen.findByRole('button', { name: 'Clear search' }));
    expect((screen.getByRole('textbox', { name: 'Search projects' }) as HTMLInputElement).value).toBe('');
    expect(screen.getByRole('button', { name: 'Needs attention 52' }).getAttribute('aria-pressed')).toBe('true');
    expect(screen.getByText('Page 1 of 6')).toBeTruthy();
    expect(onChange).toHaveBeenLastCalledWith({ search: '', filter: 'attention', page: 1, pageSize: 10 });
  });

  it('resets page when a health filter changes', async () => {
    mount([...manyReports(), report('healthy', true)], { initial: { ...initialDoctorOverviewState, page: 2 } });
    fireEvent.click(await screen.findByRole('button', { name: 'Healthy 1' }));
    expect(screen.getByText('Page 1 of 1')).toBeTruthy();
    expect(screen.getByRole('button', { name: 'View diagnosis for healthy' })).toBeTruthy();
  });

  it('clamps a preserved page when refreshed diagnoses reduce the filtered results', async () => {
    const onChange = vi.fn(); const reports = manyReports(21);
    const options = { initial: { ...initialDoctorOverviewState, filter: 'attention' as const, page: 3 }, onChange };
    const view = mount(reports, options);
    await screen.findByRole('button', { name: 'View diagnosis for site-21' });
    const refreshed = reports.map((item, index) => index < 11 ? item : { ...item, healthy: true });
    view.rerender(<I18nProvider><Harness reports={refreshed} {...options} /></I18nProvider>);
    await waitFor(() => expect(onChange).toHaveBeenLastCalledWith({ search: '', filter: 'attention', page: 2, pageSize: 10 }));
    expect(screen.getByText('Page 2 of 2')).toBeTruthy();
    expect(screen.getByRole('button', { name: 'View diagnosis for site-11' })).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Needs attention 11' })).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Healthy 10' })).toBeTruthy();
  });

  it('distinguishes no matching results from an empty report collection', async () => {
    const view = mount([report('shop')]);
    fireEvent.change(await screen.findByRole('textbox', { name: 'Search projects' }), { target: { value: 'unknown' } });
    expect(screen.getByText('No projects match your search and filter.')).toBeTruthy();
    expect(projectButtons()).toHaveLength(0);
    expect((screen.getByRole('button', { name: 'Next' }) as HTMLButtonElement).disabled).toBe(true);
    expect((screen.getByRole('button', { name: 'Previous' }) as HTMLButtonElement).disabled).toBe(true);
    view.rerender(<I18nProvider><Harness reports={[]} /></I18nProvider>);
    expect(screen.getByText('No projects to diagnose.')).toBeTruthy();
    expect(screen.queryByText('No projects match your search and filter.')).toBeNull();
  });
});
