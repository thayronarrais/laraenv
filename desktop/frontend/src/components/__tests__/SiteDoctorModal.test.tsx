// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { act, cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import SiteDoctorModal from '../SiteDoctorModal';
import { DialogProvider } from '../dialogs/DialogProvider';
import { I18nProvider } from '../../lib/i18n';

const backend = vi.hoisted(() => ({
  diagnose: vi.fn(), diagnoseAll: vi.fn(), fix: vi.fn(), cancel: vi.fn(),
  listeners: new Map<string, Set<(data: any) => void>>(),
}));
vi.mock('../../../wailsjs/go/main/App', () => ({
  GetLanguage: () => Promise.resolve('en'), SetLanguage: () => Promise.resolve(),
  DiagnoseProject: backend.diagnose, DiagnoseAllProjects: backend.diagnoseAll,
  ApplyDoctorFix: backend.fix, CancelDoctorFix: backend.cancel,
}));
vi.mock('../../../wailsjs/runtime', () => ({
  EventsOn: (name: string, handler: (data: any) => void) => {
    const handlers = backend.listeners.get(name) ?? new Set();
    handlers.add(handler); backend.listeners.set(name, handlers);
    return () => handlers.delete(handler);
  },
}));

const finding = (id: string, overrides = {}) => ({
  id, status: 'fail', severity: 'error', code: '', detail: '', fixID: id,
  requiresConfirmation: false, runtimeKind: '', installVersion: '', ...overrides,
});
const report = (project = 'shop', findings = [finding('env')]) => ({
  project, path: `C:\\sites\\${project}`, kind: 'laravel',
  healthy: findings.every(item => item.status !== 'fail' && item.status !== 'warning'), findings,
  phpRuntime: { version: '8.3', fullVersion: '8.3.30', source: 'composer.json', constraint: '^8.2', status: 'resolved' },
  nodeRuntime: { version: '', fullVersion: '', source: 'global', constraint: '', status: 'unmanaged' },
});
const result = { project: 'shop', fixID: 'env', operationID: '', success: true, output: '', exitCode: 0, truncated: false };
function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((done) => { resolve = done; });
  return { promise, resolve };
}
function mount(props = {}) {
  const onClose = vi.fn(); const onDone = vi.fn(); const onDownloads = vi.fn();
  const view = render(<I18nProvider><DialogProvider><SiteDoctorModal open projectName="shop" onClose={onClose}
    onDone={onDone} onOpenRuntimeDownloads={onDownloads} {...props} /></DialogProvider></I18nProvider>);
  return { ...view, onClose, onDone, onDownloads };
}
function emit(data: { operationID: string; project: string; line: string }) {
  act(() => { backend.listeners.get('doctor:output')?.forEach(handler => handler(data)); });
}
beforeEach(() => {
  vi.clearAllMocks(); backend.listeners.clear();
  backend.diagnose.mockResolvedValue(report());
  backend.diagnoseAll.mockResolvedValue([report(), report('blog', [finding('env', { status: 'pass', severity: 'info', fixID: '' })])]);
  backend.fix.mockImplementation(async request => ({ ...result, ...request }));
  backend.cancel.mockResolvedValue(undefined);
});
afterEach(cleanup);

describe('Site Doctor', () => {
  it('shows each finding status and only offers actions for an actionable failed check', async () => {
    backend.diagnose.mockResolvedValue(report('shop', [
      finding('env', { detail: 'The project has no .env file.' }),
      finding('app_key', { status: 'pass', severity: 'info', fixID: 'app_key' }),
      finding('hosts', { status: 'warning', severity: 'warning', fixID: '', detail: 'Hosts file needs an administrator.' }),
      finding('migrations', { status: 'skipped', severity: 'info', fixID: 'migrations' }),
    ]));
    mount();
    const env = await screen.findByRole('group', { name: '.env file' });
    expect(within(env).getByText('Failed')).toBeTruthy();
    expect(within(env).getByText('Error')).toBeTruthy();
    expect(within(env).getByText('The project has no .env file.')).toBeTruthy();
    expect(within(env).getByRole('button', { name: 'Fix .env file' })).toBeTruthy();
    expect(screen.queryByRole('button', { name: 'Fix Application key' })).toBeNull();
    expect(screen.queryByRole('button', { name: 'Fix Database migrations' })).toBeNull();
    expect(screen.getByText('Hosts file needs an administrator.')).toBeTruthy();
  });

  it('opens an overview for all reports and shows details only for the selected project', async () => {
    mount({ projectName: null });
    const shop = await screen.findByRole('button', { name: 'View diagnosis for shop' });
    expect(screen.getByRole('button', { name: 'View diagnosis for blog' })).toBeTruthy();
    expect(screen.queryByRole('group', { name: '.env file' })).toBeNull();
    fireEvent.click(shop);
    expect(screen.getByRole('heading', { name: 'shop' })).toBeTruthy();
    expect(screen.getByRole('group', { name: '.env file' })).toBeTruthy();
    expect(screen.queryByRole('heading', { name: 'blog' })).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: 'Back to all projects' }));
    expect(screen.getByRole('button', { name: 'View diagnosis for blog' })).toBeTruthy();
    expect(backend.diagnoseAll).toHaveBeenCalledTimes(1);
    expect(backend.diagnose).not.toHaveBeenCalled();
  });

  it('preserves the search, health filter, and page when returning from a project diagnosis', async () => {
    backend.diagnoseAll.mockResolvedValue(Array.from({ length: 50 }, (_, index) => report(`shop-${String(index + 1).padStart(2, '0')}`)));
    mount({ projectName: null });
    await screen.findByRole('button', { name: 'View diagnosis for shop-01' });
    fireEvent.click(screen.getByRole('button', { name: /Needs attention/ }));
    fireEvent.change(screen.getByRole('textbox', { name: 'Search projects' }), { target: { value: 'shop' } });
    fireEvent.click(screen.getByRole('button', { name: 'Next' }));
    fireEvent.click(screen.getByRole('button', { name: 'View diagnosis for shop-11' }));
    expect(screen.getByRole('heading', { name: 'shop-11' })).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Back to all projects' }));
    expect((screen.getByRole('textbox', { name: 'Search projects' }) as HTMLInputElement).value).toBe('shop');
    expect(screen.getByRole('button', { name: /Needs attention/ }).getAttribute('aria-pressed')).toBe('true');
    expect(screen.getByText('Page 2 of 5')).toBeTruthy();
    expect(screen.getByRole('button', { name: 'View diagnosis for shop-11' })).toBeTruthy();
    expect(screen.queryByRole('button', { name: 'View diagnosis for shop-01' })).toBeNull();
    expect(backend.diagnoseAll).toHaveBeenCalledTimes(1);
    expect(backend.diagnose).not.toHaveBeenCalled();
  });

  it('updates overview counts after repairing one project without diagnosing all projects again', async () => {
    mount({ projectName: null });
    fireEvent.click(await screen.findByRole('button', { name: 'View diagnosis for shop' }));
    const pending = deferred<typeof result>(); backend.fix.mockReturnValue(pending.promise);
    fireEvent.click(screen.getByRole('button', { name: 'Fix .env file' }));
    await waitFor(() => expect(backend.fix).toHaveBeenCalledTimes(1));
    expect((screen.getByRole('button', { name: 'Back to all projects' }) as HTMLButtonElement).disabled).toBe(true);
    backend.diagnose.mockResolvedValue(report('shop', [finding('env', { status: 'pass', severity: 'info', fixID: '' })]));
    await act(async () => pending.resolve({ ...result, output: 'Environment ready' }));
    await waitFor(() => expect(screen.queryByRole('button', { name: 'Fix .env file' })).toBeNull());
    fireEvent.click(screen.getByRole('button', { name: 'Back to all projects' }));
    expect(screen.getByRole('button', { name: /Healthy/ }).textContent).toContain('2');
    expect(screen.getByRole('button', { name: /Needs attention/ }).textContent).toContain('0');
    const outputToggle = screen.getByText('Repair output');
    const outputDisclosure = outputToggle.closest('details') as HTMLDetailsElement;
    expect(outputDisclosure.open).toBe(false);
    fireEvent.click(outputToggle);
    expect(outputDisclosure.open).toBe(true);
    expect(screen.getByRole('log').textContent).toContain('Environment ready');
    expect(backend.diagnoseAll).toHaveBeenCalledTimes(1);
    expect(backend.diagnose).toHaveBeenCalledTimes(1);
  });

  it('routes a missing runtime to Downloads without applying a repair', async () => {
    backend.diagnose.mockResolvedValue(report('shop', [finding('php_runtime', {
      fixID: 'download', runtimeKind: 'php', installVersion: '8.3.30',
    })]));
    const { onDownloads } = mount();
    fireEvent.click(await screen.findByRole('button', { name: 'Open PHP downloads' }));
    expect(onDownloads).toHaveBeenCalledWith('php', '8.3.30');
    expect(backend.fix).not.toHaveBeenCalled();
  });

  it('offers an optional storage link and rechecks the project after creating it', async () => {
    backend.diagnose.mockResolvedValueOnce(report('shop', [finding('storage_link', {
      status: 'optional', severity: 'info', code: 'storage_link_missing',
    })])).mockResolvedValue(report('shop', [finding('storage_link', {
      status: 'pass', severity: 'info', code: 'storage_link_ok', fixID: '',
    })]));
    const { onDone } = mount();
    const storage = await screen.findByRole('group', { name: 'Storage link' });
    expect(within(storage).getByText('Optional')).toBeTruthy();
    expect(within(storage).queryByText('Info')).toBeNull();
    expect(screen.getByText('Healthy')).toBeTruthy();
    expect(within(storage).queryByRole('button', { name: 'Fix Storage link' })).toBeNull();
    fireEvent.click(within(storage).getByRole('button', { name: 'Create link' }));
    await waitFor(() => expect(backend.fix).toHaveBeenCalledWith(expect.objectContaining({ project: 'shop', fixID: 'storage_link' })));
    await waitFor(() => expect(within(storage).getByText('Passed')).toBeTruthy());
    expect(screen.queryByRole('button', { name: 'Create link' })).toBeNull();
    expect(backend.diagnose).toHaveBeenCalledTimes(2);
    expect(onDone).toHaveBeenCalledTimes(1);
  });

  it('explains that an occupied optional storage path can be left alone without offering a destructive fix', async () => {
    backend.diagnose.mockResolvedValue(report('shop', [finding('storage_link', {
      status: 'optional', severity: 'info', code: 'storage_link_conflict', fixID: '',
    })]));
    mount();
    const storage = await screen.findByRole('group', { name: 'Storage link' });
    expect(within(storage).getByText('Optional')).toBeTruthy();
    expect(within(storage).getByText('Only needed to serve files from the local public disk. You can leave it as is if this project does not use it.')).toBeTruthy();
    expect(within(storage).queryByText('Manual review')).toBeNull();
    expect(within(storage).queryByRole('button')).toBeNull();
    expect(screen.getByText('Healthy')).toBeTruthy();
  });

  it.each(['fail', 'warning'])('explains manual review for a required %s without offering a fake fix', async status => {
    backend.diagnose.mockResolvedValue(report('shop', [finding('database', {
      status, severity: status === 'fail' ? 'error' : 'warning', code: 'database_auth_failed', fixID: '',
    })]));
    mount();
    const database = await screen.findByRole('group', { name: 'Database' });
    expect(within(database).getByText('Manual review')).toBeTruthy();
    expect(within(database).getByText('Follow the check guidance, then run the diagnosis again. Automatic repair is unavailable for this case.')).toBeTruthy();
    expect(within(database).queryByRole('button')).toBeNull();
    if (status === 'warning') expect(within(database).getAllByText('Warning')).toHaveLength(1);
    expect(screen.getByText('Needs attention')).toBeTruthy();
  });

  it('requires an explicit migration confirmation and leaves cancel side effect free', async () => {
    backend.diagnose.mockResolvedValue(report('shop', [finding('migrations', { requiresConfirmation: true })]));
    mount();
    fireEvent.click(await screen.findByRole('button', { name: 'Fix Database migrations' }));
    const confirmation = await screen.findByRole('dialog', { name: 'Run database migrations?' });
    expect(within(confirmation).getByText(/change your database/)).toBeTruthy();
    expect(backend.fix).not.toHaveBeenCalled();
    fireEvent.click(within(confirmation).getByRole('button', { name: 'Cancel' }));
    expect(backend.fix).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole('button', { name: 'Fix Database migrations' }));
    const next = await screen.findByRole('dialog', { name: 'Run database migrations?' });
    fireEvent.click(within(next).getByRole('button', { name: 'Run migrations' }));
    await waitFor(() => expect(backend.fix).toHaveBeenCalledWith(expect.objectContaining({ project: 'shop', fixID: 'migrations', confirmed: true })));
  });

  it('warns before replacing a custom virtual host configuration', async () => {
    backend.diagnose.mockResolvedValue(report('shop', [finding('vhost', { status: 'warning', severity: 'warning', requiresConfirmation: true })]));
    mount();
    fireEvent.click(await screen.findByRole('button', { name: 'Fix Virtual host' }));
    const confirmation = await screen.findByRole('dialog', { name: 'Replace the virtual host configuration?' });
    expect(within(confirmation).getByText(/remove your custom settings/)).toBeTruthy();
    expect(backend.fix).not.toHaveBeenCalled();
    fireEvent.click(within(confirmation).getByRole('button', { name: 'Replace configuration' }));
    await waitFor(() => expect(backend.fix).toHaveBeenCalledWith(expect.objectContaining({ project: 'shop', fixID: 'vhost', confirmed: true })));
  });

  it('streams only the active operation, retains command output, and refreshes the repaired report', async () => {
    const pending = deferred<typeof result>(); backend.fix.mockReturnValue(pending.promise);
    const { onClose, onDone } = mount();
    fireEvent.click(await screen.findByRole('button', { name: 'Fix .env file' }));
    await waitFor(() => expect(backend.fix).toHaveBeenCalledTimes(1));
    const request = backend.fix.mock.calls[0][0];
    expect(request).toMatchObject({ project: 'shop', fixID: 'env', confirmed: false });
    expect(request.operationID).toBeTruthy();
    emit({ operationID: 'someone-else', project: 'shop', line: 'Ignore another operation' });
    emit({ operationID: request.operationID, project: 'blog', line: 'Ignore another project' });
    emit({ operationID: request.operationID, project: 'shop', line: 'Creating environment file' });
    expect(screen.getByRole('log').textContent).toContain('Creating environment file');
    expect(screen.queryByText(/Ignore another/)).toBeNull();
    expect((screen.getByRole('button', { name: 'Close Site Doctor' }) as HTMLButtonElement).disabled).toBe(true);
    fireEvent.keyDown(window, { key: 'Escape' });
    expect(onClose).not.toHaveBeenCalled();
    backend.diagnose.mockResolvedValue(report('shop', [finding('env', { status: 'pass', severity: 'info', fixID: '' })]));
    await act(async () => pending.resolve({ ...result, operationID: request.operationID, output: 'Creating environment file\nDone\n' }));
    await waitFor(() => expect(screen.queryByRole('button', { name: 'Fix .env file' })).toBeNull());
    expect(backend.diagnose).toHaveBeenCalledTimes(2);
    expect(screen.getByRole('log').textContent).toContain('Done');
    expect(onDone).toHaveBeenCalledTimes(1);
  });

  it('keeps cancellation owned by the active operation until its command settles', async () => {
    const pending = deferred<typeof result>(); backend.fix.mockReturnValue(pending.promise);
    mount(); fireEvent.click(await screen.findByRole('button', { name: 'Fix .env file' }));
    const cancel = await screen.findByRole('button', { name: 'Cancel repair' });
    fireEvent.click(cancel);
    const request = backend.fix.mock.calls[0][0];
    await waitFor(() => expect(backend.cancel).toHaveBeenCalledWith(request.operationID));
    expect((screen.getByRole('button', { name: 'Close Site Doctor' }) as HTMLButtonElement).disabled).toBe(true);
    await act(async () => pending.resolve({ ...result, operationID: request.operationID, success: false, output: 'Operation canceled', exitCode: -1 }));
    expect(await screen.findByText('Operation canceled')).toBeTruthy();
    expect((screen.getByRole('button', { name: 'Close Site Doctor' }) as HTMLButtonElement).disabled).toBe(false);
  });

  it('rechecks only the repaired project after a failed command in an all-sites report', async () => {
    backend.fix.mockResolvedValue({ ...result, success: false, output: 'Composer could not download packages', exitCode: 2 });
    mount({ projectName: null });
    fireEvent.click(await screen.findByRole('button', { name: 'View diagnosis for shop' }));
    fireEvent.click(await screen.findByRole('button', { name: 'Fix .env file' }));
    expect((await screen.findByRole('alert')).textContent).toContain('exit code 2');
    expect(screen.getByRole('log').textContent).toContain('Composer could not download packages');
    await waitFor(() => expect(backend.diagnose).toHaveBeenCalledWith('shop'));
    expect(backend.diagnoseAll).toHaveBeenCalledTimes(1);
    fireEvent.click(screen.getByRole('button', { name: 'Back to all projects' }));
    expect(screen.getByRole('button', { name: 'View diagnosis for blog' })).toBeTruthy();
  });

  it('localizes a pending migration count instead of displaying an unexplained number', async () => {
    backend.diagnose.mockResolvedValue(report('shop', [finding('migrations', { code: 'migrations_pending', detail: '3', requiresConfirmation: true })]));
    mount();
    expect(await screen.findByText('Pending database migrations: 3.')).toBeTruthy();
    expect(screen.queryByText(/^3$/)).toBeNull();
  });

  it('tells the user when the displayed command output has reached its line limit', async () => {
    const pending = deferred<typeof result>(); backend.fix.mockReturnValue(pending.promise);
    mount(); fireEvent.click(await screen.findByRole('button', { name: 'Fix .env file' }));
    const request = backend.fix.mock.calls[0][0];
    act(() => {
      for (let index = 0; index < 1001; index++) {
        backend.listeners.get('doctor:output')?.forEach(handler => handler({ operationID: request.operationID, project: 'shop', line: `Progress ${index}` }));
      }
    });
    expect(screen.getByRole('log').textContent).not.toContain('Progress 0');
    expect(screen.getByRole('log').textContent).toContain('Progress 1000');
    expect(screen.getByText('Output was truncated. Only part of the command output is shown.')).toBeTruthy();
    await act(async () => pending.resolve({ ...result, operationID: request.operationID }));
  });

  it('shows diagnosis failure and can retry', async () => {
    backend.diagnose.mockRejectedValueOnce(new Error('Project moved'));
    mount();
    expect((await screen.findByRole('alert')).textContent).toContain('Project moved');
    fireEvent.click(screen.getByRole('button', { name: 'Run again' }));
    expect(await screen.findByRole('group', { name: '.env file' })).toBeTruthy();
  });

  it('explains a project inspection failure in an all-sites report without offering a repair', async () => {
    backend.diagnoseAll.mockResolvedValue([report('shop', [finding('document_root', {
      status: 'warning', severity: 'warning', code: 'inspection_failed', fixID: '',
    })])]);
    mount({ projectName: null });
    fireEvent.click(await screen.findByRole('button', { name: 'View diagnosis for shop' }));
    expect(await screen.findByText('The project could not be inspected. Run the checks again.')).toBeTruthy();
    expect(screen.queryByRole('button', { name: 'Fix Document root' })).toBeNull();
  });

  it('does not restore results after the modal closes and removes only its own stream listener', async () => {
    const pending = deferred<ReturnType<typeof report>>(); backend.diagnose.mockReturnValue(pending.promise);
    const external = vi.fn(); backend.listeners.set('doctor:output', new Set([external]));
    const view = mount();
    await waitFor(() => expect(backend.diagnose).toHaveBeenCalledWith('shop'));
    expect(backend.listeners.get('doctor:output')?.size).toBe(2);
    view.rerender(<I18nProvider><DialogProvider><SiteDoctorModal open={false} projectName="shop" onClose={() => {}} /></DialogProvider></I18nProvider>);
    await act(async () => pending.resolve(report()));
    expect(screen.queryByRole('dialog')).toBeNull();
    expect(backend.listeners.get('doctor:output')?.size).toBe(1);
    expect(backend.listeners.get('doctor:output')?.has(external)).toBe(true);
  });
});
