import assert from "node:assert/strict";
import { existsSync, mkdtempSync, mkdirSync, readFileSync, symlinkSync, writeFileSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import test from "node:test";
import { vendorLegal } from "./vendor-legal.mjs";

function fixture(t) {
  const root = mkdtempSync(join(tmpdir(), "laraenv-legal-"));
  t.after(() => rmSync(root, { recursive: true, force: true }));
  mkdirSync(join(root, "third_party"));
  for (const file of ["LICENSE", "NOTICE", "THIRD_PARTY_NOTICES.txt"]) writeFileSync(join(root, file), file + "\n{plain text}<script>\n");
  writeFileSync(join(root, "third_party/dependencies.json"), '{"components":[]}\n');
  return root;
}

test("legal assets preserve exact bytes and check detects stale contents without writing", t => {
  const repoRoot = fixture(t);
  vendorLegal({ repoRoot });
  const dest = join(repoRoot, "frontend/public/legal/LICENSE.txt");
  assert.equal(readFileSync(dest, "utf8"), readFileSync(join(repoRoot, "LICENSE"), "utf8"));
  vendorLegal({ repoRoot, check: true });
  writeFileSync(dest, "stale");
  assert.throws(() => vendorLegal({ repoRoot, check: true }), /stale/);
  assert.equal(readFileSync(dest, "utf8"), "stale");
});

test("missing evidence fails before creating a partial legal bundle", t => {
  const repoRoot = fixture(t);
  rmSync(join(repoRoot, "THIRD_PARTY_NOTICES.txt"));
  assert.throws(() => vendorLegal({ repoRoot }), /THIRD_PARTY_NOTICES/);
  assert.throws(() => readFileSync(join(repoRoot, "frontend/public/legal/LICENSE.txt")), /ENOENT/);
});

test("unexpected output files are rejected rather than recursively removed", t => {
  const repoRoot = fixture(t);
  vendorLegal({ repoRoot });
  const extra = join(repoRoot, "frontend/public/legal/operator.txt");
  writeFileSync(extra, "preserve");
  assert.throws(() => vendorLegal({ repoRoot }), /Unexpected/);
  assert.equal(readFileSync(extra, "utf8"), "preserve");
});

test("dangling output links cannot redirect legal writes", t => {
  const repoRoot = fixture(t);
  vendorLegal({ repoRoot });
  const destination = join(repoRoot, "frontend/public/legal/LICENSE.txt");
  const outside = join(repoRoot, "must-not-be-created.txt");
  rmSync(destination);
  symlinkSync(outside, destination, "file");
  assert.throws(() => vendorLegal({ repoRoot }), /symbolic link/);
  assert.equal(existsSync(outside), false);
});
