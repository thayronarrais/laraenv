import { AGENT_MAP_STATE, AGENT_MAP_SNAPSHOT } from "./agentActivity.mjs";

// A scenario is data. `ready` is a string that only appears on screen if the
// binding resolved, React painted and the right page is showing — that is the
// capture's readiness proof, never a timer.
//
// Readiness strings must match the DOM, not the rendered pixels: the hero
// tagline below is uppercased by CSS, so the string here is the lowercase
// source text (frontend/src/pages/Dashboard.tsx:280).
//
// Every value is fictional. The banners these produce are published on a
// public, indexed page, and the ones they replace were leaking real client
// names, local paths and server addresses.

// Shapes come from frontend/wailsjs/go/models.ts — a field name that does not
// match renders an empty row rather than throwing, so they are copied, not
// guessed.
const PROJECTS = [
  { name: "acme-shop", url: "https://acme-shop.test", path: "C:\\sites\\acme-shop",
    kind: "laravel", docRoot: "C:\\sites\\acme-shop\\public", phpVersion: "8.4",
    nodeVersion: "22.11.0", ssl: true, jsFramework: "vite", packageManager: "npm",
    proxyPort: 0, tags: [] },
  { name: "atlas-crm", url: "https://atlas-crm.test", path: "C:\\sites\\atlas-crm",
    kind: "laravel", docRoot: "C:\\sites\\atlas-crm\\public", phpVersion: "7.4",
    nodeVersion: "22.11.0", ssl: true, jsFramework: "vue", packageManager: "yarn",
    proxyPort: 0, tags: [] },
  { name: "beacon-api", url: "https://beacon-api.test", path: "C:\\sites\\beacon-api",
    kind: "public", docRoot: "C:\\sites\\beacon-api\\public", phpVersion: "8.4",
    nodeVersion: "22.11.0", ssl: true, jsFramework: "react", packageManager: "npm",
    proxyPort: 3001, tags: ["platform", "clients"] },
  { name: "harbor-docs", url: "https://harbor-docs.test", path: "C:\\sites\\harbor-docs",
    kind: "plain", docRoot: "C:\\sites\\harbor-docs", phpVersion: "8.1",
    nodeVersion: "22.11.0", ssl: true, jsFramework: "", packageManager: "",
    proxyPort: 0, tags: ["platform", "clients"] },
  { name: "lumen-billing", url: "https://lumen-billing.test", path: "C:\\sites\\lumen-billing",
    kind: "public", docRoot: "C:\\sites\\lumen-billing\\public", phpVersion: "8.4",
    nodeVersion: "24.15.0", ssl: true, jsFramework: "next.js", packageManager: "npm",
    proxyPort: 3000, tags: ["platform", "clients"] },
  { name: "orbit-landing", url: "https://orbit-landing.test", path: "C:\\sites\\orbit-landing",
    kind: "laravel", docRoot: "C:\\sites\\orbit-landing\\public", phpVersion: "8.3",
    nodeVersion: "22.11.0", ssl: true, jsFramework: "vite", packageManager: "npm",
    proxyPort: 0, tags: [] },
  { name: "sable-portal", url: "https://sable-portal.test", path: "C:\\sites\\sable-portal",
    kind: "plain", docRoot: "C:\\sites\\sable-portal", phpVersion: "7.4",
    nodeVersion: "22.11.0", ssl: true, jsFramework: "", packageManager: "",
    proxyPort: 0, tags: ["clients"] },
].map((project, index) => ({
  ...project, paused: false,
  tags: index === 0 ? ['Acme'] : index === 1 ? ['Atlas'] : index % 2 ? ['Harbor'] : ['Acme', 'Platform'],
  phpVersion: index === 0 ? '' : project.phpVersion,
  nodeVersion: index === 0 ? '' : project.nodeVersion,
  phpRuntime: { version: index === 0 ? '8.4' : project.phpVersion, fullVersion: index === 0 ? '8.4.12' : project.phpVersion,
    source: index === 0 ? 'composer.json' : 'manual', constraint: index === 0 ? '^8.2' : '', status: 'resolved' },
  nodeRuntime: { version: project.nodeVersion, fullVersion: project.nodeVersion,
    source: index === 0 ? '.nvmrc' : 'manual', constraint: index === 0 ? '22' : '', status: 'resolved' },
}));

for (const [index, name] of ['nova-catalog', 'quartz-admin', 'summit-booking', 'terra-store', 'valley-blog', 'wave-dashboard', 'zenith-api'].entries()) {
  PROJECTS.push({ ...PROJECTS[0], name, path: `C:\\sites\\${name}`, docRoot: `C:\\sites\\${name}\\public`, url: `https://${name}.test`,
    tags: index % 2 ? ['Harbor'] : ['Atlas'], jsFramework: index % 2 ? 'vue' : 'vite' });
}

const finding = (id, status, code, fixID = '', detail = '') => ({ id, status, code, fixID, detail,
  severity: status === 'fail' ? 'error' : status === 'warning' ? 'warning' : 'info', requiresConfirmation: false, runtimeKind: '', installVersion: '' });
const DOCTOR_REPORTS = PROJECTS.map((project, index) => ({
  project: project.name, path: project.path, kind: project.kind, healthy: index !== 1 && index !== 7,
  phpRuntime: project.phpRuntime, nodeRuntime: project.nodeRuntime,
  findings: [
    finding('document_root', 'pass', 'document_root_ok'),
    finding('php_runtime', 'pass', 'runtime_ok'),
    finding('node_runtime', 'pass', 'runtime_ok'),
    project.kind !== 'laravel' ? finding('env', 'skipped', 'not_applicable') : index === 1 ? finding('env', 'fail', 'env_missing', 'env') : finding('env', 'pass', 'env_ok'),
    index === 1 || index === 7 ? finding('composer_dependencies', 'warning', 'composer_dependencies_missing', 'composer_install') : finding('composer_dependencies', 'pass', 'composer_dependencies_ok'),
    project.kind === 'laravel' ? finding('storage_link', 'optional', 'storage_link_missing', index === 1 || index === 7 ? '' : 'storage_link') : finding('storage_link', 'skipped', 'not_applicable'),
    project.kind === 'laravel' ? finding('database', 'pass', 'database_ok') : finding('database', 'skipped', 'not_applicable'),
  ],
}));

const selectAcme = async page => {
  await page.getByRole('button', { name: 'Projects', exact: true }).click();
  await page.getByRole('button', { name: 'Select project acme-shop', exact: true }).click();
  await page.getByRole('region', { name: 'Project details · acme-shop', exact: true }).waitFor();
  // The selected-project effect closes old portal menus after the commit.
  // Let that effect finish before a follow-up scenario opens a new menu.
  await page.evaluate(() => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve))));
};
const assertVisible = async (locator, description) => {
  if (!await locator.isVisible()) throw new Error(`Capture proof missing: ${description}`);
};

const ROOT_KEY = 'a'.repeat(64);
const CONTEXT = { kind: 'project', projectName: 'acme-shop' };
const ROOT = { rootKey: ROOT_KEY, label: 'acme-shop', path: 'C:/sites/acme-shop', canonicalRoot: 'C:/sites/acme-shop', canRead: true, canWrite: true };
const REPOSITORY = { context: CONTEXT, rootKey: ROOT_KEY, repositoryKey: 'demo-repository-acme-shop' };
const BEFORE_README = '# Acme Shop\n\nA local Laravel storefront.\n\n## Development\n\n1. Install Composer dependencies.\n2. Configure the local environment.\n3. Open https://acme-shop.test.\n';
const AFTER_README = '# Acme Shop\n\nA local Laravel storefront.\n\n## Development\n\n1. Install Composer dependencies.\n2. Configure the local environment.\n3. Open https://acme-shop.test.\n\n## Checkout\n\nPercentage coupons apply before shipping.\nExpired coupons are ignored.\n\nRun the checkout feature tests:\n\n```shell\nphp artisan test --filter=PercentageCoupon\n```\n';
const snapshotEntries = [
  { id: 'readme', relativePath: 'README.md', originalPath: '', displayPath: 'README.md', indexCode: 'M', worktreeCode: 'M', kind: 'tracked', submoduleState: '', headOID: 'demo-head-readme', indexOID: 'demo-index-readme', headMode: '100644', indexMode: '100644', worktreeMode: '100644', canStage: true, canUnstage: true, disabledReason: '' },
  { id: 'checkout', relativePath: 'app/Services/CartTotals.php', originalPath: '', displayPath: 'app/Services/CartTotals.php', indexCode: '.', worktreeCode: 'M', kind: 'tracked', submoduleState: '', headOID: 'demo-cart', indexOID: 'demo-cart', headMode: '100644', indexMode: '100644', worktreeMode: '100644', canStage: true, canUnstage: false, disabledReason: '' },
  { id: 'coupon-test', relativePath: 'tests/Feature/PercentageCouponTest.php', originalPath: '', displayPath: 'tests/Feature/PercentageCouponTest.php', indexCode: '?', worktreeCode: '?', kind: 'untracked', submoduleState: '', headOID: '', indexOID: '', headMode: '', indexMode: '', worktreeMode: '100644', canStage: true, canUnstage: false, disabledReason: '' },
];
const diffSide = text => ({ text, revision: 'demo-' + text.length, oid: '', eol: 'lf', size: text.length, bom: false });
const WORKSPACE_STATE = {
  projects: PROJECTS, webgl: false,
  workspace: { version: 2, groups: [{ x: 0, y: 0, w: 900, h: 600, snap: 'max', activeTabIndex: 0, tabs: [
    { label: 'acme-shop · PowerShell', activeLeafIndex: 0, tree: { type: 'leaf', label: 'acme-shop · PowerShell', kind: 'pwsh', spawn: { kind: 'project', projectName: 'acme-shop', terminalKind: 'pwsh' } } },
  ] }] },
  storage: { 'laraenv.workspaceNavigation': { version: 1, primaryCollapsed: true, sidebarVisible: true, sidebarTab: 'git', sidebarWidth: 280 } },
  documents: { version: 1, visible: true, panelPercent: 52, activeTabIndex: 0, tabs: [{ mode: 'source', references: [{ context: CONTEXT, rootKey: ROOT_KEY, relativePath: 'README.md' }] }] },
  workspaceFiles: { project: 'acme-shop', root: ROOT, entries: ['app', 'config', 'resources', 'routes', 'tests'].map(name => ({ name, relativePath: name, kind: 'directory' })).concat(['.env.example', 'composer.json', 'package.json', 'README.md'].map(name => ({ name, relativePath: name, kind: 'file' }))),
    files: { 'README.md': { canonicalPath: 'C:/sites/acme-shop/README.md', text: AFTER_README, revision: 'demo-readme-2', size: AFTER_README.length, bom: false, eol: 'lf', readOnly: false } } },
  sourceControl: { snapshot: { repository: REPOSITORY, root: ROOT, snapshotRevision: 'demo-snapshot-1', indexRevision: 'demo-index-1', headOID: 'demo-head-1', branch: 'feature/checkout-coupons', upstream: 'origin/feature/checkout-coupons', gitOperation: '', unborn: false, detached: false, countsKnown: true, hasConflicts: false, busy: false, ahead: 1, behind: 0, entries: snapshotEntries,
    remotes: [{ id: 'demo-origin', name: 'origin', displayURL: 'https://example.com/acme-shop.git', canPush: true, disabledReason: '' }], operationId: '' },
    diffs: { 'readme:worktree': { id: 'demo-readme-diff', repository: REPOSITORY, entryId: 'readme', relativePath: 'README.md', originalPath: '', layer: 'worktree', kind: 'text', original: diffSide(BEFORE_README), modified: diffSide(AFTER_README), canOpenWorktree: true, message: '' } } },
};
const openWorkspace = async page => {
  await page.getByRole('button', { name: 'Terminal', exact: true }).click();
  await page.getByTestId('document-surface').locator('.monaco-editor textarea.inputarea').first().waitFor();
  await page.waitForFunction(() => window.__harness.hasListener('term:data:harness-pty-project-acme-shop'));
  await page.evaluate(() => window.dispatchEvent(new Event('resize')));
  await page.evaluate(() => window.__harness.emit('term:data:harness-pty-project-acme-shop',
    'PS C:\\sites\\acme-shop> php artisan test --filter=PercentageCoupon\r\n\r\n  PASS  Tests\\Feature\\PercentageCouponTest\r\n  ✓ applies percentage coupons before shipping\r\n  ✓ ignores expired coupons\r\n  ✓ never discounts below zero\r\n\r\n  Tests: 3 passed (7 assertions)\r\n  Duration: 0.38s\r\n\r\nPS C:\\sites\\acme-shop> '));
};

// The 203.0.113.0/24 block is reserved for documentation (RFC 5737), so
// nothing here can resolve to a real machine.
const SSH_HOSTS = [
  { name: "edge-01", address: "203.0.113.11", port: 22, user: "root", tags: ["prod"], authMode: "key", keyName: "id_ed25519" },
  { name: "edge-02", address: "203.0.113.12", port: 22, user: "root", tags: ["prod"], authMode: "key", keyName: "id_ed25519" },
  { name: "db-eu", address: "203.0.113.40", port: 22, user: "deploy", tags: ["prod"], authMode: "key", keyName: "id_ed25519" },
  { name: "worker-eu", address: "203.0.113.41", port: 22, user: "deploy", tags: ["prod"], authMode: "key", keyName: "id_ed25519" },
  { name: "staging", address: "203.0.113.90", port: 2222, user: "deploy", tags: ["staging"], authMode: "key", keyName: "id_ed25519" },
];

// The Apply button is disabled until a deployment has been detected
// (`detected = d.os_family != null`, Deployments.tsx:484), so os_family must
// be set for the modal scenario to be reachable at all.
const DEPLOYMENTS = [
  {
    id: 1,
    name: "dev",
    ssh_host_external_id: "edge-01",
    os_family: "ubuntu",
    os_version: "24.04",
    project_kind: "laravel",
    last_status: "applied",
    config: {
      detection: { webServer: "nginx", php: "8.3", database: "none" },
      domain: "example.com",
      repoUrl: "https://github.com/you/repo.git",
      branch: "main",
    },
    runs: [],
  },
];

// Agent workspace for the terminal banner. Session shapes follow
// AgentSessionView (frontend/src/components/agents/AgentSessionCard.tsx);
// usage follows AgentUsageView (AgentUsagePopover.tsx). Times are relative so
// the navigator reads "now" / "12m" instead of a stale date.
const minutesAgo = (m) => new Date(Date.now() - m * 60_000).toISOString();
const hoursAhead = (h) => new Date(Date.now() + h * 3_600_000).toISOString();

const AGENT_PROFILES = [
  { id: "claude", label: "Claude Code", version: "2.1.263", available: true, capabilities: { launch: true, resume: true, history: true, usage: true, additionalDirectories: true } },
  { id: "codex", label: "Codex", version: "0.156.0", available: true, capabilities: { launch: true, resume: true, history: true, usage: true, additionalDirectories: true } },
  { id: "kimi", label: "Kimi Code", version: "0.38.0", available: true, capabilities: { launch: true, resume: true, history: true, usage: false, additionalDirectories: true } },
  { id: "opencode", label: "OpenCode", version: "1.14.33", available: true, capabilities: { launch: true, resume: true, history: true, usage: false, additionalDirectories: false } },
];

const AGENT_SESSIONS = [
  { id: "s-acme-claude", projectName: "acme-shop", projectPath: "C:\\sites\\acme-shop", agentId: "claude", launchMode: "current",
    gitBranch: "main", terminalId: "harness-pty-agent-1", title: "Percentage coupons at checkout", state: "working",
    startedAt: minutesAgo(26), updatedAt: minutesAgo(0), unread: false, archived: false },
  { id: "s-lumen-codex", projectName: "lumen-billing", projectPath: "C:\\sites\\lumen-billing", agentId: "codex", launchMode: "branch",
    gitBranch: "feature/invoice-pdf", terminalId: "harness-pty-agent-2", title: "Invoice PDF export", state: "needs-input",
    startedAt: minutesAgo(41), updatedAt: minutesAgo(3), unread: true, archived: false },
  // Started by hand with `claude` inside the beacon-api shell tab: adopted, so
  // the navigator lists it and the shell tab is promoted to an agent tab.
  { id: "s-beacon-adopted", projectName: "beacon-api", projectPath: "C:\\sites\\beacon-api", agentId: "claude", launchMode: "current",
    gitBranch: "main", terminalId: "harness-pty-project-beacon-api", title: "Rate limiting middleware", state: "working",
    startedAt: minutesAgo(9), updatedAt: minutesAgo(1), unread: false, archived: false, adopted: true },
];

const AGENT_USAGE = [
  { provider: "claude", plan: "max", status: "available", readAt: minutesAgo(0), stale: false,
    windows: [{ id: "five_hour", label: "5 hour", usedPct: 34, resetsAt: hoursAhead(3) }, { id: "seven_day", label: "Weekly", usedPct: 61, resetsAt: hoursAhead(70) }] },
  { provider: "codex", plan: "plus", status: "available", readAt: minutesAgo(0), stale: false,
    windows: [{ id: "five_hour", label: "5 hour", usedPct: 12, resetsAt: hoursAhead(4) }, { id: "seven_day", label: "Weekly", usedPct: 48, resetsAt: hoursAhead(120) }] },
  { provider: "kimi", status: "unsupported", message: "Kimi quota unavailable", readAt: minutesAgo(0), stale: false, windows: null },
  { provider: "opencode", status: "unsupported", message: "OpenCode quota unavailable", readAt: minutesAgo(0), stale: false, windows: null },
];

// Persisted layout (frontend/src/components/terminal/serialize.ts, version 2):
// one maximized group with two agent tabs and a project shell. Labels for agent
// leaves are recomputed on restore; the shell label is kept as written.
const agentLeaf = (projectName, agentId, laraSessionId) => ({
  type: "leaf", label: `${projectName} · agent`, kind: `agent:${agentId}`,
  spawn: { kind: "agent", projectName, agentId, laraSessionId },
});
const TERMINAL_WORKSPACE = {
  version: 2,
  groups: [{
    x: 40, y: 40, w: 900, h: 560, snap: "max", activeTabIndex: 0,
    tabs: [
      { label: "acme-shop · Claude Code", activeLeafIndex: 0, tree: agentLeaf("acme-shop", "claude", "s-acme-claude") },
      { label: "lumen-billing · Codex", activeLeafIndex: 0, tree: agentLeaf("lumen-billing", "codex", "s-lumen-codex") },
      { label: "beacon-api · powershell", activeLeafIndex: 0, tree: { type: "leaf", label: "beacon-api · powershell", kind: "powershell",
        spawn: { kind: "project", projectName: "beacon-api", terminalKind: "powershell" } } },
    ],
  }],
};

// What the active agent pane shows. Written through the same "term:data:<id>"
// event the backend uses, so it goes through xterm like real output. Every
// path and result is fictional.
const ESC = "\x1b[";
const dim = (s) => `${ESC}2m${s}${ESC}0m`;
const bold = (s) => `${ESC}1m${s}${ESC}0m`;
const green = (s) => `${ESC}32m${s}${ESC}0m`;
const orange = (s) => `${ESC}38;5;215m${s}${ESC}0m`;
const violet = (s) => `${ESC}38;5;141m${s}${ESC}0m`;
// No box-drawing characters: the bundled JetBrains Mono is the latin subset,
// so "─" and "│" fall back to another font and render as dashes.
const CLAUDE_TRANSCRIPT = [
  orange("✻") + " " + bold("Welcome to Claude Code!"),
  dim("  /help for help, /status for your current setup"),
  dim("  cwd: C:\\sites\\acme-shop"),
  "",
  dim(">") + " Add percentage coupons to the checkout and cover them with tests",
  "",
  violet("●") + " I'll look at how the cart total is computed before touching it.",
  "",
  violet("●") + " " + bold("Read") + dim("(app/Services/Cart/CartTotals.php)"),
  dim("  ⎿  Read 84 lines"),
  "",
  violet("●") + " " + bold("Read") + dim("(app/Models/Coupon.php)"),
  dim("  ⎿  Read 41 lines"),
  "",
  violet("●") + " " + bold("Update") + dim("(app/Services/Cart/CartTotals.php)"),
  dim("  ⎿  Added 18 lines, removed 3 lines"),
  "",
  violet("●") + " " + bold("Write") + dim("(tests/Feature/Checkout/PercentageCouponTest.php)"),
  dim("  ⎿  Wrote 62 lines"),
  "",
  violet("●") + " " + bold("Bash") + dim("(php artisan test --filter=PercentageCoupon)"),
  "  ⎿  " + green("PASS") + "  Tests\\Feature\\Checkout\\PercentageCouponTest",
  "     " + green("✓") + " applies a percentage coupon to the subtotal",
  "     " + green("✓") + " never discounts below zero",
  "     " + green("✓") + " ignores an expired coupon",
  "     Tests: " + green("3 passed") + " (7 assertions)",
  "",
  violet("●") + " Percentage coupons now apply to the subtotal before shipping, expired",
  "  codes are ignored, and three feature tests cover the behaviour. Want me",
  "  to wire the coupon field into the checkout Blade view next?",
  "",
  orange(">") + " ",
  dim("  ? for shortcuts"),
].join("\r\n");

export const SCENARIOS = [
  {
    id: "dashboard",
    out: "baner1.png",
    docsOut: "dashboard.png",
    theme: "midnight",
    state: {},
    navigate: async () => {},
    ready: "local dev, simplified",
  },
  {
    id: "projects",
    out: "baner2.png",
    docsOut: "projects-workspace.png",
    theme: "midnight",
    seedMetrics: false,
    state: { projects: PROJECTS },
    navigate: selectAcme,
    ready: "composer.json: ^8.2",
    verify: async page => {
      await assertVisible(page.getByRole('region', { name: 'Project details · acme-shop', exact: true }), 'selected project panel');
      await assertVisible(page.getByText('Client tags', { exact: true }), 'client tag filter');
      await assertVisible(page.getByText('Showing 1–10 of 14 projects', { exact: true }), 'pagination');
    },
  },
  {
    id: "terminal",
    out: "baner3.png",
    docsOut: "agent-terminal.png",
    theme: "midnight",
    seedMetrics: false,
    // webgl:false makes Terminal.tsx fall back to xterm's DOM renderer, which
    // is the only one that paints under headless Chrome (see mock/base.mjs).
    state: {
      projects: PROJECTS,
      webgl: false,
      agents: { enabled: true, profiles: AGENT_PROFILES, sessions: AGENT_SESSIONS, usage: AGENT_USAGE },
      workspace: TERMINAL_WORKSPACE,
    },
    navigate: async (page) => {
      await page.getByRole("button", { name: "Terminal", exact: true }).click();
      // Agent tabs are restored from the persisted layout; the first one is
      // active. Wait until its pane has subscribed before writing to it.
      await page.getByText("acme-shop · Claude Code").first().waitFor({ state: "visible" });
      await page.waitForFunction(() => window.__harness.hasListener("term:data:harness-pty-agent-1"));
      // xterm opened while its pane was still hidden, so it sits at the default
      // 24 rows; the ResizeObserver refit never fires under headless Chrome, but
      // Terminal.tsx also refits on window resize. Writing before the refit
      // would scroll the top of the transcript away.
      await page.evaluate(() => window.dispatchEvent(new Event("resize")));
      await page.waitForFunction(() => {
        const rows = document.querySelector(".xterm-rows");
        return !!rows && rows.children.length > 30;
      });
      await page.evaluate((text) => window.__harness.emit("term:data:harness-pty-agent-1", text), CLAUDE_TRANSCRIPT);
      // The adopted session is applied to shell leaves as they exist when the
      // workspace first lists sessions; re-announce it now that the beacon-api
      // shell has its PTY id, the way the backend does on every state change.
      await page.evaluate((session) => window.__harness.emit("agent:session-changed", session), AGENT_SESSIONS[2]);
      await page.getByText("beacon-api · Claude Code").first().waitFor({ state: "visible" });
    },
    // Only present once xterm's DOM renderer has painted the transcript.
    ready: "Tests: 3 passed",
  },
  {
    id: "ssh",
    out: "baner4.png",
    docsOut: "ssh-manager.png",
    theme: "midnight",
    seedMetrics: false,
    state: { sshHosts: SSH_HOSTS },
    navigate: async (page) => {
      await page.getByRole("button", { name: "SSH" }).click();
    },
    ready: "edge-01",
  },
  {
    id: "deployments",
    out: "baner5.png",
    docsOut: "deployments.png",
    theme: "midnight",
    seedMetrics: false,
    state: { sshHosts: SSH_HOSTS, deployments: DEPLOYMENTS },
    navigate: async (page) => {
      await page.getByRole("button", { name: "Deployments" }).click();
      await page.getByRole("button", { name: /Apply/ }).first().click();
    },
    ready: "Apply — dev",
  },
  {
    id: "control",
    out: "baner6.png",
    docsOut: "appearance.png",
    theme: "midnight",
    seedMetrics: false,
    state: {},
    navigate: async (page) => {
      await page.getByRole("button", { name: "Settings" }).click();
      await page.getByRole("button", { name: "Appearance" }).click();
    },
    ready: "Pick a color template",
  },
];

export const DOC_SCENARIOS = [
  {
    id: 'project-commands', out: 'project-commands.png', framed: false, seedMetrics: false,
    theme: 'midnight', state: { projects: PROJECTS,
      projectCommands: { 'acme-shop': { artisan: [], npmScripts: ['build', 'dev', 'watch'] } } },
    navigate: async page => {
      await selectAcme(page);
      const panel = page.getByRole('region', { name: 'Project details · acme-shop', exact: true });
      await panel.getByRole('combobox', { name: 'Script', exact: true }).selectOption('watch');
      await panel.getByRole('button', { name: 'Run npm run watch in terminal', exact: true }).scrollIntoViewIfNeeded();
    },
    ready: 'npm run watch',
    verify: async page => {
      const panel = page.getByRole('region', { name: 'Project details · acme-shop', exact: true });
      await assertVisible(panel.getByRole('button', { name: 'Start queue', exact: true }), 'Laravel worker controls');
      await assertVisible(panel.getByRole('textbox', { name: 'Custom command', exact: true }), 'custom project command');
      if (await panel.getByRole('combobox', { name: 'Script', exact: true }).inputValue() !== 'watch') throw new Error('Watch script was not selected');
    },
  },
  {
    id: 'agent-map', out: 'agent-map.png', framed: false, seedMetrics: false,
    theme: 'midnight', state: AGENT_MAP_STATE,
    navigate: async page => {
      await page.getByRole('button', { name: 'Terminal', exact: true }).click();
      await page.getByRole('button', { name: /^4 agents/ }).click();
      await page.getByRole('dialog', { name: 'Agent map', exact: true }).waitFor();
      await page.waitForFunction(() => window.__harness.hasListener('agent:activity-changed'));
      await page.evaluate(snapshot => {
        window.__agentActivity.snapshots[snapshot.sessionId] = snapshot;
        window.__harness.emit('agent:activity-changed', snapshot);
      }, { ...AGENT_MAP_SNAPSHOT, revision: 4 });
    },
    ready: 'Review coupon calculations',
    verify: async page => {
      const dialog = page.getByRole('dialog', { name: 'Agent map', exact: true });
      await assertVisible(dialog.getByRole('button', { name: 'Close agent map', exact: true }), 'close agent map');
      await assertVisible(dialog.getByText('Tokens unavailable', { exact: true }), 'unavailable per-agent metrics');
      if (!await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)) throw new Error('Agent map overflows the viewport');
      if (await page.evaluate(() => window.__agentActivity.ptySpawns.length) !== 0) throw new Error('Opening the map spawned a PTY');
    },
  },
  {
    id: 'project-actions', out: 'project-actions.png', framed: false, seedMetrics: false,
    theme: 'midnight', state: { projects: PROJECTS },
    navigate: async page => {
      await selectAcme(page);
      await page.getByRole('button', { name: 'Customize actions', exact: true }).click();
      await page.getByRole('combobox', { name: /^Shortcut 1/ }).selectOption('browser');
      await page.getByRole('combobox', { name: /^Shortcut 2/ }).selectOption('terminal');
    },
    ready: 'Choose up to two shortcuts for every project.',
    verify: async page => {
      await assertVisible(page.getByRole('button', { name: 'Open acme-shop in terminal', exact: true }), 'configured terminal shortcut');
      if (await page.getByRole('combobox', { name: /^Shortcut 1/ }).inputValue() !== 'browser') throw new Error('Browser shortcut was not selected');
      if (await page.getByRole('combobox', { name: /^Shortcut 2/ }).inputValue() !== 'terminal') throw new Error('Terminal shortcut was not selected');
    },
  },
  {
    id: 'project-runtimes', out: 'project-runtimes.png', framed: false, seedMetrics: false,
    theme: 'midnight', state: { projects: PROJECTS }, navigate: async page => {
      await selectAcme(page);
      await page.getByRole('region', { name: 'Project details · acme-shop', exact: true }).getByText('.nvmrc: 22', { exact: true }).scrollIntoViewIfNeeded();
    },
    ready: '.nvmrc: 22',
    verify: async page => {
      const panel = page.getByRole('region', { name: 'Project details · acme-shop', exact: true });
      await assertVisible(panel.getByText('composer.json: ^8.2', { exact: true }), 'Composer PHP requirement');
      await assertVisible(panel.getByText('.nvmrc: 22', { exact: true }), 'Node version requirement');
      if (await panel.getByText('Detected', { exact: true }).count() !== 2) throw new Error('Both runtimes must be detected');
    },
  },
  {
    id: 'site-doctor', out: 'site-doctor.png', framed: false, seedMetrics: false,
    theme: 'midnight', state: { projects: PROJECTS, doctor: DOCTOR_REPORTS },
    navigate: async page => {
      await page.getByRole('button', { name: 'Projects', exact: true }).click();
      await page.getByRole('button', { name: 'Run Doctor on all sites', exact: true }).click();
      await page.getByRole('button', { name: 'View diagnosis for atlas-crm', exact: true }).click();
    },
    ready: 'The project has no .env file.',
    verify: async page => {
      const dialog = page.getByRole('dialog', { name: 'Site Doctor · atlas-crm', exact: true });
      await assertVisible(dialog, 'project diagnosis');
      await assertVisible(dialog.getByRole('button', { name: 'Fix .env file', exact: true }), 'environment repair');
      await assertVisible(dialog.getByText('Optional', { exact: true }), 'optional storage link');
    },
  },
  {
    id: 'site-doctor-overview', out: 'site-doctor-overview.png', framed: false, seedMetrics: false,
    theme: 'midnight', state: { projects: PROJECTS, doctor: DOCTOR_REPORTS },
    navigate: async page => {
      await page.getByRole('button', { name: 'Projects', exact: true }).click();
      await page.getByRole('button', { name: 'Run Doctor on all sites', exact: true }).click();
    },
    ready: 'Showing 1–10 of 14 projects',
    verify: async page => {
      const dialog = page.getByRole('dialog', { name: 'Site Doctor', exact: true });
      await assertVisible(dialog.getByRole('button', { name: 'Needs attention 2', exact: true }), 'attention summary count');
      await assertVisible(dialog.getByRole('button', { name: 'Healthy 12', exact: true }), 'healthy summary count');
      await assertVisible(dialog.getByRole('textbox', { name: 'Search projects', exact: true }), 'diagnosis search');
    },
  },
  {
    id: 'workspace', out: 'workspace.png', framed: false, seedMetrics: false,
    theme: 'midnight', state: WORKSPACE_STATE,
    navigate: async page => {
      await openWorkspace(page);
      await page.getByRole('tab', { name: 'Git', exact: true }).click();
      await page.getByRole('group', { name: 'Working tree changes', exact: true }).getByRole('button', { name: 'Review changes README.md', exact: true }).click();
      await page.locator('.monaco-diff-editor:visible').waitFor();
    },
    ready: 'Tests: 3 passed',
    verify: async page => {
      await assertVisible(page.getByText('feature/checkout-coupons', { exact: true }), 'repository branch');
      await assertVisible(page.locator('.monaco-diff-editor:visible'), 'real Monaco review');
      if (!await page.evaluate(() => window.monaco.editor.getModels().some(model => model.getValue().includes('Percentage coupons apply before shipping.')))) throw new Error('Checkout diff was not rendered');
    },
  },
  {
    id: 'workspace-files', out: 'workspace-files.png', framed: false, seedMetrics: false,
    theme: 'midnight', state: WORKSPACE_STATE,
    navigate: async page => { await openWorkspace(page); await page.getByRole('tab', { name: 'Files', exact: true }).click(); },
    ready: 'Tests: 3 passed',
    verify: async page => {
      await assertVisible(page.getByRole('tabpanel', { name: 'Files', exact: true }).getByRole('button', { name: 'README.md', exact: true }), 'fictional file tree');
      await assertVisible(page.getByTestId('document-surface').locator('.monaco-editor:visible'), 'real Monaco source editor');
    },
  },
];

export function getScenario(id) {
  return [...SCENARIOS, ...DOC_SCENARIOS].find((s) => s.id === id);
}
