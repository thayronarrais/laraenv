import assert from 'node:assert/strict';
import { mkdtempSync, mkdirSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join, resolve } from 'node:path';
import { test } from 'node:test';
import { buildDesktop } from './build-desktop.mjs';

function fixture(t, { installed = true, installCommand = 'npm ci' } = {}) {
  const parent = resolve(tmpdir());
  const root = mkdtempSync(join(parent, 'laraenv-build-desktop-'));
  t.after(() => {
    assert.equal(dirname(root), parent);
    assert.ok(root.startsWith(join(parent, 'laraenv-build-desktop-')));
    rmSync(root, { recursive: true, force: true });
  });
  mkdirSync(join(root, 'frontend'));
  if (installed) mkdirSync(join(root, 'frontend/node_modules'));
  const original = Buffer.from(`{\r\n\t"frontend:install": ${JSON.stringify(installCommand)},\r\n\t"frontend:build": "npm run build",\r\n\t"name": "LaraEnv fixture"\r\n}\r\n`);
  const configPath = join(root, 'wails.json');
  writeFileSync(configPath, original);
  return { root, configPath, original };
}

test('builds the frontend once with provisioned dependencies and restores exact config bytes', t => {
  const { root, configPath, original } = fixture(t);
  let buildCalls = 0;
  buildDesktop('C:/fixture tools/wails.exe', { root, run(binary, args, options) {
    assert.equal(binary, 'C:/fixture tools/wails.exe');
    assert.deepEqual(args, ['build', '-m', '-nosyncgomod', '-platform', 'windows/amd64', '-webview2', 'browser']);
    assert.equal(options.cwd, root);
    assert.equal(options.stdio, 'inherit');
    assert.equal(options.windowsHide, true);
    assert.ok(!options.shell && options.env === undefined, 'inherit caller Go/npm environment without a shell');
    const config = JSON.parse(readFileSync(configPath, 'utf8'));
    assert.equal(config['frontend:install'], '');
    assert.equal(config['frontend:build'], 'npm run build');
    assert.equal(config.name, 'LaraEnv fixture');
    buildCalls++;
  } });
  assert.equal(buildCalls, 1);
  assert.deepEqual(readFileSync(configPath), original);
});

test('restores exact config bytes and propagates a failed child build', t => {
  const { root, configPath, original } = fixture(t);
  const failure = new Error('fixture Wails build failed');
  assert.throws(() => buildDesktop('fixture-wails', { root, run() {
    assert.equal(JSON.parse(readFileSync(configPath, 'utf8'))['frontend:install'], '');
    throw failure;
  } }), error => error === failure);
  assert.deepEqual(readFileSync(configPath), original);
});

test('refuses unprovisioned dependencies or an unexpected install command before changing config', t => {
  for (const setup of [{ installed: false }, { installCommand: 'npm install' }]) {
    const { root, configPath, original } = fixture(t, setup);
    assert.throws(() => buildDesktop('fixture-wails', { root, run() { assert.fail('must not run Wails'); } }), /node_modules|npm ci/);
    assert.deepEqual(readFileSync(configPath), original);
  }
});
