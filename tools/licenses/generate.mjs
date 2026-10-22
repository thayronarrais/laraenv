import { execFileSync } from 'node:child_process';
import { existsSync, readFileSync, readdirSync } from 'node:fs';
import { dirname, join, relative, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';
import { assemble, collectNotices, legalName, portable, sha256, syncOutputs } from './lib.mjs';
import { collectComposer, collectGo, collectInstaller, collectNpm, jsonStream, supplementalNotices } from './collect.mjs';
import { collectRuntimePolicy } from './runtimes.mjs';

const root = resolve(dirname(fileURLToPath(import.meta.url)), '../..');
const read = path => readFileSync(join(root, path));
function runGo(binary, args, environment = {}) {
  try { return execFileSync(binary, args, { cwd: root, encoding: 'utf8', maxBuffer: 32 * 1024 * 1024, env: { ...process.env, GOPROXY: 'off', GOSUMDB: 'off', GOTOOLCHAIN: 'local', GOFLAGS: '', GOWORK: 'off', GOOS: 'windows', GOARCH: 'amd64', CGO_ENABLED: '0', ...environment }, stdio: ['ignore', 'pipe', 'pipe'] }); }
  catch (error) { throw new Error(`Read-only Go metadata command failed (${args.join(' ')}):\n${error.stderr?.toString() || error.message}`); }
}
function toolchainComponent(goroot, version, packages) {
  const files = new Map();
  function add(dir) {
    for (const entry of readdirSync(dir, { withFileTypes: true })) if (entry.isFile() && legalName(entry.name)) {
      const file = join(dir, entry.name); files.set(portable(relative(goroot, file)), readFileSync(file));
    }
  }
  add(goroot);
  for (const pkg of packages.filter(p => p.Standard && p.Dir)) {
    let directory = pkg.Dir;
    while (directory.startsWith(goroot) && directory !== goroot) { add(directory); const parent = dirname(directory); if (parent === directory) break; directory = parent; }
  }
  return { id: `pkg:golang/stdlib@${version}`, ecosystem: 'go-toolchain', name: 'Go standard library and selected vendored notices', version, declaredLicense: 'BSD-3-Clause', scopes: ['windows-desktop', 'windows-tui', 'linux-tui'], required: true, evidenceStatus: 'collected', source: { url: `https://go.googlesource.com/go/+/refs/tags/${version}` }, evidence: [...files].sort(([a], [b]) => a < b ? -1 : a > b ? 1 : 0).map(([sourcePath, bytes]) => ({ sourcePath, bytes })) };
}
export function generateProject(goBinary = 'go') {
  const policy = JSON.parse(read('tools/licenses/policy.json')); const inputPaths = ['go.mod', 'go.sum', 'frontend/package.json', 'frontend/package-lock.json', 'tools/screenshots/package.json', 'tools/screenshots/package-lock.json', 'internal/installer/catalog.go', 'internal/installer/pecl.go', policy.composer.path, 'tools/licenses/policy.json', 'tools/licenses/generate.mjs', 'tools/licenses/lib.mjs', 'tools/licenses/collect.mjs', 'tools/licenses/composer.mjs'];
  const runtimes = collectRuntimePolicy(root); inputPaths.push(...runtimes.inputPaths);
  inputPaths.push(policy.installer.sourceArchive.path);
  for (const entry of readdirSync(join(root, 'tools/licenses/notices'), { withFileTypes: true })) if (entry.isFile()) inputPaths.push(`tools/licenses/notices/${entry.name}`);
  const inputs = [...new Set(inputPaths)].map(path => ({ path, sha256: sha256(read(path)) }));
  const environment = JSON.parse(runGo(goBinary, ['env', '-json', 'GOROOT', 'GOVERSION'])); const modules = jsonStream(runGo(goBinary, ['list', '-mod=readonly', '-m', '-json', 'all']));
  const selections = new Map(); const packages = [];
  const targets = [{ name: 'windows-desktop', os: 'windows', target: '.', tags: policy.goTags }, { name: 'windows-tui', os: 'windows', target: './cmd/deploy-tui', tags: [] }, { name: 'linux-tui', os: 'linux', target: './cmd/deploy-tui', tags: [] }];
  for (const target of targets) {
    const args = ['list', '-mod=readonly', ...(target.tags.length ? ['-tags', target.tags.join(',')] : []), '-deps', '-json', target.target];
    const selected = jsonStream(runGo(goBinary, args, { GOOS: target.os })); packages.push(...selected);
    for (const pkg of selected) if (pkg.Module && !pkg.Module.Main) { const scopes = selections.get(pkg.Module.Path) || []; if (!scopes.includes(target.name)) scopes.push(target.name); selections.set(pkg.Module.Path, scopes); }
  }
  const installer = collectInstaller(root, policy);
  const components = [...collectGo(root, modules, selections, policy), ...collectNpm(root, 'frontend'), ...collectNpm(root, 'tools/screenshots'), ...collectComposer(root, policy), ...installer.components, toolchainComponent(environment.GOROOT, environment.GOVERSION, packages), ...runtimes.components];
  const monaco = JSON.parse(read('frontend/node_modules/monaco-editor/package.json'));
  if (`monaco-editor-${monaco.version}` !== policy.codicons.version) throw new Error('Monaco version changed: review Codicons font provenance before updating policy');
  if (!existsSync(join(root, policy.codicons.path))) throw new Error('Vendored Monaco Codicons font missing');
  const codicons = policy.codicons;
  components.push({ id: `embedded:monaco/codicons@${monaco.version}`, ecosystem: 'font', name: 'Microsoft Codicons font', version: monaco.version, declaredLicense: codicons.license, scopes: ['desktop-frontend', 'vendored-monaco-amd'], required: true, evidenceStatus: 'collected', source: { resourcePath: codicons.path, sha256: sha256(read(codicons.path)), url: codicons.url }, note: codicons.note, evidence: supplementalNotices(root, codicons.notices, policy) });
  const outputs = assemble({ inputs, components, runtimeReview: runtimes.runtimeReview, build: { goVersion: environment.GOVERSION, targets: targets.map(({ name, os, target, tags }) => ({ name, goos: os, goarch: 'amd64', cgo: false, target, tags })) } });
  for (const [path, bytes] of installer.sourceFiles) outputs.set(path, bytes);
  for (const input of inputs) if (sha256(read(input.path)) !== input.sha256) throw new Error(`Input changed during notice generation: ${input.path}`);
  return outputs;
}
function main() {
  let check = false, binary = 'go'; const args = process.argv.slice(2);
  for (let at = 0; at < args.length; at++) {
    const argument = args[at];
    if (argument === '--help') { console.log('Usage: node tools/licenses/generate.mjs [--check] [--strict] [--go-bin path]\nStrict license evidence is required for desktop/TUI, explicit Go requirements and frontend runtime dependencies.\n--check recomputes without writing; --go-bin selects the exact offline Go compiler.'); return; }
    if (argument === '--check') check = true;
    else if (argument === '--strict') { /* strict is always enabled for required scope */ }
    else if (argument === '--go-bin') { binary = args[++at]; if (!binary || binary.startsWith('--')) throw new Error('--go-bin needs an executable path'); }
    else throw new Error(`Unknown option: ${argument}`);
  }
  const outputs = generateProject(binary); syncOutputs(root, outputs, { check });
  const inventory = JSON.parse(outputs.get('third_party/dependencies.json'));
  console.log(`${check ? 'Verified' : 'Generated'} ${inventory.components.length} components, ${[...outputs.keys()].filter(path => path.startsWith('third_party/licenses/')).length} unique license/notice files and ${[...outputs.keys()].filter(path => path.startsWith('third_party/sources/')).length} corresponding source archive.`);
}
if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  try { main(); } catch (error) { console.error(error.message); process.exitCode = 1; }
}
