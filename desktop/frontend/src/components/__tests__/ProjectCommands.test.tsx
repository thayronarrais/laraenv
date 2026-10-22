// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { act, cleanup, fireEvent, render, screen } from '@testing-library/react';
import ProjectCommands, { projectScriptCommand } from '../ProjectCommands';
import { I18nProvider } from '../../lib/i18n';

const api = vi.hoisted(() => ({ list: vi.fn() }));
vi.mock('../../../wailsjs/go/main/App', () => ({
  GetLanguage: async () => 'en', SetLanguage: async () => {},
  ListProjectScripts: (name: string) => api.list(name),
}));
vi.mock('../../../wailsjs/runtime', () => ({ EventsOn: () => () => {} }));

beforeEach(() => { api.list.mockReset(); api.list.mockResolvedValue(['build', 'dev', 'start']); });
afterEach(cleanup);
const panel = (projectName = 'plain', onRunCommand = vi.fn(), packageManager = 'npm') =>
  <I18nProvider><ProjectCommands projectName={projectName} packageManager={packageManager} terminalKind="powershell" onRunCommand={onRunCommand} /></I18nProvider>;

describe('project command panel', () => {
  it('lists only manifest scripts and executes build without development-server arguments', async () => {
    const run = vi.fn();
    render(panel('plain', run, 'pnpm'));
    const selector = await screen.findByRole('combobox', { name: 'Script' });
    expect(Array.from(selector.querySelectorAll('option')).map(option => option.value)).toEqual(['build', 'dev', 'start']);
    expect(screen.queryByRole('option', { name: 'watch' })).toBeNull();
    expect((selector as HTMLSelectElement).value).toBe('dev');
    fireEvent.change(selector, { target: { value: 'build' } });
    fireEvent.click(screen.getByRole('button', { name: 'Run pnpm run build in terminal' }));
    expect(run).toHaveBeenCalledWith('pnpm run build');
  });

  it('runs a user command verbatim and keeps it available without package scripts', async () => {
    api.list.mockResolvedValue([]);
    const run = vi.fn();
    render(panel('plain', run));
    expect(await screen.findByText('No scripts are defined in package.json.')).toBeTruthy();
    expect(screen.queryByRole('combobox')).toBeNull();
    const button = screen.getByRole('button', { name: 'Run command' });
    expect(button.hasAttribute('disabled')).toBe(true);
    fireEvent.change(screen.getByRole('textbox', { name: 'Custom command' }), { target: { value: '   ' } });
    expect(button.hasAttribute('disabled')).toBe(true);
    fireEvent.change(screen.getByRole('textbox', { name: 'Custom command' }), { target: { value: '  php artisan test --filter="Feature test"  ' } });
    fireEvent.click(button);
    expect(run).toHaveBeenCalledWith('  php artisan test --filter="Feature test"  ');
  });

  it('recovers from script-loading failure using retry while custom commands stay available', async () => {
    api.list.mockRejectedValueOnce(new Error('unreadable manifest')).mockResolvedValueOnce(['watch:css']);
    render(panel());
    expect(await screen.findByRole('alert')).toBeTruthy();
    expect(screen.getByRole('textbox', { name: 'Custom command' })).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Try again' }));
    expect(await screen.findByRole('option', { name: 'watch:css' })).toBeTruthy();
    expect(screen.queryByRole('alert')).toBeNull();
  });

  it('refreshes manifest scripts and removes a deleted script selection', async () => {
    api.list.mockResolvedValueOnce(['dev', 'build']).mockResolvedValueOnce(['watch']);
    render(panel());
    fireEvent.change(await screen.findByRole('combobox', { name: 'Script' }), { target: { value: 'build' } });
    fireEvent.click(screen.getByRole('button', { name: 'Refresh scripts' }));
    expect(await screen.findByRole('button', { name: 'Run npm run watch in terminal' })).toBeTruthy();
    expect(screen.queryByRole('option', { name: 'build' })).toBeNull();
  });

  it('clears per-project input and ignores a previous project response', async () => {
    let resolveOld!: (scripts: string[]) => void;
    api.list.mockImplementation((name: string) => name === 'first' ? new Promise<string[]>(resolve => { resolveOld = resolve; }) : Promise.resolve(['build']));
    const { rerender } = render(panel('first'));
    fireEvent.change(screen.getByRole('textbox', { name: 'Custom command' }), { target: { value: 'npm run dev' } });
    rerender(panel('second'));
    expect((screen.getByRole('textbox', { name: 'Custom command' }) as HTMLInputElement).value).toBe('');
    expect(await screen.findByRole('option', { name: 'build' })).toBeTruthy();
    await act(async () => { resolveOld(['old-only']); });
    expect(screen.queryByRole('option', { name: 'old-only' })).toBeNull();
  });

  it('ignores a script refresh response arriving after unmount', async () => {
    let resolveRefresh!: (scripts: string[]) => void;
    api.list.mockResolvedValueOnce(['dev']).mockImplementationOnce(() => new Promise<string[]>(resolve => { resolveRefresh = resolve; }));
    const { unmount } = render(panel());
    await screen.findByRole('option', { name: 'dev' });
    fireEvent.click(screen.getByRole('button', { name: 'Refresh scripts' }));
    unmount();
    await act(async () => { resolveRefresh(['build']); });
    expect(screen.queryByRole('combobox')).toBeNull();
  });
});

describe('literal package script commands', () => {
  it.each(['npm', 'pnpm', 'yarn', 'bun'])('uses the canonical %s manager without adding flags', manager => {
    expect(projectScriptCommand(manager, 'watch:assets', 'powershell')).toBe(`${manager} run watch:assets`);
  });
  it('never interpolates a package-manager declaration as executable code', () => {
    expect(projectScriptCommand('npm; echo injected', 'dev', 'powershell')).toBe('npm run dev');
  });
  it('quotes literal script names for PowerShell and Git Bash', () => {
    expect(projectScriptCommand('npm', "build'; echo injected", 'powershell')).toBe("npm run 'build''; echo injected'");
    expect(projectScriptCommand('pnpm', "build'; echo injected", 'gitbash')).toBe("pnpm run 'build'\"'\"'; echo injected'");
    expect(projectScriptCommand('yarn', '-watch', 'powershell')).toBe('yarn run -- -watch');
  });
  it.each(['cmd', 'cmder', 'gitbash'])('encodes unsafe control characters for %s without passing shell syntax', kind => {
    const command = projectScriptCommand('npm', 'build\r\necho injected', kind);
    expect(command).toMatch(/^powershell\.exe -NoProfile -EncodedCommand [A-Za-z0-9+/=]+$/);
    const decoded = Buffer.from(command.split(' ').at(-1)!, 'base64').toString('utf16le');
    expect(decoded).toBe("npm run 'build\r\necho injected'");
  });
  it('encodes cmd percent, delayed expansion and quote syntax as one literal script argument', () => {
    const command = projectScriptCommand('bun', 'build%PATH%!x!"&echo nope', 'cmd');
    const decoded = Buffer.from(command.split(' ').at(-1)!, 'base64').toString('utf16le');
    expect(decoded).toBe('bun run \'build%PATH%!x!"&echo nope\'');
  });
});
