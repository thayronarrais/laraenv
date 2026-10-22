// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { afterEach, expect, it, vi } from 'vitest';
import { DocumentEditor, disposeDocumentModel, modelPathForBuffer } from '../DocumentEditor';
import { snapshot } from './fixtures';
import type { DocumentBuffer } from '../types';
import { languageForPath } from '../../../lib/fileLanguage';
const mockMonaco = vi.hoisted(() => ({
  models: new Map<string, { id: number; disposed: boolean; dispose: () => void; isDisposed: () => boolean }>(), next: 0,
  activeDiff: null as null | { value: string; listener: null | (() => void) },
  editDiff(value: string) { if (this.activeDiff) { this.activeDiff.value = value; this.activeDiff.listener?.(); } },
}));
vi.mock('@monaco-editor/react', async () => {
  const React = await import('react');
  return {
    default: function MockDocumentEditor(props: any) {
      const command = React.useRef<() => void>(() => {});
      const model = React.useRef<any>(null);
      React.useEffect(() => {
        let next = mockMonaco.models.get(props.path);
        if (!next || next.disposed) { next = { id: ++mockMonaco.next, disposed: false, dispose() { this.disposed = true; }, isDisposed() { return this.disposed; } }; mockMonaco.models.set(props.path, next); }
        model.current = next; // The installed wrapper calls setModel on path updates, without onMount.
      }, [props.path]);
      React.useEffect(() => {
        props.onMount({ getModel: () => model.current, hasTextFocus: () => true, addCommand: (_key: number, fn: () => void) => { command.current = fn; }, saveViewState: () => ({ line: 3 }), restoreViewState: () => null }, { KeyMod: { CtrlCmd: 1 }, KeyCode: { KeyS: 2 } });
        return () => { if (!props.keepCurrentModel) model.current?.dispose(); };
      }, []); // The installed wrapper calls onMount once per editor instance.
      return <div data-testid="source" data-path={props.path} data-readonly={String(props.options.readOnly)} data-keep={String(props.keepCurrentModel)} onKeyDown={(event: any) => { if (event.ctrlKey && event.key === 's') command.current(); }} />;
    },
    DiffEditor: (props: any) => {
      const state = React.useRef({ value: props.modified, listener: null as null | (() => void) });
      const models = React.useRef({ original: { dispose: vi.fn() }, modified: { dispose: vi.fn() } });
      const attached = React.useRef(true);
      React.useEffect(() => {
        props.onMount({
          getModel: () => attached.current ? models.current : null,
          setModel: (value: unknown) => { attached.current = value !== null; },
          getModifiedEditor: () => ({ onDidChangeModelContent: (fn: () => void) => { state.current.listener = fn; return { dispose: () => { state.current.listener = null; } }; }, getValue: () => state.current.value }),
        });
        mockMonaco.activeDiff = state.current;
        return () => { if (mockMonaco.activeDiff === state.current) mockMonaco.activeDiff = null; };
      }, []);
      React.useEffect(() => { state.current.value = props.modified; state.current.listener?.(); }, [props.modified]);
      return <div data-testid="diff" data-original={props.original} data-modified={props.modified} data-original-path={props.originalModelPath} data-modified-path={props.modifiedModelPath} />;
    },
  };
});
afterEach(() => { cleanup(); props.onSave.mockClear(); });
vi.mock('../../../lib/i18n', () => ({ useT: () => (key: string) => key }));
const buffer = (extra: Partial<DocumentBuffer> = {}): DocumentBuffer => ({ id: '/p/a.md', references: [], snapshot: snapshot('/p/a.md'), text: 'local', dirty: true, editGeneration: 1, viewState: null, pendingDiskSnapshot: null, comparisonVisible: false, status: 'ready', ...extra });
const props = { onEdit: vi.fn(), onSave: vi.fn(), onSaveMerged: vi.fn(), onViewState: vi.fn(), disabled: false };
it('keeps a URI-safe source model and handles Ctrl+S while editable', () => {
  render(<DocumentEditor buffer={buffer({ id: 'C:\\work\\a#b?.md', snapshot: snapshot('C:\\work\\a#b?.md') })} {...props} />);
  const source = screen.getByTestId('source');
  expect(source.getAttribute('data-path')).toBe(modelPathForBuffer('C:\\work\\a#b?.md'));
  expect(source.getAttribute('data-path')).not.toContain('#');
  expect(source.getAttribute('data-keep')).toBe('true');
  fireEvent.keyDown(source, { key: 's', ctrlKey: true });
  expect(props.onSave).toHaveBeenCalledTimes(1);
});
it('preserves the Monaco model across a Preview/source unmount and disposes it on last close', () => {
  const path = modelPathForBuffer('/p/a.md');
  const first = render(<DocumentEditor buffer={buffer()} {...props} />);
  const model = mockMonaco.models.get(path)!;
  first.unmount(); // Preview replaces Source, while the controller buffer stays open.
  expect(model.disposed).toBe(false);
  const second = render(<DocumentEditor buffer={buffer()} {...props} />);
  expect(mockMonaco.models.get(path)?.id).toBe(model.id);
  second.unmount();
  disposeDocumentModel('/p/a.md'); // DocumentWorkspace invokes this when its final tab closes.
  expect(model.disposed).toBe(true);
});
it('owns A, B, then A source models and captures each view state with mount-once wrapper semantics', () => {
  const a = buffer(); const b = buffer({ id: '/p/b.md', snapshot: snapshot('/p/b.md'), text: 'B' });
  const views: string[] = [];
  const view = render(<DocumentEditor buffer={a} {...props} onViewState={() => views.push('A')} />);
  const modelA = mockMonaco.models.get(modelPathForBuffer(a.id))!;
  view.rerender(<DocumentEditor buffer={b} {...props} onViewState={() => views.push('B')} />);
  const modelB = mockMonaco.models.get(modelPathForBuffer(b.id))!;
  expect(modelB).toBeTruthy(); expect(views).toContain('A');
  view.rerender(<DocumentEditor buffer={a} {...props} onViewState={() => views.push('A')} />);
  expect(mockMonaco.models.get(modelPathForBuffer(a.id))?.id).toBe(modelA.id);
  expect(views).toContain('B');
  view.unmount(); disposeDocumentModel(a.id); disposeDocumentModel(b.id);
  expect(modelA.disposed).toBe(true); expect(modelB.disposed).toBe(true);
});
it('sends manual conflict edits only to the active buffer and ignores controlled updates and handoff', () => {
  const a = buffer({ pendingDiskSnapshot: snapshot('/p/a.md', 'disk A', 'r2'), comparisonVisible: true, status: 'conflict' });
  const b = buffer({ id: '/p/b.md', snapshot: snapshot('/p/b.md'), text: 'B', pendingDiskSnapshot: snapshot('/p/b.md', 'disk B', 'r2'), comparisonVisible: true, status: 'conflict' });
  const editA = vi.fn(), editB = vi.fn();
  const view = render(<DocumentEditor buffer={a} {...props} onEdit={editA} />);
  view.rerender(<DocumentEditor buffer={b} {...props} onEdit={editB} />);
  mockMonaco.editDiff('merged B');
  expect(editB).toHaveBeenCalledWith('merged B'); expect(editA).not.toHaveBeenCalled();
  editB.mockClear();
  view.rerender(<DocumentEditor buffer={{ ...b, text: 'server B' }} {...props} onEdit={editB} />);
  expect(editB).not.toHaveBeenCalled();
  view.rerender(<DocumentEditor buffer={{ ...b, text: 'server B' }} {...props} onEdit={editB} disabled />);
  mockMonaco.editDiff('should not edit');
  expect(editB).not.toHaveBeenCalled();
});
it('shows mixed EOL read-only and compares disk against local with distinct models', () => {
  const pending = snapshot('/p/a.md', 'disk', 'r2');
  render(<DocumentEditor buffer={buffer({ snapshot: { ...snapshot('/p/a.md'), eol: 'mixed' }, pendingDiskSnapshot: pending, comparisonVisible: true, status: 'conflict' })} {...props} />);
  expect(screen.getByTestId('source').getAttribute('data-readonly')).toBe('true');
  const diff = screen.getByTestId('diff');
  expect(diff.getAttribute('data-original')).toBe('disk'); expect(diff.getAttribute('data-modified')).toBe('local');
  expect(diff.getAttribute('data-modified-path')).not.toBe(modelPathForBuffer('/p/a.md'));
});
it('offers a merged save against a disk conflict', () => {
  render(<DocumentEditor buffer={buffer({ pendingDiskSnapshot: snapshot('/p/a.md', 'disk', 'r2'), comparisonVisible: true, status: 'conflict' })} {...props} />);
  fireEvent.click(screen.getByRole('button', { name: 'documents.saveMerged' }));
  expect(props.onSaveMerged).toHaveBeenCalledTimes(1);
});
it('shares SFTP language selection for known and unknown file names', () => {
  expect(languageForPath('Dockerfile')).toBe('dockerfile');
  expect(languageForPath('src/view.tsx')).toBe('typescript');
  expect(languageForPath('README.md')).toBe('markdown');
  expect(languageForPath('notes.unknown')).toBe('plaintext');
});
