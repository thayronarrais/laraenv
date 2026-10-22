import { readFileSync, readdirSync } from 'node:fs';
import { basename, join } from 'node:path';
import { identifyLicense, sha256, validateSpdx } from './lib.mjs';

const normalized = bytes => Buffer.from(bytes.toString('utf8').replaceAll('\r\n', '\n'));
const key = row => row && typeof row === 'object' ? JSON.stringify(row, Object.keys(row).sort()) : JSON.stringify(row);
function exactRows(actual, reviewed, label) {
  const a = actual.map(key), b = reviewed.map(key);
  if (new Set(a).size !== a.length || new Set(b).size !== b.length) throw new Error(`Duplicate ${label} coverage row`);
  if (a.length !== b.length || a.some(row => !b.includes(row))) throw new Error(`Incomplete or changed ${label} coverage`);
}
function catalogRows(text) {
  const starts = [...text.matchAll(/\{Kind:\s*(Kind\w+),\s*Version:\s*"([^"]+)"/g)];
  return starts.map((match, index) => {
    const section = text.slice(match.index, starts[index + 1]?.index || text.length);
    const url = section.match(/URL:\s*"([^"]+)"/)?.[1];
    if (!url) throw new Error('Catalog source has no literal reviewed URL');
    return { kind: match[1].slice(4).toLowerCase(), version: match[2], url, manual: /Manual:\s*true/.test(section), direct: /Direct:\s*true/.test(section) };
  });
}
function peclRows(text) {
  const vc = new Map();
  for (const match of text.matchAll(/case\s+((?:"[^"]+"\s*,?\s*)+):\s*return\s+"([^"]+)"/g)) for (const series of match[1].matchAll(/"([^"]+)"/g)) vc.set(series[1], match[2]);
  const starts = [...text.matchAll(/Name:\s*"([^"]+)",\s*DisplayName:/g)], rows = [];
  for (const [index, start] of starts.entries()) {
    const section = text.slice(start.index, starts[index + 1]?.index || text.length), extension = start[1];
    for (const match of section.matchAll(/\{PHPSeries:\s*"([^"]+)",\s*Version:\s*"([^"]+)"\}/g)) {
      const phpSeries = match[1], version = match[2], tag = vc.get(phpSeries);
      if (!tag) throw new Error(`PECL toolchain mapping missing: ${phpSeries}`);
      rows.push({ extension, phpSeries, version, url: `https://windows.php.net/downloads/pecl/releases/${extension}/${version}/php_${extension}-${version}-${phpSeries}-nts-${tag}-x64.zip` });
    }
  }
  return rows;
}
function supplementalRows(text) {
  const rows = [];
  for (const match of text.matchAll(/case\s+source\.Kind\s*==\s*(Kind\w+)([\s\S]*?):\s*names\s*=\s*\[\]string\{([^}]+)\}/g)) {
    const kind = match[1].slice(4).toLowerCase(), versions = [...match[2].matchAll(/source\.Version\s*==\s*"([^"]+)"/g)].map(m => m[1]);
    const names = [...match[3].matchAll(/"([^"]+)"/g)].map(m => m[1]);
    for (const version of versions) for (const name of names) rows.push({ kind, version, name });
  }
  return rows;
}
function reviewedMetadata(component) {
  return { reviewedPolicyComponent: component.id, reviewedSoftwareLicense: component.softwareLicense, reviewedSoftwareLicenseSources: component.softwareLicenseSources, binaryTerms: component.binaryTerms, requiredActions: component.requiredActions, evidenceStatus: component.evidenceStatus, redistributionStatus: component.redistributionStatus, archiveEvidence: component.archiveEvidence, primaryEvidence: component.primaryEvidence || [], reviewNotes: component.notes || [] };
}
function metadataComponent(component, download, ecosystem, id, sourcePath) {
  return { id, ecosystem, name: download.extension || download.kind, version: download.version, ...(download.phpSeries ? { phpSeries: download.phpSeries } : {}), declaredLicense: null, scopes: [ecosystem === 'optional-pecl' ? 'optional-downloaded-php-extension' : 'optional-downloaded-tool'], required: false, ...reviewedMetadata(component), downloadEvidenceStatus: component.archiveEvidence?.some(a => a.url === download.url && a.status === 'exact-archive-reviewed') ? 'exact-archive-reviewed' : 'no-exact-archive-review', source: { catalogPath: sourcePath, url: download.url, ...(download.manual === undefined ? {} : { manual: download.manual, direct: download.direct }) }, note: 'Reviewed source/project terms and representative archive evidence retain their stated scope. This metadata does not establish complete binary dependency coverage, authenticate this download, or grant bundling/mirroring clearance.', evidence: [] };
}

function publicationRecords(policy, catalog) {
  const records = policy.publications === undefined ? [] : policy.publications;
  const fail = detail => { throw new Error(`Invalid runtime publication: ${detail}`); };
  const digest = value => typeof value === 'string' && /^[a-f0-9]{64}$/.test(value);
  const positive = value => Number.isSafeInteger(value) && value > 0;
  if (!Array.isArray(records) || records.length > 1) fail('expected at most one reviewed pgvector publication');
  for (const record of records) {
    if (record?.id !== 'pgvector-0.8.6-license-repair-20261021' || record.repository !== 'thayronarrais/laraenv' || record.releaseId !== 371369603 || record.tag !== 'pgvector-0.8.6' || record.releaseUrl !== 'https://github.com/thayronarrais/laraenv/releases/tag/pgvector-0.8.6') fail('release identity changed');
    if (record.verificationMethod !== 'anonymous-public-download-and-api-digest' || typeof record.verifiedAt !== 'string' || !/^\d{4}-\d{2}-\d{2}T.+(?:Z|[+-]\d{2}:\d{2})$/.test(record.verifiedAt) || !Number.isFinite(Date.parse(record.verifiedAt))) fail('missing verification method or ISO timestamp');
    const notice = policy.supplementalNotices.find(n => n.path === 'internal/installer/licenses/pgvector-LICENSE.txt');
    if (record.license?.path !== 'share/doc/pgvector/LICENSE' || record.license.sha256 !== notice?.sha256 || record.license.bytes !== notice?.bytes) fail('license does not match the exact embedded upstream notice');
    if (typeof record.evidencePath !== 'string' || !record.evidencePath.startsWith('build/verification/') || /[\\:\x00-\x1f]/.test(record.evidencePath) || record.evidencePath.split('/').some(p => !p || p === '.' || p === '..')) fail('evidence reference must be a portable repository-relative path');
    if (!Array.isArray(record.limits) || !record.limits.length || record.limits.some(limit => typeof limit !== 'string' || !limit.trim())) fail('explicit evidence limits are required');
    if (!Array.isArray(record.artifacts) || record.artifacts.length !== 4) fail('four archive/checksum pairs are required');
    const expected = catalog.filter(source => source.kind === 'pgvector');
    const versions = new Set(), assetIds = new Set();
    for (const artifact of record.artifacts) {
      const source = expected.find(source => source.version === artifact?.version);
      if (!source || versions.has(artifact.version)) fail('missing, duplicate or unknown catalog version');
      const name = `pgvector-${artifact.version}-win-x64.zip`;
      if (!/^pgvector-0\.8\.6-pg(?:15|16|17|18)-win-x64\.zip$/.test(name) || artifact.name !== name || artifact.url !== source.url) fail('archive name/URL differs from the exact catalog entry');
      if (!digest(artifact.sha256) || !digest(artifact.originalSha256) || !positive(artifact.bytes) || artifact.originalEntriesPreserved !== 43) fail('invalid archive digest, size or preservation count');
      const checksum = artifact.checksum;
      const checksumBytes = Buffer.from(`${artifact.sha256}  ${name}\n`, 'ascii');
      if (checksum?.name !== name + '.sha256' || checksum.url !== source.url + '.sha256' || !digest(checksum.sha256) || checksum.sha256 !== sha256(checksumBytes) || checksum.bytes !== checksumBytes.length) fail('checksum name/URL/hash/bytes do not match its archive');
      for (const id of [artifact.assetId, checksum.assetId]) {
        if (!positive(id) || assetIds.has(id)) fail('invalid or duplicate asset identity');
        assetIds.add(id);
      }
      versions.add(artifact.version);
    }
    if (expected.length !== 4 || expected.some(source => !versions.has(source.version))) fail('incomplete pgvector catalog coverage');
  }
  return records;
}

function archiveNoticeRegistry(root, policy, coverage, catalogMode = false) {
  const config = catalogMode ? policy.catalogNoticeRegistry : policy.peclNoticeRegistry;
  if (config === undefined) return { components: [], inputPaths: [], metadata: null };
  const directory = catalogMode ? 'internal/installer/runtime_notices' : 'internal/installer/pecl_notices';
  const label = catalogMode ? 'catalog' : 'PECL';
  const fail = detail => { throw new Error(`Invalid ${label} notice registry: ${detail}`); };
  const digest = value => typeof value === 'string' && /^[a-f0-9]{64}$/.test(value);
  if (config?.path !== directory + '/manifest.json' || !digest(config.sha256)) fail('unexpected manifest path or hash');
  const raw = readFileSync(join(root, config.path));
  if (sha256(raw) !== config.sha256) fail('manifest hash changed');
  let registry;
  try { registry = JSON.parse(raw.toString('utf8')); } catch { fail('manifest JSON is malformed'); }
  if (registry.schemaVersion !== 1 || !Array.isArray(registry.bundles) || !registry.bundles.length || registry.bundles.length > 64) fail('unsupported schema or bundle count');
  const components = [], paths = [config.path, 'internal/installer/pecl_supplemental_legal.go', ...(catalogMode ? ['internal/installer/catalog_supplemental_legal.go'] : [])];
  const ids = new Set(), builds = new Set(), names = new Set();
  for (const bundle of registry.bundles) {
    if (typeof bundle.id !== 'string' || !/^[A-Za-z0-9][A-Za-z0-9._-]*$/.test(bundle.id) || ids.has(bundle.id)) fail('invalid or duplicate bundle identity');
    const build = coverage.find(row => row.version === bundle.version && row.url === bundle.archiveUrl && (catalogMode ? row.kind === bundle.kind && bundle.extension === undefined && bundle.phpSeries === undefined : row.extension === bundle.extension && row.phpSeries === bundle.phpSeries && bundle.kind === undefined));
    const buildKey = JSON.stringify(catalogMode ? [bundle.kind, bundle.version] : [bundle.extension, bundle.version, bundle.phpSeries]);
    if (!build || builds.has(buildKey) || !digest(bundle.archiveSha256) || bundle.scope !== 'supplemental-notices-not-complete-binary-clearance') fail('duplicate or unreviewed exact archive scope');
    if (!Array.isArray(bundle.notices) || !bundle.notices.length || bundle.notices.length > 256) fail('bundle notice count is invalid');
    ids.add(bundle.id); builds.add(buildKey);
    let totalBytes = 0;
    for (const notice of bundle.notices) {
      if (typeof notice.path !== 'string' || !/^[A-Za-z0-9][A-Za-z0-9._-]*\.txt$/.test(notice.path) || names.has(notice.path.toLowerCase())) fail('invalid, aliased or duplicate notice path');
      let url;
      try { url = new URL(notice.sourceUrl); } catch { fail('notice source URL is malformed'); }
      if (url.protocol !== 'https:' || url.username || url.password || typeof notice.licenseDescription !== 'string' || !notice.licenseDescription.trim()) fail('notice needs an HTTPS source and descriptive license terms');
      const resourcePath = directory + '/' + notice.path;
      totalBytes += notice.bytes;
      if (!digest(notice.sha256) || !Number.isSafeInteger(notice.bytes) || notice.bytes <= 0 || notice.bytes > 8 * 1024 * 1024 || totalBytes > 32 * 1024 * 1024) fail('invalid notice hash or byte count');
      names.add(notice.path.toLowerCase()); paths.push(resourcePath);
      components.push({ id: `embedded:${catalogMode ? 'catalog' : 'pecl'}-notice/${bundle.id}/${notice.path}`, ecosystem: catalogMode ? 'embedded-catalog-notice' : 'embedded-pecl-notice', name: notice.path, version: bundle.version, declaredLicense: null, reviewedSoftwareLicense: notice.licenseDescription, redistributionStatus: 'requires-review', runtimeScope: { ...(catalogMode ? {kind:bundle.kind} : {extension:bundle.extension,phpSeries:bundle.phpSeries}), version: bundle.version, archiveUrl: bundle.archiveUrl, archiveSha256: bundle.archiveSha256, scope: bundle.scope }, scopes: [`desktop-embedded-exact-archive-${catalogMode ? 'catalog' : 'pecl'}-notice`], required: false, evidenceStatus: 'collected-exact-archive-bound-supplemental-notice', source: { resourcePath, url: notice.sourceUrl, sha256: notice.sha256, ...(notice.sourceEvidence ? {sourceEvidence:notice.sourceEvidence} : {}) }, note: 'Exact source notice embedded only for the identified archive fingerprint. Heterogeneous supplemental terms do not establish complete companion provenance, corresponding-source obligations or binary redistribution clearance.', evidence: [] });
    }
  }
  const files = readdirSync(join(root, directory), { withFileTypes: true });
  if (files.some(file => !file.isFile() || file.isSymbolicLink())) fail('registry files must be regular files');
  exactRows(files.map(file => file.name).sort(), ['manifest.json', ...components.map(c => c.name)].sort(), label + ' registry file');
  for (const component of components) {
    const bytes = readFileSync(join(root, component.source.resourcePath));
    const expected = registry.bundles.flatMap(bundle => bundle.notices).find(notice => notice.path === component.name);
    if (sha256(bytes) !== expected.sha256 || bytes.length !== expected.bytes) fail(`notice hash/bytes changed: ${component.name}`);
    component.evidence.push({ sourcePath: component.source.resourcePath, bytes });
  }
  return { components, inputPaths: paths, metadata: { path: config.path, sha256: config.sha256, schemaVersion: registry.schemaVersion, bundles: registry.bundles } };
}

export function collectRuntimePolicy(root, policy = JSON.parse(readFileSync(join(root, 'tools/licenses/runtime-policy.json')))) {
  if (policy.schemaVersion !== 1 || policy.scope?.automaticBundlingClearance !== false) throw new Error('Unsupported runtime policy or automatic clearance scope');
  const requiredInputs = ['internal/installer/catalog.go', 'internal/installer/pecl.go', 'internal/vcruntime/vcruntime.go', 'internal/webviewruntime/runtime_windows.go', 'internal/webviewruntime/signature_windows.go'];
  if (policy.inputs.length !== requiredInputs.length || new Set(policy.inputs.map(i => i.path)).size !== requiredInputs.length || requiredInputs.some(path => !policy.inputs.some(i => i.path === path))) throw new Error('Incomplete reviewed runtime input coverage');
  for (const input of policy.inputs) {
    if (input.normalization !== 'CRLF-to-LF; no other byte changes') throw new Error(`Unsupported reviewed runtime input normalization: ${input.path}`);
    if (sha256(normalized(readFileSync(join(root, input.path)))) !== input.sha256) throw new Error(`Reviewed runtime input hash changed: ${input.path}`);
  }
  const catalog = catalogRows(readFileSync(join(root, requiredInputs[0]), 'utf8'));
  exactRows(catalog, policy.catalogCoverage.sources, 'catalog');
  if (catalog.length !== policy.catalogCoverage.sourceCount || new Set(catalog.map(r => r.kind)).size !== policy.catalogCoverage.kindCount) throw new Error('Incorrect catalog coverage counts');
  const pecl = peclRows(readFileSync(join(root, requiredInputs[1]), 'utf8'));
  exactRows(pecl, policy.peclCoverage.builds, 'PECL');
  if (pecl.length !== policy.peclCoverage.buildCount || new Set(pecl.map(r => r.extension)).size !== policy.peclCoverage.extensionCount) throw new Error('Incorrect PECL coverage counts');
  if (new Set(policy.components.map(c => c.id)).size !== policy.components.length) throw new Error('Duplicate reviewed runtime policy component');
  exactRows(['microsoft-visual-c-runtime', 'microsoft-webview2-runtime'], policy.components.filter(c => c.kind === 'platform-prerequisite').map(c => c.id), 'platform prerequisite');
  const componentCatalog = policy.components.flatMap(c => c.sources || []), componentPecl = policy.components.flatMap(c => c.builds || []);
  exactRows(catalog, componentCatalog, 'component catalog'); exactRows(pecl, componentPecl, 'component PECL');
  const components = [];
  for (const component of policy.components) {
    if (!component.softwareLicense || !component.softwareLicenseSources?.length || !component.requiredActions?.length || !component.evidenceStatus || !component.redistributionStatus) throw new Error(`Missing reviewed runtime metadata: ${component.id}`);
    if (component.kind !== 'platform-prerequisite') exactRows([...new Set([...(component.sources || []), ...(component.builds || [])].map(row => row.version))], component.versions, 'component version');
    if (component.sources?.some(s => s.kind !== component.kind || !component.versions.includes(s.version)) || component.builds?.some(s => component.id !== 'pecl-' + s.extension || !component.versions.includes(s.version))) throw new Error(`Runtime component version coverage mismatch: ${component.id}`);
    for (const source of component.sources || []) components.push(metadataComponent(component, source, 'optional-download', `optional-download:${source.kind}@${source.version}`, requiredInputs[0]));
    for (const source of component.builds || []) components.push(metadataComponent(component, source, 'optional-pecl', `optional-pecl:${source.extension}@${source.version}?phpSeries=${source.phpSeries}`, requiredInputs[1]));
    if (component.kind === 'platform-prerequisite') components.push({ id: `external-prerequisite:${component.id}`, ecosystem: 'external-prerequisite', name: component.id, version: component.versions.join(', '), declaredLicense: null, scopes: ['windows-prerequisite'], required: false, ...reviewedMetadata(component), source: { url: component.softwareLicenseSources[0] }, note: 'Separately licensed Microsoft prerequisite. Reviewed distribution documentation is not redistribution clearance or an artifact inspection.', evidence: [] });
  }
  const supplementals = policy.supplementalNotices;
  if (!Array.isArray(supplementals) || supplementals.length !== 6) throw new Error('Incomplete supplemental notice coverage');
  const noticeNames = supplementals.map(n => basename(n.path));
  exactRows(readdirSync(join(root, 'internal/installer/licenses')).filter(name => name.endsWith('.txt')).sort(), [...noticeNames].sort(), 'supplemental embedded notice');
  const scopes = supplementals.flatMap(n => n.runtimeScope.versions.map(version => ({ kind: n.runtimeScope.kind, version, name: basename(n.path) })));
  exactRows(supplementalRows(readFileSync(join(root, 'internal/installer/supplemental_legal.go'), 'utf8')), scopes, 'supplemental retention scope');
  for (const notice of supplementals) {
    if (!/^internal\/installer\/licenses\/[A-Za-z0-9_-]+\.txt$/.test(notice.path) || !/^https:\/\/raw\.githubusercontent\.com\//.test(notice.url)) throw new Error('Invalid supplemental notice source');
    const bytes = readFileSync(join(root, notice.path));
    if (sha256(bytes) !== notice.sha256 || bytes.length !== notice.bytes) throw new Error(`Supplemental notice hash/bytes changed: ${notice.path}`);
    validateSpdx(notice.license);
    if (identifyLicense(bytes) !== notice.license) throw new Error(`Supplemental fixed license evidence mismatch: ${notice.path}`);
    const name = basename(notice.path);
    components.push({ id: `embedded:runtime-notice/${name}`, ecosystem: 'embedded-runtime-notice', name, version: notice.runtimeScope.versions.join(', '), runtimeScope: notice.runtimeScope, declaredLicense: notice.license, scopes: ['desktop-embedded-runtime-source-notice'], required: true, evidenceStatus: 'collected-source-notice-not-binary-clearance', source: { resourcePath: notice.path, url: notice.url, sha256: notice.sha256 }, note: 'Exact version-bound upstream source notice embedded by the installer. It supplements identified archive omissions and does not establish full binary/companion provenance, source delivery or redistribution clearance.', evidence: [{ sourcePath: notice.path, bytes }] });
  }
  const registry = archiveNoticeRegistry(root, policy, pecl), catalogRegistry = archiveNoticeRegistry(root, policy, catalog, true); components.push(...registry.components, ...catalogRegistry.components);
  return { components, inputPaths: [...new Set(['tools/licenses/runtime-policy.json', 'tools/licenses/runtimes.mjs', ...requiredInputs, 'internal/installer/installer.go', 'internal/installer/pecl_legal.go', 'internal/installer/supplemental_legal.go', ...supplementals.map(n => n.path), ...registry.inputPaths, ...catalogRegistry.inputPaths])], runtimeReview: { policyPath: 'tools/licenses/runtime-policy.json', reviewedAt: policy.reviewedAt, scope: policy.scope, retention: policy.retention, catalogSourceCount: catalog.length, peclBuildCount: pecl.length, warnings: policy.warnings, publications: publicationRecords(policy, catalog), peclNoticeRegistry: registry.metadata, catalogNoticeRegistry:catalogRegistry.metadata } };
}
