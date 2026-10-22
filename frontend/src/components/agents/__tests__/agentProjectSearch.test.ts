import { describe, expect, it } from "vitest";
import { searchAgentProjects } from "../agentProjectSearch";

const projects = [
  { name: "shop-admin", path: "C:\\sites\\shop-admin", url: "https://admin.shop.test", proxyPort: 80 },
  { name: "demo-app", path: "C:\\sites\\demo-app", url: "https://demo-app.test", proxyPort: 80 },
  { name: "shop-api", path: "C:\\sites\\shop-api", url: "https://api.shop.test", proxyPort: 80 },
];

describe("agent project search", () => {
  it("prioritizes matching project names", () => {
    expect(searchAgentProjects(projects, "shop").map((project) => project.name)).toEqual(["shop-admin", "shop-api"]);
  });

  it("also finds projects by domain", () => {
    expect(searchAgentProjects(projects, "admin.shop").map((project) => project.name)).toEqual(["shop-admin"]);
  });
});
