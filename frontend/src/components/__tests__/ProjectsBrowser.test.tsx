// @vitest-environment jsdom
import { useState } from 'react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import ProjectsBrowser, { type ProjectBrowserItem } from '../ProjectsBrowser';
import { I18nProvider } from '../../lib/i18n';

vi.mock('../../../wailsjs/go/main/App', () => ({ GetLanguage: async () => 'en', SetLanguage: async () => {} }));
vi.mock('../../../wailsjs/runtime', () => ({ EventsOn: () => () => {} }));

const project = (name: string, overrides: Partial<ProjectBrowserItem> = {}): ProjectBrowserItem => ({
  name, path: `C:\\sites\\${name}`, url: `http://${name}.test`, kind: 'laravel',
  phpVersion: '', nodeVersion: '', jsFramework: 'vite', packageManager: 'npm', tags: [], ...overrides,
});
const many = (count = 25) => Array.from({ length: count }, (_, index) => project(`site-${index + 1}`));
type Options = {
  initialTag?: string; initialSelection?: string | null;
  onSelect?: (name: string | null) => void;
  onOpenEditor?: (name: string) => void; onOpenURL?: (url: string) => void;
};
function Harness({ projects, initialTag = '__all__', initialSelection = null, onSelect = () => {}, onOpenEditor = () => {}, onOpenURL = () => {} }: Options & { projects: ProjectBrowserItem[] }) {
  const [tag, setTag] = useState(initialTag);
  const [selection, setSelection] = useState(initialSelection);
  return <ProjectsBrowser projects={projects} selectedName={selection} onSelect={name => { onSelect(name); setSelection(name); }}
    tagFilter={tag} onTagFilterChange={setTag} onOpenEditor={onOpenEditor} onOpenURL={onOpenURL} onOpenTerminal={() => {}} onOpenFolder={() => {}} defaultPhp="8.4" defaultNode="24" />;
}
const mount = (projects: ProjectBrowserItem[], options: Options = {}) => render(<I18nProvider><Harness projects={projects} {...options} /></I18nProvider>);
const selections = () => screen.queryAllByRole('button', { name: /^Select project / });
const names = () => selections().map(button => button.textContent);
const search = (value: string) => fireEvent.change(screen.getByRole('textbox', { name: 'Search projects' }), { target: { value } });
const openTags = () => fireEvent.click(screen.getByRole('button', { name: 'Filter by client tag' }));

beforeEach(() => localStorage.clear());
afterEach(cleanup);

describe('Projects browser', () => {
  it('lets a user hide shortcuts and restore the defaults without launching a project', async () => {
    const onSelect = vi.fn();
    mount([project('alpha')], { onSelect });
    fireEvent.click(await screen.findByRole('button', { name: 'Customize actions' }));
    fireEvent.change(screen.getByRole('combobox', { name: 'Shortcut 1' }), { target: { value: 'none' } });
    fireEvent.change(screen.getByRole('combobox', { name: 'Shortcut 2' }), { target: { value: 'none' } });
    expect(screen.queryByRole('button', { name: 'Open alpha in editor' })).toBeNull();
    expect(screen.queryByRole('button', { name: 'Open alpha in browser' })).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: 'Restore defaults' }));
    expect(screen.getByRole('button', { name: 'Open alpha in editor' })).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Open alpha in browser' })).toBeTruthy();
    expect(onSelect).not.toHaveBeenCalled();
  });

  it.each(['broken json', JSON.stringify({ version: 1, actions: ['remove', 'terminal'] }), JSON.stringify({ version: 2, actions: ['browser', 'folder'] })])('recovers safe defaults from invalid shortcut preferences: %s', async saved => {
    localStorage.setItem('laraenv.projectsQuickActions', saved);
    mount([project('alpha')]);
    expect(await screen.findByRole('button', { name: 'Open alpha in editor' })).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Open alpha in browser' })).toBeTruthy();
  });

  it('closes the shortcuts popup before opening selected-project details', async () => {
    mount([project('alpha')]);
    const customize = await screen.findByRole('button', { name: 'Customize actions' });
    fireEvent.click(customize);
    const firstSlot = screen.getByRole('combobox', { name: 'Shortcut 1' });
    fireEvent.keyDown(firstSlot, { key: 'Escape' });
    expect(screen.queryByRole('combobox', { name: 'Shortcut 1' })).toBeNull();
    expect(document.activeElement).toBe(customize);
    fireEvent.click(customize);
    fireEvent.click(screen.getByRole('button', { name: 'Select project alpha' }));
    await waitFor(() => expect(screen.queryByRole('combobox', { name: 'Shortcut 1' })).toBeNull());
  });

  it('naturally sorts compact rows, defaults to ten, and does not choose a project on mount', async () => {
    const onSelect = vi.fn();
    mount(many(), { onSelect });
    await screen.findByRole('textbox', { name: 'Search projects' });
    expect(names()).toEqual(['site-1', 'site-2', 'site-3', 'site-4', 'site-5', 'site-6', 'site-7', 'site-8', 'site-9', 'site-10']);
    expect(screen.getByRole('table')).toBeTruthy();
    expect(screen.getByText('Showing 1–10 of 25 projects')).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Previous page' }).hasAttribute('disabled')).toBe(true);
    expect(onSelect).not.toHaveBeenCalled();
    expect(screen.queryByText('Running')).toBeNull();
  });

  it.each([
    ['CAFE', 'Café'], ['CLIENTS/ACME', 'other'], ['clinica', 'third'],
  ])('searches names, paths and tags without case or accent distinctions: %s', async (query, expected) => {
    mount([project('Café'), project('other', { path: 'C:/clients/Acme/site' }), project('third', { tags: ['Clínica'] })]);
    await screen.findByRole('textbox', { name: 'Search projects' });
    search(query);
    expect(names()).toEqual([expected]);
  });

  it('offers searchable client tags with real counts and retains non-client tags and untagged projects', async () => {
    mount([project('one', { tags: ['Clínica', 'production'] }), project('two', { tags: ['Clínica'] }), project('three')]);
    await screen.findByRole('textbox', { name: 'Search projects' });
    openTags();
    expect(within(screen.getByRole('button', { name: 'All tags 3' })).getByText('3')).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Untagged 1' })).toBeTruthy();
    expect(screen.getByRole('button', { name: 'Filter by tag production 1' })).toBeTruthy();
    fireEvent.change(screen.getByRole('textbox', { name: 'Search tags' }), { target: { value: 'clinica' } });
    expect(screen.queryByRole('button', { name: 'Filter by tag production 1' })).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: 'Filter by tag Clínica 2' }));
    expect(names()).toEqual(['one', 'two']);
    openTags();
    fireEvent.click(screen.getByRole('button', { name: 'Untagged 1' }));
    expect(names()).toEqual(['three']);
  });

  it('exposes a focusable selection button and keeps selection separate from single editor and browser launches', async () => {
    const onSelect = vi.fn(); const onOpenEditor = vi.fn(); const onOpenURL = vi.fn();
    mount([project('alpha')], { onSelect, onOpenEditor, onOpenURL });
    const choose = await screen.findByRole('button', { name: 'Select project alpha' });
    choose.focus();
    expect(document.activeElement).toBe(choose);
    fireEvent.click(choose);
    expect(onSelect).toHaveBeenCalledTimes(1);
    expect(onSelect).toHaveBeenCalledWith('alpha');
    expect(choose.getAttribute('aria-pressed')).toBe('true');
    expect(onOpenEditor).not.toHaveBeenCalled();
    expect(onOpenURL).not.toHaveBeenCalled();
    onSelect.mockClear();
    fireEvent.click(screen.getByRole('button', { name: 'Open alpha in editor' }));
    expect(onOpenEditor).toHaveBeenCalledTimes(1);
    expect(onOpenEditor).toHaveBeenCalledWith('alpha');
    fireEvent.click(screen.getByRole('button', { name: 'Open alpha in browser' }));
    expect(onOpenURL).toHaveBeenCalledTimes(1);
    expect(onOpenURL).toHaveBeenCalledWith('http://alpha.test');
    expect(onSelect).not.toHaveBeenCalled();
  });

  it('selects once from a noninteractive row cell and focuses the native name button', async () => {
    const onSelect = vi.fn();
    mount([project('alpha')], { onSelect });
    const choose = await screen.findByRole('button', { name: 'Select project alpha' });
    fireEvent.click(screen.getByText('C:\\sites\\alpha'));
    expect(onSelect).toHaveBeenCalledTimes(1);
    expect(onSelect).toHaveBeenCalledWith('alpha');
    expect(document.activeElement).toBe(choose);
  });

  it('filters from a row tag without selecting its project', async () => {
    const onSelect = vi.fn();
    mount([project('alpha', { tags: ['Acme'] }), project('beta', { tags: ['Other'] })], { onSelect });
    await screen.findByRole('button', { name: 'Select project alpha' });
    fireEvent.click(screen.getByRole('button', { name: 'Filter by tag Acme' }));
    expect(names()).toEqual(['alpha']);
    expect(onSelect).not.toHaveBeenCalled();
  });

  it('preserves a visible selection, but clears it when search removes the project', async () => {
    const onSelect = vi.fn();
    mount([project('alpha'), project('beta')], { onSelect });
    fireEvent.click(await screen.findByRole('button', { name: 'Select project alpha' }));
    onSelect.mockClear();
    search('alp');
    expect(onSelect).not.toHaveBeenCalled();
    search('beta');
    await waitFor(() => expect(onSelect).toHaveBeenCalledWith(null));
  });

  it('clears selected details when paging away and does not automatically pick the next project', async () => {
    const onSelect = vi.fn();
    mount(many(), { onSelect });
    fireEvent.click(await screen.findByRole('button', { name: 'Select project site-1' }));
    onSelect.mockClear();
    fireEvent.click(screen.getByRole('button', { name: 'Next page' }));
    await waitFor(() => expect(onSelect).toHaveBeenCalledWith(null));
    expect(names()[0]).toBe('site-11');
    expect(screen.getByText('Page 2 of 3')).toBeTruthy();
  });

  it('resets pagination for search, tag and page-size changes, and clear restores all projects', async () => {
    const projects = many().map(item => ({ ...item, tags: ['Acme'] }));
    mount(projects);
    await screen.findByRole('textbox', { name: 'Search projects' });
    fireEvent.click(screen.getByRole('button', { name: 'Next page' }));
    search('site');
    expect(screen.getByText('Page 1 of 3')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Next page' }));
    openTags();
    fireEvent.click(screen.getByRole('button', { name: 'Filter by tag Acme 25' }));
    await waitFor(() => expect(screen.getByText('Page 1 of 3')).toBeTruthy());
    fireEvent.click(screen.getByRole('button', { name: 'Next page' }));
    fireEvent.change(screen.getByRole('combobox', { name: 'Projects per page' }), { target: { value: '20' } });
    expect(selections()).toHaveLength(20);
    expect(screen.getByText('Page 1 of 2')).toBeTruthy();
    fireEvent.change(screen.getByRole('combobox', { name: 'Projects per page' }), { target: { value: '50' } });
    expect(selections()).toHaveLength(25);
    search('site-25');
    fireEvent.click(screen.getByRole('button', { name: 'Clear filters' }));
    await waitFor(() => expect(selections()).toHaveLength(25));
    expect((screen.getByRole('textbox', { name: 'Search projects' }) as HTMLInputElement).value).toBe('');
  });

  it('clamps the page and drops stale selection after refresh deletes projects', async () => {
    const onSelect = vi.fn(); const options = { onSelect };
    const view = mount(many(), options);
    await screen.findByRole('textbox', { name: 'Search projects' });
    fireEvent.click(screen.getByRole('button', { name: 'Next page' }));
    fireEvent.click(screen.getByRole('button', { name: 'Next page' }));
    fireEvent.click(screen.getByRole('button', { name: 'Select project site-25' }));
    onSelect.mockClear();
    view.rerender(<I18nProvider><Harness projects={many(11)} {...options} /></I18nProvider>);
    await waitFor(() => expect(screen.getByText('Page 2 of 2')).toBeTruthy());
    expect(names()).toEqual(['site-11']);
    expect(onSelect).toHaveBeenCalledTimes(1);
    expect(onSelect).toHaveBeenCalledWith(null);
  });

  it('resets pagination when the parent changes the controlled tag filter', async () => {
    const onSelect = vi.fn();
    const projects = many().map(item => ({ ...item, tags: ['Acme'] }));
    const props = { projects, selectedName: null, onSelect, onTagFilterChange: () => {}, onOpenEditor: () => {}, onOpenURL: () => {}, onOpenTerminal: () => {}, onOpenFolder: () => {}, defaultPhp: '8.4', defaultNode: '24' };
    const view = render(<I18nProvider><ProjectsBrowser {...props} tagFilter="__all__" /></I18nProvider>);
    await screen.findByRole('textbox', { name: 'Search projects' });
    fireEvent.click(screen.getByRole('button', { name: 'Next page' }));
    expect(names()[0]).toBe('site-11');
    view.rerender(<I18nProvider><ProjectsBrowser {...props} selectedName="site-11" tagFilter="Acme" /></I18nProvider>);
    await waitFor(() => expect(screen.getByText('Page 1 of 3')).toBeTruthy());
    expect(names()[0]).toBe('site-1');
    expect(onSelect).toHaveBeenCalledTimes(1);
    expect(onSelect).toHaveBeenCalledWith(null);
  });

  it('closes the tag portal when project selection changes without clearing the search or filter', async () => {
    const props = { projects: [project('alpha', { tags: ['Acme'] })], onSelect: () => {}, tagFilter: 'Acme', onTagFilterChange: () => {}, onOpenEditor: () => {}, onOpenURL: () => {}, onOpenTerminal: () => {}, onOpenFolder: () => {}, defaultPhp: '8.4', defaultNode: '24' };
    const view = render(<I18nProvider><ProjectsBrowser {...props} selectedName={null} /></I18nProvider>);
    await screen.findByRole('textbox', { name: 'Search projects' });
    search('alpha');
    openTags();
    expect(screen.getByRole('textbox', { name: 'Search tags' })).toBeTruthy();
    view.rerender(<I18nProvider><ProjectsBrowser {...props} selectedName="alpha" /></I18nProvider>);
    await waitFor(() => expect(screen.queryByRole('textbox', { name: 'Search tags' })).toBeNull());
    expect((screen.getByRole('textbox', { name: 'Search projects' }) as HTMLInputElement).value).toBe('alpha');
    expect(screen.getByRole('button', { name: 'Filter by client tag' }).getAttribute('title')).toBe('Acme');
  });

  it('shows an empty result with filter recovery and exposes paused/runtime metadata without invented health', async () => {
    mount([project('paused-site', { paused: true, phpRuntime: { version: '8.3', fullVersion: '8.3.30', source: 'manual', constraint: '', status: 'resolved' } })]);
    await screen.findByRole('textbox', { name: 'Search projects' });
    expect(screen.getByText('Paused')).toBeTruthy();
    expect(screen.getByText('PHP 8.3.30')).toBeTruthy();
    search('no match');
    expect(selections()).toHaveLength(0);
    expect(screen.getByText('No projects match these filters.')).toBeTruthy();
    fireEvent.click(screen.getByRole('button', { name: 'Clear filters' }));
    expect(selections()).toHaveLength(1);
  });
});
