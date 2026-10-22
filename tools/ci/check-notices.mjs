import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { generateProject } from '../licenses/generate.mjs';
import { assemble, sha256, syncOutputs } from '../licenses/lib.mjs';

const root = resolve(dirname(fileURLToPath(import.meta.url)), '../..');

// npm omits optional platform binaries on other hosts. Only their installed
// build-tool evidence may vary: identities, lock integrity, required runtime
// evidence, Go graphs, policy inputs and every other inventory field must match.
export function comparableInventory(inventory) {
  return {
    ...inventory,
    components: inventory.components.map(component => {
      if (component.ecosystem !== 'npm' || component.optional !== true || component.required !== false) return component;
      assert.ok(component.scopes.length === 1 && ['build-development', 'build-metadata-only'].includes(component.scopes[0]), `Unexpected optional scope: ${component.id}`);
      assert.ok(['collected', 'missing-build-license', 'metadata-only'].includes(component.evidenceStatus), `Unexpected optional evidence: ${component.id}`);
      const { scopes, evidenceStatus, licenses, ...metadata } = component;
      return metadata;
    }),
  };
}

export function checkCommittedOutputs(projectRoot, inventory, policy) {
  const components = inventory.components.map(component => {
    const { licenses, ...metadata } = component;
    const evidence = licenses.map(license => {
      assert.match(license.sha256, /^[a-f0-9]{64}$/);
      assert.equal(license.outputPath, `third_party/licenses/${license.sha256}.txt`);
      const bytes = readFileSync(join(projectRoot, license.outputPath));
      assert.equal(sha256(bytes), license.sha256, `Changed license bytes: ${license.outputPath}`);
      assert.equal(bytes.length, license.bytes, `Changed license length: ${license.outputPath}`);
      return { sourcePath: license.sourcePath, bytes };
    });
    return { ...metadata, evidence };
  });
  const outputs = assemble({ ...inventory, components });
  const archive = policy.installer.sourceArchive;
  assert.equal(archive.outputPath, 'third_party/sources/wix-5.0.2-source.zip');
  const source = readFileSync(join(projectRoot, archive.path));
  assert.equal(sha256(source), archive.sha256, 'Changed WiX source archive');
  outputs.set(archive.outputPath, source);
  syncOutputs(projectRoot, outputs, { check: true });
}

function main() {
  assert.ok(process.argv.length <= 3, 'Usage: node tools/ci/check-notices.mjs [go-binary]');
  const inventory = JSON.parse(readFileSync(join(root, 'third_party/dependencies.json'), 'utf8'));
  const policy = JSON.parse(readFileSync(join(root, 'tools/licenses/policy.json'), 'utf8'));
  checkCommittedOutputs(root, inventory, policy);
  const outputs = generateProject(process.argv[2] || 'go');
  const generated = JSON.parse(outputs.get('third_party/dependencies.json'));
  assert.deepEqual(comparableInventory(generated), comparableInventory(inventory), 'Stale notice inventory; regenerate with the reviewed release dependency graph');
  console.log(`Verified committed notice bytes, aggregate and source archive; ${generated.components.length} inventory entries match (optional npm build-tool evidence may vary by host).`);
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  try { main(); } catch (error) { console.error(error.message); process.exitCode = 1; }
}
