/** Renderer preflight only. Every accepted path is still revalidated by Go. */
export type WorkspaceURL = { kind: 'fragment'; fragment: string } | { kind: 'external'; url: string } | { kind: 'local'; path: string; fragment: string } | { kind: 'blocked' };
const blocked: WorkspaceURL = { kind: 'blocked' };
export function resolveWorkspaceURL(raw: string, currentPath: string): WorkspaceURL {
  if (!raw || /[\u0000-\u001f\u007f]/.test(raw)) return blocked;
  if (raw.startsWith('#')) { try { return { kind: 'fragment', fragment: decodeURIComponent(raw.slice(1)) }; } catch { return blocked; } }
  if (/^https?:\/\//i.test(raw)) {
    try { const url = new URL(raw); return (url.protocol === 'http:' || url.protocol === 'https:') ? { kind: 'external', url: url.href } : blocked; } catch { return blocked; }
  }
  if (/^[a-z][a-z\d+.-]*:/i.test(raw) || raw.startsWith('/') || raw.startsWith('\\') || raw.includes('\\') || raw.includes('?') || /%(?:2e|2f|5c|3a)/i.test(raw)) return blocked;
  const [pathPart, fragmentPart = ''] = raw.split('#', 2);
  if (!pathPart || /%(?![0-9a-f]{2})/i.test(pathPart)) return blocked;
  let decoded: string, fragment: string;
  try { decoded = decodeURIComponent(pathPart); fragment = decodeURIComponent(fragmentPart); } catch { return blocked; }
  if (!decoded || decoded.startsWith('/') || decoded.includes('\\') || decoded.includes(':') || /[\u0000-\u001f\u007f]/.test(decoded)) return blocked;
  const parts = currentPath.split('/').slice(0, -1);
  for (const part of decoded.split('/')) {
    if (!part || part === '.') continue;
    if (part === '..') { if (!parts.length) return blocked; parts.pop(); }
    else parts.push(part);
  }
  return parts.length ? { kind: 'local', path: parts.join('/'), fragment } : blocked;
}
