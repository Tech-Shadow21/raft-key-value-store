import { NodeStatus } from "./api";

export type Health = "connecting" | "healthy" | "degraded" | "electing" | "stalled";

export type ClusterView = {
  total: number;
  online: number;
  needed: number; // how many servers must agree before a change counts
  leaderId: number | null;
  health: Health;
};

export function serverName(id: number) {
  return `Server ${id}`;
}

// Raft only makes progress while a strict majority of servers can talk to
// each other; everything the dashboard says about "safe" vs "paused" derives
// from that one rule.
export function describeCluster(ids: number[], statuses: Record<number, NodeStatus>): ClusterView {
  const total = ids.length;
  const needed = Math.floor(total / 2) + 1;
  const known = ids.map((id) => statuses[id]).filter(Boolean);
  const online = known.filter((s) => s.reachable).length;
  const leader = known.find((s) => s.reachable && s.leader);
  const leaderId = leader ? leader.id : null;

  let health: Health;
  if (known.length < total) health = "connecting";
  else if (online < needed) health = "stalled";
  else if (leaderId === null) health = "electing";
  else if (online < total) health = "degraded";
  else health = "healthy";

  return { total, online, needed, leaderId, health };
}
