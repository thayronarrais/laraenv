import { spawn } from 'node:child_process';
import { fileURLToPath } from 'node:url';

// Node 25 exposes an incomplete native localStorage without a backing file.
// Browser tests must use jsdom's per-window storage, not that Node facility.
// Feature detection keeps the runner usable on Node versions without this flag.
// Vitest starts its own workers with separate execArgv, so this must travel
// through the child environment as well as the initial runner process.
const env = { ...process.env };
if (process.allowedNodeEnvironmentFlags.has('--no-experimental-webstorage')) {
  env.NODE_OPTIONS = `${env.NODE_OPTIONS ?? ''} --no-experimental-webstorage`.trim();
}
const child = spawn(process.execPath, [
  fileURLToPath(new URL('../node_modules/vitest/vitest.mjs', import.meta.url)),
  'run', ...process.argv.slice(2),
], { stdio: 'inherit', windowsHide: true, env });
child.once('error', error => { process.stderr.write(`${error.message}\n`); process.exitCode = 1; });
child.once('exit', (code, signal) => { process.exitCode = signal ? 130 : code ?? 1; });
process.once('SIGINT', () => child.kill('SIGINT'));
process.once('SIGTERM', () => child.kill('SIGTERM'));
