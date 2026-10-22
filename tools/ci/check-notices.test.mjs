import assert from 'node:assert/strict';
import { mkdtempSync, mkdirSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join, resolve } from 'node:path';
import { test } from 'node:test';
import { checkCommittedOutputs, comparableInventory } from './check-notices.mjs';
import { assemble, sha256 } from '../licenses/lib.mjs';

const optional = { id: 'pkg:npm/platform-tool@1', ecosystem: 'npm', optional: true, required: false, scopes: ['build-development'], evidenceStatus: 'collected', source: { integrity: 'sha512-reviewed' }, licenses: [{ sha256: 'reviewed' }] };
const inventory = component => ({ build: { goVersion: 'go1.26.10' }, inputs: [{ path: 'lock', sha256: 'reviewed' }], components: [component] });

test('only optional npm build evidence can vary between supported hosts', () => {
  const absent = { ...optional, scopes: ['build-metadata-only'], evidenceStatus: 'metadata-only', licenses: [] };
  assert.deepEqual(comparableInventory(inventory(optional)), comparableInventory(inventory(absent)));
  for (const changed of [{ ...absent, id: 'different' }, { ...absent, source: { integrity: 'different' } }]) {
    assert.notDeepEqual(comparableInventory(inventory(optional)), comparableInventory(inventory(changed)));
  }
});

test('required, nonoptional and non-npm evidence remains exact', () => {
  for (const component of [{ ...optional, required: true }, { ...optional, optional: false }, { ...optional, ecosystem: 'go' }]) {
    assert.notDeepEqual(comparableInventory(inventory(component)), comparableInventory(inventory({ ...component, licenses: [] })));
  }
  assert.throws(() => comparableInventory(inventory({ ...optional, scopes: ['desktop-frontend-conservative'] })), /Unexpected optional scope/);
  assert.throws(() => comparableInventory(inventory({ ...optional, evidenceStatus: 'invented' })), /Unexpected optional evidence/);
});

test('toolchain and input digests remain exact', () => {
  const original = inventory(optional);
  for (const changed of [{ ...original, build: { goVersion: 'different' } }, { ...original, inputs: [] }]) {
    assert.notDeepEqual(comparableInventory(original), comparableInventory(changed));
  }
});

test('committed aggregate, legal bytes, source archive and extra files remain checked', t => {
  const temporaryParent = resolve(tmpdir());
  const root = mkdtempSync(join(temporaryParent, 'laraenv-ci-notices-'));
  t.after(() => {
    assert.equal(dirname(root), temporaryParent);
    assert.ok(root.startsWith(join(temporaryParent, 'laraenv-ci-notices-')));
    rmSync(root, { recursive: true, force: true });
  });
  const source = Buffer.from('retained source fixture');
  const sourcePath = 'tools/licenses/sources/wix-5.0.2-source.zip';
  const outputPath = 'third_party/sources/wix-5.0.2-source.zip';
  const policy = { installer: { sourceArchive: { path: sourcePath, outputPath, sha256: sha256(source) } } };
  const license = Buffer.from('Permission is hereby granted, free of charge. THE SOFTWARE IS PROVIDED AS IS.');
  const outputs = assemble({ components: [{ id: 'fixture', name: 'fixture', version: '1', required: true, scopes: ['windows-desktop'], declaredLicense: 'MIT', evidence: [{ sourcePath: 'LICENSE', bytes: license }] }] });
  outputs.set(sourcePath, source);
  outputs.set(outputPath, source);
  for (const [file, bytes] of outputs) {
    mkdirSync(dirname(join(root, file)), { recursive: true });
    writeFileSync(join(root, file), bytes);
  }
  const inventory = JSON.parse(readFileSync(join(root, 'third_party/dependencies.json'), 'utf8'));
  checkCommittedOutputs(root, inventory, policy);
  for (const file of ['THIRD_PARTY_NOTICES.txt', inventory.components[0].licenses[0].outputPath, outputPath]) {
    const original = readFileSync(join(root, file));
    writeFileSync(join(root, file), 'corrupt');
    assert.throws(() => checkCommittedOutputs(root, inventory, policy), /Changed license|stale generated/);
    writeFileSync(join(root, file), original);
  }
  writeFileSync(join(root, 'third_party/extra.txt'), 'unexpected');
  assert.throws(() => checkCommittedOutputs(root, inventory, policy), /extra generated/);
});
