// Baseline fake backend state. Scenarios patch this shallowly; every value
// here is fictional on purpose — these images are published publicly.

export const BASE_STATE = {
  version: "0.5.6",
  services: [
    { name: "mailpit", label: "Mailpit" },
    { name: "mysql", label: "MySQL" },
    { name: "nginx", label: "Nginx" },
  ],
  serviceStatus: "running",
  php: [
    { version: "7.4", path: "C:/php/7.4", active: false },
    { version: "8.1", path: "C:/php/8.1", active: false },
    { version: "8.3", path: "C:/php/8.3", active: false },
    { version: "8.4", path: "C:/php/8.4", active: true },
  ],
  node: [
    { version: "22.11.0", path: "C:/node/22.11.0", active: true },
    { version: "24.15.0", path: "C:/node/24.15.0", active: false },
  ],
  fpm: [
    { version: "7.4", port: 9074, status: "running", binary: "C:/php/7.4/php-cgi.exe" },
    { version: "8.1", port: 9081, status: "running", binary: "C:/php/8.1/php-cgi.exe" },
    { version: "8.3", port: 9083, status: "running", binary: "C:/php/8.3/php-cgi.exe" },
    { version: "8.4", port: 9084, status: "running", binary: "C:/php/8.4/php-cgi.exe" },
  ],
  metrics: {
    cpuPercent: 12, memPercent: 62, memUsedMB: 19712, memTotalMB: 32563,
    gpuPercent: 8, gpuVendor: "NVIDIA GeForce RTX 4060", gpuAvailable: true,
    diskPercent: 58, diskUsedGB: 535.0, diskTotalGB: 930.5, diskPath: "C:\\",
  },
  cloud: {
    loggedIn: true, baseUrl: "https://laraenv.com", email: "dev@example.com",
    name: "Alex Rivera", githubId: "", plan: "pro",
    // Optional hosted-service permissions; local deployments are independent.
    entitlements: { deployments: true },
  },
  quote: { text: "Talk is cheap. Show me the code.", author: "Linus Torvalds" },
  projects: [],
  sshHosts: [],
  deployments: [],
  crons: [],
  terminals: {
    profiles: [
      { kind: "pwsh", label: "PowerShell", available: true },
      { kind: "cmd", label: "Command Prompt", available: true },
      { kind: "gitbash", label: "Git Bash", available: true },
    ],
    defaultKind: "pwsh",
    projectKinds: {},
  },
};

// Shallow by design: a scenario always supplies a whole array or object for
// the key it cares about, so a deep merge would only make the result harder
// to predict from reading the scenario.
export function mergeState(base, patch) {
  return { ...structuredClone(base), ...structuredClone(patch) };
}

// SystemMetrics keeps the last 32 samples (frontend/src/components/SystemMetrics.tsx:22)
// and draws them as a sparkline. One sample renders a flat line, so the harness
// replays a fixed series ending on the declared sample — fixed, because the
// output has to be byte-identical between runs.
const METRICS_DEPTH = 32;
const WOBBLE = [0, 3, -2, 5, 1, -4, 6, 2, -1, 4, -3, 7, 0, -5, 3, 1];

export function buildMetricsSeries(sample) {
  const clamp = (n) => Math.max(0, Math.min(100, Math.round(n)));
  return Array.from({ length: METRICS_DEPTH }, (_, i) => {
    const last = i === METRICS_DEPTH - 1;
    const w = WOBBLE[i % WOBBLE.length];
    return {
      ...sample,
      cpuPercent: last ? sample.cpuPercent : clamp(sample.cpuPercent + w),
      memPercent: last ? sample.memPercent : clamp(sample.memPercent + w / 3),
      gpuPercent: last ? sample.gpuPercent : clamp(sample.gpuPercent + w / 2),
    };
  });
}
