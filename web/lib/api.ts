import { NodeConfig } from "./nodes";

export type NodeStatus = {
  id: number;
  term: number;
  leader: boolean;
  peers: Record<string, string>;
  reachable: true;
} | {
  id: number;
  reachable: false;
  error: string;
};

export async function fetchStatus(node: NodeConfig, signal?: AbortSignal): Promise<NodeStatus> {
  try {
    const res = await fetch(`${node.httpAddr}/api/status`, { signal, cache: "no-store" });
    if (!res.ok) throw new Error(`HTTP ${res.status}`);
    const data = await res.json();
    return { ...data, reachable: true };
  } catch (e) {
    return { id: node.id, reachable: false, error: e instanceof Error ? e.message : String(e) };
  }
}

export async function kvGet(node: NodeConfig, key: string): Promise<{ value: string; found: boolean }> {
  const res = await fetch(`${node.httpAddr}/api/kv?key=${encodeURIComponent(key)}`, { cache: "no-store" });
  if (!res.ok) throw new Error(`HTTP ${res.status}`);
  return res.json();
}

export async function kvWrite(
  node: NodeConfig,
  key: string,
  value: string,
  op: "put" | "append"
): Promise<void> {
  const res = await fetch(`${node.httpAddr}/api/kv`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ key, value, op }),
  });
  if (!res.ok) throw new Error(`HTTP ${res.status}: ${await res.text()}`);
}
