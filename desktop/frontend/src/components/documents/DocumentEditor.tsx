import { useEffect, useLayoutEffect, useRef, useState } from 'react';
import Editor, { DiffEditor, type OnMount } from '@monaco-editor/react';
import { useT } from '../../lib/i18n';
import { languageForPath } from '../../lib/fileLanguage';
import type { DocumentBuffer } from './types';

type Model = { dispose(): void; isDisposed?(): boolean };
const models = new Map<string, Model>();
/** The opaque buffer generation owns one model lifetime, independently of its display path. */
export function modelPathForBuffer(bufferId: string): string { return `inmemory://workspace/${encodeURIComponent(bufferId)}`; }
export function disposeDocumentModel(bufferId: string): void {
  const model = models.get(bufferId);
  if (model && !model.isDisposed?.()) model.dispose();
  models.delete(bufferId);
}

export type DocumentEditorProps = {
  buffer: DocumentBuffer; onEdit(text: string): void; onSave(): void; onSaveMerged(): void;
  onViewState(state: unknown): void; disabled: boolean;
};
function SourceEditor({ buffer, onEdit, onSave, onViewState, readOnly }: Pick<DocumentEditorProps, 'buffer' | 'onEdit' | 'onSave' | 'onViewState'> & { readOnly: boolean }) {
  const latest = useRef({ onEdit, onSave, readOnly }); latest.current = { onEdit, onSave, readOnly };
  const editor = useRef<Parameters<OnMount>[0] | null>(null);
  const onMount: OnMount = (instance, monaco) => {
    editor.current = instance;
    const model = instance.getModel(); if (model) models.set(buffer.id, model);
    if (buffer.viewState) instance.restoreViewState(buffer.viewState as any);
    instance.addCommand(monaco.KeyMod.CtrlCmd | monaco.KeyCode.KeyS, () => { if (instance.hasTextFocus() && !latest.current.readOnly) latest.current.onSave(); });
  };
  useEffect(() => () => { if (editor.current) onViewState(editor.current.saveViewState()); editor.current = null; }, []);
  return <Editor height="100%" theme="vs-dark" path={modelPathForBuffer(buffer.id)} language={languageForPath(buffer.references[0]?.relativePath ?? buffer.id)} value={buffer.text}
    keepCurrentModel saveViewState onMount={onMount} onChange={value => { if (!latest.current.readOnly) latest.current.onEdit(value ?? ''); }}
    options={{ readOnly, automaticLayout: true, minimap: { enabled: false }, wordWrap: 'on', occurrencesHighlight: 'off' }} />;
}

function ConflictDiff({ buffer, onEdit, onSaveMerged, readOnly, inline }: Pick<DocumentEditorProps, 'buffer' | 'onEdit' | 'onSaveMerged'> & { readOnly: boolean; inline: boolean }) {
  const t = useT();
  const pending = buffer.pendingDiskSnapshot!;
  const latest = useRef({ onEdit, readOnly, text: buffer.text }); latest.current = { onEdit, readOnly, text: buffer.text };
  const subscription = useRef<{ dispose(): void } | null>(null);
  const diff = useRef<import('monaco-editor').editor.IStandaloneDiffEditor | null>(null);
  const originalPath = `${modelPathForBuffer(buffer.id)}/disk/${encodeURIComponent(pending.revision)}`;
  const modifiedPath = `${modelPathForBuffer(buffer.id)}/merge`;
  useLayoutEffect(() => () => {
    subscription.current?.dispose(); subscription.current = null;
    const instance = diff.current;
    const model = instance?.getModel();
    if (instance && model) {
      instance.setModel(null);
      model.original.dispose(); model.modified.dispose();
    }
    diff.current = null;
  }, []);
  return <div data-testid="document-diff" data-presentation={inline ? 'inline' : 'side-by-side'} className="flex min-h-0 flex-1 flex-col border-t border-white/10">
    <div className="min-h-0 flex-1"><DiffEditor height="100%" theme="vs-dark" original={pending.text} modified={buffer.text}
      originalModelPath={originalPath}
      modifiedModelPath={modifiedPath}
      keepCurrentOriginalModel keepCurrentModifiedModel
      language={languageForPath(buffer.references[0]?.relativePath ?? buffer.id)}
      options={{ readOnly, originalEditable: false, automaticLayout: true, renderSideBySide: !inline, occurrencesHighlight: 'off' }}
      onMount={instance => {
        diff.current = instance;
        subscription.current?.dispose();
        const modified = instance.getModifiedEditor();
        subscription.current = modified.onDidChangeModelContent(() => {
          const value = modified.getValue(); const current = latest.current;
          if (!current.readOnly && value !== current.text) current.onEdit(value);
        });
      }} /></div>
    <button type="button" className="btn-primary shrink-0 self-start" disabled={readOnly} onClick={onSaveMerged}>{t('documents.saveMerged')}</button>
  </div>;
}

export function DocumentEditor({ buffer, onEdit, onSave, onSaveMerged, onViewState, disabled }: DocumentEditorProps) {
  const container = useRef<HTMLDivElement>(null);
  const [width, setWidth] = useState(0);
  useEffect(() => {
    const element = container.current;
    if (!element) return;
    const measure = () => setWidth(element.getBoundingClientRect().width);
    measure();
    if (typeof ResizeObserver === 'undefined') return;
    const observer = new ResizeObserver(measure);
    observer.observe(element);
    return () => observer.disconnect();
  }, []);
  const readOnly = disabled || buffer.snapshot.readOnly || buffer.snapshot.eol === 'mixed';
  const pending = buffer.pendingDiskSnapshot;
  return <div ref={container} className="flex h-full min-h-0 flex-col" onKeyDownCapture={event => { if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === 's' && readOnly) event.preventDefault(); }}>
    <div className="min-h-0 flex-1">
      <SourceEditor key={buffer.id} buffer={buffer} onEdit={onEdit} onSave={onSave} onViewState={onViewState} readOnly={readOnly} />
    </div>
    {pending && buffer.comparisonVisible && <ConflictDiff key={`${buffer.id}:${pending.revision}`} buffer={buffer} onEdit={onEdit} onSaveMerged={onSaveMerged} readOnly={readOnly} inline={width < 640} />}
  </div>;
}
