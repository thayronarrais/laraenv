// @vitest-environment jsdom
import { useEffect } from 'react';
import { act, cleanup, render, screen } from '@testing-library/react';
import { afterEach, expect, it, vi } from 'vitest';
import { GitReviewEditor } from '../GitReviewEditor';
import { DocumentWorkspace } from '../../documents/DocumentWorkspace';
import { DocumentOperations } from '../../documents/documentOperations';
import { fakeAPI, ref } from '../../documents/__tests__/fixtures';
import { diff, snapshot } from './uiFixtures';
const fake = vi.hoisted(() => ({ pending: [] as (() => void)[], mounts: [] as (() => void)[], models: new Map<string, any>(), widgets: [] as any[], options: [] as any[], events: [] as string[], failAt: '' }));
vi.mock('../../../lib/i18n', () => ({ useT: () => (key: string) => key }));
vi.mock('../../documents/WorkspaceFilesPanel', () => ({ WorkspaceFilesPanel: () => null }));
vi.mock('../../documents/MarkdownPreview', () => ({ MarkdownPreview: () => null }));
vi.mock('@monaco-editor/react', () => ({ loader: { config: vi.fn(), init: () => {
  const promise = new Promise((resolve, reject) => fake.pending.push(() => {
    if (fake.failAt === 'loader') { reject(Error('loader failed')); return; }
    resolve({ Uri: { parse: (value: string) => value }, editor: {
      createModel(text: string, _language: string, uri: string) {
        if (fake.failAt && uri.endsWith('/' + fake.failAt)) throw Error('model creation failed');
        const model = { uri, text, dispose() { if (fake.widgets.some(w => w.attached && [w.attached.original, w.attached.modified].includes(this))) throw Error('disposed while attached'); fake.events.push('dispose:' + uri); fake.models.delete(uri); } };
        fake.models.set(uri, model); return model;
      },
      createDiffEditor(_host: HTMLElement, options: any) {
        if (fake.failAt === 'widget') throw Error('widget creation failed');
        const widget = { attached: null as any, disposed: false, getModel() { return this.attached; }, setModel(pair: any) { if (pair && fake.failAt === 'attach') throw Error('attach failed'); this.attached = pair; if (!pair) fake.events.push('detach'); }, updateOptions(next: any) { Object.assign(options, next); }, dispose() { this.disposed = true; fake.events.push('widget-dispose'); } };
        fake.widgets.push(widget); fake.options.push(options); return widget;
      },
    } });
  }));
  // Deliberately allow an already-settled loader callback through after cancel: the owner must check unmount.
  return Object.assign(promise, { cancel() {} });
} }, default: function MockSourceEditor(props: any) {
  useEffect(() => {
    let cancelled = false; let widget: any;
    fake.pending.push(() => {
      if (cancelled) return;
      let model = fake.models.get(props.path);
      if (!model) { model = { uri: props.path, text: props.value, dispose() { fake.models.delete(props.path); }, isDisposed() { return !fake.models.has(props.path); } }; fake.models.set(props.path, model); }
      widget = { getModel: () => model, saveViewState: () => ({}), restoreViewState() {}, addCommand() {} };
      props.onMount(widget, { KeyMod: { CtrlCmd: 1 }, KeyCode: { KeyS: 2 } });
    });
    return () => { cancelled = true; if (widget && !props.keepCurrentModel) widget.getModel().dispose(); };
  }, []);
  return <span>source widget</span>;
}, DiffEditor: (props: any) => {
  useEffect(() => {
    let cancelled = false; let widget: any;
    fake.pending.push(() => {
      if (cancelled) return;
      const model = (uri: string, text: string) => {
        if (!fake.models.has(uri)) fake.models.set(uri, { uri, text, disposed: false, dispose() { if (fake.widgets.some(w => w.attached && [w.attached.original, w.attached.modified].includes(this))) throw Error('disposed while attached'); this.disposed = true; fake.events.push('dispose:' + uri); fake.models.delete(uri); } });
        return fake.models.get(uri);
      };
      widget = { attached: { original: model(props.originalModelPath, props.original), modified: model(props.modifiedModelPath, props.modified) }, getModel() { return this.attached; }, setModel(value: any) { this.attached = value; fake.events.push('detach'); } };
      fake.widgets.push(widget); fake.options.push(props.options); fake.mounts.push(() => { if (!cancelled) props.onMount(widget); });
    });
    return () => { cancelled = true; if (widget) { if (!props.keepCurrentOriginalModel) widget.attached?.original.dispose(); if (!props.keepCurrentModifiedModel) widget.attached?.modified.dispose(); widget.setModel(null); } };
  }, []);
  return <span>diff widget</span>;
} }));
const tab = (id: string, kind: ReturnType<typeof diff>['kind'] = 'text') => ({ id, ref: snapshot().repository, root: { label: `Checkout ${id}`, path: `/checkouts/${id}` }, entryId: 'one', layer: 'worktree' as const, relativePath: 'src/one.ts', snapshotRevision: 's1', status: 'ready' as const, diff: diff(kind) });
afterEach(() => { cleanup(); fake.pending = []; fake.mounts = []; fake.models.clear(); fake.widgets = []; fake.options = []; fake.events = []; fake.failAt = ''; });
it('retains visible and accessible checkout origin when another context is selected', async () => {
  const documents = new DocumentOperations(fakeAPI()); await documents.hydrate();
  const reviews = { tabs: [tab('A'), tab('B')], activeTabId: 'A', open: vi.fn(), select: vi.fn(), clearActive: vi.fn(), close: vi.fn() };
  render(<DocumentWorkspace controller={{ ...documents.state, ...documents } as any} reviews={reviews} context={{ kind: 'project', projectName: 'B' }} onClaimDocuments={vi.fn()} />);
  for (const name of ['A', 'B']) {
    const button = screen.getByRole('button', { name: `src/one.ts · git.worktree · Checkout ${name} · /checkouts/${name}` });
    expect(button.textContent).toContain(`Checkout ${name}`); expect(button.getAttribute('title')).toContain(`/checkouts/${name}/src/one.ts`);
  }
  expect(screen.getByRole('region', { name: 'git.review · Checkout A · /checkouts/A · src/one.ts · git.worktree' }).textContent).toContain('Checkout A');
});
it('disposes models when closed after widget creation before deferred onMount', async () => {
  const view = render(<GitReviewEditor tab={tab('between-effects')} />);
  await act(async () => fake.pending.splice(0).forEach(run => run()));
  expect(fake.widgets).toHaveLength(1); expect(fake.models.size).toBe(2);
  view.unmount(); expect(fake.models.size).toBe(0); expect(fake.widgets[0].disposed).toBe(true);
  expect(fake.events.slice(0, 2)).toEqual(['detach', 'widget-dispose']);
  await act(async () => fake.mounts.splice(0).forEach(run => run())); expect(fake.models.size).toBe(0);
});
it('uses independent readonly opaque models and detaches before close disposal', async () => {
  const view = render(<><GitReviewEditor key="a" tab={tab('opaque-a')} /><GitReviewEditor key="b" tab={tab('opaque-b')} /></>);
  await act(async () => { fake.pending.splice(0).forEach(run => run()); });
  await act(async () => { fake.mounts.splice(0).forEach(run => run()); });
  expect([...fake.models.keys()]).toEqual(['inmemory://git-review/opaque-a/original', 'inmemory://git-review/opaque-a/modified', 'inmemory://git-review/opaque-b/original', 'inmemory://git-review/opaque-b/modified']);
  expect(fake.options[0]).toMatchObject({ readOnly: true, originalEditable: false, renderSideBySide: false, occurrencesHighlight: 'off' });
  view.rerender(<><GitReviewEditor key="b" tab={tab('opaque-b')} /></>);
  expect([...fake.models.keys()]).toEqual(['inmemory://git-review/opaque-b/original', 'inmemory://git-review/opaque-b/modified']);
  expect(fake.events[0]).toBe('detach'); view.unmount(); expect(fake.models.size).toBe(0);
});
it('cancels deferred wrapper creation when closed before Monaco becomes ready', async () => {
  const view = render(<GitReviewEditor tab={tab('closing')} />); view.unmount();
  await act(async () => fake.pending.splice(0).forEach(run => run())); expect(fake.models.size).toBe(0); expect(fake.widgets).toHaveLength(0);
});
it.each(['loader', 'original', 'modified', 'widget', 'attach'])('cleans up partial ownership after %s failure', async failAt => {
  fake.failAt = failAt; const view = render(<GitReviewEditor tab={tab('failure')} />);
  expect(screen.getByRole('status').textContent).toBe('git.loading');
  await act(async () => fake.pending.splice(0).forEach(run => run()));
  expect(screen.getByRole('alert').textContent).toContain('git.failed'); expect(fake.models.size).toBe(0);
  expect(fake.widgets.every(widget => widget.disposed)).toBe(true);
  view.unmount(); expect(fake.models.size).toBe(0);
});
it('retains source and prior review models across rapid review switches, closing only the selected review', async () => {
  const documents = new DocumentOperations(fakeAPI()); await documents.hydrate(); await documents.open({ ...ref(), mode: 'source' });
  const controller = { ...documents.state, ...documents } as any;
  const reviews = { tabs: [tab('a'), tab('b')], activeTabId: 'a', open: vi.fn(), select: vi.fn(), clearActive: vi.fn(), close: vi.fn() };
  const props = { controller, context: null, onSelectProject: vi.fn(), onClaimDocuments: vi.fn() };
  const view = render(<DocumentWorkspace {...props} reviews={reviews} />);
  view.rerender(<DocumentWorkspace {...props} reviews={{ ...reviews, activeTabId: 'b' }} />);
  await act(async () => fake.pending.splice(0).forEach(run => run()));
  await act(async () => fake.mounts.splice(0).forEach(run => run()));
  const sourceURI = `inmemory://workspace/${documents.state.buffers[0].id}`; const source = fake.models.get(sourceURI); const a = fake.models.get('inmemory://git-review/a/original');
  expect(source?.text).toBe('base'); expect(fake.models.size).toBe(5);
  view.rerender(<DocumentWorkspace {...props} reviews={{ ...reviews, activeTabId: 'a' }} />);
  expect(fake.models.get('inmemory://git-review/a/original')).toBe(a); expect(fake.models.get(sourceURI)).toBe(source);
  view.rerender(<DocumentWorkspace {...props} reviews={{ ...reviews, tabs: [tab('a')], activeTabId: null }} />);
  expect(fake.models.size).toBe(3); expect(fake.models.get(sourceURI)).toBe(source);
  view.unmount(); expect(fake.models.size).toBe(0);
});
it.each(['binary', 'too_large', 'gitlink', 'unsupported'] as const)('renders %s summary without fake empty diff', kind => {
  render(<GitReviewEditor tab={tab('summary', kind)} />); expect(screen.getByText('git.summary.' + kind)).toBeTruthy(); expect(fake.pending).toHaveLength(0);
});
