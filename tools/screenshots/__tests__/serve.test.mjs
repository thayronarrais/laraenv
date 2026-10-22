import { describe, it, expect, beforeAll, afterAll } from "vitest";
import { mkdtemp, writeFile, mkdir } from "node:fs/promises";
import { tmpdir } from "node:os";
import path from "node:path";
import { startServer } from "../serve.mjs";

let root, server;

beforeAll(async () => {
  root = await mkdtemp(path.join(tmpdir(), "shots-"));
  await writeFile(path.join(root, "index.html"), "<h1>hi</h1>");
  await mkdir(path.join(root, "assets"));
  await writeFile(path.join(root, "assets", "app.js"), "export const x = 1;");
  server = await startServer(root);
});

afterAll(async () => { await server.close(); });

describe("startServer", () => {
  it("serves index.html at the root path", async () => {
    const res = await fetch(`${server.url}/`);
    expect(res.status).toBe(200);
    expect(await res.text()).toContain("hi");
  });

  it("serves JavaScript with a module-compatible content type", async () => {
    const res = await fetch(`${server.url}/assets/app.js`);
    expect(res.status).toBe(200);
    expect(res.headers.get("content-type")).toContain("javascript");
  });

  it("404s a file that does not exist", async () => {
    const res = await fetch(`${server.url}/nope.png`);
    expect(res.status).toBe(404);
  });

  it("answers favicon.ico with 204 so the browser logs no error", async () => {
    const res = await fetch(`${server.url}/favicon.ico`);
    expect(res.status).toBe(204);
  });

  it("refuses to escape the root directory", async () => {
    const res = await fetch(`${server.url}/../../secret.txt`);
    expect([403, 404]).toContain(res.status);
  });
});
