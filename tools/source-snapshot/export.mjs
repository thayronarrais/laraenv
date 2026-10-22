import { readFileSync, writeFileSync, mkdirSync, existsSync, lstatSync, readdirSync, chmodSync } from 'node:fs';
import { dirname, resolve, join, parse, extname } from 'node:path';
import { execFileSync } from 'node:child_process';
import { createHash } from 'node:crypto';
import { fileURLToPath } from 'node:url';

const digest = bytes => createHash('sha256').update(bytes).digest('hex');
const json = value => Buffer.from(JSON.stringify(value, null, 2) + '\n');
const git = (root, args, input) => execFileSync('git', ['--no-replace-objects', '-C', root, ...args], { input, windowsHide: true, env: { ...process.env, GIT_NO_LAZY_FETCH: '1' }, maxBuffer: 256 * 1024 * 1024, stdio: ['pipe', 'pipe', 'pipe'] });
const manifestName = 'SOURCE_SNAPSHOT_MANIFEST.json';
const archiveName = 'LaraEnv-desktop-source.zip';

export function validateSourcePaths(paths) {
  const names = new Set(), directories = new Map();
  for (const path of paths) {
    if (!path || path.startsWith('/') || /[<>"|?*\\:\x00-\x1f]/.test(path) || path.split('/').some(part => !part || part === '.' || part === '..' || /[. ]$/.test(part) || /^(con|prn|aux|nul|com[1-9]|lpt[1-9])(?:\.|$)/i.test(part))) throw Error('Unsafe source path: ' + path);
    const alias = path.toLowerCase();
    if (names.has(alias) || directories.has(alias)) throw Error('Source path alias: ' + path);
    names.add(alias);
    const parts = path.split('/');
    for (let index = 1; index < parts.length; index++) {
      const directory = parts.slice(0,index).join('/'), key = directory.toLowerCase();
      if (names.has(key) || (directories.has(key) && directories.get(key) !== directory)) throw Error('Source directory alias: ' + path);
      directories.set(key,directory);
    }
  }
}
function rejectLinks(path) {
  const absolute = resolve(path); let current = parse(absolute).root;
  for (const part of absolute.slice(current.length).split(/[\\/]/).filter(Boolean)) {
    current = join(current, part);
    try { if (lstatSync(current).isSymbolicLink()) throw Error('Link in output path'); }
    catch (error) { if (error.code !== 'ENOENT') throw error; }
  }
}
function disposition(path, policy) {
  if (path.split('/').some(part => /^(\.git|\.aws|\.codex|\.agents|\.claude|\.superpowers|\.vite|node_modules|laravel-server|backend)$/i.test(part)) || /^(out|output|prototypes)\//i.test(path) || /^build\/(bin|cache|archive|verification|source|release)\//i.test(path) || /(^|\/)\.env(?:\.|$)|\.local\.json$/i.test(path)) return 'Private, generated or operational material';
  const exclusion = policy.exclude.files.find(row => row.path === path) || policy.exclude.prefixes.find(row => path.startsWith(row.path));
  if (exclusion) return exclusion.reason;
  if (policy.include.files.includes(path) || policy.include.prefixes.some(prefix => path.startsWith(prefix)) || (!path.includes('/') && policy.include.rootExtensions.includes(extname(path)))) return null;
  throw Error('Unclassified tracked path: ' + path);
}
function credentialFindings(path, bytes) {
  const patterns = [/^-----BEGIN (?:[A-Z0-9 ]+ )?PRIVATE KEY-----/m, /\bgh[pousr]_[A-Za-z0-9]{36,}\b/, /\bgithub_pat_[A-Za-z0-9_]{60,}\b/, /\bAKIA[A-Z0-9]{16}\b/, /\bglpat-[A-Za-z0-9_-]{20,}\b/, /\bsk-proj-[A-Za-z0-9_-]{40,}\b/];
  const text = bytes.toString('utf8');
  for (const pattern of patterns) {
    const match = pattern.exec(text);
    if (match) throw Error(`Credential finding: ${path}:${text.slice(0, match.index).split('\n').length} (value redacted)`);
  }
}
function indexFiles(root) {
  const rows = git(root, ['ls-files', '--stage', '-z']).toString('utf8').split('\0').filter(Boolean).map(line => {
    const match = /^(\d+) ([a-f0-9]+) (\d)\t([\s\S]+)$/.exec(line);
    if (!match || match[3] !== '0' || !['100644', '100755'].includes(match[1])) throw Error('Unsupported index mode, link or conflict');
    return { path: match[4], mode: match[1], oid: match[2] };
  });
  validateSourcePaths(rows.map(row => row.path)); return rows.sort((a,b) => a.path < b.path ? -1 : 1);
}
function blobs(root, rows) {
  const output = git(root, ['cat-file', '--batch'], rows.map(row => row.oid).join('\n') + '\n');
  let offset = 0; const result = new Map();
  for (const row of rows) {
    const newline = output.indexOf(10, offset), header = output.subarray(offset, newline).toString('ascii');
    const match = /^([a-f0-9]+) blob (\d+)$/.exec(header);
    if (!match || match[1] !== row.oid) throw Error('Unexpected indexed blob');
    const size = Number(match[2]); offset = newline + 1;
    if (!Number.isSafeInteger(size) || offset + size >= output.length || output[offset + size] !== 10) throw Error('Truncated indexed blob');
    result.set(row.path, output.subarray(offset, offset + size)); offset += size + 1;
  }
  if (offset !== output.length) throw Error('Unexpected indexed data');
  return result;
}
const crcTable = Array.from({ length: 256 }, (_, index) => { let value = index; for (let bit = 0; bit < 8; bit++) value = value & 1 ? 0xedb88320 ^ (value >>> 1) : value >>> 1; return value >>> 0; });
function crc32(bytes) { let value = 0xffffffff; for (const byte of bytes) value = crcTable[(value ^ byte) & 255] ^ (value >>> 8); return (value ^ 0xffffffff) >>> 0; }
// Stored ZIP entries avoid dependency/install scripts. Fixed DOS date and modes
// make the artifact deterministic. ZIP64 is deliberately refused.
function zip(entries) {
  if (entries.length > 65535) throw Error('ZIP64 is unsupported');
  const locals = [], central = []; let offset = 0;
  for (const entry of entries) {
    const name = Buffer.from(entry.path), bytes = entry.bytes, crc = crc32(bytes);
    if (name.length > 65535 || bytes.length > 0xffffffff || offset > 0xffffffff) throw Error('ZIP64 is unsupported');
    const local = Buffer.alloc(30); local.writeUInt32LE(0x04034b50); local.writeUInt16LE(20,4); local.writeUInt16LE(0x800,6); local.writeUInt16LE(0x21,12); local.writeUInt32LE(crc,14); local.writeUInt32LE(bytes.length,18); local.writeUInt32LE(bytes.length,22); local.writeUInt16LE(name.length,26);
    const directory = Buffer.alloc(46); directory.writeUInt32LE(0x02014b50); directory.writeUInt16LE(0x314,4); directory.writeUInt16LE(20,6); directory.writeUInt16LE(0x800,8); directory.writeUInt16LE(0x21,14); directory.writeUInt32LE(crc,16); directory.writeUInt32LE(bytes.length,20); directory.writeUInt32LE(bytes.length,24); directory.writeUInt16LE(name.length,28); directory.writeUInt32LE(((entry.mode === '100755' ? 0o100755 : 0o100644) << 16) >>> 0,38); directory.writeUInt32LE(offset,42);
    locals.push(local,name,bytes); central.push(directory,name); offset += local.length + name.length + bytes.length;
  }
  const directories = Buffer.concat(central), end = Buffer.alloc(22);
  if (offset + directories.length + 22 > 0xffffffff) throw Error('ZIP64 is unsupported');
  end.writeUInt32LE(0x06054b50); end.writeUInt16LE(entries.length,8); end.writeUInt16LE(entries.length,10); end.writeUInt32LE(directories.length,12); end.writeUInt32LE(offset,16);
  return Buffer.concat([...locals,directories,end]);
}
export function exportSnapshot({ repoRoot, outputDir, policy }) {
  repoRoot = resolve(repoRoot); outputDir = resolve(outputDir); rejectLinks(outputDir);
  if (existsSync(outputDir)) throw Error('Output already exists; choose a new directory');
  if (policy?.schemaVersion !== 1 || !Array.isArray(policy.include?.files) || !Array.isArray(policy.include?.prefixes) || !Array.isArray(policy.include?.rootExtensions) || !Array.isArray(policy.exclude?.files) || !Array.isArray(policy.exclude?.prefixes)) throw Error('Invalid source policy');
  const rows = indexFiles(repoRoot), included = [], excluded = [];
  for (const row of rows) { const reason = disposition(row.path,policy); if (reason) excluded.push({path:row.path,reason}); else included.push(row); }
  if (!included.length) throw Error('Empty source snapshot');
  const data = blobs(repoRoot,included);
  const files = included.map(row => { const bytes = data.get(row.path); credentialFindings(row.path,bytes); return { path:row.path, mode:row.mode, bytes:bytes.length, sha256:digest(bytes) }; });
  validateSourcePaths([...files.map(file=>file.path),manifestName]);
  const manifest = { schemaVersion:1, project:'LaraEnv desktop', policyFingerprint:digest(json(policy)), files };
  const manifestBytes = json(manifest);
  const entries = [...files.map(file => ({...file,bytes:data.get(file.path)})),{path:manifestName,mode:'100644',bytes:manifestBytes}].sort((a,b)=>a.path<b.path?-1:1);
  const archiveBytes = zip(entries), archiveSha256 = digest(archiveBytes);
  mkdirSync(outputDir,{recursive:true});
  for (const entry of entries) { const target = join(outputDir,'source',entry.path); mkdirSync(dirname(target),{recursive:true}); writeFileSync(target,entry.bytes,{flag:'wx'}); if (process.platform !== 'win32') chmodSync(target,entry.mode==='100755'?0o755:0o644); }
  const archivePath = join(outputDir,archiveName);
  writeFileSync(archivePath,archiveBytes,{flag:'wx'}); writeFileSync(archivePath+'.sha256',`${archiveSha256}  ${archiveName}\n`,{flag:'wx'});
  return { files,excluded,archivePath,archiveSha256,archiveBytes:archiveBytes.length,manifestSha256:digest(manifestBytes) };
}
export function verifySnapshot(outputDir) {
  outputDir = resolve(outputDir); rejectLinks(outputDir);
  const source = join(outputDir,'source'); rejectLinks(source); rejectLinks(join(source,manifestName));
  const manifestBytes = readFileSync(join(source,manifestName));
  const manifest = JSON.parse(manifestBytes); if (manifest.schemaVersion !== 1 || !Array.isArray(manifest.files)) throw Error('Invalid snapshot manifest');
  validateSourcePaths(manifest.files.map(file=>file.path));
  const actual = [];
  function walk(directory,prefix='') { for (const file of readdirSync(directory,{withFileTypes:true})) { if (file.isSymbolicLink()) throw Error('Link in source snapshot'); const path=prefix+file.name; if(file.isDirectory())walk(join(directory,file.name),path+'/');else actual.push(path); } }
  walk(source);
  const expected = [manifestName,...manifest.files.map(file=>file.path)].sort();
  if (JSON.stringify(actual.sort()) !== JSON.stringify(expected)) throw Error('Snapshot has missing or extra files');
  const entries = manifest.files.map(file=> { const bytes=readFileSync(join(source,file.path));if(bytes.length!==file.bytes||digest(bytes)!==file.sha256)throw Error('Snapshot hash/bytes changed: '+file.path); return {...file,bytes}; });
  entries.push({path:manifestName,mode:'100644',bytes:manifestBytes});entries.sort((a,b)=>a.path<b.path?-1:1);
  const archive=readFileSync(join(outputDir,archiveName)), hash=digest(archive);
  if (!archive.equals(zip(entries)) || readFileSync(join(outputDir,archiveName+'.sha256'),'utf8')!==`${hash}  ${archiveName}\n`) throw Error('Snapshot ZIP/hash changed');
  return {files:manifest.files.length,archiveSha256:hash};
}
if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  try {
    const args=process.argv.slice(2), option=name=>{const index=args.indexOf(name);return index<0?undefined:args[index+1];};
    if (args[0]==='verify'&&args.length===2) console.log(JSON.stringify(verifySnapshot(args[1])));
    else if(args[0]==='export'&&args.length===3&&args[1]==='--output') {
      const root=fileURLToPath(new URL('../../',import.meta.url)), path='tools/source-snapshot/policy.json';
      const policy=JSON.parse(git(root,['show',':'+path]));
      const result=exportSnapshot({repoRoot:root,outputDir:resolve(option('--output')),policy});
      console.log(JSON.stringify({files:result.files.length,excluded:result.excluded.length,archivePath:result.archivePath,archiveSha256:result.archiveSha256}));
    } else throw Error('Usage: node tools/source-snapshot/export.mjs export --output <new-directory> | verify <directory>');
  } catch(error) { console.error(error.message);process.exitCode=1; }
}
