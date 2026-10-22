import { layerLabels, summaryLabels } from './labels';
import { useEffect, useLayoutEffect, useRef, useState } from 'react';
import { loader } from '@monaco-editor/react';
import '../../lib/monaco';
import type { editor } from 'monaco-editor';
import { useT } from '../../lib/i18n';
import { languageForPath } from '../../lib/fileLanguage';
import type { GitReviewTab } from './gitReviewOperations';

// Review models never enter the editable-document registry. Each mounted tab owns its pair.
type ReviewResources = { widget: editor.IStandaloneDiffEditor | null; original: editor.ITextModel | null; modified: editor.ITextModel | null };
const models = new Map<string, ReviewResources>();
function TextReview({ tab, inline }: { tab: GitReviewTab; inline: boolean }) {
  const t = useT(); const host = useRef<HTMLDivElement>(null);
  const widget = useRef<editor.IStandaloneDiffEditor | null>(null); const currentInline = useRef(inline); currentInline.current = inline;
  const [ready, setReady] = useState(false); const [error, setError] = useState<string | null>(null);
  useLayoutEffect(() => {
    let active = true;
    const owned: ReviewResources = { widget: null, original: null, modified: null };
    models.set(tab.id, owned);
    const release = () => {
      // Ownership starts at creation, rather than waiting for a wrapper's later onMount effect.
      owned.widget?.setModel(null); owned.widget?.dispose(); owned.widget = null; widget.current = null;
      owned.original?.dispose(); owned.modified?.dispose(); owned.original = null; owned.modified = null;
      if (models.get(tab.id) === owned) models.delete(tab.id);
    };
    const failed = (reason: unknown) => { release(); if (active) setError(reason instanceof Error ? reason.message : String(reason)); };
    let pending: ReturnType<typeof loader.init> | undefined;
    try {
      pending = loader.init();
      pending.then(monaco => {
        if (!active || !host.current) return;
        try {
          const language = languageForPath(tab.relativePath);
          owned.original = monaco.editor.createModel(tab.diff!.original.text, language, monaco.Uri.parse(`inmemory://git-review/${tab.id}/original`));
          owned.modified = monaco.editor.createModel(tab.diff!.modified.text, language, monaco.Uri.parse(`inmemory://git-review/${tab.id}/modified`));
          owned.widget = monaco.editor.createDiffEditor(host.current, { theme: 'vs-dark', readOnly: true, originalEditable: false, automaticLayout: true, renderSideBySide: !currentInline.current, occurrencesHighlight: 'off' });
          widget.current = owned.widget;
          owned.widget.setModel({ original: owned.original, modified: owned.modified });
          setReady(true);
        } catch (reason) { failed(reason); }
      }).catch(reason => { if (active) failed(reason); });
    } catch (reason) { failed(reason); }
    return () => { active = false; pending?.cancel(); release(); };
  }, [tab.id]);
  useEffect(() => { widget.current?.updateOptions({ renderSideBySide: !inline }); }, [inline]);
  return <div className="relative h-full min-h-0">
    {!ready && !error && <p role="status">{t('git.loading')}</p>}
    {error && <p role="alert">{t('git.failed')}: {error}</p>}
    <div ref={host} className="h-full min-h-0" />
  </div>;
}
export function GitReviewEditor({ tab }: { tab: GitReviewTab }) {
  const t = useT(); const container = useRef<HTMLDivElement>(null); const [width, setWidth] = useState(0);
  useEffect(() => {
    const element = container.current; if (!element) return;
    const measure = () => setWidth(element.getBoundingClientRect().width); measure();
    if (typeof ResizeObserver === 'undefined') return;
    const observer = new ResizeObserver(measure); observer.observe(element); return () => observer.disconnect();
  }, []);
  return <section ref={container} className="flex h-full min-h-0 min-w-0 flex-col" aria-label={`${t('git.review')} · ${tab.root.label} · ${tab.root.path} · ${tab.relativePath} · ${t(layerLabels[tab.layer])}`}>
    <header className="shrink-0 min-w-0 border-b border-white/10 px-3 py-2 text-xs" title={`${tab.root.path.replace(/[\\/]$/, '')}/${tab.relativePath}`}>
      <p className="truncate">{tab.root.label} · {tab.relativePath || t('git.review')} · {t(layerLabels[tab.layer])}</p>
      <p className="truncate text-white/50">{tab.root.path}</p>
    </header>
    <div className="min-h-0 min-w-0 flex-1">
    {tab.status === 'loading' && <p role="status">{t('git.loading')}</p>}
    {tab.problem && <p role="alert">{t('git.failed')}: {tab.problem.message}</p>}
    {tab.diff?.kind === 'text' ? <TextReview key={tab.id} tab={tab} inline={width < 640} /> : tab.diff && <div className="p-4"><p>{t(summaryLabels[tab.diff.kind])}</p><p>{tab.diff.message}</p></div>}
    </div>
  </section>;
}
