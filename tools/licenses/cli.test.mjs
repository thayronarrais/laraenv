import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';
import { test } from 'node:test';
const cli = fileURLToPath(new URL('./generate.mjs', import.meta.url));
test('notice generator documents the read-only check and compiler selection', () => {
  const result = spawnSync(process.execPath, [cli, '--help'], { encoding: 'utf8' });
  assert.equal(result.status, 0, result.stderr); assert.match(result.stdout, /--check/); assert.match(result.stdout, /--go-bin/);
});
test('notice generator rejects unknown options before reading dependencies', () => {
  const result = spawnSync(process.execPath, [cli, '--unexpected'], { encoding: 'utf8' });
  assert.notEqual(result.status, 0); assert.match(result.stderr, /Unknown option/);
});
