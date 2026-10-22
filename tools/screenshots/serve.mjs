import http from "node:http";
import { createReadStream } from "node:fs";
import { stat } from "node:fs/promises";
import path from "node:path";

const TYPES = {
  ".html": "text/html; charset=utf-8",
  ".js": "text/javascript; charset=utf-8",
  ".mjs": "text/javascript; charset=utf-8",
  ".css": "text/css; charset=utf-8",
  ".png": "image/png",
  ".svg": "image/svg+xml",
  ".woff": "font/woff",
  ".woff2": "font/woff2",
  ".json": "application/json; charset=utf-8",
};

// Read-only, loopback-only, ephemeral port. It exists so the app bundle is
// loaded over http rather than file:// (module scripts need an origin).
export function startServer(rootDir) {
  const root = path.resolve(rootDir);

  const server = http.createServer(async (req, res) => {
    try {
      const rel = decodeURIComponent(new URL(req.url, "http://localhost").pathname);

      // Chrome requests this on its own and the Wails bundle has no favicon,
      // so a 404 here is browser noise. Answering 204 keeps it out of the
      // console without having to soften the capture's error check.
      if (rel === "/favicon.ico") { res.writeHead(204).end(); return; }

      const target = path.resolve(root, "." + (rel === "/" ? "/index.html" : rel));

      // path.resolve collapses "..", so anything outside root is rejected here.
      if (target !== root && !target.startsWith(root + path.sep)) {
        res.writeHead(403).end("forbidden");
        return;
      }

      const info = await stat(target);
      if (!info.isFile()) { res.writeHead(404).end("not found"); return; }

      res.writeHead(200, {
        "content-type": TYPES[path.extname(target).toLowerCase()] || "application/octet-stream",
        "content-length": info.size,
        "cache-control": "no-store",
      });
      createReadStream(target).pipe(res);
    } catch {
      res.writeHead(404).end("not found");
    }
  });

  return new Promise((resolve) => {
    server.listen(0, "127.0.0.1", () => {
      const { port } = server.address();
      resolve({
        url: `http://127.0.0.1:${port}`,
        close: () => new Promise((done) => server.close(done)),
      });
    });
  });
}
