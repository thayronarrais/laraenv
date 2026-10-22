import type { ActivityNode, ActivitySnapshot } from "./activityTypes";
export interface ActivityTreeNode { node: ActivityNode; children: ActivityTreeNode[] }
export function activityCounts(snapshot: ActivitySnapshot) {
  const agents = snapshot.nodes.filter(node => node.kind === "agent");
  return {
    agents: agents.length,
    working: agents.filter(node => node.state === "working" || node.state === "starting").length,
    waiting: agents.filter(node => node.state === "needs-input").length,
    unknown: agents.filter(node => node.state === "unknown").length,
    lowerBound: snapshot.truncated,
  };
}
export function activityTree(snapshot: ActivitySnapshot): ActivityTreeNode[] {
  const byId = new Map<string, ActivityTreeNode>();
  for (const node of [...snapshot.nodes, ...snapshot.backgroundTasks]) {
    if (!byId.has(node.id)) byId.set(node.id, { node, children: [] });
  }
  const parents = new Map<string, string>();
  const roots: ActivityTreeNode[] = [];
  const items = [...byId.values()];
  // Keep the principal first even when the transport lists children first.
  items.sort((left, right) => Number(right.node.id === snapshot.rootId) - Number(left.node.id === snapshot.rootId));
  for (const item of items) {
    const parent = item.node.id === snapshot.rootId ? undefined : byId.get(item.node.parentId ?? "");
    let ancestor: string | undefined = parent?.node.id;
    while (ancestor && ancestor !== item.node.id) ancestor = parents.get(ancestor);
    if (!parent || ancestor === item.node.id) roots.push(item);
    else { parent.children.push(item); parents.set(item.node.id, parent.node.id); }
  }
  return roots;
}
export function activityDurationMs(node: ActivityNode, now: number, observation: Pick<ActivitySnapshot, "stale" | "messageCode" | "capabilities">): number | null {
  if (!node.startedAt) return null;
  // An observed start does not establish continued runtime after observation is lost.
  // Explicit end timestamps still describe a known, fixed interval.
  if (node.endedAt === undefined && (!observation.capabilities.liveState || node.state === "unknown" || observation.stale
    || observation.messageCode === "observation-ended" || observation.messageCode === "hooks-disabled")) return null;
  const started = Date.parse(node.startedAt);
  const ended = node.endedAt === undefined ? now : Date.parse(node.endedAt);
  if (!Number.isFinite(started) || !Number.isFinite(ended)) return null;
  return Math.max(0, ended - started);
}
