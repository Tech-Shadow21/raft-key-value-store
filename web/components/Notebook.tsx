"use client";

import { useState } from "react";
import { isTimeout, kvGet, kvWrite } from "@/lib/api";
import { NodeConfig } from "@/lib/nodes";
import { ClusterView } from "@/lib/cluster";
import { EventKind } from "./ActivityFeed";

type Result = { tone: "good" | "info" | "bad"; text: string } | null;
type Action = "save" | "add" | "lookup";

export function Notebook({
  target,
  view,
  onEvent,
}: {
  target: NodeConfig | null;
  view: ClusterView;
  onEvent: (kind: EventKind, msg: string) => void;
}) {
  const [label, setLabel] = useState("");
  const [note, setNote] = useState("");
  const [busy, setBusy] = useState<Action | null>(null);
  const [result, setResult] = useState<Result>(null);
  const [saved, setSaved] = useState<string[]>([]);

  const canAct = !!target && !busy;

  async function run(action: Action, key = label.trim()) {
    if (!target || !key) return;
    setBusy(action);
    setResult(null);
    try {
      if (action === "lookup") {
        const r = await kvGet(target, key);
        setResult(
          r.found
            ? { tone: "info", text: `“${key}” says: ${r.value}` }
            : { tone: "info", text: `Nothing is saved under “${key}” yet.` }
        );
        if (r.found) setNote(r.value);
        setLabel(key);
      } else {
        await kvWrite(target, key, note, action === "add" ? "append" : "put");
        const verb = action === "add" ? "Added to" : "Saved";
        setResult({
          tone: "good",
          text: `${verb} “${key}”. A majority of servers (at least ${view.needed} of ${view.total}) confirmed it before this message appeared, so it will survive a server failure.`,
        });
        onEvent("you", `You ${action === "add" ? "added to" : "saved"} “${key}”. At least ${view.needed} of ${view.total} servers agreed and stored it.`);
        setSaved((s) => [key, ...s.filter((k) => k !== key)].slice(0, 12));
      }
    } catch (e) {
      const text = isTimeout(e)
        ? action === "lookup"
          ? "The servers couldn’t agree in time — probably because too few are online. Try again once more servers are back."
          : "Not confirmed yet: too few servers are online to agree on this change. It may still go through on its own once enough servers are back — use Look up later to check."
        : `Couldn’t reach the servers (${e instanceof Error ? e.message : String(e)}).`;
      setResult({ tone: "bad", text });
    } finally {
      setBusy(null);
    }
  }

  const resultStyle = {
    good: "border-emerald-200 bg-emerald-50 text-emerald-900 dark:border-emerald-900 dark:bg-emerald-950/50 dark:text-emerald-100",
    info: "border-sky-200 bg-sky-50 text-sky-900 dark:border-sky-900 dark:bg-sky-950/50 dark:text-sky-100",
    bad: "border-red-200 bg-red-50 text-red-900 dark:border-red-900 dark:bg-red-950/50 dark:text-red-100",
  };

  const input =
    "w-full rounded-xl border border-slate-300 bg-white px-4 py-2.5 text-base outline-none transition focus:border-indigo-500 focus:ring-4 focus:ring-indigo-500/15 dark:border-slate-700 dark:bg-slate-950";

  return (
    <div className="rounded-2xl border border-slate-200 bg-white p-5 shadow-sm sm:p-6 dark:border-slate-800 dark:bg-slate-900">
      <div className="grid gap-4 sm:grid-cols-[1fr_2fr]">
        <label className="block">
          <span className="mb-1.5 block text-sm font-medium">Label</span>
          <input
            className={input}
            placeholder="e.g. shopping-list"
            value={label}
            onChange={(e) => setLabel(e.target.value)}
            onKeyDown={(e) => e.key === "Enter" && run("lookup")}
          />
        </label>
        <label className="block">
          <span className="mb-1.5 block text-sm font-medium">Note</span>
          <input
            className={input}
            placeholder="e.g. milk, eggs, bread"
            value={note}
            onChange={(e) => setNote(e.target.value)}
            onKeyDown={(e) => e.key === "Enter" && run("save")}
          />
        </label>
      </div>

      <div className="mt-4 flex flex-wrap gap-2">
        <Button primary disabled={!canAct || !label.trim()} busy={busy === "save"} onClick={() => run("save")}>
          Save note
        </Button>
        <Button disabled={!canAct || !label.trim()} busy={busy === "add"} onClick={() => run("add")}>
          Add to end of note
        </Button>
        <Button disabled={!canAct || !label.trim()} busy={busy === "lookup"} onClick={() => run("lookup")}>
          Look up
        </Button>
      </div>
      <p className="mt-2 text-xs text-slate-500 dark:text-slate-400">
        <b>Save</b> replaces whatever was under that label. <b>Add to end</b> keeps the old note and appends to it.
        <b> Look up</b> reads the current note.
      </p>

      {!target && (
        <p className="mt-4 text-sm text-red-700 dark:text-red-400">No server is reachable right now, so the notebook is unavailable.</p>
      )}
      {result && <div className={`mt-4 rounded-xl border px-4 py-3 text-sm ${resultStyle[result.tone]}`}>{result.text}</div>}

      {saved.length > 0 && (
        <div className="mt-5 border-t border-slate-200 pt-4 dark:border-slate-800">
          <p className="mb-2 text-xs font-medium uppercase tracking-wide text-slate-500">Labels you used this visit — tap to look up</p>
          <div className="flex flex-wrap gap-2">
            {saved.map((k) => (
              <button
                key={k}
                disabled={!canAct}
                onClick={() => run("lookup", k)}
                className="rounded-full border border-slate-300 px-3 py-1 text-sm transition hover:border-indigo-400 hover:bg-indigo-50 disabled:opacity-50 dark:border-slate-700 dark:hover:bg-indigo-950/40"
              >
                {k}
              </button>
            ))}
          </div>
        </div>
      )}
    </div>
  );
}

function Button({
  children,
  primary,
  disabled,
  busy,
  onClick,
}: {
  children: React.ReactNode;
  primary?: boolean;
  disabled: boolean;
  busy: boolean;
  onClick: () => void;
}) {
  return (
    <button
      disabled={disabled}
      onClick={onClick}
      className={`rounded-xl px-4 py-2.5 text-sm font-medium transition disabled:cursor-not-allowed disabled:opacity-40 ${
        primary
          ? "bg-indigo-600 text-white hover:bg-indigo-500"
          : "border border-slate-300 bg-white hover:bg-slate-50 dark:border-slate-700 dark:bg-slate-900 dark:hover:bg-slate-800"
      }`}
    >
      {busy ? "Waiting for servers to agree…" : children}
    </button>
  );
}
