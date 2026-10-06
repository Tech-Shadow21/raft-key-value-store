export type EventKind = "good" | "warn" | "bad" | "info" | "you";
export type FeedEvent = { id: number; time: string; kind: EventKind; msg: string };

const DOT: Record<EventKind, string> = {
  good: "bg-emerald-500",
  warn: "bg-amber-500",
  bad: "bg-red-500",
  info: "bg-sky-500",
  you: "bg-indigo-500",
};

export function ActivityFeed({ events }: { events: FeedEvent[] }) {
  return (
    <div className="max-h-80 overflow-y-auto rounded-2xl border border-slate-200 bg-white p-2 shadow-sm dark:border-slate-800 dark:bg-slate-900">
      {events.length === 0 ? (
        <p className="p-4 text-sm text-slate-500">Watching for changes… Anything that happens to the servers will be explained here.</p>
      ) : (
        <ol>
          {events.map((e) => (
            <li key={e.id} className="flex gap-3 rounded-xl px-3 py-2.5 text-sm hover:bg-slate-50 dark:hover:bg-slate-800/50">
              <span className={`mt-1.5 h-2 w-2 shrink-0 rounded-full ${DOT[e.kind]}`} />
              <span className="flex-1 leading-relaxed">{e.msg}</span>
              <time className="shrink-0 text-xs tabular-nums text-slate-400">{e.time}</time>
            </li>
          ))}
        </ol>
      )}
    </div>
  );
}
