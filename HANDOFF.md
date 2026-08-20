# Session Handoff

Read this first, before `docs/19-roadmap.md`, when picking work back up. It
describes what actually exists right now — the roadmap describes what's
intended. When they disagree, this file is more current and the roadmap's
checkboxes should be trusted less than the prose here.

**Last updated:** 2026-08-19 (everything below is now committed; see the
"Update, 2026-08-19" note further down for what changed getting there,
including the ADR-018 laptop-only hosting pivot). Last commit on `main` is
`6dad399` (ci(web): add lint/typecheck/build job). Nothing has been pushed to
GitHub yet — this is all local to `main` in this working tree.

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
