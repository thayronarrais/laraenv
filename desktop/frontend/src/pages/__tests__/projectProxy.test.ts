import { describe, expect, it } from "vitest";
import { nextProxyPort, showProxyToggle } from "../projectProxy";

describe("proxy toggle visibility", () => {
  it("hides the toggle when nothing is proxied and no dev port is known", () => {
    expect(showProxyToggle(0, undefined)).toBe(false);
  });
  it("shows the toggle when a dev port was detected", () => {
    expect(showProxyToggle(0, 3000)).toBe(true);
  });
  it("always shows the toggle while a proxy is active, even with the dev server stopped", () => {
    expect(showProxyToggle(3000, undefined)).toBe(true);
  });
});

describe("next proxy port", () => {
  it("disables an active proxy regardless of dev server state", () => {
    expect(nextProxyPort(3000, undefined)).toBe(0);
    expect(nextProxyPort(3000, 3001)).toBe(0);
  });
  it("wires the detected dev port when no proxy is active", () => {
    expect(nextProxyPort(0, 5173)).toBe(5173);
  });
  it("returns null when there is nothing to wire yet", () => {
    expect(nextProxyPort(0, undefined)).toBeNull();
    expect(nextProxyPort(0, 0)).toBeNull();
  });
});
