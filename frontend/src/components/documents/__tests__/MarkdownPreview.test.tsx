// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { MarkdownPreview } from '../MarkdownPreview';
import { deferred, ref } from './fixtures';

const image = vi.fn();
const browser = vi.fn();
vi.mock('../api', () => ({ workspaceDocumentApi: { image: (...args: unknown[]) => image(...args) } }));
vi.mock('../../../../wailsjs/runtime', () => ({ BrowserOpenURL: (...args: unknown[]) => browser(...args) }));
vi.mock('../../../lib/i18n', () => ({ useT: () => (key: string) => key }));
afterEach(cleanup);
beforeEach(() => { image.mockReset(); browser.mockReset(); });

it('renders GFM without executing HTML or protocols and opens safe links in the same root', () => {
  const open = vi.fn();
  const text = '| A | B |\n|---|---|\n| 1 | 2 |\n<script>alert(1)</script><img src=x onerror=alert(1)>\n[Next](next%20file.md#Part) [Bad](javascript:alert(1)) [Escape](%2e%2e/secret.md) [Drive](C:secret.md) [ADS](file.md:stream)';
  render(<MarkdownPreview location={{ ...ref('p', 'docs/start.md') }} text={text} onOpenDocument={open} onClaimDocuments={vi.fn()} />);
  expect(screen.getByRole('table')).toBeTruthy();
  expect(document.querySelector('script, img[onerror]')).toBeNull();
  fireEvent.click(screen.getByText('Next'));
  expect(open).toHaveBeenCalledWith(expect.objectContaining({ rootKey: ref().rootKey, relativePath: 'docs/next file.md' }));
  for (const label of ['Bad', 'Escape', 'Drive', 'ADS']) fireEvent.click(screen.getByText(label));
  expect(open).toHaveBeenCalledTimes(1);
  expect(browser).not.toHaveBeenCalled();
});

it('loads bounded local raster images and makes remote images explicit', async () => {
  image.mockResolvedValue({ code: 'ok', value: { mime: 'image/png', size: 4, dataUrl: 'data:image/png;base64,AAAA' } });
  const { rerender } = render(<MarkdownPreview location={ref('p', 'docs/start.md')} text={'![local](space%20name.png) ![remote](https://example.test/a.png)'} onOpenDocument={vi.fn()} onClaimDocuments={vi.fn()} />);
  await waitFor(() => expect(screen.getByAltText('local').getAttribute('src')).toBe('data:image/png;base64,AAAA'));
  expect(image).toHaveBeenCalledWith(expect.objectContaining({ relativePath: 'docs/space name.png' }));
  expect(image).not.toHaveBeenCalledWith(expect.objectContaining({ relativePath: 'https://example.test/a.png' }));
  expect(document.querySelector('img[src^="https:"]')).toBeNull();
  fireEvent.click(screen.getByRole('button', { name: /remote/i }));
  expect(browser).toHaveBeenCalledWith('https://example.test/a.png');
  image.mockResolvedValue({ code: 'ok', value: { mime: 'image/svg+xml', size: 4, dataUrl: 'data:image/svg+xml;base64,AAAA' } });
  rerender(<MarkdownPreview location={ref('p', 'docs/start.md')} text={'![bad](bad.svg)'} onOpenDocument={vi.fn()} onClaimDocuments={vi.fn()} />);
  await waitFor(() => expect(image).toHaveBeenCalledTimes(2));
  expect(document.querySelector('img[src^="data:image/svg"]')).toBeNull();
});
it('keeps heading IDs stable and scrolls within Preview for a fragment', () => {
  const scroll = vi.fn(); Element.prototype.scrollIntoView = scroll;
  const props = { location: ref('p', 'docs/start.md'), text: '## Hello World\n\n[Jump](#hello-world)', onOpenDocument: vi.fn(), onClaimDocuments: vi.fn() };
  const view = render(<MarkdownPreview {...props} />);
  expect(screen.getByRole('heading', { name: 'Hello World' }).id).toBe('hello-world');
  view.rerender(<MarkdownPreview {...props} />);
  expect(screen.getByRole('heading', { name: 'Hello World' }).id).toBe('hello-world');
  fireEvent.click(screen.getByText('Jump'));
  expect(scroll).toHaveBeenCalledTimes(1);
  expect(props.onOpenDocument).not.toHaveBeenCalled();
});
it('blocks encoded escapes, drive streams and active protocols before navigation or image reads', () => {
  const open = vi.fn();
  const variants = ['javascript:alert(1)', 'data:text/html,hi', 'file:///secret', 'command:run', '//example.test/x', '%2e%2e/secret', 'C:secret', 'a.md:stream', '%2fsecret'];
  const text = variants.map((url, index) => `[bad${index}](${url}) ![image${index}](${url})`).join('\n\n');
  render(<MarkdownPreview location={ref('p', 'docs/start.md')} text={text} onOpenDocument={open} onClaimDocuments={vi.fn()} />);
  for (const index of variants.keys()) fireEvent.click(screen.getByText(`bad${index}`));
  expect(open).not.toHaveBeenCalled(); expect(browser).not.toHaveBeenCalled(); expect(image).not.toHaveBeenCalled();
  expect(document.querySelector('img')).toBeNull();
});

it.each(['rejected', 'invalid', 'oversized', 'promise'])('shows bounded local image failure and retries: %s', async failure => {
  if (failure === 'promise') image.mockRejectedValueOnce(new Error('offline'));
  else image.mockResolvedValueOnce(failure === 'rejected' ? { code: 'unavailable', message: 'gone' } : { code: 'ok', value: { mime: failure === 'invalid' ? 'image/svg+xml' : 'image/png', size: failure === 'oversized' ? 8388609 : 4, dataUrl: 'data:image/png;base64,AAAA' } });
  render(<MarkdownPreview location={ref()} text="![](picture.png)" onOpenDocument={vi.fn()} onClaimDocuments={vi.fn()} />);
  expect((await screen.findByRole('status')).textContent).toContain('documents.imageFailed');
  expect(document.querySelector('img')).toBeNull();
  image.mockResolvedValueOnce({ code: 'ok', value: { mime: 'image/png', size: 3, dataUrl: 'data:image/png;base64,AAAA' } });
  fireEvent.click(screen.getByRole('button', { name: 'documents.retry' }));
  await waitFor(() => expect(document.querySelector('img')?.getAttribute('src')).toBe('data:image/png;base64,AAAA'));
});
it('ignores a local image failure from the previous source', async () => {
  const old = deferred<any>(); image.mockReturnValueOnce(old.promise).mockResolvedValue({ code: 'ok', value: { mime: 'image/png', size: 3, dataUrl: 'data:image/png;base64,AAAA' } });
  const props = { location: ref(), onOpenDocument: vi.fn(), onClaimDocuments: vi.fn() };
  const view = render(<MarkdownPreview {...props} text="![old](old.png)" />);
  view.rerender(<MarkdownPreview {...props} text="![new](new.png)" />);
  await waitFor(() => expect(screen.getByAltText('new').getAttribute('src')).toBe('data:image/png;base64,AAAA'));
  old.reject(new Error('late')); await Promise.resolve();
  expect(screen.queryByRole('status')).toBeNull();
});
