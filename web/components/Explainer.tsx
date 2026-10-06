function steps(needed: number, total: number) {
  return [
  {
    n: "1",
    title: "One server is in charge",
    text: "The servers pick one of themselves to be the coordinator. Every change goes through it, so there is never confusion about which change came first.",
  },
  {
    n: "2",
    title: "Changes need a majority",
    text: `Before a change counts, the coordinator copies it to the others and waits until most of them (${needed} out of ${total}) confirm. Only then do you see “Saved”.`,
  },
  {
    n: "3",
    title: "If one fails, the rest carry on",
    text: "When the coordinator stops responding, the remaining servers hold a quick vote and choose a new one. Because every saved change was already on most servers, nothing is lost.",
  },
  ];
}

export function HowItWorks({ needed, total }: { needed: number; total: number }) {
  return (
    <div className="grid gap-4 sm:grid-cols-3">
      {steps(needed, total).map((s) => (
        <div key={s.n} className="rounded-2xl border border-slate-200 bg-white p-5 shadow-sm dark:border-slate-800 dark:bg-slate-900">
          <span className="flex h-8 w-8 items-center justify-center rounded-full bg-indigo-100 text-sm font-bold text-indigo-700 dark:bg-indigo-950 dark:text-indigo-300">
            {s.n}
          </span>
          <p className="mt-3 font-semibold">{s.title}</p>
          <p className="mt-1.5 text-sm leading-relaxed text-slate-600 dark:text-slate-400">{s.text}</p>
        </div>
      ))}
    </div>
  );
}

const GLOSSARY: [string, string][] = [
  ["Server", "Raft node (a kvserver process)"],
  ["Coordinator / In charge", "Raft leader"],
  ["Backup copy", "Raft follower"],
  ["Vote for a new coordinator", "Leader election (RequestVote RPCs)"],
  ["Election round", "Raft term"],
  ["Majority agreed", "Entry committed on a quorum via AppendEntries"],
  ["Label / Note", "Key / value in the replicated map"],
  ["Save · Add to end · Look up", "Put · Append · Get (all go through the Raft log)"],
  ["Paused to protect your data", "No quorum — the cluster cannot commit or elect"],
  ["Switch off · Switch on", "Crash + restart: Raft killed, RPCs refused, rebuilt from persisted state (-demo-controls)"],
];

export function Glossary() {
  return (
    <div className="overflow-hidden rounded-2xl border border-slate-200 bg-white text-sm dark:border-slate-800 dark:bg-slate-900">
      <table className="w-full">
        <thead className="bg-slate-50 text-left text-slate-500 dark:bg-slate-800/60">
          <tr>
            <th className="px-4 py-2 font-medium">On this page</th>
            <th className="px-4 py-2 font-medium">Technical term</th>
          </tr>
        </thead>
        <tbody>
          {GLOSSARY.map(([plain, tech]) => (
            <tr key={plain} className="border-t border-slate-200 dark:border-slate-800">
              <td className="px-4 py-2">{plain}</td>
              <td className="px-4 py-2 font-mono text-xs text-slate-600 dark:text-slate-400">{tech}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
