import assert from 'node:assert/strict';
import { test } from 'node:test';
import { mkdtempSync, mkdirSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { collectNpm, collectGo, collectComposer, collectInstaller, jsonStream } from './collect.mjs';
import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
const mit = 'Permission is hereby granted, free of charge to any person obtaining a copy. THE SOFTWARE IS PROVIDED AS IS.';
function fixture() {
  const root = mkdtempSync(join(tmpdir(), 'laraenv-collector-')); mkdirSync(join(root, 'frontend/node_modules/demo'), { recursive: true });
  writeFileSync(join(root, 'frontend/package.json'), JSON.stringify({ dependencies: { demo: '^1.0.0' } }));
  writeFileSync(join(root, 'frontend/package-lock.json'), JSON.stringify({ packages: { '': { dependencies: { demo: '^1.0.0' } }, 'node_modules/demo': { version: '1.0.0', license: 'MIT' }, 'node_modules/other-platform': { version: '2', license: 'MIT', dev: true, optional: true } } }));
  writeFileSync(join(root, 'frontend/node_modules/demo/package.json'), JSON.stringify({ name: 'demo', version: '1.0.0' })); writeFileSync(join(root, 'frontend/node_modules/demo/LICENSE'), mit);
  return root;
}
test('installed npm package must match lock and root manifest must match lock', () => {
  const root = fixture(); const result = collectNpm(root, 'frontend'); assert.equal(result.length, 2); assert.equal(result[1].evidenceStatus, 'metadata-only');
  writeFileSync(join(root, 'frontend/node_modules/demo/package.json'), JSON.stringify({ name: 'demo', version: '1.0.1' }));
  assert.throws(() => collectNpm(root, 'frontend'), /installed.*lock/i);
  const other = fixture(); writeFileSync(join(other, 'frontend/package.json'), JSON.stringify({ dependencies: { demo: '^2.0.0' } }));
  assert.throws(() => collectNpm(other, 'frontend'), /manifest.*lock/i);
});
test('required Go modules include explicit requirements and selected packages, with expanded test modules retained metadata-only', () => {
  const root = fixture(); const cache = join(root, 'module'); mkdirSync(cache); writeFileSync(join(cache, 'LICENSE'), mit);
  writeFileSync(join(root, 'go.mod'), 'module example\nrequire (\n example.org/required v1.0.0\n)\n');
  const modules = [{ Path: 'example.org/required', Version: 'v1.0.0', Dir: cache }, { Path: 'example.org/test', Version: 'v2.0.0' }];
  const result = collectGo(root, modules, new Map([['example.org/required', ['windows-desktop']]]));
  assert.equal(result[0].required, true); assert.equal(result[1].evidenceStatus, 'metadata-only');
  assert.throws(() => collectGo(root, [{ Path: 'example.org/required', Version: 'v1.0.0' }], new Map()), /source.*missing/i);
  const uncached = collectGo(root, modules, new Map());
  const cached = collectGo(root, [modules[0], { ...modules[1], Dir: cache, Sum: 'h1:incidental-cache' }], new Map());
  assert.deepEqual(cached, uncached);
  assert.equal(cached[1].evidenceStatus, 'metadata-only'); assert.equal(cached[1].evidence.length, 0);
});
test('Go JSON stream parser handles braces inside strings', () => {
  assert.deepEqual(jsonStream('{"Path":"a}\\n{"}\n{"Path":"b"}\n'), [{ Path: 'a}\n{' }, { Path: 'b' }]);
});
test('the real embedded Composer inventory preserves package licenses and separate nested resource terms', () => {
  const root = fileURLToPath(new URL('../../', import.meta.url));
  const policy = JSON.parse(readFileSync(new URL('./policy.json', import.meta.url)));
  const components = collectComposer(root, policy);
  assert.equal(components.filter(c => c.ecosystem === 'composer').length, 29);
  assert.equal(components.find(c => c.name === 'Mozilla CA certificate bundle').declaredLicense, 'MPL-2.0');
  assert.equal(components.find(c => c.name === 'Unicode data embedded by Symfony').declaredLicense, 'Unicode-3.0');
  assert.ok(components.find(c => c.name === 'Unicode data embedded by Symfony').source.resources.some(r => r.path.includes('polyfill-intl-normalizer/Resources/unidata/')));
  assert.throws(() => collectComposer(root, { ...policy, composer: { ...policy.composer, sha256: '0'.repeat(64) } }), /hash/i);
});
test('WiX installer records carry pinned package provenance and a required source archive', () => {
  const root = fileURLToPath(new URL('../../', import.meta.url)); const policy = JSON.parse(readFileSync(new URL('./policy.json', import.meta.url)));
  const result = collectInstaller(root, policy);
  assert.equal(result.components.length, 2); assert.ok(result.components.every(c => c.required && c.declaredLicense === 'MS-RL'));
  assert.equal(result.sourceFiles.size, 1); assert.ok(result.sourceFiles.has('third_party/sources/wix-5.0.2-source.zip'));
  assert.throws(() => collectInstaller(root, { ...policy, installer: { ...policy.installer, sourceArchive: { ...policy.installer.sourceArchive, sha256: '0'.repeat(64) } } }), /source archive hash/i);
});
