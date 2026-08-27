# Session Handoff

Read this first, before `docs/19-roadmap.md`, when picking work back up. It
describes what actually exists right now — the roadmap describes what's
intended. When they disagree, this file is more current and the roadmap's
checkboxes should be trusted less than the prose here.

**Last updated:** 2026-08-27 — see that date's own update at the bottom of
this file for what's new: a real, previously-nonexistent `raw_observation`
partition-maintenance job, a `discover`-tool SSRF gap and a Stage-3 dedup
advisory-lock gap both closed, and the second P5 ATS adapter —
**Zoho Recruit** — built and verified live against three real tenants,
including the first concrete `services_it` company (SID Global Solutions)
toward this file's own long-flagged IT-consultancy coverage gap. Everything
before that, through the 2026-08-26 update, was pushed to GitHub already.
The partition-maintenance/discover-SSRF/dedup-lock fixes are committed
locally to `main` but not yet pushed; the Zoho Recruit work is uncommitted
in the working tree as of this writing (see that section's own closing
note).

---

## Phase: P1 done, P2 done, P3 done

See [`docs/19-roadmap.md`](docs/19-roadmap.md) for what each milestone means.
Full P0–P3 status detail is annotated inline in that document now; this is the
short version.

**P0 — It runs.** Repo side done, and as of 2026-08-19 that's now the whole of
P0: [ADR-018](docs/adr/ADR-018-laptop-only-hosting.md) drops the Oracle/
Tailscale requirement entirely (Oracle needs a card that isn't available) in
favor of running the same Compose stack locally, on demand, with real fetching
— no host, no deploy step, no dead-man's switch. What's left is the user
filling in real credentials in `.env` and running it, plus the `age` backup key
on offline media, neither of which is code.

**P1 — First real notification.** Code-complete: politeness gate, SSRF-safe
fetcher, change detection, adaptive scheduler, Greenhouse/Lever/Ashby
adapters (with fixtures), Tier-0 classification, Stage-1 dedup, crude
scoring, Telegram notifier, all four catastrophe tests. ~2,267 seed sources
(target was 60). What's left is exclusively runtime verification — 24h+ of
actually polling — which needs a deployment, not more code.

**P2 — Worth reading.** Built: Python service (`apps/brain`), local
embeddings, Tier-1/Tier-2 classification, the `advocacy` role family, company
taxonomy, dedup Stages 2 and 3, 6 of the 7 P2-scoped scoring subscores (a
couple of these — `competition_estimate`, `ease_of_applying` — were actually
scoped for P5 and got built early). As of 2026-08-18, the three remaining
gaps are closed:
- **The ADR-016 provider cascade is built** (`apps/brain/scout_brain/llm.py`
  — `CascadeClient`, rate-limited hosted providers for Gemini/Groq/OpenRouter,
  local Ollama fallback, config in `infra/config/llm_providers.yaml`). No
  hosted provider is actually configured anywhere yet (no API keys signed up
  for), so every real run today still uses the local-only fallback tier —
  the cascade code path with a hosted provider present is unit-tested but not
  yet exercised in production.
- **`TaskExplain` is implemented** (`apps/brain/scout_brain/explain.py`).
  `apps/collector/internal/scoring.Explain` writes a deterministic template
  synchronously at score time (so a notification is never sent unexplained);
  `TaskExplain` upgrades it to an LLM-generated, personalized sentence
  asynchronously. Deliberately uses a **local-only** Ollama client, never the
  hosted cascade — the prompt draws on `job_score`'s resume-derived match
  data, and AGENTS.md rule 9 requires that stay on the host.
- **`evals/` has a real harness** — `evals/run.py`/`evals/report.py`, three
  suites (`classify_role`, `dedup`, `explanation`) each calling the actual
  production prompt, golden sets in `evals/golden/`, wired into CI as the
  `evals` job. **Read `evals/README.md`'s "Honest current state" section
  before trusting a green run** — at today's local-only tier, `classify_role`
  and `dedup` recall do not clear the docs/17 gate thresholds. That is a real,
  measured quality gap (a 3B local model versus the hosted models Tier 2 is
  designed around), not a broken harness.

**P3 — Daily driver.** Code-complete. Built and browser-verified against real
data as of 2026-08-18:
- The full save/applied/interviewing state machine (`infra/migrations/
  000014_user_job_state.up.sql`, `POST /v1/jobs/{group_id}/state`,
  `GET /v1/applications`) — an explicit transition graph in
  `apps/api/internal/jobs/state.go`, invalid transitions return 409, 24
  tests against a real Postgres.
- **The "I found this elsewhere first" control**, captured at the moment
  of clicking Apply, sticky once set true (`user_job_state.found_elsewhere_first`)
  — the whole Scout-first SLO instrument (docs/16 §2.1).
- A Pipeline view (`/pipeline`) — Saved/Applied/Screening/Interviewing/
  Offer columns plus a collapsed Archive, a stale-application nudge past
  14 days, and a `<select>` state-change menu on every card (the
  keyboard-accessible alternative docs/12 §4.5 requires — no drag-and-drop
  was built, so this menu is the only way to move a card, not a fallback).
- Save/Dismiss/Apply buttons on every job card and the detail page, with
  card-state styling (saved = accent border, applied = muted, dismissed =
  collapsed one-liner with undo).
- Keyword search (`GET /v1/search`, `/search` page) over `job.search_vector`
  (a generated, GIN-indexed tsvector) plus company name. `mode=semantic`/
  `hybrid` are accepted but degrade to keyword — a real query embedding
  needs a Go→Python call ADR-001 forbids synchronously, unbuilt.
- Job-detail/feed/applications API responses all carry `state` now.

Also built since (2026-08-18, same session, continued):
- **SSE live feed** (`GET /v1/stream`) — Postgres LISTEN/NOTIFY via a
  single-process `Broker`, in-memory ring buffer for Last-Event-ID replay,
  30s heartbeat. `apps/collector` publishes `job.new` on every genuinely
  new job; `job.score_updated`/`notification.sent` have no write path yet.
  Browser-verified end to end (a real `pg_notify` reached a real
  `EventSource` in the Opportunities page with zero reload).
- **Daily digest to Telegram**, 08:00 IST — checked every notifier tick
  (cheap no-op outside the window), not a separate cron job. Overnight
  jobs, closing-soon tracked deadlines, weekly applied/interviewing/pending
  counts. Deliberately missing the week-over-week response-rate trend and
  the "One thing" skill-gap coaching line (P5 scope).
- **The remaining 5 ATS adapters** — Workable, SmartRecruiters, Recruitee,
  Teamtailor, Workday, all registered in `apps/collector/cmd/main.go`.
  SmartRecruiters and Workday paginate (dependency-injected fetch function
  for testability — `packages/fetch.Fetcher`'s SSRF guard can't safely
  target a local test server from another package). Workday needed POST
  support, added to `packages/fetch.Request` (`Method`/`Body`/
  `ContentType`, default GET, fully backward compatible) with its own
  tests. **Teamtailor and Workday are unverified against a live board** —
  built from the documented shape, not a captured real response, unlike
  every other adapter; `make fixtures-diff` against a real board is the
  next step before trusting them the way the other six are trusted.
  Workable/SmartRecruiters/Workday also can't fetch full descriptions
  without an N+1 per-posting request, so `DescriptionHTML` stays honestly
  empty for those three — documented in each package comment.

- **IMAP email-alert ingestion** (`apps/collector/internal/emailalert`,
  `apps/collector/internal/scheduler/email.go`) — the docs/05 design was a
  webhook (`POST /api/v1/hooks/email`), which turned out to be unbuildable
  under ADR-014 (no public ingress); the actual implementation polls an
  IMAP mailbox instead, same shape as the notifier's own Telegram
  `getUpdates` long-poll. Docs/05 has been corrected to describe the poll,
  not the webhook.
  - `emailalert.Poller` logs in, searches `UNSEEN`, and hands every posting
    from a matched sender to a `Handler`. There is no persisted cursor —
    IMAP's own `\Seen` flag (set implicitly by fetching a message's body
    without `PEEK`) is the checkpoint.
  - Four provider configs — LinkedIn, Indeed, Glassdoor, Handshake — all
    one `regexProvider{name, senderPattern, linkPattern}` value sharing a
    single regex-based extractor (`regexprovider.go`), not four bespoke
    parsers. **All four are unverified against a live alert email** — same
    calibration caveat already carried by the Teamtailor/Workday ATS
    adapters, and for the same reason: built from the platforms'
    documented/observed link shapes, not a captured real message. `docs/14
    §5` singles out Handshake by name: it's the one provider where email
    alerts are the *only* acceptable path (university-linked account),
    not just the cheapest one.
  - `Scheduler.IngestEmailAlert` is the write path: find-or-create the
    company (slug derived from the company name, so the same employer
    named consistently collides on conflict rather than duplicating) and
    source (`legal_posture = 'email_only'`, which is what keeps the normal
    HTTP scheduler's `SelectDueSources` from ever polling it), resolve the
    tracking redirect to a canonical URL through the same SSRF-guarded
    fetcher every adapter uses, then reuse `processPosting` directly —
    the exact normalize→classify→dedup→score→write pipeline every
    HTTP-sourced posting goes through, not a parallel reimplementation.
  - Known, accepted gap: no cross-reference to a known ATS source to
    re-fetch the richer posting (that was in the original docs/05 design,
    never implemented) — the job is written from what the alert email
    itself contains. Also: a company matched here that already exists
    under a differently-derived slug from another source becomes a second
    company row — company-identity merging across sources is a separate,
    harder problem this didn't need to solve to be useful.
  - `SCOUT_EMAIL_IMAP_HOST/PORT/USER/PASSWORD/MAILBOX` in `.env.example`.
    All-empty is a valid disabled state (mirrors `heartbeat.Pinger`/
    `telegram.Client`'s own `Enabled()` pattern) — the collector runs fine
    with no mailbox configured, same as today.
  - 20 new tests (`emailalert`'s own package plus `scheduler/email_test.go`),
    including a real end-to-end run against Postgres proving company/source
    creation, `legal_posture = 'email_only'`, a real job + job_score row,
    and idempotent re-ingestion of the identical posting.

- **GCC coverage** (`infra/seed/gcc_sources.sql`, `make seed-gcc-sources`) —
  39 of docs/05's ~150-entry target (21 verified 2026-08-18; 18 more
  across three rounds 2026-08-19: Cisco, J&J, Walmart Global Tech, HP,
  Deutsche Bank, Barclays, 3M, Medtronic, Johnson Controls, Applied
  Materials, Illumina, Danaher, Northern Trust, Baxter, State Street,
  Stryker, Abbott, AIG), each individually verified live against a real
  tenant (not guessed from a brand name — a wrong tenant slug is a
  permanently-broken source row). 37 Workday, 2 SmartRecruiters (Nagarro
  and Bosch — docs/05 originally assumed Workday for these and was
  wrong; found on SmartRecruiters instead). Per-tenant yield is thinning
  round over round (2-20 new postings per company now vs. 50-250+ in the
  first two rounds) — expected, since the largest employers were found
  first, not a sign the method stopped working. All
  `status = 'pending_review'`, same 48-hour review convention as every
  other seeded source.
  - **Found and fixed a real production bug in the process**: the
    scheduler's poll loop performed its own plain conditional GET for
    every source regardless of adapter, and never called an adapter's own
    `Fetch` in production at all — only `apps/collector/internal/discovery`
    did, for candidate assessment. Harmless for every adapter through P2
    (a plain GET is exactly what Greenhouse/Lever/Ashby/Workable/
    SmartRecruiters/Recruitee/Teamtailor need), but Workday's CXS endpoint
    400s on a bare GET — it requires POST with a JSON body. Every Workday
    source, including this task's own 19, would have failed every poll
    forever, unnoticed, until this was found. Fixed via a new
    `padapter.OwnFetcher` interface (`RequiresOwnFetch() bool`) that
    `Scheduler.fetchResult` checks before falling back to the generic GET
    — narrow and opt-in, so the other seven adapters and their existing
    fixture-based tests are untouched. `packages/adapter.RawResponse`
    gained a `RetryAfter` field so a 429 reaching the scheduler this way
    still drives the existing backoff logic. See docs/06 section 8's "Who
    actually calls Fetch" and docs/05 section 5.2 for the full writeup.
  - **Location narrowing is `search_text`, not a per-tenant facet ID**:
    `adapters/ats/workday.Fetch` reads `source.adapter_config["search_text"]`
    and sends it as Workday's own full-text search on every page —
    verified live to turn Lowe's 11,961-posting board into 56
    Bengaluru-matching results in one request, docs/05's literal "3
    requests instead of 3,000." A true per-tenant facet ID is more
    precise (obtained for two tenants by driving each one's own search UI
    and reading the resulting `locations=` parameter) but needs that same
    manual discovery per tenant; `search_text` generalizes with zero
    per-tenant curation cost, at the honest price of missing a posting
    spelled "Bangalore" instead of "Bengaluru."
  - Verified end-to-end against a live seeded row with the real fetcher
    and real adapter (not a mock): 62 real, current Bengaluru postings
    parsed correctly from Lowe's actual board.
  - Not yet true: the exit criterion "≥100 GCC entities polled" — these
    21 are seeded and proven fetchable, not yet promoted to `active` (that
    promotion is a deliberate, reviewed action per CONTRIBUTING.md, same
    as every other seeded source) and reaching 100+ needs more curated
    entries, not more code — `infra/seed/gcc_sources.sql`'s own closing
    comment lists exactly which attempted candidates didn't resolve and
    why, as a starting point for the next pass.

- **Observability stack** (`infra/compose/observability.yml`,
  `make observability-up`) — Prometheus, Loki, Promtail, Tempo, the
  otel-collector, and Grafana, all real and verified running against the
  local stack (not just written and untested): Prometheus scraping real
  metrics from `api`/`collector`/`brain` (real `scout_queue_depth`,
  `python_*` process metrics confirmed via direct query), Loki receiving
  real structured JSON log lines via Promtail's Docker service discovery,
  a real OTel trace (`apps/api`'s HTTP requests, via `otelhttp`) landing
  in Tempo end-to-end, and Grafana provisioned with all three datasources
  plus the Overview dashboard (logged in and screenshotted with real
  panel titles rendering — see `docs/16-observability.md`'s own "Build
  status" section for exactly which subset of that document's full scope
  this covers vs. defers, e.g. no cross-service trace propagation, no
  dashboards 2-6, no alerting).
  - **A second real, previously-unnoticed bug found and fixed along the
    way**: `source.yield_ratio` — the column `interval.Compute`'s own
    `yield_factor` input reads — had never actually been written since
    schema creation. It silently defaulted to 0 for every source through
    P1 and P2, which pins `yield_factor` at its maximum (fastest-poll)
    end regardless of a source's real yield. Fixed via an EMA (`yield_ratio
    = yield_ratio * 0.99 + (new_jobs > 0 ? 1 : 0) * 0.01`) computed in the
    same `UpdateSourceAfterPoll` write every poll already goes through —
    see `packages/db/queries/source.sql`'s own comment, and
    `TestUpdateSourceAfterPoll_YieldRatioTracksRecentPolls` for the proof.
  - Networking note for anyone running this locally: `local.yml` and
    `production.yml` both got their own implicit/project-scoped Docker
    network renamed to a literal, predictable name (`scout-local` /
    `scout`) so `observability.yml` — a separate file, merged in via
    `-f` — can join the same real network and resolve `api`/`collector`/
    etc. by name. This is a real, tested, necessary change (verified with
    `docker compose config` and a real `up` in both directions), not
    incidental — see `observability.yml`'s own header comment for the
    full reasoning, including why it deliberately has no `name:` key of
    its own (to avoid silently pointing a merged run at a second,
    disconnected `postgres_data` volume).
  - Grafana has no published host port anywhere (production's own
    "nothing publishes a host port" rule, kept intact for this stack
    too) — production access is `infra/caddy/Caddyfile`'s new `/grafana/*`
    route; local access without Caddy running needs a manual tunnel (the
    compose file's own comment shows one).
  - Not built: Sentry and Uptime Kuma (external, hosted — need an account
    this repo can't provision), the full alert-rule catalog, dashboards
    2-6, PII log scrubbing, and full pipeline trace propagation through
    the queue into `apps/brain`/`apps/notifier`.

---

## Suggested next step

P3 is done — every subsystem docs/19-roadmap.md's P3 section lists
(feed/detail/pipeline, state machine, SSE, digest, all 8 ATS adapters,
email-alert ingestion, GCC coverage, observability) is built. What's next
is P4 (native app shell, docs/19-roadmap.md) or closing the honestly-
documented gaps this phase left behind rather than starting new scope —
see this file's own "Not built" notes under Task 10 (ATS cross-reference
for email postings), Task 11 (the remaining ~130 GCC entities), and Task
12 (cross-service tracing, dashboards 2-6, alerting) for the concrete
list. None of P3's own exit criteria (900+ active sources, Scout-first
rate, notification precision, a full week actually used) can be checked
yet — those need real, sustained runtime on a deployed host, which is
P0's own remaining item, not more code.

If closing out P2 fully first is preferred instead: sign up for at least one
free hosted LLM provider (Gemini/Groq/OpenRouter) and add its key as a
`SCOUT_BRAIN_*_API_KEY` env var (locally) or GitHub Actions secret (CI) — see
`infra/config/llm_providers.yaml` — then re-run `make evals` to see whether
`classify_role`/`dedup` clear their gates at that tier. That measurement
hasn't been taken yet.

**Update, 2026-08-19 (later the same day):** everything above described as
"uncommitted since `4645c22`" is now committed, in twelve chunked commits by
subsystem plus two follow-up bug-fix commits — nothing described as "built" in
this document is uncommitted anymore. Along the way:

- **A real PII leak was caught before it happened**: a fixture PDF and a seed
  SQL script both contained the user's actual resume (name, phone, email, work
  history). Both are now gitignored with safe templates in their place —
  see `apps/api/internal/resume/fixtures/` and `infra/seed/resume.sql.example`.
- **`mypy --strict` was wired up** for `apps/brain`, `evals/`, and
  `packages/riverpy` — AGENTS.md required it but it was never installed or run
  anywhere. Fixed all 135 real errors this surfaced, including a real
  concurrent-dedup crash (`dedup_stage3.py`'s `_merge_job_groups`) and a real
  eval-harness correctness bug (a missing LLM field silently corrupting
  `classify_role`'s precision/recall stats). `packages/riverpy` also had its
  own test suite that ran nowhere (not CI, not Makefile) — wired in.
- **A flaky scheduler test** was root-caused (cross-package test pollution via
  the shared local Postgres) and fixed properly rather than retried around.
- **`apps/web` had zero CI coverage** — added lint/typecheck/build. Its one real
  bug (a ref mutated during render in `useLiveFeed`) is fixed and browser-verified.
- **[ADR-018](docs/adr/ADR-018-laptop-only-hosting.md) is new**: Oracle Cloud
  needs a card the user doesn't have, so P0 now runs laptop-only — no remote
  host, no Tailscale, the same Compose stack run locally with real fetching
  whenever the user chooses to (`live` environment, `docs/15-infrastructure-
  deployment.md`). This is a real, named tradeoff (loses the overnight
  coverage window the project's own PRD says is the reason it exists), not a
  free simplification — see the ADR for the full reasoning and reversal
  conditions.

Nothing has been pushed to GitHub yet — everything above is local to `main` in
this working tree.

**Update, still 2026-08-19 (later still):** `golangci-lint run` had never
actually been run against the P1-P3 code either — 43 issues, all fixed (see
that commit for the breakdown; a few were real, not style: an unvalidated
query param that could silently wrap `int16`, and two dedup functions
returning unwrapped errors). Also: **Teamtailor and Workday are now both
verified against live boards, and the Teamtailor verification found a real
bug, not just confirmed the adapter worked.** The real Teamtailor response
is a JSON Feed 1.1 document — `{"version", "items": [...]}` — not the bare
kebab-case array the adapter was built against; that version would have
failed to parse every real Teamtailor source, 100% of the time, silently
until the first live poll. Rewritten and reverified against two independent
real boards (career.teamtailor.com, southpole.teamtailor.com — 30 real
postings parsed and validated correctly). Workday was reconfirmed working
against Lowe's live board today (64 current Bengaluru postings through the
real Fetch/Parse/Validate path, not fixtures) — it was already verified
2026-08-18 during GCC seeding, this just checked it still holds. Both
adapters' doc comments and `adapters/README.md` are updated accordingly.

---

## Housekeeping / things to know before touching this repo

- **Everything is committed.** The `fetch`/`ssrf` refactor that was mid-flight
  is finished and committed. The stray `discover` binary is deleted and
  `.gitignore` now excludes build artifacts at the repo root, plus the two
  real-resume files (see the PII note above).
- `go build ./...`, `go test ./... -count=1`, `make lint-py` (ruff + mypy
  --strict across `apps/brain`/`evals`/`packages/riverpy`), and `make
  lint-web` (eslint + tsc --strict) are all clean as of this writing.
- **Nothing has been pushed to GitHub.** All of the above is local to `main`
  in this working tree — say the word when it should go up.
- The repository is public — resume, application history, seed lists, and
  anything from `.env` must never be committed. This almost happened this
  session (see the PII note above); double-check before `git add`-ing
  anything under `infra/seed/` or a resume-adjacent fixtures directory.

**Update, still 2026-08-19 (later still):** two more items closed. The
PII log-scrubbing middleware docs/16-observability.md required but never
had — built for both Go (`packages/logging.Scrub`, wired into all four Go
binaries) and Python (`scout_brain.logging_scrub.ScrubbingFilter`), with
tests seeding known PII patterns and a named regression test for a real
false-positive a naive phone-number regex would have caused (matching
UUID/content-hash digit runs). And GCC coverage is now 29 of ~150, not
21 — 8 more Workday tenants live-verified today (Cisco, J&J, Walmart
Global Tech, HP, Deutsche Bank, Barclays, 3M, Medtronic), including
finding and fixing a real error in the prior pass: Walmart's tenant was
marked "not resolved, transient outage" when it was actually just the
wrong Workday subdomain number (wd504, not wd5).

**Update, still 2026-08-19 (end of session):** three more real bugs found
and fixed, none of them known before this pass:

- **The digest's "closing soon — N days left" countdown used the real
  system clock instead of its own injected clock parameter**
  (`apps/notifier/internal/digest/digest.go`'s `render(nowIST, ...)`
  called `time.Until()` — `time.Sub(time.Now())` — not
  `.Sub(nowIST)`). Not just a test-determinism bug: if `render()` ever
  ran even slightly after the data it renders was queried, the countdown
  would silently drift from the digest's actual as-of time in
  production. Found because real wall-clock crossed midnight mid-session
  and broke a test; traced to the actual bug instead of just re-running.
  Two more tests were exposed as a result — both had been passing by
  accident, silently testing the bug's behavior rather than the
  feature's.
- **Grafana's Prometheus datasource has pointed at the wrong port since
  it was first provisioned.** `infra/compose/observability.yml`'s
  prometheus service deliberately listens on 9091 (9090 collides with
  every Scout service's own `/metrics` port on the same network), but
  `datasources.yml` still said 9090. The Overview dashboard has never
  actually shown real data — every panel read "No data" with a
  connection-refused error one click away, which looks exactly like "no
  traffic yet," not "broken," so nobody noticed. Fixed, and verified
  against the real running stack (not just JSON review): brought the
  observability stack up, confirmed the datasource connects, loaded the
  dashboard in a browser, confirmed panels render.
- Built the **Ingestion dashboard** (`infra/grafana/dashboards/
  ingestion.json`, docs/16 section 8's dashboard 2) with everything it
  asks for that has a real metric or column today — per-source-kind
  success rate/yield/304-ratio/duration from Prometheus, plus a new
  **Postgres datasource** for two tables (open circuit breakers; 20
  worst-performing sources by yield) that Prometheus has no per-row data
  for. Dashboards 3-6 are deliberately still not built — their core
  metrics (pipeline stage timings, dedup-merge counts, LLM cost/cache
  rate, notification fatigue counters, container/Postgres exporters)
  don't exist yet, and a dashboard pointed at metrics nobody emits is a
  worse state than no dashboard.
- Verified end-to-end (not just config review) that logs, metrics, and
  traces are all genuinely flowing: real traces in Tempo for `apps/api`'s
  `/health` and `/metrics` handlers, real `scout_api_request_duration_seconds`
  data in Prometheus (3,987 real health-check observations), and real
  structured logs in Loki from every running container once queried over
  a wide enough time range (Loki's label-values endpoint defaults to a
  1-hour lookback, which briefly looked like several containers' logs
  were missing — they weren't, they were just quiet in the last hour).
  One transient Loki ring hiccup (~19:01-20:15) self-healed on its own;
  logged here in case it recurs, not fixed because it wasn't reproducible.

GCC coverage expansion (21 → 39) also happened this session — see the
update above this one for the count and company list.

**Also fixed along the way**: golangci-lint had never been run clean
either (43 issues, all real categories — see that commit) —
`apps/api/internal/jobs/handler.go`'s `parseInt16` took an unvalidated
query param straight to `int16` with no bounds check (a real overflow a
user could trigger via `?min_priority=999999999`), and two dedup
functions returned unwrapped errors, both now fixed alongside the
mechanical shadow/noctx/revive cleanup.

**Update, 2026-08-20: the LLM cascade is real now, and a bigger bug than
any of the above was found getting there.** The user signed up for a
Gemini API key and asked to have it set up. That surfaced three stacked
problems, each hiding the next:

1. `gemini-2.5-flash` — the model this session's earlier fix had already
   corrected `infra/config/llm_providers.yaml` to — turned out to also be
   dead, specifically for new accounts (a live 404 from Google's own API
   said so directly: "no longer available to new users... use
   gemini-3.6-flash"). Fixed, verified with a real 200 response.
2. **The bigger one**: `infra/docker/brain.Dockerfile` never copied
   `infra/config/llm_providers.yaml` into the image at all, and
   `infra/compose/local.yml`'s brain service never passed any of the
   three `SCOUT_BRAIN_*_API_KEY` vars into the container's environment.
   `load_llm_config()` degrades a missing config file to local-Ollama-only
   *silently*, by design (so a host with nothing configured still runs)
   — which means this was completely invisible. **The entire ADR-016
   hosted-LLM cascade has never been reachable from any real running
   `apps/brain` container, in any session, regardless of what API keys
   anyone configured, until this fix.** Every dedup/classify/summarize
   call this project has ever made in a real Docker environment ran
   local-only, silently, the whole time.

Both fixed and verified against the actual running container (rebuilt
the image, recreated the container, confirmed the config file and the
key are both now genuinely present, then ran the real
`build_cascade_client()` code path `scout_brain/worker.py` uses in
production — got a real answer back from `gemini-3.6-flash`, not a
mock). `infra/config/llm_providers.yaml`'s rate limits are deliberately
conservative (10 RPM / 250 RPD) rather than guessed, since Google no
longer publishes a static free-tier table — check the account's own AI
Studio "Rate Limit" page and raise these if the real quota is higher.

Real, once this is running for a while: re-run `make evals` (not the
default local-only invocation — point it at the cascade) to see whether
`classify_role`/`dedup` now clear their gate thresholds at the hosted
tier. That measurement still hasn't been taken.

**One more correction the same day**: the user checked the account's own
AI Studio Rate Limit page (the source Google's docs now say is
authoritative — no public static table exists anymore) and it showed
`gemini-3.6-flash` gets 5 RPM / 20 RPD on this account — thin enough
that any real day of classify/dedup traffic would exhaust it in minutes
and spend the rest of the day silently on local-only. The "Lite"
variants get 15 RPM / 500 RPD instead (25x the daily budget) for a
quality trade that matters little on a structured task. Switched to
`gemini-3.5-flash-lite`, confirmed live, rebuilt, reverified end-to-end
against the real running container the same way as above.

Everything in this session is committed to `main` locally. **Nothing has
been pushed to GitHub.**

---

**Update, 2026-08-20 (later the same day): the shadow-run promotion gap is
closed, three more notification triggers are real, and the whole thing
finally went up to GitHub.**

- **Found and fixed the same class of bug docs/05 section 13 always
  claimed was handled**: 1,504 of 1,505 registered sources were stuck in
  `pending_review` forever, because `SelectDueSources` only ever selected
  `status = 'active'` and nothing ever promoted a new source out of
  review. `apps/collector/internal/scheduler/shadow.go`'s
  `ReviewShadowSources` now runs every 15 minutes: a source polls,
  parses, and dedups silently for 48h (never notifies —
  `SelectUnnotifiedJobGroups` now filters `source.status <>
  'pending_review'`), then gets promoted to `active` at a yield-based
  tier or flagged for review. `infra/migrations/000015`.
- **Three more of docs/11's seven per-job triggers are real** (was 2 of
  7): `remote_high_quality` (Tier 3 — docs/07 section 6 already defines
  that as "remote, India-eligible" by construction, so `location_tier ==
  3` needed no separate check), `newgrad_match`, and
  `deadline_approaching`.
- **Real BATCHED hourly delivery, built from scratch.** `urgency` was
  previously inert — every notification delivered instantly regardless
  of class. `newgrad_match` needed the real thing: `scheduled_for` (sat
  unused in the schema since 000009) now gets set to the next IST hour
  boundary, and matching notifications render as one grouped Telegram
  message instead of N. Caught a real bug in the first cut: computing
  the boundary with `time.Truncate` rounds against the Unix epoch, which
  for UTC+5:30 silently lands on `:00`/`:30` instead of the real IST
  hour — fixed to compute against wall-clock IST explicitly, the same
  way `deliver.go`'s quiet-hours check already has to.
- **`deadline_approaching` needed a real schema decision, not a
  work-around.** It fires twice per job (T-72h and T-24h), which
  `notification_dedup_idx` — one notification per `(job_group, trigger)`
  ever — can't express with a single trigger value without weakening the
  guarantee. `infra/migrations/000016` splits it into
  `deadline_t72h`/`deadline_t24h` instead: two real, distinct events,
  each still fired exactly once. `DeadlineSweeper`
  (`apps/notifier/internal/trigger/deadline.go`) is a separate evaluator
  from `Evaluator`, not a fifth case in it — it sweeps currently-tracked
  jobs by deadline rather than evaluating newly-scored job_groups once.
- **Two real bugs found via test flakiness, both root-caused:**
  1. `GetUndeliveredNotifications` had no `user_id` filter in SQL at
     all — it fetched every user's queued notifications and relied on a
     Go-side `if n.UserID != userID` check after the fact. Harmless in
     production (ADR-015's single user), but it meant
     `apps/notifier/internal/trigger`'s own tests writing more
     notifications for the same shared real test user started leaking
     into `apps/notifier/internal/deliver`'s exact-count assertions once
     `go test ./...` ran both packages concurrently. Fixed at the query.
  2. That fix alone wasn't enough — both packages still reuse the same
     real sole `app_user` row by design (every package's own `testUser`
     mirrors `GetSoleUser`'s single-user architecture). `deliver_test.go`
     now has `freshTestUser`, which creates a dedicated isolated user for
     every test whose assertion depends on an exact sent/queued count,
     the same fix shape as the scheduler's own cross-package flake
     mentioned above.
- **The entire project — 33 local-only commits plus this session's work,
  ~48,600 lines that had never left this machine — went up to GitHub for
  the first time.** Rewritten as 60 commits organized by module
  (migrations, `packages/*`, `adapters/*`, each app, `infra/*`) rather
  than pushed as-is, with the `Co-Authored-By: Claude` trailer removed
  from every commit. A `backup-pre-rewrite-2026-08-20` branch holds the
  original history in case anything needs to be recovered from it.

Nothing left to do on the notification system without hitting a real
external dependency or a genuinely new feature: `watchlist_hiring` needs
a user-managed watchlist (schema + UI, not built), `prestige_opening`
needs a curated company list (a content decision, not a code gap), and
P4 (Capacitor/FCM) and Web Push/email channels all need a Firebase
project, an Android SDK, and SMTP/VAPID credentials this environment
doesn't have.

---

**Update, 2026-08-23/24 (overnight session): Telegram is finally really
connected, and a real permanent-notification-loss bug is fixed.**

- **Telegram was never actually wired up.** `notification_channel` held a
  placeholder `chat_id: 555` inserted 2026-08-11 during early development
  and never replaced — every real send since then (there had only ever
  been one: today's 08:00 digest) failed with Telegram's own "Bad Request:
  chat not found." Ran `notifier -setup` for real this time: the user
  messaged `@scout_kelyon_alerts_bot`, the flow captured the real chat id
  (`8813898212`), and `notification_channel.config` now holds it. A digest
  fired for real immediately after and was delivered successfully —
  confirmed live, not just "no error."
- **Found and fixed a real, permanent notification-loss bug while
  diagnosing the above.** `GetUndeliveredNotifications` excluded any
  notification with a `notification_delivery` row at all — including one
  with `status = 'failed'`. There was no code path anywhere that ever
  revisited a failed send. The one real digest send that failed against
  the fake chat_id was silently, permanently dropped; nothing about that
  failure would ever have surfaced without direct DB inspection.
  `infra/migrations/000020` adds `notification_delivery.next_attempt_at`;
  `GetUndeliveredNotifications` now also offers a `'failed'` row back once
  `attempts < 5` and its backoff window has elapsed;
  `InsertNotificationDelivery` became `UpsertNotificationDelivery` (ON
  CONFLICT on the existing `(notification_id, channel_id)` unique index,
  so a retry updates the same row rather than fighting that constraint).
  `apps/notifier/internal/deliver`'s `retryBackoff` is exponential
  (2/4/8/16 min, capped 30). Four new tests reproduce the bug and prove
  the fix: stays queued after one failure, not retried before backoff,
  retried and delivered after backoff, and permanently stops after 5
  attempts. `apps/notifier/internal/digest` calls the same renamed query
  but was **not** given the same retry treatment — `DigestAlreadySentSince`
  checks whether *a* digest notification exists today, not whether one was
  *delivered*, so a failed digest still won't retry until tomorrow's
  window. Real, still open, deliberately not closed tonight — see "Known
  gaps" below.
- **SmartRecruiters was never fetching anything, for a different reason
  than assumed.** Both live sources (Bosch, Nagarro) had `total_polls =
  total_successes` but `total_jobs_found = 0` since 2026-08-18 — looked
  like a healthy adapter finding nothing, not a broken one. The real cause,
  found via a real captured Bosch response in `raw_observation` (stored by
  today's earlier `dfede0d` fix): the adapter's `postingUrl` field
  **does not exist** on the real API at all — not a rename, an invented
  field that was never there. The only URL-shaped thing the list endpoint
  actually returns is `company.identifier`; the real public apply link is
  `https://jobs.smartrecruiters.com/{identifier}/{id}` (no SEO slug
  needed — SmartRecruiters resolves the bare form). Every posting from
  both sources had been failing `Validate`'s "empty apply URL" check on
  every single poll since the adapter was written, silently, for five
  days. Fixed in `adapters/ats/smartrecruiters/adapter.go`
  (`publicPostingURL`); all four fixtures were also wrong (built from the
  same imagined schema) and are now the real captured shape. Verified: all
  existing adapter tests pass with real assertions (not relaxed), and a
  10-minute live run (`make live-run DURATION=600`) confirms real postings
  now parse and validate from both sources.
- Both `collector` and `notifier` images were rebuilt and the containers
  recreated to pick up both fixes — this was not just committed, it's
  running.

**Known gaps opened or left by this session, not closed tonight:**
- `apps/notifier/internal/digest`'s per-day idempotency check
  (`DigestAlreadySentSince`) doesn't distinguish "sent" from "failed and
  never retried" the way the instant/batched path now does. Fixing it
  properly needs a real decision (retry within the same day? what's the
  dedup key for a retried digest, given `InsertDigestNotification` always
  creates a fresh row?) that wasn't made tonight — flagged rather than
  guessed at, per AGENTS.md's own guidance on notification changes.
- SmartRecruiters' `department` field is frequently an empty `{}` in the
  real API (the real department data lives in the unused `function` field
  instead) — not touched tonight, since the diagnosed failure was
  specifically the apply-URL field, not department mapping.

---

**Update, 2026-08-24 (same overnight session, continued): user-facing feedback
from actually using the running app — four more real fixes.**

- **The AI summary's real latency bug: a 28,836-job queue backlog, not a
  slow model.** A user opening a job detail page enqueues `TaskSummarize`
  (`apps/api`'s detail handler) into the same `brain_deep` river queue as
  every bulk `TaskDedupStage3`/`TaskClassifyTier2`/`TaskExplain` job the
  collector's own ingest path produces — and every job in that queue
  shared river's default priority (1), so a fresh interactive request
  landed at the back of whatever bulk backlog a recent live run had just
  produced, ordered by `scheduled_at` alone. Confirmed live: 28,836
  available `brain_deep` jobs right after tonight's SmartRecruiters
  live-run test. `packages/queue`'s new `bulkBrainDeepPriority` (river
  priority 4, lowest) demotes every bulk task; `TaskSummarize` keeps
  river's default (1, highest) — `river_job_prioritized_fetching_index`
  sorts on priority before `scheduled_at`, so a fresh summary request is
  now claimed on the consumer's very next fetch regardless of backlog
  size. The existing 28,023-row backlog was also backfilled to priority 4
  directly (a one-time `UPDATE`, not a migration — `river_job` is
  operational queue state, not user data) so the fix took effect
  immediately rather than only for future enqueues. Two new regression
  tests (`TestEnqueueBrainDeep_SummarizeGetsHighPriority`,
  `TestEnqueueDedupStage3_GetsBulkPriority`) assert the priority split
  directly.
  - **Also switched `TaskSummarize` off the shared hosted LLM cascade
    onto its own local-only Ollama client** (`apps/brain/scout_brain/
    worker.py`) — not a data-sensitivity requirement the way
    `TaskExplain`'s local-only client is (posting text is public), a
    latency one: the cascade's `RateLimitedProvider.call` blocks
    synchronously on its own rpm window before attempting a hosted
    request, shared with whatever bulk dedup/classify traffic is running
    at that moment. A real local `qwen2.5:3b-instruct` call measured
    ~2.4s directly against Ollama. Verified end-to-end against the real
    running stack: a job with no prior summary went from click to a
    written, readable `job.ai_summary` in a few seconds, not indefinitely
    (previously unbounded — nobody had actually timed it before tonight).
- **Telegram messages were thin and used emoji throughout — both fixed.**
  `trigger.Payload` was carrying only title/company/URL/location/comp/
  deadline even though `SelectUnnotifiedJobGroups` already joined
  role_family, seniority, work_mode, and `job_score.explanation` (the
  same "why this matched you" sentence `apps/collector/internal/
  scoring.Explain` writes synchronously at score time, upgradable by
  `TaskExplain`) and simply never passed them through. Extended the query
  (+`j.role_family`, +`js.explanation`) and `Payload`, and rewrote
  `renderMessage`/`renderBatchMessage`/`renderDeadlineMessage`
  (`apps/notifier/internal/deliver`) to actually show them — location +
  work mode + role family + seniority on one line, compensation,
  freshness ("Posted 12 minutes ago"), a deadline countdown, and the real
  match explanation, not just a bare title and a link. Every emoji
  removed from every Telegram-bound string in the codebase — `deliver.go`,
  `digest.go`, and the `-setup` confirmation message in `cmd/main.go` — a
  deliberate, explicit product decision, not an oversight; docs/11's own
  mockups (sections 6.1 and 6.5, the two channels actually built) are
  corrected to match. Two new tests:
  `TestRenderMessage_IncludesEnrichedFields` asserts the new content
  actually appears in the rendered text, `TestNotifierMessages_ContainNoEmoji`
  scans every render path's output for emoji Unicode ranges as a direct
  regression guard.
- **The web sidebar scrolled away with the page instead of staying put.**
  `Shell.tsx`'s `<aside>` had no positioning at all — `body`/`html` are
  `min-h-full`/`h-full` (grow with content, not fixed to the viewport), so
  the whole flex row including the sidebar scrolled together at the
  document level. Added `sticky top-0 h-screen` to the aside. Verified via
  direct DOM inspection after rebuilding the `web` image (it has no
  volume mount — a source edit alone does nothing until the image is
  rebuilt, unlike the Go services' `docker compose build` habit this
  session already had): `getComputedStyle` confirms `position: sticky`,
  `top: 0px`, and the aside's `getBoundingClientRect().top` stays exactly
  `0` while `window.scrollY` is 800–1000px. The Browser pane's own
  screenshot capture reproducibly blanks out at any non-zero scroll
  position this session (a tool/environment quirk — DOM state, computed
  styles, and element counts all confirm real content is present and
  correctly positioned throughout), so this was verified programmatically
  rather than by a visual screenshot.
- **Internship/consultancy/advocacy scarcity is a sourcing gap, not a bug**
  — checked against real data rather than guessed at. `company.company_type`
  is populated for only 113 of 1,429 companies (73 product, 38 GCC, and
  **2** `services_engineering` — Thoughtworks and Nagarro, contributing
  exactly 100 jobs between them, both from tonight's SmartRecruiters fix).
  No IT-consultancy-focused source curation has happened beyond those two;
  docs/05 section 5.2 already names EPAM, Globant, and Publicis Sapient as
  "checked against every adapter this project supports... resolved on
  none" — a real, already-documented gap, not new information. Similarly,
  `role_family = 'advocacy.*'` postings (804, all `advocacy.solutions`)
  are whatever happened to surface from the existing general source mix,
  not the product of any dedicated DevRel/DevEx source curation.
  Internships specifically: 883 of ~46,000 jobs are `seniority =
  'internship'` — the web feed's `SENIORITY_FILTER` already includes it
  (`["internship", "new_grad", "entry", "unknown"]`, `apps/web/app/(app)/
  opportunities/page.tsx` and two other pages sharing the same hardcoded
  constant), so this isn't a filtering bug either; internships are
  genuinely a small slice of what the current source mix contains. Same
  shape as the GCC coverage gap this file already tracks — the fix is
  curating more consultancy/services-firm and internship-heavy sources
  (the same kind of one-by-one verified seeding `infra/seed/gcc_sources.sql`
  already does for GCCs), not a code change, and wasn't attempted tonight
  given the size of that undertaking.
- Rebuilt and restarted every affected container tonight (`collector`,
  `notifier`, `api`, `brain`, `web`) — nothing above is only-committed,
  all of it is running against the real local stack.

---

**Update, 2026-08-24 (same overnight session, still continued): source
discovery expanded massively, the daily live-run got real resource
discipline, and the first P5 item — a Keka adapter — is built and
verified live.**

- **Source discovery: ~1,500 → 3,562 sources**, via `apps/collector/cmd/
  discover` (the existing MIT-licensed Greenhouse/Lever/Ashby
  candidate-list tool, run several times tonight, briefly with its
  `maxNewCandidatesPerRun` cap raised from 1,500 to 15,000 for one pass to
  clear the backlog rather than repeatedly resampling the same
  already-assessed slice — reverted to 1,500 afterward, confirmed via
  `git diff` showing no net change). First pass, unfiltered: 2,053 new
  companies, almost entirely non-India (the candidate lists have no
  location signal at all). Added a hand-curated `india_candidates.go`
  (~130 guessed Indian-startup slugs across the three platforms) — real
  hit rate was near zero (most guesses were simply wrong slugs), but the
  handful that did resolve (Groww, PhonePe, Branch, Observe.AI) were
  inserted regardless of whether they had an entry-level posting *at
  verification time*, since the shadow-review pipeline is built to keep
  watching, not require an instant hit. **The bigger finding**: the
  original *unfiltered* global discovery run already surfaces real India
  roles from companies that aren't Indian at all — Pure Storage (US) and
  Ajax both turned up with genuine Bengaluru entry-level/new-grad
  postings, confirming broad, unrestricted discovery is the right
  default, not a narrower India-only one. Ran two more 30-minute
  `make live-run` passes to poll through the resulting backlog; real
  numbers climbed from 86 → 212 India-located entry-level jobs over the
  session (2,904 total India jobs, 1,913 in Bengaluru) — 2,057 of the
  3,436 newly-discovered `pending_review` sources still hadn't been
  polled even once by the end of the session, so this number keeps
  climbing on its own as future live-run windows work through the
  backlog.
- **The daily live-run now tears down completely and stays inside a real
  CPU budget.** `infra/scripts/daily-live-run.sh` used to leave
  postgres/redis/brain/notifier running indefinitely after every window
  (only `collector` was ever stopped) — a real, unbounded background
  resource cost with nothing to do with "on demand." Now does
  `docker compose down` on all five instead. `infra/compose/local.yml`
  gained explicit `cpus:` limits on every service in that set (postgres
  1.0, brain 1.2, collector 0.5, notifier 0.3, redis 0.3 — ~3.3 cores
  combined ceiling), verified live via `docker inspect`'s `NanoCpus`
  field on real running containers, not just config review. One
  known, accepted transitional gap: the in-flight live-run window that
  happened to be running when this script was edited used its
  already-buffered copy of the old logic when it woke from `sleep` (a
  real, if narrow, bash gotcha — editing a running script mid-execution)
  — confirmed live, cleaned up manually once. Every run from here on uses
  the new logic correctly, confirmed via a full fresh up/inspect/down
  cycle. **One remaining manual step, not something code can fix**:
  Docker Desktop's "Start Docker Desktop when you sign in" is off, and
  the script silently skips the whole run if Docker isn't already
  running — the 9am schedule needs that toggled on to fire reliably
  unattended. Also confirmed and documented for the user: closing the
  laptop lid sleeps it, launchd defers the missed calendar job to next
  wake rather than dropping it, and this is [ADR-018](docs/adr/ADR-018-laptop-only-hosting.md)'s
  own accepted trade-off, not a bug to route around (`docs/15-infrastructure-deployment.md`
  section 6's own `caffeinate -s` + "never sleep on power" prescription
  is explicit that lid-close is still a gap even with both applied).
- **`adapters/ats/keka` — the first P5 item, built and verified live**,
  not scaffolded. docs/05 rated Keka "Low effort" without the endpoint
  ever being confirmed against a real tenant; the real one
  (`{tenant}.keka.com/careers/api/jobs/default/active`, no auth, no
  pagination) was found by inspecting three real companies' live careers
  pages in a browser (fynd.keka.com, pennanttech.keka.com,
  achiralabs.keka.com) rather than guessed at. **Real finding along the
  way**: Keka has at least two different careers-portal templates in the
  wild — the plain JSON endpoint this adapter targets (2 of 3 tenants
  checked) and a second "embed widget" variant (Fynd's own custom-built
  page) that needs an organization GUID scraped out of a `<script>` tag
  before an equivalent `/embedjobs/...` endpoint resolves. Deliberately
  not supported — a two-step HTML-scrape-then-JSON-fetch is a materially
  different and riskier shape than every other adapter in this codebase,
  and "some real Keka tenants won't resolve" is an honest, bounded,
  documented gap (the adapter's own doc comment, `adapters/README.md`,
  and docs/05 all say so) rather than a silently wrong one. New
  migration `000021` adds `ats_keka` to the `source_kind` enum (Postgres
  enums need a migration to grow, by design — 000002's own comment).
  Full fixture set (standard/empty/malformed/shape-changed/
  duplicate-ids/unicode-heavy) and 9 tests, all passing; separately
  verified against the real live pennanttech.keka.com API through the
  actual `Fetch`→`Parse`→`Validate` chain (not just fixtures) via a
  throwaway verification binary, deleted afterward — 2 real postings
  parsed and validated correctly, real apply URLs. Two real tenants
  seeded as `pending_review` sources (Pennant Technologies,
  which has real open roles; Achira Labs, which currently has zero —
  kept anyway, same "shadow-review can wait for it" reasoning as
  tonight's India-candidate insertions). Darwinbox and Zoho Recruit
  (docs/05's other two P5-rated "Low effort" Indian ATS platforms)
  remain unbuilt — real estimation, not guessed: this one took real
  browser-based endpoint discovery before a line of adapter code was
  written, and each of the other two needs the same treatment
  independently, not an assumption that they share Keka's shape.

---

**Update, 2026-08-26: a filter bar shipped without ever being written up, CI
turned out to be broken in four different real ways, and a real PII exposure
was found live on GitHub and purged.** This picks up after a gap this file
itself didn't track: five commits landed on `main` on 2026-08-23 (auth +
app-shell restructuring and companies/watchlist/calendar/insights/settings
pages, ADR-018's daily live-run window automated via launchd, a root-level
`start.sh`/`stop.sh`, a hydration-warning fix, and a routine sqlc regen) that
this file never mentioned — they're real and already on GitHub, just
undocumented here until now.

- **A URL-state filter bar for Opportunities and Search**
  (`apps/web/components/FilterBar.tsx`, `apps/web/lib/useJobFilters.ts`,
  `apps/web/lib/taxonomy.ts`) existed in the working tree, fully wired into
  both pages and the search API's new `role_family`/`seniority`/`work_mode`
  params (`apps/api/internal/search/handler.go`, `packages/db/queries/
  search.sql`), but had never been mentioned in this file or browser-verified.
  Verified today against the real running stack (`pnpm dev` pointed at the
  live `api` container, not a rebuilt Docker image — the web container has no
  volume mount, so its own build is stale): checkbox toggles update the URL
  (`?seniority=internship&seniority=new_grad...`), Opportunities' seeded
  defaults render correctly, and toggling a facet on either page re-fires a
  real request against the live API and changes the real result set (a
  Senior SRE role appeared on Opportunities after checking "Senior"; Search's
  result count and network calls both reflected an added `role_family`
  filter). Zero console errors across the whole session.

- **CI has been red on every push since 2026-08-23, for four unrelated real
  reasons — none of them what anyone would have guessed from the commit
  history:**
  1. **The `go` job's "pass" was a false green.** It has never had a
     Postgres service. 20 test files (`grep -rl "testPool(t)"`) silently
     `t.Skip()` without one — `TestSearch_MatchesOnTitle`,
     `TestList_OnlyReturnsCompaniesWithAtLeastOneOpenJob`,
     `TestReviewShadowSources`, all of `apps/notifier/internal/trigger`'s
     deadline tests, all of it — none of these have ever actually executed
     in CI, on any push, ever. Fixed by adding the same `pgvector/
     pgvector:pg16` Postgres service the `migrations` job already uses,
     applying migrations the same way, and seeding the sole user +
     `weight_version` via the new `infra/seed/seed.sql.example` (see the PII
     item below for why that's the committed file and not `seed.sql`
     itself). Verified for real, not just by editing YAML: stood up an
     identical throwaway Postgres + Redis locally, ran `go test -count=1
     ./...` against them — **0 skips, 0 failures**, the first time this
     repository's test suite has ever actually exercised its
     Postgres-backed tests end to end.
  2. **golangci-lint failed with "the Go language version (go1.24) used to
     build golangci-lint is lower than the targeted Go version (1.25.0)."**
     `go.mod` requires `go 1.25.0` (bumped by a dependency, probably when
     `golang.org/x/oauth2` was added for the Google Calendar integration);
     `golangci-lint-action@v6`'s `version: latest` resolved a stale v1.64.8
     binary that predates golangci-lint's v2 line and can't even parse
     `.golangci.yml`'s `version: "2"` schema. Fixed: bumped the action to
     `@v9` and pinned `version: v2.12.2` (matching the locally-installed
     version this was verified against — `golangci-lint run` reports 0
     issues), and bumped both `go` and `golangci` jobs' `actions/setup-go`
     to `go-version: '1.25'` to match `go.mod` instead of silently relying
     on Go's auto-toolchain-download to paper over the mismatch.
  3. **sqlfluff failed on real, long-standing formatting violations — not
     just in the one migration the first broken run happened to show.**
     Re-ran with the exact CI-pinned `sqlfluff==3.5.0` (installed in an
     isolated venv, not whatever was on the CLI PATH) against both
     `infra/migrations` and `packages/db/queries` — the latter had never
     actually run in this CI failure at all, since the workflow's two
     sqlfluff steps are sequential and the first one failing skips the
     second. Found 7 migrations and 15 query files with real violations
     (indentation, capitalization, line length, join-operand order,
     spacing). `sqlfluff fix` cleared all but one automatically; the one
     manual fix was a >100-char string literal in `infra/migrations/
     000019` (split across a `||` concatenation, identical resulting
     string, verified against `apps/collector/internal/scheduler/
     shadow.go`'s own construction of that exact text). One violation
     wasn't a formatting issue at all: `packages/db/queries/dedup.sql`'s
     `SelectStage2Candidates` had `LIMIT 200` with no `ORDER BY` — a real,
     if narrow, dedup-candidate-selection determinism gap AGENTS.md rule 4
     makes worth closing, not just silencing. Added `ORDER BY posted_at
     DESC` (mirrors `SelectRecentDescriptionsForCompany`'s existing
     convention in the same file) so a company that ever clears 200 open
     roles in one role_family/seniority pair within 45 days gets a
     deterministic, freshness-biased truncation instead of an arbitrary one
     that could vary between calls. `docs/08-dedup-identity.md`'s own
     literal copy of the query updated to match. `make db-generate` re-run
     for every touched query file; `make db-verify` and the full Go test
     suite both confirm nothing broke.
  4. **`apps/web`'s typecheck step failed on `Cannot find name 'LayoutProps'`
     in `app/layout.tsx`** — reproduced locally by deleting `.next` and
     running `tsc --noEmit` alone (fails), then `next build` followed by
     `tsc --noEmit` (passes). `LayoutProps` is a Next.js-generated ambient
     type (`.next/types/**/*.ts`, referenced by `tsconfig.json`'s own
     `include`) that only exists after `next build` or `next dev` has run
     once — CI ran `typecheck` before `build`, so every fresh checkout hit
     this; local runs only passed by accident, from `.next/types` left over
     from earlier dev-server use in the same tree. Fixed by swapping the
     step order.
  5. Left alone, deliberately: the `eval harness` job's red X. It fails on
     `classify_role`/`dedup` not clearing their docs/17 gate thresholds —
     this is the exact, already-documented gap `evals/README.md`'s "Honest
     current state" section describes (local-only Ollama tier in CI, no
     hosted-cascade API key available there), not a new regression. Worth
     revisiting when CI has a hosted provider key, not tonight.

- **A real PII exposure was found and fixed while diagnosing the CI-Postgres
  gap.** Seeding a fresh test database meant reading `infra/seed/seed.sql`
  closely for the first time in a while, which turned out to hardcode real
  personal data — actual university, degree, 2027 graduation year, and a
  detailed, specifically-tailored skill/skill-level list — committed
  (`afcc666`, `2c02bae`) and live on GitHub since the 2026-08-20 push. This
  is the same class of leak this file already describes catching once, for
  a *different* file (`infra/seed/resume.sql`) — that one was caught before
  it ever reached a commit; this one wasn't, and had been public for six
  days. Only the email address had been parameterized out (`:'user_email'`,
  passed in by `infra/scripts/seed.sh`); everything else was hardcoded.
  Remediated for real, not just going forward: `git filter-repo --path
  infra/seed/seed.sql --invert-paths` against a fresh mirror clone (verified
  zero remaining matches for the real university name across every commit
  on every branch afterward, including the local-only `backup-pre-
  rewrite-2026-08-20` branch, which had the same content), then `git push
  --force-with-lease origin main`. Confirmed via the GitHub API that
  `main` now points at the rewritten history and the file is absent from
  the current tree. The repo has zero forks and zero stargazers, so no
  other clone needed to be told to re-pull. `infra/seed/seed.sql` is now
  gitignored (real data restored locally from a pre-purge mirror backup so
  `make seed` keeps working unchanged); `infra/seed/seed.sql.example` is the
  new committed placeholder template, same pattern as `resume.sql`/
  `resume.sql.example`, and doubles as what CI now seeds a fresh test
  database with (see the CI item above).

Everything above is committed and pushed. Nothing in this update is still
sitting uncommitted the way most of this file's earlier entries describe —
that was itself part of what today's session found and closed.

---

**Update, 2026-08-27: three real P5 bugs closed (partition maintenance, a
discover-tool SSRF gap, a dedup advisory-lock miss), and the second P5 ATS
adapter — Zoho Recruit — is built and verified live.**

- **`raw_observation`'s own documented partition maintenance job never
  existed.** 000005's migration comment and `restore-drill.sh` both describe
  "a nightly job [that] maintains a rolling window... and drops partitions
  older than six months" — invisible only because 000005 pre-created 18
  months of partitions upfront (through 2028-01) and real data is still
  well inside that window. `Scheduler.MaintainRawObservationPartitions`
  (`apps/collector/internal/scheduler/partitions.go`) now re-runs the
  migration's own DO block on a schedule (a third ticker in `Run()`, plus
  once immediately at startup) — create through 3 months ahead, detach and
  drop anything more than 6 months past.
- **`discover`'s own candidate-list HTTP client bypassed the SSRF dialer**
  — every other outbound call in this codebase goes through
  `packages/fetch.Fetcher` or the shared `ssrf.DialContext`; the discovery
  tool's own client didn't. Fixed to share the same dialer.
- **Stage 3 dedup merges could run without the company advisory lock**
  AGENTS.md's "Common mistakes" section already names by name ("Concurrent
  dedup for the same company creates duplicate groups"). Fixed in
  `apps/brain`.
- CI's Python job wasn't seeded the same way the Go job now is (only fixed
  2026-08-26) — fixed to seed both.

**`adapters/ats/zohorecruit` — the second P5 ATS adapter, built and
verified live against three real tenants**, not scaffolded. docs/05's
original guess (`{tenant}.zohorecruit.in/recruit/portal/...`, a separate
public JSON API) was wrong, the same way SmartRecruiters' and Teamtailor's
original guesses were — found by inspecting real tenants' live career
sites in a browser, not by reading Zoho's docs.

- **The real shape**: there is no JSON API. `{tenant}.zohorecruit.com/jobs/
  Careers` is an HTML page, and the job array is embedded as
  HTML-entity-encoded JSON inside a single `<input id="jobs" value="...">`
  element. `/jobs/Careers` is the one path confirmed to work on every
  tenant checked — the shorter `/careers` alias resolved for one tenant
  (increff) and timed out entirely for another (educohire), so the adapter
  always seeds and fetches `/jobs/Careers`.
- **Per-tenant module configuration varies which fields the embedded array
  carries** — confirmed by checking two tenants live: increff's board
  omits `Job_Description` entirely (title/location/experience only);
  educohire's includes it. Every field the adapter reads is therefore
  optional; a tenant missing `Job_Description` gets an honestly empty
  `DescriptionHTML`, the same N+1-driven gap Workable/SmartRecruiters/
  Workday already carry, not a new one.
- **Apply URLs need only the numeric job id** — `/jobs/Careers/{id}`
  resolves identically with or without the SEO slug segment the real site
  appends (confirmed live against two tenants), so the adapter never
  attempts to reproduce Zoho's own slugification.
- New migration `000022` adds `ats_zohorecruit` to the `source_kind` enum
  (Postgres enums need a migration to grow, by design — 000002's own
  comment, same pattern 000021 used for Keka). `packages/db/gen`
  regenerated (`make db-generate`), verified against a real migrated
  Postgres (`make migrate` applied 000022 cleanly).
- Full fixture set (standard/empty/malformed/shape-changed/duplicate-ids/
  unicode-heavy, plus a no-`id="jobs"`-input case) and 15 tests, all
  passing. Separately verified against three real live tenants through the
  actual `Fetch`→`Parse`→`Validate` chain (not just fixtures) via a
  throwaway verification binary, deleted afterward:
  `increff.zohorecruit.com` (10 real postings, no descriptions — the
  tenant-config gap above), `onebyzero.zohorecruit.com` (14 real postings,
  full descriptions, real SDE/QA/AI-engineer roles), `sidgs.zohorecruit.com`
  (28 real postings, full descriptions). All three seeded as
  `pending_review` sources (robots.txt `Allow: /jobs/*` on
  zohorecruit.com, platform-wide, checked before seeding as `permitted`)
  — Increff (`product`, retail SaaS), One By Zero (`product`,
  customerplus.io), and **SID Global Solutions** (`services_it` — a real,
  verified IT/digital-transformation consultancy, the first concrete
  addition toward the `company_type = services_it/services_consulting`
  coverage gap this file has flagged since 2026-08-24).
- golangci-lint, sqlfluff, and `go test ./... -count=1` against a real
  local Postgres all clean for everything this adapter touched.

**Found in the process, not fixed (out of scope for this adapter,
flagged as a separate task instead)**: `go test ./...` against this
project's real local dev Postgres — not a fresh CI database — surfaces two
pre-existing failures unrelated to anything above:
`TestList_OnlyReturnsCompaniesWithAtLeastOneOpenJob`
(`apps/api/internal/companies`) and `TestSearch_MatchesOnTitle`
(`apps/api/internal/search`). Both insert a uniquely-named fixture row and
assert it appears in a *default, paginated* response; against this
project's real accumulated data (~1,429 companies, ~46,000 jobs) the fresh
row doesn't rank into the default page, so the assertion fails even though
the feature works. `TestList_QueryFiltersByName`, right next to the first
failing test, already shows the fix shape (scope the query to the fixture
instead of relying on the unfiltered default) — not applied here since it
touches unrelated test files.

Everything in this Zoho Recruit update — the adapter, migration 000022,
`packages/db/gen`, the doc updates, and the three seeded sources — is
uncommitted in the working tree as of this writing. The
partition-maintenance/discover-SSRF/dedup-lock/CI-seed fixes earlier in this
same dated update were already committed (and are not yet pushed) before
this adapter work began.
