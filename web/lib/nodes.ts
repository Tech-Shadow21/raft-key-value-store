// Cluster node addresses for the dashboard, e.g.
//   NEXT_PUBLIC_KV_NODES="1=http://localhost:8301,2=http://localhost:8302,3=http://localhost:8303"
// Each address is a kvserver's -http listen address (its JSON dashboard
// API port, not the Raft RPC port).
export type NodeConfig = { id: number; httpAddr: string };

const DEFAULT_NODES = "1=http://localhost:8301,2=http://localhost:8302,3=http://localhost:8303";

export function getNodes(): NodeConfig[] {
  const raw = process.env.NEXT_PUBLIC_KV_NODES || DEFAULT_NODES;
  return raw
    .split(",")
    .map((s) => s.trim())
    .filter(Boolean)
    .map((entry) => {
      const [idStr, addr] = entry.split("=");
      return { id: Number(idStr), httpAddr: addr };
    });
}
