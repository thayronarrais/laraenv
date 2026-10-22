import { execFileSync } from 'node:child_process';
import { existsSync, readFileSync, statSync, writeFileSync } from 'node:fs';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const projectRoot = resolve(dirname(fileURLToPath(import.meta.url)), '../..');
const usage = 'Usage: node tools/ci/build-desktop.mjs <wails-cli-path>';

export function buildDesktop(wailsBinary, { root = projectRoot, run = execFileSync } = {}) {
  if (!wailsBinary) throw new Error(usage);
  const configPath = join(root, 'wails.json');
  const original = readFileSync(configPath);
  const config = JSON.parse(original.toString('utf8'));
  if (config['frontend:install'] !== 'npm ci') throw new Error('Expected frontend:install to be npm ci');
  const dependencies = join(root, 'frontend/node_modules');
  if (!existsSync(dependencies) || !statSync(dependencies).isDirectory()) {
    throw new Error('Provision locked frontend/node_modules with npm ci before building');
  }
  config['frontend:install'] = '';
  try {
    writeFileSync(configPath, `${JSON.stringify(config, null, 2)}\n`);
    return run(wailsBinary, ['build', '-m', '-nosyncgomod', '-platform', 'windows/amd64', '-webview2', 'browser'], {
      cwd: root, stdio: 'inherit', windowsHide: true,
    });
  } finally {
    writeFileSync(configPath, original);
  }
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  try {
    if (process.argv.length !== 3) throw new Error(usage);
    buildDesktop(process.argv[2]);
  } catch (error) { console.error(error.message); process.exitCode = 1; }
}
