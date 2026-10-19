# Local HTTPS across runtimes

These runtime-trust improvements are included in LaraEnv 0.5.7.

LaraEnv keeps the existing local CA. A partial, corrupt, or mismatched CA
certificate/key pair now produces a recovery error instead of generating a
different CA silently. Restore the matching original files; deleting a trusted
CA is not an automatic repair. This change does not rotate certificates or
change the installed/portable Windows trust policy.

## PHP

PHP's `extras/ssl/cacert.pem` retains its existing roots. LaraEnv appends its
public local CA once, configures empty/default `curl.cainfo` and
`openssl.cafile` directives, and preserves active custom CA paths. A small
valid custom bundle is accepted; size alone no longer triggers replacement.
Invalid existing bundles are preserved and reported. Only a missing bundle
uses the existing Mozilla bundle download flow.

The bundle is refreshed during startup, PHP installation, and local certificate
creation. A known generated path is rebased when the environment moves, while
external/custom paths remain intact. If you explicitly use another trust
bundle, add the public LaraEnv CA to that bundle yourself. Never add the CA's
private key. Restart existing processes after changing runtime trust.

## Node and npm

New local terminals, agent terminals, managed development servers, queue
workers and cron processes receive the public CA certificate path through
`NODE_EXTRA_CA_CERTS`. An existing variable is preserved, including a variable
with different casing or an intentionally empty value. The parent process and
Windows trust store are not modified by environment preparation. Settings
passed explicitly by an application or npm's own CA configuration remain
authoritative. Remote SSH shells are outside this integration.

## Laravel Vite

For an HTTPS-enabled Laravel checkout served directly by PHP, LaraEnv can
provide `VITE_DEV_SERVER_KEY` and `VITE_DEV_SERVER_CERT` to a newly launched
process. It checks that the installed Laravel Vite plugin supports those
variables and that the existing leaf certificate/key are usable for the
checkout's hostname. It does not edit `vite.config`, `.env`, or project source.

Explicit Vite certificate variables in the inherited environment or project
environment files take priority. A conflicting `APP_URL`, unsupported or absent
plugin, absent certificate, paused project, or a checkout without an HTTPS
route prevents automatic Vite configuration. Custom environment modes are
checked conservatively because a process environment would otherwise override
their configured TLS values. If `APP_URL` is entirely absent, only the child
receives the local HTTPS URL.

Projects using LaraEnv's HTTP reverse-proxy upstream retain their current Vite
configuration. Enabling HTTPS on that upstream requires coordinated proxy/HMR
configuration; exporting certificate variables alone would break forwarding.
The Node public-CA environment still applies to those projects.

## CA name constraints

Existing and newly created CAs retain their current DNS scope in this change.
Project TLDs and editable preview aliases are configurable, so restricting new
CAs to a fixed suffix such as `.test` would break supported custom hostnames.
Changing a trusted CA requires an explicit design covering allowed names,
certificate regeneration, Windows trust replacement, and rollback. No silent
migration or speculative restriction is performed here.

## Verification scope

Regression tests use generated temporary CAs, a loopback TLS server and a real
Node client. The PHP-generated trust bundle is used to verify a real local TLS
handshake through Go's certificate verifier. Windows `certutil` calls in CA
tests are stubbed; tests do not install a Windows root, contact a public site,
or modify installed user runtimes. Full browser HMR and external project
configurations remain manual integration checks.
