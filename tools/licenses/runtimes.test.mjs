import assert from 'node:assert/strict';
import { existsSync, readFileSync, mkdtempSync, mkdirSync, writeFileSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { test } from 'node:test';
import { assemble, identifyLicense, sha256 } from './lib.mjs';

const root = fileURLToPath(new URL('../../', import.meta.url));
const policy = () => JSON.parse(readFileSync(join(root, 'tools/licenses/runtime-policy.json')));
async function collector() {
  assert.ok(existsSync(new URL('./runtimes.mjs', import.meta.url)), 'Reviewed runtime collector must exist');
  return (await import('./runtimes.mjs')).collectRuntimePolicy;
}
function fixture(t, includePeclRegistry = true, includeCatalogRegistry = true) {
  const dir = mkdtempSync(join(tmpdir(), 'laraenv-runtime-policy-'));
  t.after(() => rmSync(dir, { recursive: true, force: true }));
  const paths = [...policy().inputs.map(i => i.path), 'internal/installer/installer.go', 'internal/installer/pecl_legal.go', 'internal/installer/supplemental_legal.go', ...['pgvector-LICENSE', 'redis-license', 'redis-COPYING', 'redis-jemalloc-COPYING', 'redis-lua-COPYRIGHT', 'redis-hiredis-COPYING'].map(n => `internal/installer/licenses/${n}.txt`)];
  const registry = policy().peclNoticeRegistry;
  if (includePeclRegistry && registry) {
    const manifest = JSON.parse(readFileSync(join(root, registry.path)));
    paths.push(registry.path, 'internal/installer/pecl_supplemental_legal.go', ...manifest.bundles.flatMap(bundle => bundle.notices.map(notice => `internal/installer/pecl_notices/${notice.path}`)));
  }
  const catalogRegistry = policy().catalogNoticeRegistry;
  if (includeCatalogRegistry && catalogRegistry) {
    const manifest = JSON.parse(readFileSync(join(root, catalogRegistry.path)));
    paths.push(catalogRegistry.path, 'internal/installer/catalog_supplemental_legal.go', 'internal/installer/pecl_supplemental_legal.go', ...manifest.bundles.flatMap(bundle => bundle.notices.map(notice => `internal/installer/runtime_notices/${notice.path}`)));
  }
  for (const path of paths) { mkdirSync(dirname(join(dir, path)), { recursive: true }); writeFileSync(join(dir, path), readFileSync(join(root, path))); }
  return dir;
}

function catalogRegistryFixture(t) {
  const dir = fixture(t, true, false), p = policy();
  const bytes = Buffer.from('Exact Redis module copyright\r\nFull permission/disclaimer fixture\r\n');
  const registry = { schemaVersion: 1, bundles: [{ id: 'redis-5.0.14.1-additional-20261021', kind: 'redis', version: '5.0.14.1', archiveUrl: 'https://github.com/tporadowski/redis/releases/download/v5.0.14.1/Redis-x64-5.0.14.1.zip', archiveSha256: '018ea18a35876383cbb5f4cd0258adfc87747cf9d619bce1cf73a2e36f720ccf', scope: 'supplemental-notices-not-complete-binary-clearance', notices: [{ path: 'redis-module-NOTICE.txt', sha256: sha256(bytes), bytes: bytes.length, sourceUrl: 'https://example.org/source/redis-module.c', licenseDescription: 'Complete source notice; not blanket clearance' }] }] };
  const registryDir = join(dir, 'internal/installer/runtime_notices'); mkdirSync(registryDir, { recursive: true });
  writeFileSync(join(registryDir, registry.bundles[0].notices[0].path), bytes);
  writeFileSync(join(dir, 'internal/installer/catalog_supplemental_legal.go'), '// inert fixture retention helper\n');
  function save() { const text = JSON.stringify(registry, null, 2) + '\n'; writeFileSync(join(registryDir, 'manifest.json'), text); p.catalogNoticeRegistry = { path: 'internal/installer/runtime_notices/manifest.json', sha256: sha256(Buffer.from(text)) }; }
  save(); return { dir, p, bytes, registry, registryDir, save };
}

test('catalog notice registry preserves exact Redis scope without changing existing PECL bundles', async t => {
  const {dir,p,bytes} = catalogRegistryFixture(t), result = (await collector())(dir,p);
  const notices = result.components.filter(c=>c.ecosystem==='embedded-catalog-notice');
  assert.equal(notices.length,1);
  assert.equal(notices[0].runtimeScope.kind,'redis');
  assert.equal(notices[0].runtimeScope.archiveSha256,'018ea18a35876383cbb5f4cd0258adfc87747cf9d619bce1cf73a2e36f720ccf');
  assert.ok(notices[0].evidence[0].bytes.equals(bytes));
  assert.equal(notices[0].declaredLicense,null);
  assert.ok(result.inputPaths.includes('internal/installer/catalog_supplemental_legal.go'));
  assert.equal(result.runtimeReview.catalogNoticeRegistry.bundles.length,1);
  const outputs=assemble({components:result.components,runtimeReview:result.runtimeReview});
  assert.ok(outputs.get(`third_party/licenses/${sha256(bytes)}.txt`).equals(bytes));
  assert.match(outputs.get('THIRD_PARTY_NOTICES.txt'),/Exact archive:.*018ea18a/);
});

test('catalog notice registry rejects drift, invented scope, unsafe aliases and unlisted files', async t => {
  const collect=await collector();
  for(const mutate of [
    f=>{f.registry.bundles[0].kind='php';f.save();},
    f=>{f.registry.bundles[0].version='99.0';f.save();},
    f=>{f.registry.bundles[0].archiveUrl='https://example.org/unreviewed.zip';f.save();},
    f=>{f.registry.bundles[0].extension='redis';f.save();},
    f=>{f.registry.bundles[0].notices[0].path='../outside.txt';f.save();},
    f=>{writeFileSync(join(f.registryDir,'redis-module-NOTICE.txt'),'tampered');},
    f=>{writeFileSync(join(f.registryDir,'extra.txt'),'unlisted');},
    f=>{f.p.catalogNoticeRegistry.sha256='0'.repeat(64);},
  ]) {const f=catalogRegistryFixture(t);mutate(f);assert.throws(()=>collect(f.dir,f.p),/catalog.*registry|notice.*hash|coverage|registry.*file/i);}
});

function registryFixture(t) {
  const dir = fixture(t, false), p = policy();
  const bytes = Buffer.from('Companion attribution\r\nwith heterogeneous terms.\r\n');
  const registry = { schemaVersion: 1, bundles: [{ id: 'mongodb-1.20.1-php8.4', extension: 'mongodb', version: '1.20.1', phpSeries: '8.4', archiveUrl: 'https://windows.php.net/downloads/pecl/releases/mongodb/1.20.1/php_mongodb-1.20.1-8.4-nts-vs17-x64.zip', archiveSha256: '1'.repeat(64), scope: 'supplemental-notices-not-complete-binary-clearance', notices: [{ path: 'mongodb-companion-NOTICE.txt', sha256: sha256(bytes), bytes: bytes.length, sourceUrl: 'https://example.org/versioned/NOTICE', licenseDescription: 'Mixed project and companion terms; not a single SPDX grant' }] }] };
  const registryDir = join(dir, 'internal/installer/pecl_notices'); mkdirSync(registryDir, { recursive: true });
  writeFileSync(join(registryDir, registry.bundles[0].notices[0].path), bytes);
  writeFileSync(join(dir, 'internal/installer/pecl_supplemental_legal.go'), '// inert fixture retention helper\n');
  function save() { const text = JSON.stringify(registry, null, 2) + '\n'; writeFileSync(join(registryDir, 'manifest.json'), text); p.peclNoticeRegistry = { path: 'internal/installer/pecl_notices/manifest.json', sha256: sha256(Buffer.from(text)) }; }
  save(); return { dir, p, bytes, registry, registryDir, save };
}

function publication() {
  const prefix = 'https://github.com/thayronarrais/laraenv/releases/download/pgvector-0.8.6/';
  const rows = [
    ['18', 'f1a3f2adabcdd9c5cb99a338cd2cce94f471c14af0d4d6a80bb0bf25c8591fba', 159619, 662100821, '2ccd5521e2850ab49fcdb3c6610e011c7b7d67bb8cc3a521dd6603353e0c4f7e', '2fd7e061d1d741335318cb42253e03564186015c88c3c5631d7b046f6c208b74', 662100827],
    ['17', '28e08226af4417c3a77da1df6f72a4314ea98c832db069a0cd3d9e1c8d11b99b', 159825, 662100825, 'ff18a856d0e047609822bf228161595328548b65df5806970661864a594021ac', 'a93d6aaab96868fe45e04312de14dfb01f060f1652e9d9818a0315c1d933c7b7', 662100826],
    ['16', '3ba886dcda520a1998d13db6afcfb503e892df1b3da68943526fea1c1e64bb34', 159711, 662100822, 'd3d09bca2941b90bfd3a41937074ea79f1abedbe9bc3dc551eeede6ed177d4b3', '0ac4344c28f04dcb42dc825a94c41b25cb547ebc2b8d6601aec0076a1c0be2e6', 662100854],
    ['15', '690933153c14d49e570db4773fba94517361e878b70f89c43bde554a6a05d0b1', 159701, 662100858, '50cda184ea71b5401fc48e962bbf9876dda52d56ab9a5466010d69a38f04102b', '509fb37df661a6738ce5b8f89e3b29c832ebbea609906d10207ee8054d4e63af', 662100853],
  ];
  return { id: 'pgvector-0.8.6-license-repair-20261021', repository: 'thayronarrais/laraenv', releaseId: 371369603, tag: 'pgvector-0.8.6', releaseUrl: 'https://github.com/thayronarrais/laraenv/releases/tag/pgvector-0.8.6', verifiedAt: '2026-10-21T10:15:00Z', verificationMethod: 'anonymous-public-download-and-api-digest', license: { path: 'share/doc/pgvector/LICENSE', sha256: '6bba9ebeb73e27477463b05e5ef1bf303bccbddb3db9bbc95905d351604d6a87', bytes: 1104 }, artifacts: rows.map(([pg, sha256, bytes, assetId, originalSha256, checksumSha256, checksumId]) => { const name = `pgvector-0.8.6-pg${pg}-win-x64.zip`; return { version: `0.8.6-pg${pg}`, name, url: prefix + name, sha256, bytes, assetId, originalSha256, originalEntriesPreserved: 43, checksum: { name: name + '.sha256', url: prefix + name + '.sha256', sha256: checksumSha256, bytes: 98, assetId: checksumId } }; }), evidencePath: 'build/verification/runtime-publication-20261021/pgvector-review/public-verification.json', limits: ['Notice packaging repair; source/binary correspondence remains unverified.'] };
}

test('reviewed runtime collection covers every catalog, PECL build and platform prerequisite without clearing them', async () => {
  const result = (await collector())(root, policy());
  assert.equal(result.components.filter(c => c.ecosystem === 'optional-download').length, 40);
  assert.equal(result.components.filter(c => c.ecosystem === 'optional-pecl').length, 41);
  assert.equal(result.components.filter(c => c.ecosystem === 'external-prerequisite').length, 2);
  const runtime = result.components.find(c => c.id === 'optional-download:redis@5.0.14.1');
  assert.equal(runtime.declaredLicense, null);
  assert.match(runtime.reviewedSoftwareLicense, /BSD-3-Clause.*LGPL-2.1-or-later.*CC0-1.0/);
  assert.equal(runtime.redistributionStatus, 'requires-review');
  assert.ok(runtime.requiredActions.length > 0);
  const imagick = result.components.find(c => c.id === 'optional-pecl:imagick@3.7.0?phpSeries=8.3');
  assert.match(imagick.reviewedSoftwareLicense, /ImageMagick AND companion-library terms/);
  assert.equal(imagick.downloadEvidenceStatus, 'exact-archive-reviewed');
  assert.ok(runtime.binaryTerms.some(term=>term.includes('reverse engineer')));
  assert.ok(imagick.binaryTerms.some(term=>term.includes('FLIF')&&term.includes('LGPL-3.0')));
  assert.equal(result.components.find(c => c.id === 'optional-pecl:imagick@3.8.1?phpSeries=8.5').downloadEvidenceStatus, 'no-exact-archive-review');
  assert.equal(new Set(result.components.map(c => c.id)).size, result.components.length);
  assert.equal(result.runtimeReview.warnings.length, 7);
  assert.ok(result.runtimeReview.warnings.some(w => w.id === 'pgvector-published-license-omission'));
});

test('reviewed source hashes tolerate CRLF conversion but reject changed source content', async t => {
  const dir = fixture(t), collect = await collector();
  const path = join(dir, 'internal/installer/catalog.go');
  const text = readFileSync(path, 'utf8').replaceAll('\r\n', '\n');
  writeFileSync(path, text.replaceAll('\n', '\r\n'));
  assert.equal(collect(dir, policy()).components.filter(c => c.ecosystem === 'optional-download').length, 40);
  writeFileSync(path, text + '// changed source\n');
  assert.throws(() => collect(dir, policy()), /reviewed runtime input hash changed.*catalog/i);
});

test('missing, duplicate and invented catalog review rows cannot silently lose coverage', async () => {
  const collect = await collector();
  for (const change of [p => p.catalogCoverage.sources.pop(), p => p.catalogCoverage.sources.push(p.catalogCoverage.sources[0]), p => { p.catalogCoverage.sources[0].url = 'https://example.invalid/unreviewed.zip'; }, p => { p.components.find(c => c.id === 'nginx').sources.pop(); }]) {
    const p = policy(); change(p); assert.throws(() => collect(root, p), /catalog|coverage|duplicate/i);
  }
});

test('missing, duplicate and invented PECL review rows cannot silently lose PHP-series coverage', async () => {
  const collect = await collector();
  for (const change of [p => p.peclCoverage.builds.pop(), p => p.peclCoverage.builds.push(p.peclCoverage.builds[0]), p => { p.peclCoverage.builds[0].phpSeries = '99.0'; }, p => { p.components.find(c => c.id === 'pecl-xdebug').builds.pop(); }]) {
    const p = policy(); change(p); assert.throws(() => collect(root, p), /PECL|coverage|duplicate/i);
  }
});

test('missing prerequisite reviews stop generation', async () => {
  const collect = await collector();
  const missing = policy(); missing.components = missing.components.filter(c => c.id !== 'microsoft-webview2-runtime');
  assert.throws(() => collect(root, missing), /platform.*coverage|component.*coverage/i);
});

test('invented component versions stop generation', async () => {
  const collect = await collector(), invented = policy(); invented.components.find(c => c.id === 'pecl-xdebug').versions.push('99.0.0');
  assert.throws(() => collect(root, invented), /component.*version.*coverage/i);
});

test('required embedded notices carry exact original bytes, pinned sources and fixed licenses', async () => {
  const result = (await collector())(root, policy());
  const notices = result.components.filter(c => c.ecosystem === 'embedded-runtime-notice');
  assert.equal(notices.length, 6);
  const expected = new Map([['pgvector-LICENSE.txt', 'PostgreSQL'], ['redis-license.txt', 'BSD-3-Clause'], ['redis-COPYING.txt', 'BSD-3-Clause'], ['redis-jemalloc-COPYING.txt', 'BSD-2-Clause'], ['redis-lua-COPYRIGHT.txt', 'MIT'], ['redis-hiredis-COPYING.txt', 'BSD-3-Clause']]);
  for (const component of notices) {
    assert.equal(component.required, true);
    assert.equal(component.declaredLicense, expected.get(component.name));
    assert.equal(component.source.sha256, sha256(component.evidence[0].bytes));
    assert.ok(Buffer.from(component.evidence[0].bytes).equals(readFileSync(join(root, component.source.resourcePath))));
    assert.match(component.source.url, /^https:\/\/raw\.githubusercontent\.com\//);
    assert.equal(component.evidenceStatus, 'collected-source-notice-not-binary-clearance');
  }
  assert.deepEqual(notices.find(c => c.name === 'pgvector-LICENSE.txt').runtimeScope, { kind: 'pgvector', versions: ['0.8.6-pg15', '0.8.6-pg16', '0.8.6-pg17', '0.8.6-pg18'] });
  const outputs = assemble({ components: notices });
  for (const component of notices) assert.ok(outputs.get(`third_party/licenses/${component.source.sha256}.txt`).equals(component.evidence[0].bytes));
});

test('PostgreSQL license identification accepts the exact uppercase upstream grant', () => {
  assert.equal(identifyLicense(readFileSync(join(root, 'internal/installer/licenses/pgvector-LICENSE.txt'))), 'PostgreSQL');
});

test('supplemental byte drift and broader unreviewed Go retention scopes stop collection', async t => {
  const collect = await collector(), dir = fixture(t);
  const notice = join(dir, 'internal/installer/licenses/redis-license.txt');
  const original = readFileSync(notice), lf = original.toString('utf8').replaceAll('\r\n', '\n');
  writeFileSync(notice, original.includes(13) ? lf : lf.replaceAll('\n', '\r\n'));
  assert.throws(() => collect(dir, policy()), /supplemental.*hash/i);
  const p = policy(); p.supplementalNotices[0].runtimeScope.versions.push('99.0.0-pg99');
  assert.throws(() => collect(root, p), /supplemental.*scope|retention.*scope/i);
});

test('runtime inputs include policy, retention sources and every embedded source notice', async () => {
  const result = (await collector())(root, policy());
  for (const path of ['tools/licenses/runtime-policy.json', 'tools/licenses/runtimes.mjs', 'internal/installer/supplemental_legal.go', 'internal/installer/pecl_legal.go', 'internal/installer/installer.go', 'internal/vcruntime/vcruntime.go', 'internal/webviewruntime/signature_windows.go', 'internal/installer/licenses/redis-lua-COPYRIGHT.txt']) assert.ok(result.inputPaths.includes(path), path);
});

test('aggregate and custom inventory retain reviewed obligations and historical warnings', async () => {
  const result = (await collector())(root, policy());
  const outputs = assemble({ components: result.components, runtimeReview: result.runtimeReview });
  const inventory = JSON.parse(outputs.get('third_party/dependencies.json'));
  assert.equal(inventory.runtimeReview.warnings.length, 7);
  assert.equal(inventory.runtimeReview.scope.automaticBundlingClearance, false);
  const text = outputs.get('THIRD_PARTY_NOTICES.txt');
  assert.match(text, /pgvector-published-license-omission/);
  assert.match(text, /already published assets/);
  assert.match(text, /Reviewed software license.*companion-library terms/);
  assert.match(text, /Required action:/);
  assert.ok(!outputs.get('third_party/dependencies.json').includes(root));
});

test('publication and warning dispositions survive offline rendering without losing historical findings', async t => {
  const p = policy(), record = publication(); p.publications = [record];
  const warning = p.warnings.find(w => w.id === 'pgvector-published-license-omission');
  warning.status = 'resolved-by-publication';
  warning.resolution = { summary: 'Public ZIP notice repaired; binary provenance remains separate.', evidenceReference: record.id };
  const result = (await collector())(fixture(t), p);
  const outputs = assemble({ components: result.components, runtimeReview: result.runtimeReview });
  const inventory = JSON.parse(outputs.get('third_party/dependencies.json'));
  assert.deepEqual(inventory.runtimeReview.publications, [record]);
  assert.equal(inventory.runtimeReview.warnings.length, 7);
  assert.equal(inventory.runtimeReview.warnings.find(w => w.id === warning.id).reason, warning.reason);
  const text = outputs.get('THIRD_PARTY_NOTICES.txt');
  assert.match(text, /Status: resolved-by-publication/);
  assert.match(text, /Public ZIP notice repaired; binary provenance remains separate\./);
  assert.ok(text.includes(record.artifacts[0].sha256));
  assert.ok(text.includes(record.artifacts[0].checksum.sha256));
  assert.ok(text.includes(record.artifacts[0].originalSha256));
  assert.ok(text.includes(record.releaseUrl));
  assert.ok(text.includes(record.license.sha256));
  assert.ok(text.includes(record.limits[0]));
});

test('older policies can omit publication and warning disposition fields', async () => {
  const p = policy(); delete p.publications;
  for (const warning of p.warnings) { delete warning.status; delete warning.resolution; }
  const result = (await collector())(root, p);
  assert.deepEqual(result.runtimeReview.publications, []);
  assert.equal(result.runtimeReview.warnings.length, 7);
  assert.match(assemble({ components: [], runtimeReview: result.runtimeReview }).get('THIRD_PARTY_NOTICES.txt'), /Status: not recorded/);
});

test('publication identity, archive coverage, hashes and license tampering stop collection', async () => {
  const collect = await collector();
  const mutations = [
    r => { r.repository = 'thayronarrais/laraenv-builder'; },
    r => { r.releaseId = 1; },
    r => { r.tag = 'latest'; },
    r => { r.artifacts.pop(); },
    r => { r.artifacts[1] = structuredClone(r.artifacts[0]); },
    r => { r.artifacts[0].sha256 = 'invalid'; },
    r => { r.artifacts[0].originalSha256 = 'invalid'; },
    r => { r.license.sha256 = '0'.repeat(64); },
    r => { r.license.bytes = 1; },
    r => { r.artifacts[0].originalEntriesPreserved = 42; },
  ];
  for (const mutate of mutations) {
    const p = policy(), record = publication(); mutate(record); p.publications = [record];
    assert.throws(() => collect(root, p), /publication/i);
  }
});

test('publication aliases and inconsistent sidecar identities cannot masquerade as catalog artifacts', async () => {
  const collect = await collector();
  const mutations = [
    r => { r.releaseUrl += '?alias=true'; },
    r => { r.artifacts[0].url += '?alias=true'; },
    r => { r.artifacts[0].name = r.artifacts[0].name.toUpperCase(); },
    r => { r.artifacts[0].checksum.name = r.artifacts[1].checksum.name; },
    r => { r.artifacts[0].checksum.url += '/'; },
    r => { r.artifacts[0].checksum.sha256 = '0'.repeat(64); },
    r => { r.artifacts[0].checksum.assetId = r.artifacts[0].assetId; },
    r => { r.evidencePath = 'C:/private/proof.json'; },
  ];
  for (const mutate of mutations) {
    const p = policy(), record = publication(); mutate(record); p.publications = [record];
    assert.throws(() => collect(root, p), /publication/i);
  }
});

test('optional PECL registry preserves exact heterogeneous notice bytes and archive-bound scope', async t => {
  const { dir, p, bytes, registry } = registryFixture(t);
  const result = (await collector())(dir, p);
  const notices = result.components.filter(c => c.ecosystem === 'embedded-pecl-notice');
  assert.equal(notices.length, 1);
  assert.equal(notices[0].declaredLicense, null);
  assert.equal(notices[0].required, false);
  assert.equal(notices[0].runtimeScope.archiveSha256, '1'.repeat(64));
  assert.equal(notices[0].reviewedSoftwareLicense, 'Mixed project and companion terms; not a single SPDX grant');
  const output = assemble({ components: notices, runtimeReview: result.runtimeReview });
  assert.ok(output.get(`third_party/licenses/${sha256(bytes)}.txt`).equals(bytes));
  assert.ok(output.get('THIRD_PARTY_NOTICES.txt').includes('1'.repeat(64)));
  assert.ok(output.get('THIRD_PARTY_NOTICES.txt').includes(registry.bundles[0].archiveUrl));
  const inventory = JSON.parse(output.get('third_party/dependencies.json'));
  assert.deepEqual(inventory.runtimeReview.peclNoticeRegistry.bundles, registry.bundles);
  for (const path of ['internal/installer/pecl_notices/manifest.json', 'internal/installer/pecl_notices/mongodb-companion-NOTICE.txt', 'internal/installer/pecl_supplemental_legal.go']) assert.ok(result.inputPaths.includes(path));
});

test('PECL registry hash and notice byte drift stop collection', async t => {
  const collect = await collector(), fixture = registryFixture(t);
  fixture.p.peclNoticeRegistry.sha256 = '0'.repeat(64);
  assert.throws(() => collect(fixture.dir, fixture.p), /PECL.*registry.*hash/i);
  fixture.save();
  writeFileSync(join(fixture.registryDir, 'mongodb-companion-NOTICE.txt'), fixture.bytes.toString().replaceAll('\r\n', '\n'));
  assert.throws(() => collect(fixture.dir, fixture.p), /PECL.*notice.*hash|PECL.*notice.*bytes/i);
});

test('PECL registry rejects missing or unlisted files instead of silently dropping notices', async t => {
  const collect = await collector(), extra = registryFixture(t), missing = registryFixture(t);
  writeFileSync(join(extra.registryDir, 'unlisted.txt'), 'unlisted');
  assert.throws(() => collect(extra.dir, extra.p), /PECL.*file.*coverage|PECL.*registry.*file/i);
  rmSync(join(missing.registryDir, 'mongodb-companion-NOTICE.txt'));
  assert.throws(() => collect(missing.dir, missing.p), /PECL.*file.*coverage|PECL.*registry.*file/i);
});

test('PECL registry rejects duplicate identities, path aliases and unreviewed archive scope', async t => {
  const collect = await collector();
  const mutations = [
    r => { r.bundles.push(structuredClone(r.bundles[0])); },
    r => { r.bundles[0].notices.push(structuredClone(r.bundles[0].notices[0])); },
    r => { r.bundles[0].phpSeries = '99.0'; },
    r => { r.bundles[0].archiveUrl += '?alias=true'; },
    r => { r.bundles[0].archiveSha256 = 'invalid'; },
    r => { r.bundles[0].scope = 'cleared'; },
    r => { r.bundles[0].notices[0].path = '../outside.txt'; },
    r => { r.bundles[0].notices[0].sourceUrl = 'http://example.org/NOTICE'; },
  ];
  for (const mutate of mutations) {
    const fixture = registryFixture(t); mutate(fixture.registry); fixture.save();
    assert.throws(() => collect(fixture.dir, fixture.p), /PECL.*registry|PECL.*notice/i);
  }
});
