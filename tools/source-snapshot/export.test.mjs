import assert from 'node:assert/strict';
import { mkdtempSync, mkdirSync, readFileSync, writeFileSync, existsSync, rmSync, symlinkSync, cpSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join, resolve } from 'node:path';
import { execFileSync } from 'node:child_process';
import { createHash } from 'node:crypto';
import { test } from 'node:test';

const digest = bytes => createHash('sha256').update(bytes).digest('hex');
const git = (root, ...args) => execFileSync('git', ['-C', root, ...args], { windowsHide: true, stdio: ['pipe', 'pipe', 'pipe'] });
function fixture(t) {
  const root = mkdtempSync(join(tmpdir(), 'laraenv-public-source-'));
  t.after(() => { assert.equal(dirname(root), resolve(tmpdir())); assert.ok(root.startsWith(join(resolve(tmpdir()),'laraenv-public-source-'))); rmSync(root, { recursive: true, force: true }); });
  git(root, 'init', '--quiet');
  const policy = { schemaVersion: 1, include: { files: ['README.md'], prefixes: ['internal/', 'frontend/'], rootExtensions: ['.go'] }, exclude: { files: [{ path: 'internal-only.md', reason: 'Private review' }], prefixes: [] } };
  const put = (path, text, stage = true) => { const file = join(root, path); mkdirSync(dirname(file), { recursive: true }); writeFileSync(file, text); if (stage) git(root, 'add', '--', path); };
  put('README.md', 'Desktop source\n');
  put('main.go', 'package main\n');
  put('internal/client.go', 'package internal\n// Optional cloud client stays public.\n');
  put('internal-only.md', 'Private review material\n');
  put('laravel-server/app.php', 'private backend\n');
  put('.env', 'LARAENV_TEST_FIXTURE=private\n');
  put('out/scratch.txt', 'untracked output\n', false);
  put('frontend/source.ts', 'export const text = "Exact bytes\\r\\n";\r\n');
  return { root, policy, put, output: join(root, 'candidate') };
}
async function api() { return import('./export.mjs'); }

async function importedSnapshot(t) {
  const f = fixture(t), { exportSnapshot } = await api();
  const policy = JSON.parse(readFileSync(new URL('./policy.json', import.meta.url)));
  git(f.root, 'rm', '--cached', '--', 'internal-only.md');
  f.put('.gitignore', readFileSync(new URL('../../.gitignore', import.meta.url)));
  f.put('tools/source-snapshot/policy.json', JSON.stringify(policy, null, 2) + '\n');
  const first = exportSnapshot({ repoRoot: f.root, outputDir: f.output, policy });
  const imported = join(f.root, 'imported');
  cpSync(join(f.output, 'source'), imported, { recursive: true });
  git(imported, 'init', '--quiet');
  git(imported, 'add', '--all');
  return { ...f, policy, first, imported };
}

test('production policy re-exports an imported snapshot without tracking its generated manifest', async t => {
  const f = await importedSnapshot(t), { exportSnapshot, verifySnapshot } = await api();
  assert.ok(!git(f.imported, 'ls-files', '-z').toString().split('\0').includes('SOURCE_SNAPSHOT_MANIFEST.json'));
  const output = join(f.root, 'candidate-imported');
  const result = exportSnapshot({ repoRoot: f.imported, outputDir: output, policy: f.policy });
  assert.equal(result.archiveSha256, f.first.archiveSha256);
  assert.equal(verifySnapshot(output).files, result.files.length);
});

test('production policy regenerates a force-staged old manifest while unknown paths remain fail-closed', async t => {
  const f = await importedSnapshot(t), { exportSnapshot, verifySnapshot } = await api();
  writeFileSync(join(f.imported, 'SOURCE_SNAPSHOT_MANIFEST.json'), 'stale imported integrity artifact\n');
  git(f.imported, 'add', '--force', '--', 'SOURCE_SNAPSHOT_MANIFEST.json');
  const output = join(f.root, 'candidate-forced');
  const result = exportSnapshot({ repoRoot: f.imported, outputDir: output, policy: f.policy });
  assert.ok(result.excluded.some(file => file.path === 'SOURCE_SNAPSHOT_MANIFEST.json'));
  assert.ok(!result.files.some(file => file.path === 'SOURCE_SNAPSHOT_MANIFEST.json'));
  assert.equal(result.archiveSha256, f.first.archiveSha256);
  assert.equal(verifySnapshot(output).files, result.files.length);
  const manifest = JSON.parse(readFileSync(join(output, 'source/SOURCE_SNAPSHOT_MANIFEST.json')));
  assert.deepEqual(manifest.files, result.files);

  writeFileSync(join(f.imported, 'unreviewed.txt'), 'new unclassified payload\n');
  git(f.imported, 'add', '--', 'unreviewed.txt');
  const refused = join(f.root, 'candidate-unknown');
  assert.throws(() => exportSnapshot({ repoRoot: f.imported, outputDir: refused, policy: f.policy }), /Unclassified tracked path: unreviewed\.txt/);
  assert.equal(existsSync(refused), false);
});

test('Git replacement refs cannot substitute indexed source bytes', async t => {
  const f = fixture(t), { exportSnapshot } = await api();
  const indexed = git(f.root,'rev-parse',':main.go').toString().trim();
  const replacement = execFileSync('git',['-C',f.root,'hash-object','-w','--stdin'],{input:'replacement source\n',windowsHide:true}).toString().trim();
  git(f.root,'replace',indexed,replacement);
  exportSnapshot({repoRoot:f.root,outputDir:f.output,policy:f.policy});
  assert.equal(readFileSync(join(f.output,'source/main.go'),'utf8'),'package main\n');
});

test('snapshot exports only indexed reviewed desktop bytes and excludes private/untracked data', async t => {
  const f = fixture(t), { exportSnapshot } = await api();
  f.put('main.go', 'UNSTAGED LOCAL CHANGE\n', false);
  const result = exportSnapshot({ repoRoot: f.root, outputDir: f.output, policy: f.policy });
  assert.equal(readFileSync(join(f.output, 'source/main.go'), 'utf8'), 'package main\n');
  assert.equal(readFileSync(join(f.output, 'source/internal/client.go'), 'utf8'), 'package internal\n// Optional cloud client stays public.\n');
  for (const path of ['.git', '.env', 'laravel-server', 'out', 'internal-only.md']) assert.equal(existsSync(join(f.output, 'source', path)), false, path);
  assert.equal(result.files.length, 4);
  for (const file of result.files) assert.equal(digest(readFileSync(join(f.output, 'source', file.path))), file.sha256);
  assert.ok(result.excluded.some(row => row.path === 'laravel-server/app.php'));
  const manifest = JSON.parse(readFileSync(join(f.output, 'source/SOURCE_SNAPSHOT_MANIFEST.json')));
  assert.deepEqual(manifest.files, result.files);
  assert.ok(!JSON.stringify(manifest).includes('internal-only.md'));
  assert.ok(!JSON.stringify(manifest).includes(f.root));
});

test('unclassified tracked paths and credential findings stop before output exists without exposing values', async t => {
  const { exportSnapshot } = await api();
  for (const kind of ['unknown', 'credential']) {
    const f = fixture(t);
    const secret = 'gh' + 'p_' + 'A'.repeat(36);
    f.put(kind === 'unknown' ? 'unreviewed.txt' : 'internal/config.go', kind === 'unknown' ? 'new file' : `package internal\nconst token = "${secret}"\n`);
    assert.throws(() => exportSnapshot({ repoRoot: f.root, outputDir: f.output, policy: f.policy }), error => {
      assert.ok(!error.message.includes(secret));
      assert.match(error.message, kind === 'unknown' ? /unclassified.*unreviewed/i : /credential.*internal\/config.go.*2/i);
      return true;
    });
    assert.equal(existsSync(f.output), false);
  }
});

test('existing destinations and link redirection preserve previous data', async t => {
  const f = fixture(t), { exportSnapshot } = await api();
  mkdirSync(f.output); writeFileSync(join(f.output, 'keep.txt'), 'existing');
  assert.throws(() => exportSnapshot({ repoRoot: f.root, outputDir: f.output, policy: f.policy }), /exist/i);
  assert.equal(readFileSync(join(f.output, 'keep.txt'), 'utf8'), 'existing');
  const redirect = join(f.root, 'redirect');
  symlinkSync(f.output, redirect, process.platform === 'win32' ? 'junction' : 'dir');
  assert.throws(() => exportSnapshot({ repoRoot: f.root, outputDir: join(redirect, 'new'), policy: f.policy }), /link/i);
  assert.equal(existsSync(join(f.output, 'new')), false);
});

test('Windows aliases, traversal and git links are rejected before materialization', async t => {
  const { validateSourcePaths } = await api();
  for (const paths of [['internal/a.go', 'INTERNAL/A.go'], ['internal/a.go', 'INTERNAL/b.go'], ['internal/a.go', 'internal/a.go/file.go'], ['../escape.go'], ['C:/escape.go'], ['internal/file.'], ['internal/NUL.txt'], ['internal/a:stream'], ['internal/a?.go'], ['internal\\a.go']]) assert.throws(() => validateSourcePaths(paths), /unsafe|alias/i);
  const f = fixture(t), { exportSnapshot } = await api();
  const blob = git(f.root, 'hash-object', '-w', '--stdin');
  git(f.root, 'update-index', '--add', '--cacheinfo', `120000,${blob.toString().trim()},internal/link`);
  assert.throws(() => exportSnapshot({ repoRoot: f.root, outputDir: f.output, policy: f.policy }), /mode|link/i);
  assert.equal(existsSync(f.output), false);
});

test('ZIP and public manifest are deterministic and materialized byte drift is detected', async t => {
  const f = fixture(t), { exportSnapshot, verifySnapshot } = await api();
  const first = exportSnapshot({ repoRoot: f.root, outputDir: f.output, policy: f.policy });
  const secondDir = join(f.root, 'candidate2');
  const second = exportSnapshot({ repoRoot: f.root, outputDir: secondDir, policy: f.policy });
  assert.equal(first.archiveSha256, second.archiveSha256);
  assert.ok(readFileSync(first.archivePath).equals(readFileSync(second.archivePath)));
  assert.equal(verifySnapshot(f.output).files, 4);
  writeFileSync(join(f.output, 'source/main.go'), 'tampered');
  assert.throws(() => verifySnapshot(f.output), /hash|bytes/i);
});
