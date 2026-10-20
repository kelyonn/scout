# ADR-019: Cardless hosted pipeline — GitHub Actions + Neon + Upstash + Render + Vercel

**Status:** Accepted
**Date:** 2026-10-20
**Supersedes:** [ADR-018](ADR-018-laptop-only-hosting.md)'s "laptop-only, no remote host"

## Context

ADR-018 removed the always-on host because every VM free tier (Oracle, AWS, GCP,
Azure) needs a card for verification. The cost of that decision showed up in use:
the app only works after the user starts the Compose stack by hand, the phone
can only reach it over Tailscale to the laptop, and the overnight window that is
Scout's reason to exist is unserved. The user has used it very little since.

ADR-018 also named the way out: stop requiring an always-on *host*. The pipeline
is already built for that. Every durability-critical piece of state is in
Postgres or Redis (source circuit breakers and `next_poll_at`, rate-limit token
buckets, notification dedup), the collector and notifier are loops around
one-shot units (`Scheduler.RunOnce`, `runTick`), and queue consumers claim jobs
with `FOR UPDATE SKIP LOCKED`. A fresh process per run loses nothing.

## Decision

Everything runs on free tiers that need **no card**:

| Piece | Where | Notes |
| --- | --- | --- |
| Postgres | Neon free | scale-to-zero compute; first query after idle is slower |
| Redis | Upstash free | holds nothing durable (rule 8); rate limits and robots cache only |
| collector, brain, notifier | GitHub Actions, `ingest.yml`, every 15 min | free and unlimited because the repo is public; best-effort timing |
| api | Render free web service | sleeps after ~15 min idle; ~1 min cold start on the first request |
| web | Vercel free | Next.js, proxies to the api with the bearer token server-side |

The three workers gain a bounded run-once mode: `collector -once`,
`notifier -once`, `python -m scout_brain.worker --once`. Each does its work and
exits; the workflow chains them. The collector's compliance gate, self-check and
rate limits are unchanged and still on every path (rule 1).

The laptop Compose stack remains the development environment and the fallback.
`infra/compose/production.yml` and the Tailscale deploy scripts are unchanged and
still valid if a host ever becomes available.

## Consequences

- **Works with the laptop off**, including overnight. Notifications fire from
  Actions regardless of whether the api is awake.
- **Latency:** a new posting is seen within roughly one 15-minute cycle plus
  scheduler jitter, not seconds. Docs/11's latency budget assumed continuous
  polling; for a job search that is acceptable.
- **Dead-man's switch:** the "heartbeat stops" model assumed a long-lived
  process. Under this ADR a failed or missing scheduled run is the signal;
  GitHub emails on failed workflows, and `probe.yml` remains an independent check.
- **Ollama is not available** in Actions or on Render. Per rule 6, features that
  needed it (`summarize`, `explain`) degrade; dedup adjudication and Tier 2
  classification use the hosted free-tier cascade (ADR-016) when keys are set.
  Rule 9 is unaffected: resume text is never sent to a hosted model.
- **The user's data now lives at Neon, and the api and web are on the public
  internet** behind the existing bearer-token login. Rule 9 is about LLMs and still
  holds, but this is a real change from "never leaves the laptop". Secrets are
  GitHub/Render/Vercel encrypted variables only, never committed (rule 10).
- Email-alert ingestion (long-lived IMAP) is not part of `-once` and only runs in
  the long-running collector.
- Free tiers can be withdrawn. Every component is replaceable by its Compose
  equivalent without code changes, which is the portability guarantee ADR-014
  established.

## Reversal triggers

Move the whole pipeline to a real always-on host (spare hardware, or a provider
that accepts the user) if: scheduled runs are being dropped often enough that
the 24h-fresh guarantee in docs/06 fails, Neon's compute-hour cap is hit
regularly, or either of Neon/Upstash/Render withdraws its no-card free tier.
