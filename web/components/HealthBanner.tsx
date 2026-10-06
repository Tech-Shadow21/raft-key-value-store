import { ClusterView, serverName } from "@/lib/cluster";

const STYLES = {
  connecting: "border-slate-300 bg-slate-50 text-slate-800 dark:border-slate-700 dark:bg-slate-900 dark:text-slate-200",
  healthy: "border-emerald-300 bg-emerald-50 text-emerald-900 dark:border-emerald-800 dark:bg-emerald-950/60 dark:text-emerald-100",
  degraded: "border-amber-300 bg-amber-50 text-amber-900 dark:border-amber-800 dark:bg-amber-950/60 dark:text-amber-100",
  electing: "border-sky-300 bg-sky-50 text-sky-900 dark:border-sky-800 dark:bg-sky-950/60 dark:text-sky-100",
  stalled: "border-red-300 bg-red-50 text-red-900 dark:border-red-800 dark:bg-red-950/60 dark:text-red-100",
} as const;

const ICONS = { connecting: "…", healthy: "✓", degraded: "!", electing: "↻", stalled: "■" } as const;

export function HealthBanner({ view }: { view: ClusterView }) {
  const { health, online, total, needed, leaderId } = view;
  const offline = total - online;

  let title: string;
  let body: string;
  switch (health) {
    case "connecting":
      title = "Checking on the servers…";
      body = "Contacting each server to see who is online.";
      break;
    case "healthy":
      title = "Everything is working normally";
      body = `All ${total} servers are online and keep identical copies of your data. ${serverName(leaderId!)} is coordinating changes right now.`;
      break;
    case "degraded":
      title = `${offline} server${offline === 1 ? " is" : "s are"} offline — but your data is still safe`;
      body = `The system only needs ${needed} of ${total} servers to agree on a change, and ${online} are online, so saving and reading still work. Any server that comes back will catch up automatically.`;
      break;
    case "electing":
      title = "Choosing a new coordinator…";
      body = "The servers are voting on who should coordinate changes. This normally takes about a second. Nothing is lost in the meantime.";
      break;
    case "stalled":
      title = "Paused to protect your data";
      body = `Only ${online} of ${total} servers ${online === 1 ? "is" : "are"} online, but ${needed} must agree before anything changes. Rather than risk servers disagreeing, the system waits until enough of them are back. Your saved data is not lost.`;
      break;
  }

  return (
    <div className={`flex gap-4 rounded-2xl border p-5 ${STYLES[health]}`} role="status" aria-live="polite">
      <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-full bg-white/70 text-lg font-bold dark:bg-black/30">
        {ICONS[health]}
      </div>
      <div>
        <p className="text-lg font-semibold">{title}</p>
        <p className="mt-1 text-sm leading-relaxed opacity-90">{body}</p>
      </div>
    </div>
  );
}
