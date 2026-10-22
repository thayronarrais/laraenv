import { useEffect, useState, type ReactNode } from 'react';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import { BrowserOpenURL } from '../../../wailsjs/runtime';
import { useT } from '../../lib/i18n';
import { workspaceDocumentApi } from './api';
import type { DocumentLocationRef, WorkspaceDocumentRef } from './types';
import { resolveWorkspaceURL } from './workspaceUrls';

const rasterMimes = new Set(['image/png', 'image/jpeg', 'image/gif', 'image/webp']);
const imageData = /^data:image\/(?:png|jpeg|gif|webp);base64,[A-Za-z0-9+/]+={0,2}$/;
const fragments = new Map<string, string>();
const fragmentKey = (location: DocumentLocationRef) => `${location.rootKey}:${location.relativePath}`;
const slug = (text: string) => text.toLowerCase().normalize('NFKD').replace(/[\u0300-\u036f]/g, '').replace(/[^\p{L}\p{N}\s-]/gu, '').trim().replace(/\s+/g, '-');
const textOf = (children: ReactNode): string => typeof children === 'string' || typeof children === 'number' ? String(children) : Array.isArray(children) ? children.map(textOf).join('') : (children && typeof children === 'object' && 'props' in children) ? textOf((children as any).props.children) : '';

export type MarkdownPreviewProps = { location: DocumentLocationRef; text: string; onOpenDocument(ref: WorkspaceDocumentRef): void; onClaimDocuments(): void };

function LocalImage({ source, alt, location }: { source: string; alt: string; location: DocumentLocationRef }) {
  const t = useT();
  const [failed, setFailed] = useState(false);
  const [attempt, setAttempt] = useState(0);
  const [data, setData] = useState<string | null>(null);
  useEffect(() => {
    let current = true;
    setData(null); setFailed(false);
    void workspaceDocumentApi.image({ ...location, relativePath: source }).then(result => {
      if (!current) return;
      if (result.code !== 'ok' && result.code !== 'changed') { setFailed(true); return; }
      const value = result.value;
      if (Number.isFinite(value.size) && value.size >= 0 && value.size <= 8 * 1024 * 1024 && value.dataUrl.length <= 4 * Math.ceil(8 * 1024 * 1024 / 3) + 32 && rasterMimes.has(value.mime) && imageData.test(value.dataUrl) && value.dataUrl.startsWith(`data:${value.mime};base64,`)) setData(value.dataUrl);
      else setFailed(true);
    }).catch(() => { if (current) setFailed(true); });
    return () => { current = false; };
  }, [attempt, location.context.kind, location.rootKey, source, location.context.kind === 'project' ? location.context.projectName : location.context.sessionId]);
  return data ? <img src={data} alt={alt} loading="lazy" onError={() => { setData(null); setFailed(true); }} /> : failed ? <span role="status" className="inline-flex max-w-full flex-wrap gap-2 text-xs"><span>{t('documents.imageFailed')}</span><button type="button" onClick={() => setAttempt(value => value + 1)}>{t('documents.retry')}</button></span> : <span role="img" aria-label={alt}>{alt}</span>;
}

export function MarkdownPreview({ location, text, onOpenDocument, onClaimDocuments }: MarkdownPreviewProps) {
  const t = useT();
  const headings = new Map<string, number>();
  useEffect(() => {
    const key = fragmentKey(location);
    const fragment = fragments.get(key);
    if (!fragment) return;
    fragments.delete(key);
    requestAnimationFrame(() => document.getElementById(fragment)?.scrollIntoView());
  }, [location.relativePath, text]);
  const heading = (Tag: 'h1' | 'h2' | 'h3' | 'h4' | 'h5' | 'h6') => ({ children, ...rest }: any) => {
    const base = slug(textOf(children)); const count = headings.get(base) ?? 0; headings.set(base, count + 1);
    return <Tag {...rest} id={count ? `${base}-${count}` : base}>{children}</Tag>;
  };
  return <div className="prose prose-invert h-full max-w-none overflow-auto p-4" onPointerDownCapture={onClaimDocuments} onFocusCapture={onClaimDocuments}>
    <ReactMarkdown remarkPlugins={[remarkGfm]} skipHtml urlTransform={(url) => resolveWorkspaceURL(url, location.relativePath).kind === 'blocked' ? '' : url}
      components={{
        h1: heading('h1'), h2: heading('h2'), h3: heading('h3'), h4: heading('h4'), h5: heading('h5'), h6: heading('h6'),
        a: ({ href, children }) => {
          const target = resolveWorkspaceURL(href ?? '', location.relativePath);
          return target.kind === 'blocked' ? <span>{children}</span> : <a href="#" onClick={event => {
            event.preventDefault(); onClaimDocuments();
            if (target.kind === 'external') BrowserOpenURL(target.url);
            else if (target.kind === 'fragment') document.getElementById(target.fragment)?.scrollIntoView();
            else { if (target.fragment) fragments.set(fragmentKey({ ...location, relativePath: target.path }), target.fragment); onOpenDocument({ ...location, relativePath: target.path, mode: /\.md(?:own)?$/i.test(target.path) ? 'preview' : 'source' }); }
          }}>{children}</a>;
        },
        img: ({ src, alt }) => {
          const target = resolveWorkspaceURL(src ?? '', location.relativePath);
          if (target.kind === 'external') return <button type="button" onClick={() => { onClaimDocuments(); BrowserOpenURL(target.url); }}>{t('documents.openRemoteImage')}: {alt}</button>;
          if (target.kind !== 'local') return <span>{alt}</span>;
          return <LocalImage source={target.path} alt={alt ?? ''} location={location} />;
        },
      }}>{text}</ReactMarkdown>
  </div>;
}
