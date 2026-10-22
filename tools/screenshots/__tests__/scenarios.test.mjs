import { describe, it, expect } from "vitest";
import { SCENARIOS, DOC_SCENARIOS, getScenario } from "../mock/scenarios.mjs";

describe("scenario catalog", () => {
  it("declares every field the capture pipeline reads", () => {
    for (const s of SCENARIOS) {
      expect(typeof s.id, `${s.id}.id`).toBe("string");
      expect(s.out, `${s.id}.out`).toMatch(/^baner[1-6]\.png$/);
      expect(typeof s.theme, `${s.id}.theme`).toBe("string");
      expect(typeof s.state, `${s.id}.state`).toBe("object");
      expect(typeof s.navigate, `${s.id}.navigate`).toBe("function");
      expect(typeof s.ready, `${s.id}.ready`).toBe("string");
      expect(s.ready.length, `${s.id}.ready must be substantial`).toBeGreaterThan(2);
      if (s.docsOut) expect(s.docsOut, `${s.id}.docsOut`).toMatch(/^[a-z0-9-]+\.png$/);
    }
  });

  it("uses each output filename exactly once", () => {
    const outs = SCENARIOS.map((s) => s.out);
    expect(new Set(outs).size).toBe(outs.length);
  });

  it("uses each id exactly once", () => {
    const ids = SCENARIOS.map((s) => s.id);
    expect(new Set(ids).size).toBe(ids.length);
  });

  it("looks a scenario up by id", () => {
    expect(getScenario("dashboard").out).toBe("baner1.png");
    expect(getScenario("ssh").docsOut).toBe("ssh-manager.png");
  });

  it("returns undefined for an unknown id", () => {
    expect(getScenario("nope")).toBeUndefined();
  });

  it("has raw documentation captures for each release workflow", () => {
    expect(DOC_SCENARIOS.map(s => s.out)).toEqual([
      "project-commands.png",
      "agent-map.png",
      "project-actions.png", "project-runtimes.png", "site-doctor.png",
      "site-doctor-overview.png", "workspace.png", "workspace-files.png",
    ]);
    expect(getScenario("projects").docsOut).toBe("projects-workspace.png");
    for (const scenario of DOC_SCENARIOS) {
      expect(scenario.framed).toBe(false);
      expect(scenario.seedMetrics).toBe(false);
      expect(typeof scenario.verify).toBe("function");
      expect(getScenario(scenario.id)).toBe(scenario);
    }
  });

  it("uses fictional observed activity without claiming per-agent tokens", () => {
    const scenario = getScenario("agent-map");
    expect(scenario.seedMetrics).toBe(false);
    const snapshot = scenario.state.agentActivity.snapshots["map-claude"];
    expect(snapshot.nodes).toHaveLength(4);
    expect(snapshot.backgroundTasks.length).toBeGreaterThan(0);
    expect(snapshot.capabilities.tokens).toBe(false);
    expect(snapshot.nodes.every(node => node.tokens === undefined)).toBe(true);
    expect(scenario.state.workspace.groups[0].tabs.every(tab => tab.tree.spawn.kind === "agent")).toBe(true);
  });

  it("does not advertise Laravel storage repairs on other stacks or before missing dependencies", () => {
    const reports = getScenario("site-doctor").state.doctor;
    for (const report of reports) {
      const storage = report.findings.find(f => f.id === "storage_link");
      if (report.kind !== "laravel") expect(storage).toMatchObject({ status: "skipped", code: "not_applicable", fixID: "" });
      if (report.findings.some(f => f.id === "composer_dependencies" && f.code === "composer_dependencies_missing")) expect(storage.fixID).toBe("");
    }
  });
});
