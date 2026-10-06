import { NodeStatus } from "@/lib/api";
import { NodeConfig } from "@/lib/nodes";
import { serverName } from "@/lib/cluster";

export function ServerCard({
  node,
  status,
  showTech,
  hasMajority,
  powerBusy,
  onPower,
}: {
  node: NodeConfig;
  status: NodeStatus | undefined;
  showTech: boolean;
  hasMajority: boolean;
  powerBusy: boolean;
  onPower: (on: boolean) => void;
}) {
  const online = status?.reachable === true;
  const switchedOff = status?.reachable === false && status.switchedOff === true;
  const canControl = !!status && (status.reachable ? status.demoControls : !!status.demoControls);
  // A leader cut off from the majority still believes it leads, but it can't
  // commit anything, so don't present it as "in charge".
  const rawLeader = online && status.leader;
  const leader = rawLeader && hasMajority;

  let role: string;
  let roleHint: string;
  if (!status) {
    role = "Checking…";
    roleHint = "";
  } else if (switchedOff) {
    role = "Switched off";
    roleHint = hasMajority
      ? "Turned off on purpose. The other servers carry on without it."
      : "Turned off on purpose.";
  } else if (!online) {
    role = "Offline";
    roleHint = hasMajority
      ? "Not responding. The other servers carry on without it."
      : "Not responding.";
  } else if (!hasMajority) {
    role = "Waiting for others";
    roleHint = "Can’t make changes on its own. Waiting for enough servers to come back.";
  } else if (leader) {
    role = "Coordinator";
    roleHint = "Receives every change and makes sure the others copy it.";
  } else {
    role = "Backup copy";
    roleHint = "Keeps an identical copy and can take over if needed.";
  }

  const frame = leader
    ? "border-amber-400 ring-4 ring-amber-200/60 dark:border-amber-500 dark:ring-amber-500/20"
    : online
      ? "border-slate-200 dark:border-slate-700"
      : "border-dashed border-red-300 dark:border-red-800";

  return (
    <div className={`relative flex flex-col items-center rounded-2xl border-2 bg-white p-5 text-center shadow-sm transition-all dark:bg-slate-900 ${frame}`}>
      {leader && (
        <span className="absolute -top-3 rounded-full bg-amber-400 px-3 py-0.5 text-xs font-semibold text-amber-950">
          ★ In charge
        </span>
      )}
      <div className={`flex w-full flex-1 flex-col items-center ${online || !status ? "" : "opacity-60"}`}>
      <ServerIcon online={online} />
      <p className="mt-3 text-base font-semibold">{serverName(node.id)}</p>
      <p className="mt-1 flex items-center gap-1.5 text-sm">
        <span className={`h-2 w-2 rounded-full ${online ? "bg-emerald-500" : status ? "bg-red-500" : "bg-slate-400"}`} />
        <span className={online ? "text-emerald-700 dark:text-emerald-400" : "text-slate-600 dark:text-slate-400"}>
          {online ? "Online" : switchedOff ? "Switched off" : status ? "Offline" : "Checking…"}
        </span>
      </p>
      <p className={`mt-3 text-sm font-medium ${leader ? "text-amber-700 dark:text-amber-400" : "text-slate-700 dark:text-slate-300"}`}>
        {role}
      </p>
      <p className="mt-1 text-xs leading-snug text-slate-500 dark:text-slate-400">{roleHint}</p>

      {showTech && (
        <dl className="mt-4 w-full space-y-0.5 border-t border-slate-200 pt-3 text-left font-mono text-[11px] text-slate-500 dark:border-slate-700 dark:text-slate-400">
          <div className="flex justify-between gap-2"><dt>node id</dt><dd>{node.id}</dd></div>
          <div className="flex justify-between gap-2"><dt>raft role</dt><dd>{!online ? "—" : rawLeader ? "leader" : "follower"}</dd></div>
          <div className="flex justify-between gap-2"><dt>term</dt><dd>{online ? status.term : "—"}</dd></div>
          <div className="flex justify-between gap-2"><dt>api</dt><dd className="truncate">{node.httpAddr.replace(/^https?:\/\//, "")}</dd></div>
          {status && !status.reachable && (
            <div className="flex justify-between gap-2"><dt>error</dt><dd className="truncate">{status.error}</dd></div>
          )}
        </dl>
      )}
      </div>

      {canControl && (
        <button
          disabled={powerBusy}
          onClick={() => onPower(!online)}
          className={`mt-4 w-full rounded-xl px-3 py-2 text-sm font-medium transition disabled:cursor-wait disabled:opacity-50 ${
            online
              ? "border border-red-300 text-red-700 hover:bg-red-50 dark:border-red-800 dark:text-red-400 dark:hover:bg-red-950/40"
              : "bg-emerald-600 text-white hover:bg-emerald-500"
          }`}
        >
          {powerBusy ? (online ? "Switching off…" : "Switching on…") : online ? "⏻ Switch off" : "⏻ Switch on"}
        </button>
      )}
    </div>
  );
}

function ServerIcon({ online }: { online: boolean }) {
  const light = online ? "fill-emerald-500" : "fill-slate-300 dark:fill-slate-600";
  return (
    <svg viewBox="0 0 48 48" className="h-14 w-14" aria-hidden>
      {[6, 19, 32].map((y) => (
        <g key={y}>
          <rect x="6" y={y} width="36" height="10" rx="2.5" className="fill-slate-100 stroke-slate-400 dark:fill-slate-800 dark:stroke-slate-500" strokeWidth="1.5" />
          <circle cx="13" cy={y + 5} r="1.8" className={`${light} ${online ? "animate-pulse" : ""}`} />
          <rect x="20" y={y + 4} width="16" height="2" rx="1" className="fill-slate-300 dark:fill-slate-600" />
        </g>
      ))}
    </svg>
  );
}
