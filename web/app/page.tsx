"use client";

import { useEffect, useRef, useState } from "react";
import { getNodes } from "@/lib/nodes";
import { fetchStatus, NodeStatus, setPower } from "@/lib/api";
import { describeCluster, Health, serverName } from "@/lib/cluster";
import { HealthBanner } from "@/components/HealthBanner";
import { ServerCard } from "@/components/ServerCard";
import { Notebook } from "@/components/Notebook";
import { ActivityFeed, EventKind, FeedEvent } from "@/components/ActivityFeed";
import { Glossary, HowItWorks } from "@/components/Explainer";

const POLL_MS = 1000;
const nodes = getNodes();
const ids = nodes.map((n) => n.id);

export default function Dashboard() {
  const [statuses, setStatuses] = useState<Record<number, NodeStatus>>({});
  const [events, setEvents] = useState<FeedEvent[]>([]);
  const [showTech, setShowTech] = useState(false);
  const [powerBusy, setPowerBusy] = useState<number | null>(null);
  const nextEventId = useRef(0);

  const view = describeCluster(ids, statuses);

  function addEvents(items: { kind: EventKind; msg: string }[]) {
    if (items.length === 0) return;
    const time = new Date().toLocaleTimeString([], { hour: "numeric", minute: "2-digit", second: "2-digit" });
    // Items arrive oldest-first; the feed is newest-first.
    const stamped = items.map((e) => ({ ...e, time, id: nextEventId.current++ })).reverse();
    setEvents((l) => [...stamped, ...l].slice(0, 50));
  }

  useEffect(() => {
    let cancelled = false;
    const poll = async () => {
      const results = await Promise.all(nodes.map((n) => fetchStatus(n)));
      if (cancelled) return;
      setStatuses(Object.fromEntries(results.map((r) => [r.id, r])));
    };
    poll();
    const id = setInterval(poll, POLL_MS);
    return () => {
      cancelled = true;
      clearInterval(id);
    };
  }, []);

  // Turn raw status flips into plain-English events, so a failover reads as a
  // story ("Server 3 stopped responding… Server 2 is now in charge") rather
  // than a table of numbers changing.
  const prev = useRef<{ statuses: Record<number, NodeStatus>; leaderId: number | null; health: Health } | null>(null);
  useEffect(() => {
    if (view.health === "connecting") return;
    const p = prev.current;
    const out: { kind: EventKind; msg: string }[] = [];

    if (!p) {
      out.push({
        kind: "info",
        msg: `Connected. ${view.online} of ${view.total} servers are online${
          view.leaderId !== null ? ` and ${serverName(view.leaderId)} is in charge` : ""
        }.`,
      });
    } else {
      for (const id of ids) {
        const was = p.statuses[id];
        const now = statuses[id];
        if (was?.reachable && !now?.reachable) {
          const what = now?.switchedOff ? "was switched off" : "stopped responding";
          out.push(
            p.leaderId === id
              ? { kind: "bad", msg: `${serverName(id)}, the coordinator, ${what}. The others will vote for a new one.` }
              : { kind: "warn", msg: `${serverName(id)} ${what}. The others keep working without it.` }
          );
        } else if (was && !was.reachable && now?.reachable) {
          out.push({
            kind: "good",
            msg: `${serverName(id)} ${was.switchedOff ? "was switched back on" : "is back online"} and is catching up on anything it missed.`,
          });
        }
      }
      if (view.leaderId !== null && view.leaderId !== p.leaderId) {
        out.push({
          kind: "good",
          msg:
            p.leaderId === null
              ? `${serverName(view.leaderId)} won the vote and is now in charge.`
              : `${serverName(view.leaderId)} is now in charge (taking over from ${serverName(p.leaderId)}).`,
        });
      }
      if (view.health === "stalled" && p.health !== "stalled") {
        out.push({ kind: "bad", msg: `Too few servers are online, so changes are paused until at least ${view.needed} are back. Saved data is kept.` });
      } else if (p.health === "stalled" && view.health !== "stalled") {
        out.push({ kind: "good", msg: "Enough servers are back online — changes are allowed again." });
      }
    }

    addEvents(out);
    prev.current = { statuses, leaderId: view.leaderId, health: view.health };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [statuses]);

  async function power(id: number, on: boolean) {
    setPowerBusy(id);
    try {
      await setPower(nodes.find((n) => n.id === id)!, on);
    } catch (e) {
      addEvents([{ kind: "bad", msg: `Couldn’t switch ${serverName(id)} ${on ? "on" : "off"} (${e instanceof Error ? e.message : String(e)}).` }]);
    } finally {
      // Hold the button until the next poll reflects the change.
      setTimeout(() => setPowerBusy(null), POLL_MS);
    }
  }

  const demoControls = Object.values(statuses).some((s) => s.demoControls);
  const leaderNode = nodes.find((n) => n.id === view.leaderId) ?? null;
  const anyOnline = nodes.find((n) => statuses[n.id]?.reachable) ?? null;

  return (
    <div className="min-h-screen bg-slate-50 text-slate-900 dark:bg-slate-950 dark:text-slate-100">
      <div className="mx-auto max-w-5xl space-y-12 px-4 py-10 sm:px-6">
        <header className="flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
          <div>
            <h1 className="text-3xl font-bold tracking-tight">Shared Notebook</h1>
            <p className="mt-2 max-w-2xl leading-relaxed text-slate-600 dark:text-slate-400">
              Your notes are stored on {view.total} separate servers at once. They work as a team and keep identical copies,
              so if one of them breaks, nothing is lost and the notebook keeps working. This page shows what they are doing, live.
            </p>
          </div>
          <label className="flex shrink-0 cursor-pointer items-center gap-2 text-sm text-slate-600 select-none dark:text-slate-400">
            <input type="checkbox" checked={showTech} onChange={(e) => setShowTech(e.target.checked)} className="h-4 w-4 accent-indigo-600" />
            Show technical details
          </label>
        </header>

        <HealthBanner view={view} />

        <Section
          title="The servers"
          subtitle={
            demoControls
              ? "Each box is one computer holding a full copy of the notebook. Try switching off the one in charge and watch what happens."
              : "Each box is one computer holding a full copy of the notebook."
          }
        >
          <div className="grid gap-5 pt-3 sm:grid-cols-3">
            {nodes.map((n) => (
              <ServerCard
                key={n.id}
                node={n}
                status={statuses[n.id]}
                showTech={showTech}
                hasMajority={view.online >= view.needed}
                powerBusy={powerBusy === n.id}
                onPower={(on) => power(n.id, on)}
              />
            ))}
          </div>
        </Section>

        <Section title="Try it: write in the notebook" subtitle="Give your note a label, then save it. You can look it up again any time — even after a server fails.">
          <Notebook target={leaderNode ?? anyOnline} view={view} onEvent={(kind, msg) => addEvents([{ kind, msg }])} />
        </Section>

        <Section title="What’s happening" subtitle="A plain-English log of everything the servers do, newest first.">
          <ActivityFeed events={events} />
        </Section>

        <Section title="How it works" subtitle="Three simple rules keep every copy identical.">
          <HowItWorks needed={view.needed} total={view.total} />
        </Section>

        {showTech && (
          <Section title="Technical glossary" subtitle="How the words on this page map to the Raft consensus algorithm underneath.">
            <Glossary />
          </Section>
        )}

        <footer className="pb-4 text-center text-xs text-slate-400">
          Updates every {POLL_MS / 1000} second{POLL_MS === 1000 ? "" : "s"} · built on the Raft consensus algorithm
        </footer>
      </div>
    </div>
  );
}

function Section({ title, subtitle, children }: { title: string; subtitle: string; children: React.ReactNode }) {
  return (
    <section>
      <h2 className="text-xl font-semibold">{title}</h2>
      <p className="mt-1 mb-4 text-sm text-slate-600 dark:text-slate-400">{subtitle}</p>
      {children}
    </section>
  );
}
