import { NodeConfig } from "./nodes";

export type NodeStatus = {
  id: number;
  term: number;
  leader: boolean;
  peers: Record<string, string>;
  reachable: true;
  demoControls: boolean;
} | {
  id: number;
  reachable: false;
  error: string;
  // Process is up but switched off from the dashboard, so it can be turned
  // back on from here (unlike a crashed process).
  switchedOff?: boolean;
  demoControls?: boolean;
};

// The server's Clerk retries until a majority agrees, so without a majority a
// request would hang forever. Cap it so the UI can explain why instead.
const STATUS_TIMEOUT_MS = 1500;
const KV_TIMEOUT_MS = 6000;

export async function fetchStatus(node: NodeConfig): Promise<NodeStatus> {
  try {
    const res = await fetch(`${node.httpAddr}/api/status`, {
      signal: AbortSignal.timeout(STATUS_TIMEOUT_MS),
      cache: "no-store",
    });
    if (!res.ok) throw new Error(`HTTP ${res.status}`);
    const data = await res.json();
    // Older servers don't report `powered`; treat a missing field as on.
    if (data.powered === false) {
      return { id: data.id, reachable: false, error: "switched off", switchedOff: true, demoControls: !!data.demoControls };
    }
    return { ...data, demoControls: !!data.demoControls, reachable: true };
  } catch (e) {
    return { id: node.id, reachable: false, error: e instanceof Error ? e.message : String(e) };
  }
}

export async function kvGet(node: NodeConfig, key: string): Promise<{ value: string; found: boolean }> {
  const res = await fetch(`${node.httpAddr}/api/kv?key=${encodeURIComponent(key)}`, {
    signal: AbortSignal.timeout(KV_TIMEOUT_MS),
    cache: "no-store",
  });
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
    signal: AbortSignal.timeout(KV_TIMEOUT_MS),
  });
  if (!res.ok) throw new Error(`HTTP ${res.status}: ${await res.text()}`);
}

export function isTimeout(e: unknown): boolean {
  return e instanceof DOMException && (e.name === "TimeoutError" || e.name === "AbortError");
}

export async function setPower(node: NodeConfig, on: boolean): Promise<void> {
  const res = await fetch(`${node.httpAddr}/api/power`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ on }),
    signal: AbortSignal.timeout(KV_TIMEOUT_MS),
  });
  if (!res.ok) throw new Error(`HTTP ${res.status}: ${await res.text()}`);
}
