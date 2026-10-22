import { createHash } from 'node:crypto';
import { existsSync, readFileSync, readdirSync, lstatSync, mkdirSync, writeFileSync, rmSync } from 'node:fs';
import { dirname, join, relative, resolve, sep } from 'node:path';

export const sha256 = bytes => createHash('sha256').update(bytes).digest('hex');
export const portable = value => value.replaceAll('\\', '/');
export const legalName = name => /^(?:licen[cs]e|copying|notice|patents|authors|third[-_]?party(?:notices)?)(?:$|[._-])/i.test(name);
const identifiers = new Set(['MIT', 'MIT-0', 'Apache-2.0', 'ISC', 'BSD-2-Clause', 'BSD-3-Clause', '0BSD', 'OFL-1.1', 'CC-BY-4.0', 'CC0-1.0', 'BlueOak-1.0.0', 'MPL-2.0', 'MS-RL', 'Unicode-3.0', 'Unicode-DFS-2016', 'PSF-2.0', 'PostgreSQL', 'PHP-3.01', 'GPL-2.0-only', 'GPL-2.0-or-later', 'LGPL-2.1-or-later', 'AGPL-3.0-only']);
export function validateSpdx(value) {
  if (typeof value !== 'string' || !value.trim()) throw new Error('Missing SPDX declaration');
  const tokens = value.match(/[A-Za-z0-9.+-]+|[()]/g) || [];
  if (tokens.join('') !== value.replace(/\s/g, '')) throw new Error(`Invalid SPDX expression: ${value}`);
  let at = 0;
  function primary() { const t = tokens[at++]; if (t === '(') { expression(); if (tokens[at++] !== ')') throw new Error(`Invalid SPDX expression: ${value}`); } else if (!identifiers.has(t)) throw new Error(`Unknown SPDX identifier: ${t}`); }
  function expression() { primary(); while (tokens[at] === 'AND' || tokens[at] === 'OR') { at++; primary(); } }
  expression(); if (at !== tokens.length) throw new Error(`Invalid SPDX expression: ${value}`); return true;
}
export function identifyLicense(bytes) {
  const t = Buffer.from(bytes).toString('utf8');
  if (/Apache License[\s\S]*Version 2\.0[\s\S]*END OF TERMS AND CONDITIONS/i.test(t)) return 'Apache-2.0';
  if (/Microsoft Reciprocal License \(MS-RL\)/i.test(t) && /Reciprocal Grants/i.test(t) && /Conditions and Limitations/i.test(t) && /The software is licensed "as-is\.?"/i.test(t)) return 'MS-RL';
  if (/Mozilla Public License[\s\S]*(?:Version|ver\.) 2\.0/i.test(t) && /Definitions/i.test(t) && /Termination/i.test(t)) return 'MPL-2.0';
  if (/SIL OPEN FONT LICENSE Version 1\.1/i.test(t) && /TERMINATION/i.test(t)) return 'OFL-1.1';
  if (/Creative Commons Attribution 4\.0 International Public License/i.test(t) && /Section 8/i.test(t)) return 'CC-BY-4.0';
  if (/Blue Oak Model License/i.test(t) && /1\.0\.0/.test(t) && /Disclaimer/i.test(t)) return 'BlueOak-1.0.0';
  if (/CC0 1\.0 Universal/i.test(t) && /Waiver/i.test(t)) return 'CC0-1.0';
  if (/UNICODE LICENSE V3/i.test(t) && /Permission is hereby granted/i.test(t) && /AS IS/i.test(t)) return 'Unicode-3.0';
  if (/PostgreSQL Global Development Group/i.test(t) && /Permission to use, copy, modify, and distribute/i.test(t) && /without a written agreement/i.test(t) && /IN NO EVENT SHALL THE UNIVERSITY OF CALIFORNIA/i.test(t) && /ON AN "AS IS" BASIS/i.test(t)) return 'PostgreSQL';
  if (/Permission to use, copy, modify,? and\/or distribute/i.test(t) && /AS IS/i.test(t)) return 'ISC';
  if (/Redistribution and use in source and binary forms/i.test(t) && /AS IS/i.test(t)) return /Neither the name|names of its contributors/i.test(t) ? 'BSD-3-Clause' : 'BSD-2-Clause';
  if (/Permission is hereby granted, free of charge/i.test(t) && /THE SOFTWARE IS PROVIDED/i.test(t)) return /MIT No Attribution|MIT-0/i.test(t) ? 'MIT-0' : 'MIT';
  if (/Permission to use, copy, modify, and distribute/i.test(t) && /Unicode/i.test(t) && /AS IS/i.test(t)) return /UNICODE LICENSE V3/i.test(t) ? 'Unicode-3.0' : 'Unicode-DFS-2016';
  return null;
}
export function collectNotices(dir, prefix = '') {
  if (!dir || !existsSync(dir)) return [];
  const result = [];
  for (const entry of readdirSync(dir, { withFileTypes: true }).sort((a, b) => a.name < b.name ? -1 : a.name > b.name ? 1 : 0)) {
    if (entry.isSymbolicLink()) throw new Error(`Symlink in license evidence tree: ${prefix}${entry.name}`);
    if (entry.name === 'node_modules' || entry.name === '.git') continue;
    const sourcePath = prefix + entry.name;
    if (entry.isDirectory()) result.push(...collectNotices(join(dir, entry.name), sourcePath + '/'));
    else if (entry.isFile() && legalName(entry.name)) result.push({ sourcePath, bytes: readFileSync(join(dir, entry.name)) });
  }
  return result;
}
export function assemble({ inputs = [], components = [], build = {}, runtimeReview }) {
  const output = new Map(); const rendered = [];
  for (const component of [...components].sort((a, b) => a.id < b.id ? -1 : a.id > b.id ? 1 : 0)) {
    if (component.declaredLicense) validateSpdx(component.declaredLicense);
    const evidence = [...(component.evidence || [])].sort((a, b) => a.sourcePath < b.sourcePath ? -1 : a.sourcePath > b.sourcePath ? 1 : 0);
    if (component.required && !evidence.some(e => identifyLicense(e.bytes))) throw new Error(`Missing complete license evidence for ${component.id}`);
    const licenses = evidence.map(e => {
      const bytes = Buffer.from(e.bytes); const digest = sha256(bytes); const outputPath = `third_party/licenses/${digest}.txt`;
      output.set(outputPath, bytes);
      return { sourcePath: e.sourcePath, sha256: digest, bytes: bytes.length, outputPath };
    });
    const { evidence: _evidence, ...metadata } = component;
    rendered.push({ ...metadata, scopes: [...component.scopes].sort(), licenses });
  }
  const inventory = { schemaVersion: 1, firstParty: { name: 'LaraEnv', license: 'Apache-2.0', scope: 'Original desktop code; third-party components retain their own licenses and notices.' }, build, ...(runtimeReview ? { runtimeReview } : {}), inputs: [...inputs].sort((a, b) => a.path < b.path ? -1 : a.path > b.path ? 1 : 0), components: rendered };
  output.set('third_party/dependencies.json', JSON.stringify(inventory, null, 2) + '\n');
  let notice = 'LaraEnv — Third-party licenses and notices\n\nOriginal LaraEnv desktop code is licensed under Apache-2.0 (see LICENSE).\nThird-party code, fonts, data, and tools retain their own licenses.\nThis inventory distinguishes collected evidence from metadata-only dependencies;\ncollection does not replace component-specific license obligations.\n\n';
  if (runtimeReview) {
    notice += `Runtime review: ${runtimeReview.policyPath} (${runtimeReview.reviewedAt})\nOptional runtime/platform reviews do not grant automatic bundling or mirroring clearance.\n`;
    for (const warning of runtimeReview.warnings) {
      notice += `Runtime warning ${warning.id} [${warning.severity}]: ${warning.reason}\nStatus: ${warning.status || 'not recorded'}\nRequired action: ${warning.requiredAction}\n`;
      if (warning.resolution?.summary) notice += `Resolution: ${warning.resolution.summary}\n`;
      if (warning.resolution?.evidenceReference) notice += `Resolution evidence: ${warning.resolution.evidenceReference}\n`;
    }
    for (const publication of runtimeReview.publications || []) {
      notice += `\nRuntime publication: ${publication.id}\nRelease: ${publication.releaseUrl}\nVerified: ${publication.verifiedAt} (${publication.verificationMethod})\nLicense: ${publication.license.path} — SHA-256 ${publication.license.sha256}, ${publication.license.bytes} bytes\n`;
      for (const artifact of publication.artifacts) notice += `Published archive: ${artifact.name} — SHA-256 ${artifact.sha256}, ${artifact.bytes} bytes\nURL: ${artifact.url}\nOriginal archive SHA-256: ${artifact.originalSha256}; ${artifact.originalEntriesPreserved} original entries preserved\nChecksum: ${artifact.checksum.name} — SHA-256 ${artifact.checksum.sha256}\nChecksum URL: ${artifact.checksum.url}\n`;
      notice += `Publication evidence reference: ${publication.evidencePath}\n`;
      for (const limit of publication.limits) notice += `Publication evidence limit: ${limit}\n`;
      notice += 'Offline validation checks record consistency; it does not authenticate the record or establish source/binary correspondence.\n';
    }
    notice += '\n';
  }
  for (const component of rendered) {
    notice += `${component.name} ${component.version}\n${component.id}\nScopes: ${component.scopes.join(', ')}\nDeclared license: ${component.declaredLicense || 'not declared'}\nEvidence: ${component.evidenceStatus}\n`;
    if (component.reviewedSoftwareLicense) notice += `Reviewed software license (descriptive; not blanket clearance): ${component.reviewedSoftwareLicense}\nRedistribution status: ${component.redistributionStatus}\n`;
    if (component.runtimeScope?.archiveSha256) notice += component.runtimeScope.kind ? `Exact archive: ${component.runtimeScope.kind} ${component.runtimeScope.version} (SHA-256 ${component.runtimeScope.archiveSha256})\nArchive URL: ${component.runtimeScope.archiveUrl}\nScope limit: ${component.runtimeScope.scope}\n` : `Exact PECL archive scope: ${component.runtimeScope.extension} ${component.runtimeScope.version}, PHP ${component.runtimeScope.phpSeries}\nArchive URL: ${component.runtimeScope.archiveUrl}\nArchive SHA-256: ${component.runtimeScope.archiveSha256}\nScope limit: ${component.runtimeScope.scope}\n`;
    if (component.source?.sourceEvidence) notice += `Source notice evidence: ${JSON.stringify(component.source.sourceEvidence)}\n`;
    for (const url of component.reviewedSoftwareLicenseSources || []) notice += `Reviewed license source: ${url}\n`;
    if (component.downloadEvidenceStatus) notice += `This download's archive evidence: ${component.downloadEvidenceStatus}\n`;
    for (const term of component.binaryTerms || []) notice += `Binary terms / evidence limit: ${term}\n`;
    for (const action of component.requiredActions || []) notice += `Required action: ${action}\n`;
    for (const note of component.reviewNotes || []) notice += `Review scope: ${note}\n`;
    if (component.source?.url) notice += `Source: ${component.source.url}\n`;
    if (component.source?.correspondingSource) notice += `Corresponding source: ${component.source.correspondingSource}\n`;
    if (component.source?.resourcePath) notice += `Resource: ${component.source.resourcePath}\n`;
    if (component.source?.archivePath) notice += `Accompanying source archive: ${component.source.archivePath} (SHA-256 ${component.source.archiveSha256})\n`;
    if (component.note) notice += `Attribution / scope: ${component.note}\n`;
    if (component.reviewedException) notice += `Reviewed evidence exception: ${component.reviewedException}\n`;
    for (const file of component.licenses) notice += `  ${file.sourcePath} -> ${file.outputPath}\n`;
    notice += '\n';
  }
  for (const [file, bytes] of [...output].filter(([p]) => p.startsWith('third_party/licenses/')).sort(([a], [b]) => a < b ? -1 : a > b ? 1 : 0)) notice += `\n===== ${file} =====\n${bytes.toString('utf8')}\n`;
  output.set('THIRD_PARTY_NOTICES.txt', notice.replace(/\r\n|\r/g, '\n'));
  return new Map([...output].sort(([a], [b]) => a < b ? -1 : a > b ? 1 : 0));
}
function generatedFiles(root) {
  const found = [];
  function walk(dir) { if (!existsSync(dir)) return; if (lstatSync(dir).isSymbolicLink()) throw new Error('Symlink in generated output'); for (const entry of readdirSync(dir, { withFileTypes: true })) { const file = join(dir, entry.name); if (entry.isSymbolicLink()) throw new Error('Symlink in generated output'); if (entry.isDirectory()) walk(file); else found.push(portable(relative(root, file))); } }
  walk(join(root, 'third_party')); if (existsSync(join(root, 'THIRD_PARTY_NOTICES.txt'))) found.push('THIRD_PARTY_NOTICES.txt'); return found;
}
export function syncOutputs(root, outputs, { check = false } = {}) {
  root = resolve(root); const mismatches = [];
  // lstat also sees dangling links. Check every target and ancestor before
  // reading, creating, replacing, or removing any generated file.
  for (const file of outputs.keys()) {
    let current = resolve(root, file);
    if (!current.startsWith(root + sep)) throw new Error('Unsafe generated output path');
    while (current === root || current.startsWith(root + sep)) {
      try { if (lstatSync(current).isSymbolicLink()) throw new Error(`Symlink in generated output: ${file}`); }
      catch (error) { if (error.code !== 'ENOENT') throw error; }
      if (current === root) break;
      current = dirname(current);
    }
  }
  for (const [file, bytes] of outputs) {
    const destination = resolve(root, file);
    if (!destination.startsWith(root + sep) || !(file === 'THIRD_PARTY_NOTICES.txt' || file.startsWith('third_party/'))) throw new Error('Unsafe generated output path');
    if (!existsSync(destination)) mismatches.push(`missing generated ${file}`);
    else if (lstatSync(destination).isSymbolicLink() || !readFileSync(destination).equals(Buffer.from(bytes))) mismatches.push(`stale generated ${file}`);
  }
  const extras = generatedFiles(root).filter(file => !outputs.has(file));
  mismatches.push(...extras.map(file => `extra generated ${file}`));
  if (check) { if (mismatches.length) throw new Error(mismatches.join('\n')); return; }
  for (const [file, bytes] of outputs) { const destination = join(root, file); mkdirSync(dirname(destination), { recursive: true }); writeFileSync(destination, bytes); }
  for (const file of extras) rmSync(join(root, file));
}
