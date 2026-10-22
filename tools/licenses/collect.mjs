import { existsSync, readFileSync } from 'node:fs';
import { join } from 'node:path';
import { collectNotices, identifyLicense, sha256, validateSpdx } from './lib.mjs';
import { parsePhar } from './composer.mjs';

const json = file => JSON.parse(readFileSync(file, 'utf8').replace(/^\uFEFF/, ''));
const stableObject = value => JSON.stringify(Object.fromEntries(Object.entries(value || {}).sort(([a], [b]) => a < b ? -1 : a > b ? 1 : 0)));
export function collectInstaller(root, policy) {
  const installer = policy.installer; const archive = installer.sourceArchive;
  const sourceBytes = readFileSync(join(root, archive.path));
  if (sha256(sourceBytes) !== archive.sha256) throw new Error('WiX corresponding source archive hash changed');
  const evidence = supplementalNotices(root, installer.notices, policy);
  const components = installer.packages.map(pkg => ({
    id: `pkg:nuget/${pkg.name}@${installer.version}`, ecosystem: 'installer-resource', name: pkg.name, version: installer.version, declaredLicense: installer.license,
    scopes: ['msi-installer-resource'], required: true, evidenceStatus: 'collected-with-corresponding-source',
    source: { url: `https://www.nuget.org/packages/${pkg.name}/${installer.version}`, packageSha256: pkg.sha256, extensionDllSha256: pkg.dllSha256, msiBinary: pkg.msiBinary, reference: installer.sourceCommit, correspondingSource: archive.url, archivePath: archive.outputPath, archiveSha256: archive.sha256 },
    note: 'Unmodified WiX 5.0.2 installer resources retain MS-RL. The accompanying pinned source archive and full MS-RL text must be delivered with the MSI. Other original desktop files retain their separate license. No additional licensing exception was found in the versioned license/source review.', evidence
  }));
  return { components, sourceFiles: new Map([[archive.outputPath, sourceBytes]]) };
}
export function supplementalNotices(root, names, policy) {
  return names.map(name => {
    const bytes = readFileSync(join(root, 'tools/licenses/notices', name)); const expected = policy.noticeSources?.[name]?.sha256;
    if (expected && sha256(bytes) !== expected) throw new Error(`Supplemental notice hash changed: ${name}`);
    return { sourcePath: `supplemental/${name}`, bytes };
  });
}
export function collectComposer(root, policy) {
  const config = policy.composer; const archive = readFileSync(join(root, config.path));
  if (sha256(archive) !== config.sha256) throw new Error('Embedded Composer PHAR hash changed: review provenance and nested resources before generation');
  const entries = parsePhar(archive); const installedBytes = entries.get('vendor/composer/installed.json');
  if (!installedBytes) throw new Error('Embedded Composer installed package manifest missing');
  const packages = JSON.parse(installedBytes).packages; if (!Array.isArray(packages)) throw new Error('Invalid Composer installed manifest');
  const base = { scopes: ['embedded-composer'], required: true, evidenceStatus: 'collected' };
  const source = { artifactPath: config.path, sha256: config.sha256, url: config.sourceUrl };
  const components = [{ ...base, id: `pkg:composer/composer/composer@${config.version}`, ecosystem: 'composer', name: 'composer/composer', version: config.version, declaredLicense: 'MIT', source, evidence: [{ sourcePath: 'LICENSE', bytes: entries.get('LICENSE') || Buffer.alloc(0) }] }];
  for (const pkg of packages) {
    const declaration = pkg.license.join(' OR '); validateSpdx(declaration);
    const prefix = `vendor/${pkg.name}/`; const evidence = [...entries].filter(([name]) => name.startsWith(prefix) && /^(?:.*\/)?(?:LICENSE|COPYING|NOTICE)(?:\.[^/]*)?$/i.test(name.slice(prefix.length))).map(([name, bytes]) => ({ sourcePath: name.slice(prefix.length), bytes }));
    components.push({ ...base, id: `pkg:composer/${pkg.name}@${pkg.version}`, ecosystem: 'composer', name: pkg.name, version: pkg.version, declaredLicense: declaration, source: { ...source, packagePath: prefix.slice(0, -1), url: pkg.source?.url || pkg.dist?.url || null, reference: pkg.source?.reference || null }, evidence });
  }
  for (const resource of config.resources) {
    const bytes = entries.get(resource.path); if (!bytes || sha256(bytes) !== resource.sha256) throw new Error(`Embedded resource hash changed: ${resource.path}`);
    const pkg = resource.package ? components.find(c => c.name === resource.package) : null;
    const evidence = resource.notices ? supplementalNotices(root, resource.notices, policy) : pkg?.evidence;
    components.push({ ...base, id: `embedded:composer/${resource.path}`, ecosystem: 'embedded-resource', name: resource.name, version: config.version, declaredLicense: resource.license, source: { ...source, resourcePath: resource.path, resourceSha256: resource.sha256, url: resource.url, ...(resource.correspondingSource ? { correspondingSource: resource.correspondingSource } : {}) }, note: resource.note, evidence: evidence || [] });
  }
  const unicodePaths = [...entries.keys()].filter(name => /vendor\/symfony\/(?:string\/Resources\/data|polyfill-(?:mbstring|intl-normalizer)\/Resources\/unidata)\//.test(name)).sort();
  if (!unicodePaths.length) throw new Error('Expected Composer Unicode resources missing');
  components.push({ ...base, id: 'embedded:composer/unicode-data', ecosystem: 'embedded-resource', name: 'Unicode data embedded by Symfony', version: config.version, declaredLicense: config.unicode.license, source: { ...source, url: config.unicode.url, resources: unicodePaths.map(path => ({ path, sha256: sha256(entries.get(path)) })) }, note: config.unicode.note, evidence: supplementalNotices(root, config.unicode.notices, policy) });
  return components;
}
export function collectNpm(root, project) {
  const manifest = json(join(root, project, 'package.json')); const lock = json(join(root, project, 'package-lock.json'));
  if (!lock.packages?.['']) throw new Error(`Missing package-lock v2/v3 package graph: ${project}`);
  for (const field of ['dependencies', 'devDependencies', 'optionalDependencies']) if (stableObject(manifest[field]) !== stableObject(lock.packages[''][field])) throw new Error(`Manifest/lock mismatch: ${project} ${field}`);
  return Object.entries(lock.packages).filter(([location]) => location).sort(([a], [b]) => a < b ? -1 : a > b ? 1 : 0).map(([location, entry]) => {
    if (entry.link) throw new Error(`Unreviewed linked npm dependency: ${project}/${location}`);
    validateSpdx(entry.license);
    const dir = join(root, project, location); const installed = existsSync(join(dir, 'package.json')) ? json(join(dir, 'package.json')) : null;
    if (installed && installed.version !== entry.version) throw new Error(`Installed/lock version mismatch: ${project}/${location}: ${installed.version} != ${entry.version}`);
    const required = project === 'frontend' && !entry.dev;
    if (required && !installed) throw new Error(`Required npm source missing: ${project}/${location}`);
    const name = installed?.name || location.split('node_modules/').pop();
    const evidence = installed ? collectNotices(dir) : [];
    return { id: `pkg:npm/${name}@${entry.version}?project=${project}&path=${location}`, ecosystem: 'npm', name, version: entry.version, declaredLicense: entry.license, scopes: [required ? 'desktop-frontend-conservative' : installed ? 'build-development' : 'build-metadata-only'], required, evidenceStatus: installed ? (evidence.length ? 'collected' : 'missing-build-license') : 'metadata-only', source: { lockPath: `${project}/package-lock.json`, packagePath: location, integrity: entry.integrity || null, url: entry.resolved || null }, optional: !!entry.optional, evidence };
  });
}
export function collectGo(root, modules, selections, policy = {}) {
  const requirements = new Set([...readFileSync(join(root, 'go.mod'), 'utf8').matchAll(/^\s*(?:require\s+)?([^\s()]+)\s+(v\S+)/gm)].map(m => m[1]));
  const sums = new Map(existsSync(join(root, 'go.sum')) ? readFileSync(join(root, 'go.sum'), 'utf8').trim().split(/\r?\n/).map(line => { const [name, version, sum] = line.split(/\s+/); return [`${name}@${version}`, sum]; }) : []);
  return modules.filter(m => !m.Main).sort((a, b) => a.Path < b.Path ? -1 : a.Path > b.Path ? 1 : 0).map(module => {
    const selected = selections.get(module.Path) || []; const required = requirements.has(module.Path) || selected.length > 0;
    if (required && !module.Dir) throw new Error(`Required Go source missing: ${module.Path}@${module.Version}`);
    let evidence = required && module.Dir ? collectNotices(module.Dir) : [];
    const override = policy.goOverrides?.[`${module.Path}@${module.Version}`];
    if (override && required) {
      for (const source of override.evidence) {
        const bytes = readFileSync(join(module.Dir, source.path)); if (sha256(bytes) !== source.sha256) throw new Error(`Go override evidence hash changed: ${module.Path}/${source.path}`);
        evidence.push({ sourcePath: source.path, bytes });
      }
      evidence.push(...supplementalNotices(root, override.notices, policy));
    }
    const primary = evidence.find(e => !e.sourcePath.includes('/') && identifyLicense(e.bytes)) || evidence.find(e => identifyLicense(e.bytes));
    const declaredLicense = override?.license || (primary ? identifyLicense(primary.bytes) : null);
    return { id: `pkg:golang/${module.Path}@${module.Version}`, ecosystem: 'go', name: module.Path, version: module.Version, declaredLicense, scopes: selected.length ? selected : [requirements.has(module.Path) ? 'source-dependency-conservative' : 'expanded-build-test-metadata'], required, evidenceStatus: evidence.length ? 'collected' : 'metadata-only', source: { lockPath: 'go.sum', sum: sums.get(`${module.Path}@${module.Version}`) || null, url: `https://pkg.go.dev/${module.Path}@${module.Version}` }, ...(override && required ? { reviewedException: override.rationale } : {}), evidence };
  });
}
export function jsonStream(input) {
  const values = []; let depth = 0, start = -1, quoted = false, escaped = false;
  for (let at = 0; at < input.length; at++) {
    const char = input[at];
    if (quoted) { if (escaped) escaped = false; else if (char === '\\') escaped = true; else if (char === '"') quoted = false; continue; }
    if (char === '"') quoted = true;
    else if (char === '{') { if (!depth) start = at; depth++; }
    else if (char === '}') { depth--; if (depth < 0) throw new Error('Invalid Go JSON stream'); if (!depth) values.push(JSON.parse(input.slice(start, at + 1))); }
    else if (!depth && !/\s|\uFEFF/.test(char)) throw new Error('Invalid Go JSON stream');
  }
  if (depth || quoted) throw new Error('Truncated Go JSON stream'); return values;
}
