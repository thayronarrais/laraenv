// Shared Monaco language selection for local workspace files and SFTP.
export function languageForPath(path: string): string {
  const lower = path.toLowerCase();
  const ext = lower.includes('.') ? lower.slice(lower.lastIndexOf('.') + 1) : '';
  const name = lower.includes('/') ? lower.slice(lower.lastIndexOf('/') + 1) : lower;
  if (name === 'dockerfile') return 'dockerfile';
  if (name === 'makefile') return 'makefile';
  switch (ext) {
    case 'php': return 'php';
    case 'js': case 'mjs': case 'cjs': return 'javascript';
    case 'ts': case 'tsx': case 'jsx': return 'typescript';
    case 'json': return 'json';
    case 'yml': case 'yaml': return 'yaml';
    case 'md': case 'markdown': return 'markdown';
    case 'html': case 'htm': return 'html';
    case 'css': return 'css';
    case 'scss': case 'sass': return 'scss';
    case 'sh': case 'bash': case 'zsh': return 'shell';
    case 'py': return 'python';
    case 'go': return 'go';
    case 'rs': return 'rust';
    case 'rb': return 'ruby';
    case 'sql': return 'sql';
    case 'xml': return 'xml';
    case 'toml': case 'ini': case 'conf': case 'cfg': case 'env': return 'ini';
    case 'log': return 'log';
    case 'dockerfile': return 'dockerfile';
  }
  return 'plaintext';
}
