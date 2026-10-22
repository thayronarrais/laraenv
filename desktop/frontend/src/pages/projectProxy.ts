// Reverse-proxy toggle rules for a project row.
//
// A project with an active proxy must always be able to disable it — even
// when its dev server is stopped and no port is known — otherwise the user
// is stuck with a vhost that forwards to nothing.

export function showProxyToggle(proxyPort: number, devPort: number | undefined): boolean {
  return proxyPort > 0 || !!devPort;
}

/** 0 = disable the active proxy; a port = wire it; null = nothing to wire yet. */
export function nextProxyPort(proxyPort: number, devPort: number | undefined): number | null {
  if (proxyPort > 0) return 0;
  return devPort ? devPort : null;
}
