"use client";

import { useEffect, useRef, useState } from "react";
import { getNodes } from "@/lib/nodes";
import { fetchStatus, kvGet, kvWrite, NodeStatus } from "@/lib/api";

const POLL_MS = 1000;

export default function Dashboard() {
  const nodes = useRef(getNodes()).current;
  const [statuses, setStatuses] = useState<Record<number, NodeStatus>>({});
  const [key, setKey] = useState("");
  const [value, setValue] = useState("");
  const [op, setOp] = useState<"put" | "append">("put");
  const [result, setResult] = useState<string>("");
  const [busy, setBusy] = useState(false);
  const [log, setLog] = useState<{ t: string; msg: string }[]>([]);

  useEffect(() => {
    let cancelled = false;
    const poll = async () => {
      const results = await Promise.all(nodes.map((n) => fetchStatus(n)));
      if (cancelled) return;
      setStatuses((prev) => {
        const next = { ...prev };
        for (const r of results) next[r.id] = r;
        return next;
      });
    };
    poll();
    const id = setInterval(poll, POLL_MS);
    return () => {
      cancelled = true;
      clearInterval(id);
    };
  }, [nodes]);

  // Track leader changes and node reachability flips for a lightweight
  // event feed, so a leader failover is visible without staring at a table.
  const prevStatuses = useRef<Record<number, NodeStatus>>({});
  useEffect(() => {
    const prev = prevStatuses.current;
    const events: string[] = [];
    for (const s of Object.values(statuses)) {
      const p = prev[s.id];
      if (s.reachable && (!p || !p.reachable)) {
        events.push(`node ${s.id} came online`);
      }
      if (!s.reachable && p?.reachable) {
        events.push(`node ${s.id} went unreachable`);
      }
      if (s.reachable && p?.reachable && s.leader && !p.leader) {
        events.push(`node ${s.id} became leader (term ${s.term})`);
      }
    }
    if (events.length > 0) {
      setLog((l) =>
        [...events.map((msg) => ({ t: new Date().toLocaleTimeString(), msg })), ...l].slice(0, 30)
      );
    }
    prevStatuses.current = statuses;
  }, [statuses]);

  const leader = Object.values(statuses).find((s) => s.reachable && s.leader);
  const anyReachable = Object.values(statuses).find((s) => s.reachable);
  const target = leader ?? anyReachable;

  async function submit(kind: "get" | "write") {
    if (!target || !target.reachable || !key) return;
    const node = nodes.find((n) => n.id === target.id)!;
    setBusy(true);
    setResult("");
    try {
      if (kind === "get") {
        const r = await kvGet(node, key);
        setResult(r.found ? r.value : "(no such key)");
      } else {
        await kvWrite(node, key, value, op);
        setResult("ok");
      }
    } catch (e) {
      setResult(`error: ${e instanceof Error ? e.message : String(e)}`);
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="min-h-screen bg-neutral-950 text-neutral-100 font-mono">
      <div className="max-w-4xl mx-auto px-6 py-10 space-y-10">
        <header>
          <h1 className="text-xl font-semibold tracking-tight">raft-kv-store</h1>
          <p className="text-neutral-500 text-sm mt-1">
            live cluster dashboard — polling {nodes.length} node
            {nodes.length === 1 ? "" : "s"} every {POLL_MS}ms
          </p>
        </header>

        <section>
          <h2 className="text-sm uppercase tracking-wider text-neutral-500 mb-3">Cluster</h2>
          <div className="border border-neutral-800 rounded-lg overflow-hidden">
            <table className="w-full text-sm">
              <thead>
                <tr className="bg-neutral-900 text-neutral-400 text-left">
                  <th className="px-4 py-2 font-medium">Node</th>
                  <th className="px-4 py-2 font-medium">Status</th>
                  <th className="px-4 py-2 font-medium">Term</th>
                  <th className="px-4 py-2 font-medium">Role</th>
                </tr>
              </thead>
              <tbody>
                {nodes.map((n) => {
                  const s = statuses[n.id];
                  const reachable = s?.reachable;
                  return (
                    <tr key={n.id} className="border-t border-neutral-800">
                      <td className="px-4 py-2">
                        node {n.id}
                        <span className="text-neutral-600 ml-2 text-xs">{n.httpAddr}</span>
                      </td>
                      <td className="px-4 py-2">
                        <span
                          className={`inline-flex items-center gap-1.5 ${
                            reachable ? "text-emerald-400" : "text-red-500"
                          }`}
                        >
                          <span
                            className={`h-1.5 w-1.5 rounded-full ${
                              reachable ? "bg-emerald-400" : "bg-red-500"
                            }`}
                          />
                          {reachable ? "up" : "unreachable"}
                        </span>
                      </td>
                      <td className="px-4 py-2 text-neutral-300">
                        {reachable ? (s as { term: number }).term : "—"}
                      </td>
                      <td className="px-4 py-2">
                        {reachable && (s as { leader: boolean }).leader ? (
                          <span className="text-amber-400 font-semibold">leader</span>
                        ) : reachable ? (
                          <span className="text-neutral-500">follower</span>
                        ) : (
                          <span className="text-neutral-700">—</span>
                        )}
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        </section>

        <section>
          <h2 className="text-sm uppercase tracking-wider text-neutral-500 mb-3">Get / Put / Append</h2>
          <div className="border border-neutral-800 rounded-lg p-4 space-y-3">
            <div className="flex gap-2">
              <input
                className="flex-1 bg-neutral-900 border border-neutral-800 rounded px-3 py-2 text-sm outline-none focus:border-neutral-600"
                placeholder="key"
                value={key}
                onChange={(e) => setKey(e.target.value)}
              />
              <input
                className="flex-1 bg-neutral-900 border border-neutral-800 rounded px-3 py-2 text-sm outline-none focus:border-neutral-600"
                placeholder="value"
                value={value}
                onChange={(e) => setValue(e.target.value)}
              />
              <select
                className="bg-neutral-900 border border-neutral-800 rounded px-2 py-2 text-sm outline-none"
                value={op}
                onChange={(e) => setOp(e.target.value as "put" | "append")}
              >
                <option value="put">put</option>
                <option value="append">append</option>
              </select>
            </div>
            <div className="flex gap-2">
              <button
                disabled={busy || !target || !key}
                onClick={() => submit("get")}
                className="px-3 py-1.5 text-sm rounded bg-neutral-800 hover:bg-neutral-700 disabled:opacity-40 disabled:cursor-not-allowed transition"
              >
                Get
              </button>
              <button
                disabled={busy || !target || !key}
                onClick={() => submit("write")}
                className="px-3 py-1.5 text-sm rounded bg-emerald-700 hover:bg-emerald-600 disabled:opacity-40 disabled:cursor-not-allowed transition"
              >
                {op === "put" ? "Put" : "Append"}
              </button>
              {!target && <span className="text-red-500 text-sm self-center">no reachable node</span>}
            </div>
            {result && (
              <div className="text-sm bg-neutral-900 border border-neutral-800 rounded px-3 py-2 text-neutral-300">
                {result}
              </div>
            )}
          </div>
        </section>

        <section>
          <h2 className="text-sm uppercase tracking-wider text-neutral-500 mb-3">Events</h2>
          <div className="border border-neutral-800 rounded-lg p-4 h-48 overflow-y-auto text-sm space-y-1">
            {log.length === 0 && <div className="text-neutral-600">watching for leader changes…</div>}
            {log.map((e, i) => (
              <div key={i} className="text-neutral-400">
                <span className="text-neutral-600">{e.t}</span> — {e.msg}
              </div>
            ))}
          </div>
        </section>
      </div>
    </div>
  );
}
