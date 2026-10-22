// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import Projects from '../Projects';
import { DialogProvider } from '../../components/dialogs/DialogProvider';
import { I18nProvider } from '../../lib/i18n';

const backend = vi.hoisted(() => ({ api: {} as Record<string, ReturnType<typeof vi.fn>> }));
vi.mock('../../../wailsjs/go/main/App', async importOriginal => {
  const original = await importOriginal<Record<string, unknown>>();
  return Object.fromEntries(Object.entries(original).map(([name, value]) => {
    if (typeof value !== 'function') return [name, value];
    const mock = vi.fn().mockResolvedValue(undefined);
    backend.api[name] = mock;
    return [name, mock];
  }));
});
vi.mock('../../../wailsjs/runtime', () => ({
  EventsOn: () => () => {}, EventsOff: () => {}, ClipboardSetText: () => Promise.resolve(true),
}));
vi.mock('../../components/SiteDoctorModal', () => ({
  default: ({ open, projectName, onClose }: { open: boolean; projectName: string | null; onClose(): void }) => open
    ? <div role="dialog" aria-label="Doctor test" onKeyDown={event => { if (event.key === 'Escape') onClose(); }}><span data-testid="doctor-scope">{projectName ?? 'all'}</span>
      <button onClick={onClose}>Close test Doctor</button></div> : null,
}));
vi.mock('../../components/DevServerLogsModal', () => ({ default: () => null }));

const project = (name: string, tags = ['Client A']) => ({
  name, url: `http://${name}.test`, path: `C:\\sites\\${name}`, kind: 'laravel', docRoot: `C:\\sites\\${name}\\public`,
  phpVersion: '', nodeVersion: '', ssl: false, jsFramework: 'vite', packageManager: 'npm', proxyPort: 0, tags,
  phpRuntime: { version: '8.3', fullVersion: '8.3.30', source: 'composer.json', constraint: '^8.3', status: 'resolved' },
  nodeRuntime: { version: '22', fullVersion: '22.10.0', source: 'package.json', constraint: '^22', status: 'resolved' },
});
const fixtures = [project('alpha'), project('beta', ['Client B']),
  ...Array.from({ length: 56 }, (_, index) => project(`project-${String(index).padStart(2, '0')}`, ['Client C']))];

function setWidth(width: number) {
  vi.stubGlobal('innerWidth', width);
  vi.stubGlobal('matchMedia', vi.fn((query: string) => ({
    matches: /max-width\s*:\s*1099px/.test(query) && width <= 1099,
    media: query, onchange: null, addEventListener: vi.fn(), removeEventListener: vi.fn(),
    addListener: vi.fn(), removeListener: vi.fn(), dispatchEvent: vi.fn(),
  })));
}
function mount(width = 1280) {
  setWidth(width);
  const flash = vi.fn();
  const onOpenTerminal = vi.fn();
  const onRunCommand = vi.fn();
  render(<I18nProvider><DialogProvider><Projects flash={flash} onOpenLocalHttps={vi.fn()}
    onOpenTerminal={onOpenTerminal} onRunCommand={onRunCommand} onOpenAgent={vi.fn()} onOpenCron={vi.fn()} /></DialogProvider></I18nProvider>);
  return { flash, onOpenTerminal, onRunCommand };
}
async function select(name = 'alpha') {
  fireEvent.click(await screen.findByRole('button', { name: `Select project ${name}` }));
  return screen.findByRole('region', { name: /Project details/i });
}
function expectCalledOnce(mock: ReturnType<typeof vi.fn>, ...args: unknown[]) {
  expect(mock).toHaveBeenCalledTimes(1);
  expect(mock).toHaveBeenCalledWith(...args);
}

beforeEach(() => {
  localStorage.clear();
  Object.values(backend.api).forEach(mock => mock.mockReset().mockResolvedValue(undefined));
  backend.api.GetLanguage.mockResolvedValue('en');
  backend.api.GetLanguageOverrides.mockResolvedValue({ catalogues: {}, rejected: [] });
  backend.api.ListProjects.mockResolvedValue(fixtures);
  backend.api.GetProjectsConfig.mockResolvedValue({ tld: 'test', root: 'C:\\sites', phpVersions: {}, ssl: {}, devCommands: {} });
  backend.api.ListPhpVersions.mockResolvedValue([{ version: '8.3', active: true, path: '' }, { version: '8.4', active: false, path: '' }]);
  backend.api.ListNodeVersions.mockResolvedValue([{ version: '22', active: true, path: '' }, { version: '24', active: false, path: '' }]);
  backend.api.ListTerminals.mockResolvedValue({ profiles: [{ kind: 'gitbash', label: 'Git Bash', available: true, path: '' }], defaultKind: 'gitbash', projectKinds: {} });
  backend.api.ListEditors.mockResolvedValue({ profiles: [{ kind: 'vscode', label: 'VS Code', available: true, path: '' }], defaultKind: 'vscode', defaultCustomCmd: '', projectKinds: {}, projectCustomCmds: {} });
  backend.api.GetDevServerStatus.mockResolvedValue({ running: false, pid: 0, port: 0, script: '' });
  backend.api.GetQueueWorkerStatus.mockResolvedValue({ running: false, pid: 0, args: [] });
  backend.api.ListProjectScripts?.mockResolvedValue(['build', 'dev', 'watch']);
  backend.api.ServiceStatus.mockResolvedValue('running');
  backend.api.LocalCAStatus.mockResolvedValue({ portable: true, exists: true, trusted: true, checkError: '' });
});
afterEach(() => { cleanup(); vi.unstubAllGlobals(); });

describe('Projects workspace integration', () => {
  it('confirms the HTTPS state and browser URL when no runtime event arrives', async () => {
    backend.api.SetProjectSSL.mockImplementation(async (name: string, enabled: boolean) => {
      expect(name).toBe('alpha');
      expect(enabled).toBe(true);
      backend.api.ListProjects.mockResolvedValue(fixtures.map(item => item.name === 'alpha'
        ? { ...item, ssl: true, url: 'https://alpha.test' } : item));
    });
    mount();
    const panel = await select();
    fireEvent.click(within(panel).getByRole('button', { name: /^(SSL off|Enable HTTPS)$/ }));
    await waitFor(() => expect(within(panel).getByRole('button', { name: /^(SSL on|Disable HTTPS)$/ })).toBeTruthy());
    fireEvent.click(within(panel).getByRole('button', { name: 'Browser' }));
    expectCalledOnce(backend.api.OpenURL, 'https://alpha.test');
    expect(backend.api.TrustLocalCA).not.toHaveBeenCalled();
  });

  it('shows progress and prevents duplicate HTTPS changes while a request is pending', async () => {
    let finish!: () => void;
    backend.api.SetProjectSSL.mockImplementation(() => new Promise<void>(resolve => { finish = resolve; }));
    mount();
    const panel = await select();
    const toggle = within(panel).getByRole('button', { name: /^(SSL off|Enable HTTPS)$/ });
    fireEvent.click(toggle);
    fireEvent.click(toggle);
    expectCalledOnce(backend.api.SetProjectSSL, 'alpha', true);
    expect(toggle.hasAttribute('disabled')).toBe(true);
    expect(within(panel).getByRole('status').textContent).toMatch(/Enabling HTTPS/);
    finish();
    await waitFor(() => expect(toggle.hasAttribute('disabled')).toBe(false));
  });

  it('keeps a successful HTTPS action confirmed when a later list refresh is unavailable', async () => {
    backend.api.SetProjectSSL.mockImplementation(async () => {
      backend.api.ListProjects.mockRejectedValue(new Error('List unavailable'));
    });
    mount();
    const panel = await select();
    fireEvent.click(within(panel).getByRole('button', { name: 'Enable HTTPS' }));
    await waitFor(() => expect(within(panel).getByRole('button', { name: 'Disable HTTPS' }).hasAttribute('disabled')).toBe(false));
    fireEvent.click(within(panel).getByRole('button', { name: 'Browser' }));
    expectCalledOnce(backend.api.OpenURL, 'https://alpha.test');
    expect(within(panel).queryByRole('alert')).toBeNull();
  });

  it('keeps a pending HTTPS action scoped to its project when selection changes', async () => {
    let finishAlpha!: () => void;
    backend.api.SetProjectSSL.mockImplementation((name: string) => name === 'alpha'
      ? new Promise<void>(resolve => { finishAlpha = resolve; }) : Promise.resolve());
    mount();
    const alpha = await select();
    fireEvent.click(within(alpha).getByRole('button', { name: 'Enable HTTPS' }));
    const beta = await select('beta');
    const betaToggle = within(beta).getByRole('button', { name: 'Enable HTTPS' });
    expect(betaToggle.hasAttribute('disabled')).toBe(false);
    fireEvent.click(betaToggle);
    await waitFor(() => expect(within(beta).getByRole('button', { name: 'Disable HTTPS' }).hasAttribute('disabled')).toBe(false));
    const alphaAgain = await select();
    expect(within(alphaAgain).getByRole('button', { name: 'Enable HTTPS' }).hasAttribute('disabled')).toBe(true);
    finishAlpha();
    await waitFor(() => expect(within(alphaAgain).getByRole('button', { name: 'Disable HTTPS' }).hasAttribute('disabled')).toBe(false));
    expect(backend.api.SetProjectSSL.mock.calls).toEqual([['alpha', true], ['beta', true]]);
  });

  it('waits for explicit CA trust and reports a trust failure without undoing HTTPS', async () => {
    backend.api.LocalCAStatus.mockResolvedValue({ portable: true, exists: true, trusted: false, checkError: '' });
    backend.api.TrustLocalCA.mockRejectedValue(new Error('Windows denied trust'));
    backend.api.SetProjectSSL.mockImplementation(async () => {
      backend.api.ListProjects.mockResolvedValue(fixtures.map(item => item.name === 'alpha'
        ? { ...item, ssl: true, url: 'https://alpha.test' } : item));
    });
    mount();
    const panel = await select();
    fireEvent.click(within(panel).getByRole('button', { name: 'Enable HTTPS' }));
    const confirmation = await screen.findByRole('dialog', { name: 'Trust the local certificate authority?' });
    expect(backend.api.TrustLocalCA).not.toHaveBeenCalled();
    fireEvent.click(within(confirmation).getByRole('button', { name: 'Trust local CA' }));
    expect((await within(panel).findByRole('alert')).textContent).toContain('Windows denied trust');
    await waitFor(() => expect(within(panel).getByRole('button', { name: 'Disable HTTPS' }).hasAttribute('disabled')).toBe(false));
    expectCalledOnce(backend.api.TrustLocalCA);
    expectCalledOnce(backend.api.NotifyLocalCAUntrusted, 'alpha');
  });

  it('keeps the original HTTPS state and displays a failed backend action beside the URL', async () => {
    backend.api.SetProjectSSL.mockRejectedValue(new Error('Certificate directory is read-only'));
    const { flash } = mount();
    const panel = await select();
    fireEvent.click(within(panel).getByRole('button', { name: /^(SSL off|Enable HTTPS)$/ }));
    const error = await within(panel).findByRole('alert');
    expect(error.textContent).toContain('Certificate directory is read-only');
    expect(within(panel).getByRole('button', { name: /^(SSL off|Enable HTTPS)$/ }).hasAttribute('disabled')).toBe(false);
    expect(within(panel).queryByRole('button', { name: /^(SSL on|Disable HTTPS)$/ })).toBeNull();
    expect(backend.api.LocalCAStatus).not.toHaveBeenCalled();
    expect(backend.api.TrustLocalCA).not.toHaveBeenCalled();
    expect(flash).toHaveBeenCalledWith(expect.stringContaining('Certificate directory is read-only'));
  });

  it('offers the HTTPS action beside the selected project URL without scrolling the tools', async () => {
    mount();
    const panel = await select();
    const header = within(panel).getByLabelText('Selected project');
    expect(within(header).getByRole('button', { name: 'http://alpha.test' })).toBeTruthy();
    fireEvent.click(within(header).getByRole('button', { name: 'Enable HTTPS' }));
    await waitFor(() => expectCalledOnce(backend.api.SetProjectSSL, 'alpha', true));
  });

  it('paginates 58 projects and shows one separate selected-project panel', async () => {
    mount();
    await screen.findByRole('button', { name: 'Select project alpha' });
    expect(screen.getAllByRole('button', { name: /^Select project / })).toHaveLength(10);
    expect(screen.queryByRole('button', { name: 'PHP 8.3.30' })).toBeNull();
    const panel = await select();
    expect(within(panel).getByRole('heading', { name: 'alpha' })).toBeTruthy();
    expect(screen.getAllByRole('region', { name: /Project details/i })).toHaveLength(1);
    expect(backend.api.OpenProjectInEditor).not.toHaveBeenCalled();
    expect(backend.api.OpenURL).not.toHaveBeenCalled();
    fireEvent.click(within(panel).getByRole('button', { name: 'Close project details' }));
    expect(screen.queryByRole('region', { name: /Project details/i })).toBeNull();
  });

  it('routes PHP and Node selection to the selected project without launching it', async () => {
    mount();
    const panel = await select();
    fireEvent.click(within(panel).getByRole('button', { name: 'PHP 8.3.30' }));
    fireEvent.click(screen.getByRole('button', { name: 'PHP 8.4' }));
    await waitFor(() => expectCalledOnce(backend.api.SetProjectPhp, 'alpha', '8.4'));
    fireEvent.click(within(panel).getByRole('button', { name: 'Node 22.10.0' }));
    fireEvent.click(screen.getByRole('button', { name: 'Node 24' }));
    await waitFor(() => expectCalledOnce(backend.api.SetProjectNode, 'alpha', '24'));
    expect(backend.api.OpenProjectInEditor).not.toHaveBeenCalled();
    expect(backend.api.OpenURL).not.toHaveBeenCalled();
  });

  it('keeps Doctor scoped to the selected project and the global all-project action', async () => {
    mount();
    const panel = await select();
    fireEvent.click(within(panel).getByRole('button', { name: 'Doctor' }));
    expect(screen.getByTestId('doctor-scope').textContent).toBe('alpha');
    fireEvent.click(screen.getByRole('button', { name: 'Close test Doctor' }));
    fireEvent.click(screen.getByRole('button', { name: 'Run Doctor on all sites' }));
    expect(screen.getByTestId('doctor-scope').textContent).toBe('all');
    expect(within(panel).getByRole('heading', { name: 'alpha' })).toBeTruthy();
  });

  it('opens list quick actions once without changing the selected project', async () => {
    mount();
    const panel = await select('beta');
    fireEvent.click(screen.getByRole('button', { name: 'Open alpha in editor' }));
    await waitFor(() => expectCalledOnce(backend.api.OpenProjectInEditor, 'alpha', ''));
    fireEvent.click(screen.getByRole('button', { name: 'Open alpha in browser' }));
    expectCalledOnce(backend.api.OpenURL, 'http://alpha.test');
    expect(within(panel).getByRole('heading', { name: 'beta' })).toBeTruthy();
    expect(within(panel).queryByRole('heading', { name: 'alpha' })).toBeNull();
  });

  it('customizes the two listing shortcuts and remembers them after reopening Projects', async () => {
    mount();
    await screen.findByRole('button', { name: 'Select project alpha' });
    fireEvent.click(screen.getByRole('button', { name: 'Customize actions' }));
    const first = screen.getByRole('combobox', { name: 'Shortcut 1' });
    const second = screen.getByRole('combobox', { name: 'Shortcut 2' });
    expect((first as HTMLSelectElement).value).toBe('editor');
    expect((second as HTMLSelectElement).value).toBe('browser');
    fireEvent.change(first, { target: { value: 'browser' } });
    expect((second as HTMLSelectElement).value).toBe('editor');
    fireEvent.change(second, { target: { value: 'terminal' } });
    expect(JSON.parse(localStorage.getItem('laraenv.projectsQuickActions') ?? 'null')).toEqual({
      version: 1, actions: ['browser', 'terminal'],
    });
    cleanup();
    mount();
    expect(await screen.findByRole('button', { name: 'Open alpha in browser' })).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Open alpha in terminal' })).toBeTruthy();
    expect(screen.queryByRole('button', { name: 'Open alpha in editor' })).toBeNull();
  });

  it('opens the row terminal using the global default when the project has no preference', async () => {
    localStorage.setItem('laraenv.projectsQuickActions', JSON.stringify({ version: 1, actions: ['browser', 'terminal'] }));
    const { onOpenTerminal } = mount();
    const panel = await select('beta');
    fireEvent.click(screen.getByRole('button', { name: 'Open alpha in terminal' }));
    expectCalledOnce(onOpenTerminal, 'alpha');
    expect(backend.api.SetProjectTerminal).not.toHaveBeenCalled();
    expect(within(panel).getByRole('heading', { name: 'beta' })).toBeTruthy();
  });

  it('forwards the saved project terminal preference rather than opening the global default', async () => {
    localStorage.setItem('laraenv.projectsQuickActions', JSON.stringify({ version: 1, actions: ['browser', 'terminal'] }));
    backend.api.ListTerminals.mockResolvedValue({
      profiles: [
        { kind: 'gitbash', label: 'Git Bash', available: true, path: '' },
        { kind: 'powershell', label: 'PowerShell', available: true, path: '' },
      ], defaultKind: 'gitbash', projectKinds: { alpha: 'powershell' },
    });
    const { onOpenTerminal } = mount();
    const panel = await select('beta');
    fireEvent.click(screen.getByRole('button', { name: 'Open alpha in terminal' }));
    expectCalledOnce(onOpenTerminal, 'alpha', 'powershell');
    expect(backend.api.SetProjectTerminal).not.toHaveBeenCalled();
    expect(within(panel).getByRole('heading', { name: 'beta' })).toBeTruthy();
  });

  it('opens the configured row folder for its project without changing the detail selection', async () => {
    localStorage.setItem('laraenv.projectsQuickActions', JSON.stringify({ version: 1, actions: ['folder', 'browser'] }));
    mount();
    const panel = await select('beta');
    fireEvent.click(screen.getByRole('button', { name: 'Open folder for alpha' }));
    await waitFor(() => expectCalledOnce(backend.api.OpenProjectFolder, 'alpha'));
    expect(within(panel).getByRole('heading', { name: 'beta' })).toBeTruthy();
    expect(backend.api.OpenURL).not.toHaveBeenCalled();
  });

  it('allows hiding both listing shortcuts while retaining project selection and the detail tools', async () => {
    localStorage.setItem('laraenv.projectsQuickActions', JSON.stringify({ version: 1, actions: ['none', 'none'] }));
    mount();
    await screen.findByRole('button', { name: 'Select project alpha' });
    expect(screen.queryByRole('button', { name: 'Open alpha in editor' })).toBeNull();
    expect(screen.queryByRole('button', { name: 'Open alpha in browser' })).toBeNull();
    expect(screen.queryByRole('button', { name: 'Open alpha in terminal' })).toBeNull();
    expect(screen.queryByRole('button', { name: 'Open folder for alpha' })).toBeNull();
    const panel = await select();
    expect(within(panel).getByRole('button', { name: 'Git Bash' })).toBeTruthy();
    expect(within(panel).getByRole('button', { name: 'Browser' })).toBeTruthy();
  });

  it('closes the old runtime menu when changing projects and uses the new callback target', async () => {
    mount();
    const panel = await select();
    fireEvent.click(within(panel).getByRole('button', { name: 'PHP 8.3.30' }));
    expect(screen.getByRole('button', { name: /^Automatic/ })).toBeTruthy();
    const next = await select('beta');
    expect(screen.queryByRole('button', { name: /^Automatic/ })).toBeNull();
    fireEvent.click(within(next).getByRole('button', { name: 'PHP 8.3.30' }));
    fireEvent.click(screen.getByRole('button', { name: 'PHP 8.4' }));
    await waitFor(() => expectCalledOnce(backend.api.SetProjectPhp, 'beta', '8.4'));
  });

  it('starts the identified Dev and Queue processes and preserves the terminal shortcut', async () => {
    const { onOpenTerminal } = mount();
    const panel = await select();
    fireEvent.click(within(panel).getByRole('button', { name: 'Start Dev' }));
    await waitFor(() => expectCalledOnce(backend.api.StartDevServer, 'alpha'));
    fireEvent.click(within(panel).getByRole('button', { name: 'Start queue' }));
    await waitFor(() => expectCalledOnce(backend.api.StartQueueWorker, 'alpha'));
    fireEvent.click(within(panel).getByRole('button', { name: 'Git Bash' }));
    await waitFor(() => expectCalledOnce(onOpenTerminal, 'alpha', 'gitbash'));
    expectCalledOnce(backend.api.SetProjectTerminal, 'alpha', 'gitbash');
    expect(backend.api.StopDevServer).not.toHaveBeenCalled();
    expect(backend.api.StopQueueWorker).not.toHaveBeenCalled();
  });

  it('exposes Dev and its configuration for an npm project without a recognized framework', async () => {
    backend.api.ListProjects.mockResolvedValue([{ ...project('plain-npm'), kind: 'plain', jsFramework: '' }]);
    backend.api.GetDevServerStatus.mockResolvedValue({ running: true, pid: 123, port: 3000, script: 'dev' });
    mount();
    const panel = await select('plain-npm');
    fireEvent.click(await within(panel).findByRole('button', { name: 'Stop Dev' }));
    await waitFor(() => expectCalledOnce(backend.api.StopDevServer, 'plain-npm'));
    expect(within(panel).getByRole('button', { name: 'Configure command' }).hasAttribute('disabled')).toBe(true);
    expect(within(panel).queryByRole('button', { name: 'Start queue' })).toBeNull();
  });

  it('shows the inherited Dev command when the project has no package or framework', async () => {
    backend.api.ListProjects.mockResolvedValue([{ ...project('custom-only'), kind: 'plain', jsFramework: '', packageManager: '' }]);
    backend.api.GetProjectsConfig.mockResolvedValue({ tld: 'test', root: 'C:\\sites', phpVersions: {}, ssl: {},
      devCommands: { 'custom-only': '  ' }, defaultDevCommand: 'php artisan serve --port={port}' });
    mount();
    const panel = await select('custom-only');
    const start = await within(panel).findByRole('button', { name: 'Start Dev' });
    expect(start.title).toContain('php artisan serve --port={port}');
    fireEvent.click(within(panel).getByRole('button', { name: 'Configure command' }));
    expect(await screen.findByPlaceholderText('npm run dev')).toBeTruthy();
  });

  it('runs a script in the selected project terminal without replacing the managed Dev command', async () => {
    backend.api.ListProjects.mockResolvedValue([{ ...project('plain-pnpm'), kind: 'plain', jsFramework: '', packageManager: 'pnpm' }]);
    backend.api.ListTerminals.mockResolvedValue({ profiles: [{ kind: 'gitbash', label: 'Git Bash', available: true, path: '' }],
      defaultKind: 'powershell', projectKinds: { 'plain-pnpm': 'gitbash' } });
    const { onRunCommand } = mount();
    const panel = await select('plain-pnpm');
    const scripts = await within(panel).findByRole('combobox', { name: 'Script' });
    fireEvent.change(scripts, { target: { value: 'build' } });
    fireEvent.click(within(panel).getByRole('button', { name: 'Run pnpm run build in terminal' }));
    expectCalledOnce(onRunCommand, 'plain-pnpm', 'pnpm run build', 'gitbash');
    expect(backend.api.SetProjectDevCommand).not.toHaveBeenCalled();
    expect(backend.api.StartDevServer).not.toHaveBeenCalled();
  });

  it('dismisses narrow project details before opening a script terminal', async () => {
    const { onRunCommand } = mount(900);
    fireEvent.click(await screen.findByRole('button', { name: 'Select project alpha' }));
    const dialog = await screen.findByRole('dialog', { name: /Project details/i });
    fireEvent.change(await within(dialog).findByRole('combobox', { name: 'Script' }), { target: { value: 'watch' } });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Run npm run watch in terminal' }));
    await waitFor(() => expect(screen.queryByRole('dialog', { name: /Project details/i })).toBeNull());
    expectCalledOnce(onRunCommand, 'alpha', 'npm run watch', 'gitbash');
  });

  it('traps focus in narrow details, closes on Escape and returns focus to the selection button', async () => {
    mount(900);
    const trigger = await screen.findByRole('button', { name: 'Select project alpha' });
    trigger.focus();
    fireEvent.click(trigger);
    const dialog = await screen.findByRole('dialog', { name: /Project details/i });
    await waitFor(() => expect(dialog.contains(document.activeElement)).toBe(true));
    const focusable = within(dialog).getAllByRole('button').filter(button => !button.hasAttribute('disabled'));
    const first = focusable[0];
    const last = focusable[focusable.length - 1];
    last.focus();
    fireEvent.keyDown(last, { key: 'Tab' });
    expect(document.activeElement).toBe(first);
    fireEvent.keyDown(first, { key: 'Tab', shiftKey: true });
    expect(document.activeElement).toBe(last);
    fireEvent.keyDown(dialog, { key: 'Escape' });
    await waitFor(() => expect(screen.queryByRole('dialog', { name: /Project details/i })).toBeNull());
    expect(document.activeElement).toBe(trigger);
  });

  it('closes a runtime menu on Escape without dismissing the narrow project details', async () => {
    mount(900);
    fireEvent.click(await screen.findByRole('button', { name: 'Select project alpha' }));
    const dialog = await screen.findByRole('dialog', { name: /Project details/i });
    fireEvent.click(within(dialog).getByRole('button', { name: 'PHP 8.3.30' }));
    const option = screen.getByRole('button', { name: /^Automatic/ });
    option.focus();
    fireEvent.keyDown(option, { key: 'Escape' });
    await waitFor(() => expect(screen.queryByRole('button', { name: /^Automatic/ })).toBeNull());
    expect(screen.getByRole('dialog', { name: /Project details/i })).toBe(dialog);
    fireEvent.keyDown(dialog, { key: 'Escape' });
    await waitFor(() => expect(screen.queryByRole('dialog', { name: /Project details/i })).toBeNull());
  });

  it('lets Doctor handle Escape without closing the project details underneath', async () => {
    mount(900);
    fireEvent.click(await screen.findByRole('button', { name: 'Select project alpha' }));
    const dialog = await screen.findByRole('dialog', { name: /Project details/i });
    fireEvent.click(within(dialog).getByRole('button', { name: 'Doctor' }));
    fireEvent.keyDown(screen.getByRole('dialog', { name: 'Doctor test' }), { key: 'Escape' });
    expect(screen.queryByRole('dialog', { name: 'Doctor test' })).toBeNull();
    expect(screen.getByRole('dialog', { name: /Project details/i })).toBe(dialog);
  });

  it('keeps a portable HTTPS confirmation operable above narrow project details', async () => {
    backend.api.LocalCAStatus.mockResolvedValue({ portable: true, exists: true, trusted: false, checkError: '' });
    mount(900);
    fireEvent.click(await screen.findByRole('button', { name: 'Select project alpha' }));
    const details = await screen.findByRole('dialog', { name: /Project details/i });
    fireEvent.click(within(details).getByRole('button', { name: 'Enable HTTPS' }));
    const confirmation = await screen.findByRole('dialog', { name: 'Trust the local certificate authority?' });
    const trust = within(confirmation).getByRole('button', { name: 'Trust local CA' });
    await waitFor(() => expect(document.activeElement).toBe(trust));
    fireEvent.keyDown(trust, { key: 'Tab' });
    expect(confirmation.contains(document.activeElement)).toBe(true);
    fireEvent.keyDown(trust, { key: 'Escape' });
    await waitFor(() => expect(screen.queryByRole('dialog', { name: 'Trust the local certificate authority?' })).toBeNull());
    expect(screen.getByRole('dialog', { name: /Project details/i })).toBe(details);
    expect(backend.api.TrustLocalCA).not.toHaveBeenCalled();
  });

  it('returns focus to project search when filtering removes the narrow details selection and its original row', async () => {
    mount(900);
    await screen.findByRole('button', { name: 'Select project alpha' });
    const search = screen.getByRole('textbox', { name: 'Search projects' });
    fireEvent.click(screen.getByRole('button', { name: 'Next page' }));
    const trigger = await screen.findByRole('button', { name: 'Select project project-12' });
    trigger.focus();
    fireEvent.click(trigger);
    const details = await screen.findByRole('dialog', { name: /Project details/i });
    fireEvent.click(within(details).getByRole('button', { name: 'Client C' }));
    await waitFor(() => expect(screen.queryByRole('dialog', { name: /Project details/i })).toBeNull());
    expect(trigger.isConnected).toBe(false);
    expect(screen.queryByRole('button', { name: 'Select project project-12' })).toBeNull();
    await waitFor(() => expect(document.activeElement).toBe(search));
  });
});
