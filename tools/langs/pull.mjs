// Optional maintainer helper to fetch translated catalogues from canonical
// public main. en.json and the runtime-fragment manifest are deliberately
// excluded so a pull does not overwrite locally authored source changes.
//
// This must never block a release: the public repo may not have langs/ yet
// (404), GitHub may be unreachable, a request may be rate-limited, or a 200
// may carry a non-JSON body (captive portal, proxy interstitial, GitHub
// error page). Any of those produce a warning and exit 0, leaving the
// committed copies in place. The one thing that does get rejected is a
// corrupt per-file download — a locale file that fails to parse as JSON is
// skipped rather than overwriting a good file.
//
// Note: every "stop here, exit 0" path below returns from main() instead of
// calling process.exit(). On Windows, Node (undici's fetch keeps a libuv
// async handle open) can crash with a UV_HANDLE_CLOSING assertion if
// process.exit() is called right after a fetch — letting the script fall off
// the end naturally avoids that and still yields exit code 0.
import { writeFileSync } from "node:fs";

const API = "https://api.github.com/repos/thayronarrais/laraenv/contents/langs?ref=main";

async function main() {
  let res;
  try {
    res = await fetch(API, { headers: { Accept: "application/vnd.github+json" } });
  } catch (e) {
    // Network failure, DNS error, unreachable GitHub, etc. A maintainer
    // cutting a release on a plane must still succeed, using the committed
    // copies.
    console.warn(`warning: could not reach the public repo (${e.message})`);
    console.warn("using the committed copies; the release is not blocked by this.");
    return;
  }
  if (!res.ok) {
    console.warn(`warning: could not list langs/ (${res.status} ${res.statusText})`);
    console.warn("using the committed copies; the release is not blocked by this.");
    return;
  }
  const listingBody = await res.text();
  let entries;
  try {
    entries = JSON.parse(listingBody);
  } catch (e) {
    // A 200 with a non-JSON body — a captive portal, a proxy interstitial,
    // a GitHub error page — is exactly the "bad hotel wifi" case this script
    // must survive. Warn and fall back to the committed copies.
    console.warn(`warning: could not parse the langs/ listing, not valid JSON — ${e.message}`);
    console.warn("using the committed copies; the release is not blocked by this.");
    return;
  }
  let pulled = 0;
  for (const entry of entries) {
    // en.json is authored here (see the file header); .do-not-translate.json
    // is a shared manifest, not a translation — both are excluded so this
    // never overwrites either with a "pulled" copy.
    if (!entry.name.endsWith(".json") || entry.name === "en.json" || entry.name === ".do-not-translate.json") continue;
    let file;
    try {
      file = await fetch(entry.download_url);
    } catch (e) {
      console.warn(`warning: could not fetch ${entry.name} (${e.message})`);
      continue;
    }
    if (!file.ok) {
      console.warn(`warning: could not fetch ${entry.name} (${file.status})`);
      continue;
    }
    const body = await file.text();
    try {
      JSON.parse(body);
    } catch (e) {
      console.warn(`warning: ${entry.name} from public main is not valid JSON, skipping — ${e.message}`);
      continue;
    }
    writeFileSync(`langs/${entry.name}`, body);
    pulled++;
  }
  console.log(`pulled ${pulled} translation file(s) from the public repo.`);
}

await main();
