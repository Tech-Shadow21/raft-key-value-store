# Dashboard

A Next.js app that shows a Raft Key Value Store cluster live, in plain language.
It polls each server's JSON API once a second and talks to the servers directly
from the browser.

## Running

Start the servers with `-http` (and optionally `-demo-controls`) as described in the
main [README](../README.md), then:

```bash
cp .env.local.example .env.local   # edit if your -http addresses differ
npm install
npm run dev                        # http://localhost:3000
```

`NEXT_PUBLIC_KV_NODES` lists each server as `id=http://host:port`, comma separated.

## Code layout

| Path | Purpose |
| --- | --- |
| `app/page.tsx` | Page layout, status polling, and the plain language event log |
| `components/HealthBanner.tsx` | One sentence summary of cluster health |
| `components/ServerCard.tsx` | One card per server, with optional power button and technical details |
| `components/Notebook.tsx` | Save, add to, and look up notes (Put, Append, Get) |
| `components/ActivityFeed.tsx` | Event log |
| `components/Explainer.tsx` | How it works cards and the technical glossary |
| `lib/api.ts` | Calls to the server JSON API, with timeouts |
| `lib/cluster.ts` | Derives overall health from per server status |
| `lib/nodes.ts` | Reads server addresses from the environment |
