import { describe, it, expect } from "vitest";
import { BASE_STATE, mergeState, buildMetricsSeries } from "../mock/state.mjs";

describe("mergeState", () => {
  it("replaces only the keys present in the patch", () => {
    const out = mergeState({ a: 1, b: 2 }, { b: 3 });
    expect(out).toEqual({ a: 1, b: 3 });
  });

  it("does not mutate the base", () => {
    const base = { a: 1, list: [1, 2] };
    mergeState(base, { list: [9] });
    expect(base.list).toEqual([1, 2]);
  });

  it("replaces arrays wholesale rather than concatenating", () => {
    const out = mergeState({ list: [1, 2] }, { list: [9] });
    expect(out.list).toEqual([9]);
  });

  it("returns a copy of the base when the patch is empty", () => {
    const out = mergeState({ a: 1 }, {});
    expect(out).toEqual({ a: 1 });
  });
});

describe("buildMetricsSeries", () => {
  it("returns exactly the depth the chart keeps", () => {
    expect(buildMetricsSeries(BASE_STATE.metrics)).toHaveLength(32);
  });

  it("ends on the declared sample so the readout matches the state", () => {
    const series = buildMetricsSeries(BASE_STATE.metrics);
    expect(series.at(-1).cpuPercent).toBe(BASE_STATE.metrics.cpuPercent);
    expect(series.at(-1).memPercent).toBe(BASE_STATE.metrics.memPercent);
  });

  it("varies across the series so the sparkline is not a flat line", () => {
    const cpus = new Set(buildMetricsSeries(BASE_STATE.metrics).map((s) => s.cpuPercent));
    expect(cpus.size).toBeGreaterThan(4);
  });

  it("is deterministic", () => {
    expect(buildMetricsSeries(BASE_STATE.metrics))
      .toEqual(buildMetricsSeries(BASE_STATE.metrics));
  });

  it("keeps every value in range", () => {
    for (const s of buildMetricsSeries(BASE_STATE.metrics)) {
      expect(s.cpuPercent).toBeGreaterThanOrEqual(0);
      expect(s.cpuPercent).toBeLessThanOrEqual(100);
    }
  });
});

describe("BASE_STATE", () => {
  it("carries the fields every scenario relies on", () => {
    for (const key of ["version", "services", "php", "node", "fpm", "metrics", "cloud", "quote"]) {
      expect(BASE_STATE, `missing ${key}`).toHaveProperty(key);
    }
  });

  it("has no real-world paths or client names", () => {
    const json = JSON.stringify(BASE_STATE);
    expect(json).not.toMatch(/laraenv\\\\www|private-client-|private-project|private-site/i);
  });
});
