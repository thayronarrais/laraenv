import { inflateRawSync } from 'node:zlib';

export function crc32(bytes) {
  let crc = 0xffffffff;
  for (const byte of bytes) { crc ^= byte; for (let bit = 0; bit < 8; bit++) crc = (crc >>> 1) ^ ((crc & 1) ? 0xedb88320 : 0); }
  return (crc ^ 0xffffffff) >>> 0;
}
// This reads the PHAR container format only. It never evaluates PHP or PHP metadata.
export function parsePhar(input) {
  const bytes = Buffer.from(input); const marker = Buffer.from('__HALT_COMPILER();');
  let at = bytes.indexOf(marker); if (at < 0) throw new Error('PHAR halt marker missing'); at += marker.length;
  while ([32, 9].includes(bytes[at])) at++;
  if (bytes.subarray(at, at + 2).toString() === '?>') at += 2;
  if (bytes[at] === 13) at++;
  if (bytes[at] === 10) at++;
  function take(size) { if (!Number.isSafeInteger(size) || size < 0 || at + size > bytes.length) throw new Error('PHAR truncated or out of bounds'); const data = bytes.subarray(at, at + size); at += size; return data; }
  const u32 = () => take(4).readUInt32LE();
  const manifestSize = u32(); const manifestEnd = at + manifestSize;
  if (manifestEnd > bytes.length) throw new Error('PHAR manifest out of bounds');
  const count = u32(); if (count > 100000) throw new Error('PHAR entry count out of bounds');
  take(2); take(4); take(u32()); take(u32());
  const entries = []; const names = new Set(); let expandedSize = 0;
  for (let index = 0; index < count; index++) {
    const name = take(u32()).toString('utf8');
    if (!name || name.includes('\\') || name.startsWith('/') || name.split('/').some(p => p === '..' || p === '.') || /^[a-z]:/i.test(name) || names.has(name)) throw new Error(`Unsafe or duplicate PHAR path: ${name}`);
    names.add(name); const size = u32(); take(4); const packedSize = u32(); const crc = u32(); const flags = u32(); take(u32());
    expandedSize += size; if (expandedSize > 128 * 1024 * 1024) throw new Error('PHAR expanded size exceeds audit limit');
    entries.push({ name, size, packedSize, crc, flags });
    if (at > manifestEnd) throw new Error('PHAR manifest entry out of bounds');
  }
  if (at !== manifestEnd) throw new Error('PHAR manifest boundary mismatch');
  const result = new Map();
  for (const entry of entries) {
    const packed = take(entry.packedSize); const compression = entry.flags & 0xf000;
    if (compression !== 0 && compression !== 0x1000) throw new Error(`Unsupported PHAR compression: ${entry.name}`);
    const contents = compression ? inflateRawSync(packed, { maxOutputLength: entry.size + 1 }) : packed;
    if (contents.length !== entry.size) throw new Error(`PHAR size mismatch: ${entry.name}`);
    if (crc32(contents) !== entry.crc) throw new Error(`PHAR CRC mismatch: ${entry.name}`);
    result.set(entry.name, contents);
  }
  return result;
}
