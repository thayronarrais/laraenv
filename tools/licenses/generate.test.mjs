import assert from 'node:assert/strict';
import { mkdtempSync, mkdirSync, writeFileSync, readFileSync, readdirSync, symlinkSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { test } from 'node:test';
import { assemble, validateSpdx, syncOutputs, collectNotices, identifyLicense } from './lib.mjs';

const license = 'MIT License\nCopyright Example\nPermission is hereby granted, free of charge, to any person obtaining a copy\nof this software and associated documentation files (the Software), to deal\nin the Software without restriction.\nTHE SOFTWARE IS PROVIDED AS IS.\n';
function temp() { return mkdtempSync(join(tmpdir(), 'laraenv-licenses-')); }
function fixture() { return { inputs: [{ path: 'frontend/package-lock.json', sha256: 'abc' }], components: [{ id: 'pkg:npm/demo@1.0.0', ecosystem: 'npm', name: 'demo', version: '1.0.0', scopes: ['desktop-frontend'], declaredLicense: 'MIT', evidenceStatus: 'collected', required: true, evidence: [{ sourcePath: 'LICENSE', bytes: Buffer.from(license) }] }] }; }

test('unknown or malformed SPDX declarations fail instead of becoming cleared', () => {
  assert.equal(validateSpdx('(MIT OR Apache-2.0) AND BSD-3-Clause'), true);
  assert.throws(() => validateSpdx('Some-Custom-Terms'), /SPDX/);
  assert.throws(() => validateSpdx('MIT OR'), /SPDX/);
  assert.throws(() => validateSpdx('MIT Apache-2.0'), /SPDX/);
});
test('required components without complete license evidence stop generation', () => {
  const input = fixture(); input.components[0].evidence = [];
  assert.throws(() => assemble(input), /license evidence/i);
});
test('a title or copyright notice alone cannot satisfy a required license', () => {
  const input = fixture(); input.components[0].evidence = [{ sourcePath: 'LICENSE', bytes: Buffer.from('MIT License\nCopyright Someone') }];
  assert.throws(() => assemble(input), /license evidence/i);
});
test('Unicode data terms are identified separately from MIT code terms', () => {
  const bytes = readFileSync(new URL('./notices/unicode-LICENSE.txt', import.meta.url));
  assert.equal(identifyLicense(bytes), 'Unicode-3.0');
});
test('WiX Microsoft Reciprocal License remains separately identified', () => {
  validateSpdx('MS-RL');
  assert.equal(identifyLicense(readFileSync(new URL('./notices/wix-5.0.2-LICENSE.txt', import.meta.url))), 'MS-RL');
});
test('rendered aggregate uses canonical LF while hashed upstream text keeps CRLF', () => {
  const input = fixture(); input.components[0].evidence.push({ sourcePath: 'NOTICE', bytes: Buffer.from('upstream\r\nnotice\r\n') });
  const output = assemble(input); assert.ok(!output.get('THIRD_PARTY_NOTICES.txt').includes('\r'));
  assert.ok([...output.values()].some(x => Buffer.isBuffer(x) && x.equals(Buffer.from('upstream\r\nnotice\r\n'))));
});
test('aggregate includes resource attribution and the exact corresponding source URL', () => {
  const input = fixture(); Object.assign(input.components[0], { note: 'Retain resource attribution', source: { resourcePath: 'nested/data.pem', url: 'https://example.org/license', correspondingSource: 'https://example.org/pinned/data.pem' } });
  const text = assemble(input).get('THIRD_PARTY_NOTICES.txt');
  assert.match(text, /Retain resource attribution/); assert.match(text, /nested\/data.pem/); assert.match(text, /https:\/\/example.org\/pinned\/data.pem/);
});
test('metadata-only components are retained without being marked cleared', () => {
  const input = fixture(); input.components.push({ id: 'pkg:npm/optional@1', name: 'optional', version: '1', ecosystem: 'npm', declaredLicense: 'MIT', scopes: ['build-metadata-only'], required: false, evidenceStatus: 'metadata-only', evidence: [] });
  const output = assemble(input); const inventory = JSON.parse(output.get('third_party/dependencies.json'));
  assert.equal(inventory.components[1].evidenceStatus, 'metadata-only');
  assert.equal(inventory.components[1].licenses.length, 0);
});
test('same input gives identical portable outputs and preserves exact notice bytes', () => {
  const input = fixture(); input.components[0].evidence.push({ sourcePath: 'nested/ThirdPartyNotices.txt', bytes: Buffer.from('UPSTREAM NOTICE\r\n') });
  const one = assemble(input); const two = assemble(structuredClone(input));
  assert.deepEqual(one, two);
  assert.ok(one.get('THIRD_PARTY_NOTICES.txt').includes('UPSTREAM NOTICE'));
  assert.ok([...one.keys()].some(p => p.startsWith('third_party/licenses/')));
  assert.ok(!one.get('third_party/dependencies.json').includes(tmpdir()));
  assert.ok([...one.values()].some(v => Buffer.isBuffer(v) && v.equals(Buffer.from('UPSTREAM NOTICE\r\n'))));
});
test('notice collection includes nested upstream terms and skips nested dependency trees', () => {
  const dir = temp(); mkdirSync(join(dir, 'nested')); mkdirSync(join(dir, 'node_modules'));
  writeFileSync(join(dir, 'LICENSE'), license); writeFileSync(join(dir, 'nested', 'ThirdPartyNotices.txt'), 'nested terms'); writeFileSync(join(dir, 'node_modules', 'LICENSE'), 'other package');
  assert.deepEqual(collectNotices(dir).map(x => x.sourcePath), ['LICENSE', 'nested/ThirdPartyNotices.txt']);
});
test('--check detects missing stale and extra files without changing any file', () => {
  const dir = temp(); const output = assemble(fixture());
  assert.throws(() => syncOutputs(dir, output, { check: true }), /generated/i);
  assert.deepEqual(readdirSync(dir), []);
  syncOutputs(dir, output); syncOutputs(dir, output, { check: true });
  writeFileSync(join(dir, 'THIRD_PARTY_NOTICES.txt'), 'stale');
  assert.throws(() => syncOutputs(dir, output, { check: true }), /stale/i);
  assert.equal(readFileSync(join(dir, 'THIRD_PARTY_NOTICES.txt'), 'utf8'), 'stale');
  syncOutputs(dir, output); writeFileSync(join(dir, 'third_party', 'licenses', 'extra.txt'), 'extra');
  assert.throws(() => syncOutputs(dir, output, { check: true }), /extra/i);
});
test('a symlink at the generated directory boundary is rejected before writes', () => {
  const dir = temp(), outside = temp(); symlinkSync(outside, join(dir, 'third_party'), process.platform === 'win32' ? 'junction' : 'dir');
  assert.throws(() => syncOutputs(dir, assemble(fixture())), /Symlink/i);
  assert.deepEqual(readdirSync(outside), []);
  assert.ok(!readdirSync(dir).includes('THIRD_PARTY_NOTICES.txt'));
});
test('root notice symlinks including dangling targets are rejected before any generated write', () => {
  for (const dangling of [false, true]) {
    const dir = temp(), outside = join(temp(), 'outside.txt');
    if (!dangling) writeFileSync(outside, 'untouched');
    symlinkSync(outside, join(dir, 'THIRD_PARTY_NOTICES.txt'), 'file');
    assert.throws(() => syncOutputs(dir, assemble(fixture())), /Symlink/i);
    assert.ok(!readdirSync(dir).includes('third_party'));
    if (!dangling) assert.equal(readFileSync(outside, 'utf8'), 'untouched');
  }
});
