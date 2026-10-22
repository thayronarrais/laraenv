import assert from 'node:assert/strict';
import { test } from 'node:test';
import { deflateRawSync } from 'node:zlib';
import { parsePhar, crc32 } from './composer.mjs';
const u32 = x => { const b = Buffer.alloc(4); b.writeUInt32LE(x); return b; };
function phar(name = 'LICENSE', contents = Buffer.from('license'), compressed = false) {
  const payload = compressed ? deflateRawSync(contents) : contents;
  const n = Buffer.from(name); const entry = Buffer.concat([u32(n.length), n, u32(contents.length), u32(123), u32(payload.length), u32(crc32(contents)), u32(compressed ? 0x1000 : 0), u32(0)]);
  const header = Buffer.concat([u32(1), Buffer.from([0x11, 0]), u32(0), u32(0), u32(0), entry]);
  return Buffer.concat([Buffer.from('<?php __HALT_COMPILER(); ?>\r\n'), u32(header.length), header, payload]);
}
test('static PHAR parsing reads compressed license bytes without executing PHP', () => {
  assert.equal(parsePhar(phar('vendor/a/LICENSE', Buffer.from('exact\r\n'), true)).get('vendor/a/LICENSE').toString(), 'exact\r\n');
});
test('static PHAR parsing rejects CRC corruption and unsafe paths', () => {
  const bytes = phar(); bytes[bytes.length - 1] ^= 1;
  assert.throws(() => parsePhar(bytes), /CRC/i);
  assert.throws(() => parsePhar(phar('../LICENSE')), /path/i);
});
test('static PHAR parsing rejects truncated manifests and payloads', () => {
  assert.throws(() => parsePhar(phar().subarray(0, 42)), /bounds|truncated/i);
  assert.throws(() => parsePhar(phar().subarray(0, -1)), /bounds|truncated/i);
});
