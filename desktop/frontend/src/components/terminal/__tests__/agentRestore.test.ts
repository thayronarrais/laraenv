import { describe, expect, it } from "vitest";
import { isUnavailableAgentHistoryError } from "../agentRestore";

describe("agent terminal restoration", () => {
  it("recognizes missing provider history so a stale tab can be removed", () => {
    expect(isUnavailableAgentHistoryError("provider conversation is unavailable: Claude Code cannot find session abc")).toBe(true);
    expect(isUnavailableAgentHistoryError("network temporarily unavailable")).toBe(false);
  });
});
