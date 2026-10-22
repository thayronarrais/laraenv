import type { AgentLaunchProjectOption } from "../AgentLauncherMenu";

export function searchAgentProjects(projects: AgentLaunchProjectOption[], query: string, limit = 8) {
  const term = query.trim().toLowerCase();
  return projects
    .map((project, index) => {
      const name = project.name.toLowerCase();
      const searchable = `${project.name} ${project.path} ${project.url}`.toLowerCase();
      const rank = !term ? 3 : name === term ? 0 : name.startsWith(term) ? 1 : searchable.includes(term) ? 2 : -1;
      return { project, rank, index };
    })
    .filter((item) => item.rank >= 0)
    .sort((left, right) => left.rank - right.rank || left.project.name.localeCompare(right.project.name) || left.index - right.index)
    .slice(0, limit)
    .map((item) => item.project);
}
