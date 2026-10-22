// @vitest-environment jsdom
// The root ErrorBoundary must catch render errors and show a recovery screen
// instead of letting React unmount the whole tree (blank window).
import { describe, it, expect } from "vitest";
import React, { act } from "react";
import { createRoot } from "react-dom/client";
import ErrorBoundary from "../ErrorBoundary";

(globalThis as any).IS_REACT_ACT_ENVIRONMENT = true;

function Bomb(): JSX.Element {
  throw new Error("boom from render");
}

describe("ErrorBoundary", () => {
  it("renders the fallback with the error message instead of unmounting", () => {
    const host = document.createElement("div");
    document.body.appendChild(host);
    const root = createRoot(host);
    // React re-throws boundary-caught errors to window in dev; swallow those so
    // the test only fails if the boundary itself doesn't handle the error.
    const swallow = (e: ErrorEvent) => e.preventDefault();
    window.addEventListener("error", swallow);
    try {
      act(() => {
        root.render(
          <ErrorBoundary>
            <Bomb />
          </ErrorBoundary>,
        );
      });
    } finally {
      window.removeEventListener("error", swallow);
    }
    expect(host.textContent).toContain("Something went wrong");
    expect(host.textContent).toContain("boom from render");
    act(() => root.unmount());
  });

  it("renders children normally when nothing throws", () => {
    const host = document.createElement("div");
    document.body.appendChild(host);
    const root = createRoot(host);
    act(() => {
      root.render(
        <ErrorBoundary>
          <span>all good</span>
        </ErrorBoundary>,
      );
    });
    expect(host.textContent).toBe("all good");
    act(() => root.unmount());
  });
});
