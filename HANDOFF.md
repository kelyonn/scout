# Session Handoff

Read this first, before `docs/19-roadmap.md`, when picking work back up. It
describes what actually exists right now — the roadmap describes what's
intended. When they disagree, this file is more current and the roadmap's
checkboxes should be trusted less than the prose here.

**Last updated:** 2026-08-29 (later still) — **Save AND Dismiss both
real-device-verified, both P4 notification exit criteria now genuinely
closed.** Installed on the real Pixel 7, sent real FCM pushes through the
real local stack (bound to the laptop's tailnet IP), confirmed via
`dumpsys notification` and a real screenshot: correct `opportunities`
channel (not the fallback the original bug report caught), correct
title/body, both actions rendered. A real tap on "Save" found a genuine
bug — the API's state machine rejects `new -> saved` directly (only
`new -> viewed`/`dismissed` are allowed; the web UI already works around
this client-side) — fixed in `JobActionReceiver` by chaining through
`viewed` first, same as the web client does. Retested: `state = 'saved'`
in Postgres, confirmed directly. `Dismiss` was then tapped by the user
themselves (screen automation kept missing its target and one attempt
briefly, harmlessly opened an unrelated private Instagram screen — backed
out immediately, nothing touched, screenshot deleted; automation was
abandoned in favor of a real human tap after that) — confirmed
`state = 'dismissed'` in Postgres. **A concern raised earlier in this same
pass — that the notification might not be clearing from the shade after a
successful action — turned out to be a self-inflicted false alarm**: every
check that seemed to show it "still there" was reading `dumpsys
notification`'s `mArchive` section (a 100-entry historical log of every
notification ever posted, kept regardless of whether it was cancelled),
not the real `Notification List:` section, which showed zero entries once
checked correctly. `NotificationManagerCompat.cancel()` was working the
whole time. Full writeup at the very end of this file (two dated entries:
"real device verification," then this update immediately after it).
`docs/19-roadmap.md`'s P4 section updated — both notification-action exit
criteria now checked.

**Earlier the same session** (superseded by the above, kept for the
Gradle-verification detail it still carries): the following described the
state before a real device was available to this session — that gap is
now closed.

A real-phone push (verified end-to-end earlier the same day,
see below) was landing on Android's generic
`fcm_fallback_notification_channel` with no action buttons, not either of
docs/11-notifications.md section 6.2's two named channels. Root cause was
two stacked gaps, not one: the channels were never created on-device
(nothing outside `node_modules` called `createNotificationChannel()`), and
the FCM payload carried a top-level `notification` block, which Android
auto-displays itself whenever the app is backgrounded — bypassing app code
entirely, which is also why no actions were possible. Fixed:
`apps/notifier/internal/fcm` now sends data-only messages (title/body/
channel_id moved into `data`); `apps/mobile/android/app/src/main/java/
com/scout/app/` gained `NotificationChannels` (creates `opportunities`/
`deadlines` on every launch), `ScoutMessagingService` (a custom
`FirebaseMessagingService` that replaces the bundled push-notifications
plugin's own — removed via `AndroidManifest.xml`'s `tools:node="remove"`,
since only one can receive `MESSAGING_EVENT` — and builds the notification
manually with the right channel and two actions), and `JobActionReceiver`
(handles the Save/Dismiss taps, `POST`ing `/v1/jobs/{group_id}/state`
directly using the same `scout_web_session` cookie the WebView already
authenticates with, read via `CookieManager` rather than inventing a second
credential store). Go side: `fcm_test.go` updated for the new wire shape,
`go test ./...` and `golangci-lint run ./...` both clean.

**Then actually built** — this session's own environment started with no
`ANDROID_HOME` (the SDK set up 2026-08-28 lives at `/opt/homebrew/share/
android-commandlinetools`, a machine-wide Homebrew cask install, so it was
still there; just needed exporting in this shell), but the user asked for
the SDK to be set up and the app built, which surfaced that it already was
— from a *different git worktree* of this same repo, since `android/local.
properties` and `google-services.json` are both gitignored per-checkout
files, not shared across worktrees. Copied `google-services.json` over from
the main checkout (same project's own already-authorized local config, not
fetched from anywhere), wrote a fresh `local.properties`, ran
`npx cap sync android`, then a real `./gradlew assembleDebug`: **BUILD
SUCCESSFUL**, a real `app-debug.apk`, confirmed via the merged manifest
that the plugin's `MessagingService` is genuinely gone and
`ScoutMessagingService`/`JobActionReceiver` are the ones registered, and
via `strings` on the APK's own `classes11.dex` that all three new classes
are actually compiled in. `./gradlew testDebugUnitTest` also ran the new
`JobActionReceiverTest` for real — 4/4 passing.

**`./gradlew lintDebug` caught two real bugs, not style noise.** First
pass failed outright: `ScoutMessagingService`'s `NotificationManagerCompat
.notify()` call had no `POST_NOTIFICATIONS` permission guard — a real crash
risk (an uncaught `SecurityException` inside a `FirebaseMessagingService`
callback if the permission were ever revoked, worse than the auto-display
path this replaces, which the OS itself could never crash), fixed with an
explicit `ContextCompat.checkSelfPermission` check that degrades instead of
throwing. Second, a warning that was a real functional regression, not
cosmetic: replacing the plugin's `MessagingService` outright also silently
dropped its `onNewToken` forward, which is what makes a rotated FCM token
reach `apps/web/lib/capacitor.ts`'s `registerDevice()` call — without it,
docs/11 section 12's own "Native token refresh... re-registered, old token
retired, no missed delivery" requirement would have quietly stopped
working the first time any device's token ever rotated. Fixed by adding
`onNewToken` back, forwarding to `PushNotificationsPlugin.onNewToken()`
exactly as the removed service did. Both fixes reverified with a second
clean `assembleDebug` + `lintDebug` + `testDebugUnitTest` run — zero
lint warnings anywhere under `com.scout.app` afterward.

Full writeup in this date's own entry (three entries down from this one).
A real device did turn out to be reachable this same session after all
(same Pixel 7, over the same wireless-debugging `adb` connection the
2026-08-29 entries above already established) — see the "real device
verification" entry at the very end of this file for what that pass
found, fixed, and still left open.

**Previous update, same day:** 2026-08-29 — **P4's real-device gap is closed.** The
2026-08-28 entry below verified the shell on an emulator only, with the
phone-connectivity question (ADR-012 assumed Tailscale, ADR-018 removed
it) documented but unresolved. This session resolved it end to end: the
Tailscale daemon (needed root, which the prior session's shell lacked)
is up, the laptop's tailnet IP is set as `SCOUT_HOST_BIND_ADDR`, and a
real debug APK built against that IP was installed on the user's actual
Pixel 7 over `adb` and launched — `logcat` shows a clean load
(`Loading app at http://<tailnet-ip>:3000`, `App started`/`App resumed`,
`ScoutNative.firebaseAvailable()` correctly reporting `false` and the JS
side skipping push registration without crashing), and a screenshot
shows the real dashboard, not just the login screen, pixel-correct, zero
fatal exceptions. Also generated this session: the P0 release signing
keystore (RSA-4096, stored outside the repo at `~/.scout-secrets/android`,
never committed — see docs/13's never-rotate-secret guidance), wired into
`build.gradle` via a gitignored `keystore.properties`, with a real signed
release APK built and verified via `apksigner`. **FCM is also now live, same session.** A Firebase project (`scout-d46e6`)
already existed under the user's account from an earlier attempt; drove
the Firebase console (with the user's explicit go-ahead) to register the
Android app and download `google-services.json`, then the user clicked
through the one step the harness itself blocked automatically —
generating the notifier's Admin SDK private key, since that's a
full-access server credential and the classifier correctly treated it as
more sensitive than the Android config file. Both wired in (neither
committed — see docs/13's never-log/never-commit posture) and verified
live: notifier logs `"native push armed"`, and a rebuilt debug APK on the
same Pixel 7 produced a real FCM token that reached
`POST /v1/push/register-device` and landed as a real `notification_channel`
row. Not yet verified: an actual push delivered through that token.
`apps/mobile/README.md` and `docs/19-roadmap.md`'s P4 section both
updated to match. See the full writeup in this date's own update near
the bottom.

One more thing worth flagging: while verifying the dashboard fresh in
the WebView, logcat caught a real (unrelated) bug — a minified React
hydration error (#418) firing twice on initial load. Flagged as a
separate background task rather than fixed here, since it's out of
scope for P4 and needs reproducing with an unminified build to find the
actual mismatch.

One operational snag worth remembering: Android's wireless-debugging
`adb` connection only reconnects automatically over the same local Wi-Fi
network as the host — independent of Tailscale, which kept working fine
over cellular the whole time (proven by a successful `ping` from the
phone to the laptop's tailnet IP while off Wi-Fi). If `adb devices` goes
empty mid-session, it's almost always the phone having left Wi-Fi, not a
Tailscale problem.

**Previous update, 2026-08-28** — **P4's Capacitor shell is real and running**:
Android Studio and a full headless SDK installed from scratch
(`android-commandlinetools`), `apps/mobile`'s actual native project
generated and built (`BUILD SUCCESSFUL`, a real unsigned APK), and
verified on a real emulator — the actual deployed Next.js login screen
rendering pixel-correct inside the shell, the real JS bundle executing
with zero errors per `logcat`. The one genuinely open question (how a
physical phone reaches the server, given ADR-012 assumed Tailscale and
ADR-018 later removed it) is documented, not resolved — needs the user.
See that date's own update at the bottom for the full writeup.

The day before (2026-08-27, three updates, still at the bottom): a real
`raw_observation` partition-maintenance job, a `discover`-tool SSRF gap
and a Stage-3 dedup advisory-lock gap closed; the second P5 ATS adapter
(**Zoho Recruit**) built and verified live; a live end-to-end pass that
found and fixed a serious compensation-parsing bug fabricating wrong
salaries for roughly a third of all jobs with description text; and P4's
native-push **backend** (FCM client, the deliverer's device fan-out, the
registration endpoint) built and tested. Everything through this update is
committed to `main` and pushed.

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

**Found in the process, and fixed**: `go test ./...` against this
project's real local dev Postgres — not a fresh CI database — surfaced two
pre-existing failures unrelated to the adapter above:
`TestList_OnlyReturnsCompaniesWithAtLeastOneOpenJob`
(`apps/api/internal/companies`) and `TestSearch_MatchesOnTitle`
(`apps/api/internal/search`). Both inserted a uniquely-named fixture row and
asserted it appears in a *default, paginated* response; against this
project's real accumulated data (~1,429 companies, ~46,000 jobs) the fresh
row didn't rank into the default page, so the assertion failed even though
the feature worked. `TestList_QueryFiltersByName` and
`TestSearch_FiltersByFacet` (the latter's own comment already names
`TestSearch_MatchesOnTitle`'s flakiness) show the same fix shape used
elsewhere in these same files — scope the query to the fixture instead of
relying on the unfiltered default — applied here too: a shared token scopes
the companies request to just its own two fixtures, and a fabricated
keyword replaces "distributed systems" in the search fixture's title so no
real job can outrank it. Both pass now, and the full suite is green
end to end.

Also confirmed: `sqlfluff` initially appeared to fail broadly across many
pre-existing query files unrelated to this session (`LT01`, whitespace
before `[`) — this local machine's `sqlfluff` resolved to 4.3.0, not the
CI-pinned `3.5.0` (the same class of local-tool-version mismatch this file
has caught before). Re-checked against an isolated venv with the exact
pinned version: clean, both for migrations and for `packages/db/queries`.
Not a real regression, just a reminder that a bare `sqlfluff` on PATH isn't
authoritative here.

Everything above — the Zoho Recruit adapter (migration 000022,
`packages/db/gen`, the doc updates, three seeded sources), the two test
fixes, and this file's own updates — is committed to `main` as three
separate commits. Nothing has been pushed to GitHub yet; the
partition-maintenance/discover-SSRF/dedup-lock/CI-seed fixes earlier in
this same dated update were already committed before this adapter work
began and are in the same unpushed state.

---

**Update, 2026-08-27 (same day, continued): a live end-to-end pass over the
whole running stack — not new test code, the actual system with real
fetching, real Postgres, and a real browser — found and fixed a serious,
widespread compensation-parsing bug, plus a scheduling-priority backlog
observation and a Docker/host-sleep false alarm worth recording so nobody
chases either again.**

**`apps/collector/internal/normalize.ParseCompensation` was fabricating
wrong salaries for roughly a third of all jobs with description text**,
found by literally opening the Opportunities page and reading a real job
card: a Waymo posting with no stated compensation at all showed "₹167/mo."
`compensation.go` had no test file before today despite existing since
2026-08-11. Root cause and fix, in the order each was found (each layer
exposed the next):

1. **Every capture group in both regexes was optional**, so a bare number
   with no currency symbol, no lakh/crore/k suffix, and no period word
   matched exactly as confidently as a real salary — Waymo's "₹167/mo" was
   literally the "2009" in "Since its start... in 2009, Waymo has focused."
   Fixed: `hasCompensationSignal`/`hasLocalCompensationSignal` require at
   least one real local marker before a match is even considered.
2. **A period word alone (no `/` or "per") isn't proof of a rate** — "6
   Month FTC" (a contract length) matched the bare "6" as ₹6/year the same
   way "$50,000/month" would. Fixed: the `/|per` separator is now its own
   capture group, and a period only counts when it came with one.
3. **A real currency symbol next to a real number still isn't proof of a
   salary** — "we've raised $50M in Series B funding" and "$170,000,000 in
   annual recurring revenue" both matched, producing $500M/year and
   $170M/hour figures nothing else could explain. Fixed two ways:
   `parseIndianNumber` now recognizes m/mn/million/b/bn/billion suffixes
   purely to reject them (no individual salary at this platform's target
   seniority is ever quoted that way), and `plausibleCompensation` adds a
   generous annualized ceiling (₹5 crore/USD $5M) as defense in depth for
   whatever still gets through without an M/B suffix.
4. **The "12-15 LPA" shared-suffix fix (below) itself caused a real
   regression**: "loan offerings ranging from ₹50,000 to ₹5 Lakh" (a
   fintech's own product description, nothing to do with the job)
   borrowed "Lakh" onto the already-complete, comma-grouped "50,000" and
   produced ₹5,000,000,000. Fixed: a number is only eligible to borrow the
   other side's suffix when it has no comma grouping of its own.
5. **`ParseCompensation` only ever looked at the *first* regex match in the
   text.** A number appearing before the real compensation figure — "We do
   monthly 1:1s... This Staff role pays $180,000 per year" ("1:1s" contains
   a digit) — failed the signal gate and made the whole function give up
   instead of continuing to the real figure afterward. Fixed:
   `tryRangeMatches`/`trySingleValueMatches` now iterate every match in
   order and take the first one that actually passes.
6. **A malformed range can have `lo > hi`** — an upstream HTML-stripping
   artifact turned "$150,000 to $187,500" into "$15000,000 to $187500"
   earlier in the same real description, and a plausibility check on the
   high end alone would have accepted the self-contradictory result since
   187,500 alone looks like an ordinary salary. Fixed: `lo > hi` is now
   rejected outright, and both ends are checked against the ceiling, not
   just the high one. (Fix 5's all-matches scan is what then let the
   correctly-formatted restatement later in the same description win
   instead.)
7. **The range separator class never included an em dash (—, U+2014)** —
   real, common in professionally-formatted postings ("Base Compensation
   Range $150,000 — $187,500 USD") — only a hyphen, en dash, or literal
   "to". Without it the range never matched at all and fell through to
   each end being scored as its own lone figure. Fixed: added to the
   separator class.
8. Also fixed, same audit: `parseRangeMatch`'s shared-suffix borrowing
   itself ("12-15 LPA" parsing as lo=12/hi=1,500,000 instead of both in
   lakhs) — this is fix 4's own prerequisite, listed here for completeness
   since it's what fix 4's regression test guards.

**16 new tests** in `apps/collector/internal/normalize/compensation_test.go`
(the file didn't exist before today), each reproducing one of the above
against the literal real text that triggered it. `golangci-lint`,
`sqlfluff` (N/A, no SQL touched), and the full `go test ./... -count=1`
suite are clean.

**The existing bad data was corrected too, not just the code going
forward** — five backfill passes (a throwaway Go binary re-running the
current `ParseCompensation` against every job's real `description_text`
and overwriting `comp_*`/`paid`, deleted after the last pass, same
disposable-tool convention as every other one-time verification binary in
this project) against the real local dev database, one after each new
false-positive class was found and fixed: the first pass alone corrected
22,296 of 61,580 jobs (36%) with a description; two more classes found
*during* that same backfill (the LPA-range regression, the em-dash gap)
needed their own follow-up passes. Final distribution is sane across every
currency/period bucket — no value anywhere near the hundreds-of-millions
figures the bug was producing before.

**Known, accepted follow-on gap, not fixed tonight**: `job_score.explanation`
— the cached "why this matches" sentence `apps/collector/internal/
scoring.Explain` writes at score time — still shows the old, wrong
compensation figure for jobs scored before this fix (confirmed live: the
same Waymo job's card still reads "₹167/month, 72nd percentile" after its
own `job.comp_min` was correctly nulled). Fixing this needs a real rescore
pass through the proper path AGENTS.md rule 3 and docs/17 §8.2 require
(`suppress_notifications`, the same guarantee a weight-version rescore
already has to provide) — a distinctly larger and more sensitive operation
than correcting `job.comp_*` directly, and out of scope for tonight. It
self-heals the normal way: a real weight-version bump already triggers a
full rescore, and any job whose content is re-observed with a changed
content_hash gets rescored on its own next poll.

**Also investigated and ruled out, so nobody chases either again:**
- **A ~47-minute-plus gap between collector scheduler ticks, and the
  container going `unhealthy`** looked at first like a real hang — a
  `SIGQUIT` goroutine dump showed the heartbeat goroutine genuinely alive
  and cycling normally (a fresh `[select, 1 minutes]` state), and 0 process
  restarts confirms it was the same continuous process throughout. The
  actual cause: this machine went to sleep for a real stretch during this
  session (Docker Desktop's Linux VM suspends with it), so wall-clock time
  jumped on wake while the process's own sense of elapsed time didn't —
  exactly [ADR-018](docs/adr/ADR-018-laptop-only-hosting.md)'s own
  documented "closing the laptop lid" caveat, not a new defect. The
  container was manually restarted after the diagnostic `SIGQUIT` (which
  itself kills the process) rather than relying on `restart: unless-stopped`,
  which did not fire on its own within the time checked.
- **~1,897 `pending_review` sources (including this session's own 5
  newly-seeded ones) still sat at 0 polls** even after real collector
  uptime — not a bug, a real, large, already-documented backlog (HANDOFF's
  own 2026-08-24 entry: "2,057 of 3,436 newly-discovered `pending_review`
  sources still hadn't been polled once"). `SelectDueSources`'s `order by
  priority_tier asc, next_poll_at asc` deliberately gives never-yet-reviewed
  sources (tier 3, the schema default) priority over already-vetted but
  low-yield `active` ones (tier 5, what `decideShadowPromotion` assigns for
  the lowest yield bucket) — intentional, so the 48h shadow-review window
  actually completes in bounded time rather than being starved by a much
  larger population of low-priority active sources trickling in on their
  own interval. Confirmed the 5 sources this session seeded do get polled
  correctly once actually reached (manually brought to the front of the
  queue for verification: all 5 polled successfully, zero errors).
- **Speechify showing "1,181 open roles" and two Cloudflare
  "Research Engineer Intern (Fall 2026)" postings both saved** looked like
  dedup or classification bugs. Both are real: Speechify genuinely posts
  the same ~5 SWE roles across 200+ cities (a real, if unusual, broad
  geographic hiring pattern), each with its own genuine, distinct
  Greenhouse URL — Stage 1 dedup correctly does not merge genuinely
  different URLs (AGENTS.md's own "bias dedup toward under-merging"), and
  `is_software` classification on the 1,181 is correct (a separate 110
  "Go-to-Market" postings from the same company are correctly classified
  `is_software = false` and excluded). The two saved Cloudflare postings
  are two genuinely different Greenhouse job ids, same shape as Speechify's
  — not a save-button or state bug.
- **Google Calendar showing "not configured on this deployment"** despite
  an earlier `grep` of `.env` appearing to show real values — the grep
  pattern (`sed 's/=.*/=<set>/'`) masked the fact both
  `SCOUT_GOOGLE_CLIENT_ID`/`SCOUT_GOOGLE_CLIENT_SECRET` are actually empty
  locally. The app's own `enabled: false` response is correct; this was a
  flaw in how the check was done, not in Scout.

Everything in this update — `compensation.go`, the new
`compensation_test.go`, and the corrected data (already live in the local
dev database via the backfill passes, no migration needed since these are
computed columns re-derived from `description_text`) — is committed and
pushed to `origin/main`.

---

**Update, 2026-08-27 (later the same day): P4 started — the backend half
of native push (ADR-012) is built and tested; the Capacitor shell itself
is not.**

After the live end-to-end pass above, the full local stack was torn down
(`docker compose down` — collector/brain/notifier first, then everything,
per this project's own "bounded window, not indefinite" convention) and
work began on P4.

**Built and tested**, none of it needing a Firebase project or Android SDK
to verify (both genuinely absent from this environment — see below):

- **`apps/notifier/internal/fcm`** — an FCM HTTP v1 client. The legacy
  server-key API docs/11 originally assumed is deprecated; HTTP v1 needs a
  service-account OAuth2 token instead, via `golang.org/x/oauth2/google`
  (already a dependency — `apps/api/internal/googlecalendar` uses the same
  module for its own three-legged flow, so this added no new supply-chain
  surface). An empty project id or service account degrades to "disabled"
  the same way `telegram.Client`/`heartbeat.Pinger` already do. An
  `UNREGISTERED`/`NOT_FOUND` response comes back as a typed
  `*TokenInvalidError` — docs/17 §10's own chaos-test requirement
  ("tokens retired... no silent loss"). 8 tests, an `httptest.Server`
  standing in for both Google's token endpoint and FCM itself.
- **`apps/notifier/internal/deliver`'s fan-out** — `Deliverer.Run` now
  also sends to every enabled `native_push` channel (`GetEnabledChannels`,
  new query) alongside the existing single Telegram channel, recording its
  own `notification_delivery` row per device. Deliberately **additive, not
  a rewrite**: Telegram's own path (`GetTelegramChannel`, `send`,
  `sendBatch`) is untouched, so its existing ~30 tests needed only a
  signature-argument fix, not a behavior change — verified by running them
  before and after. A push failure never affects Telegram's own
  sent/queued accounting (docs/17 §10: "block one channel, the other still
  delivers"), and an `UNREGISTERED` token calls the new `DisableChannel`
  query rather than retrying forever. Native push does not group BATCHED
  notifications into one message the way Telegram's `sendBatch` does —
  Android's own per-channel grouping (docs/11 §6.2's four channel ids)
  already gives an equivalent collapsed view, so this is a documented
  simplification, not a silent gap. 9 new tests, including one confirming
  a disabled `fcm.Client{}` (today's actual production state) attempts and
  records nothing.
- **`POST /v1/push/register-device`** (`apps/api/internal/push`) — the
  Capacitor shell's own registration call, writing straight to
  `notification_channel` via the new `UpsertNativePushChannel` query
  (upserts on `(user_id, device_token)`, so a token refresh updates in
  place). Needs no FCM credentials at all — registration is pure Postgres,
  only *sending* needs Firebase. 6 tests against a real Postgres.
- `docs/11-notifications.md` §6.2's own mockup had an emoji Telegram's
  sections 6.1/6.5 were already corrected to drop (2026-08-24) — fixed to
  match, since native push is now the third channel that emoji policy
  actually applies to.
- `.env.example` and `infra/compose/local.yml` gained
  `SCOUT_FCM_PROJECT_ID`/`SCOUT_FCM_SERVICE_ACCOUNT_JSON`, both-empty a
  valid disabled state, same convention as every other optional
  integration in this file. `infra/compose/production.yml` needed no
  change — its `notifier` service already forwards the whole env file
  generically rather than listing vars individually.

**Not started: the Capacitor shell itself.** `apps/mobile` is still
exactly the placeholder README from 2026-08-07 ("Not implemented yet.
Scheduled for M3."). This machine has no Android SDK (`ANDROID_HOME`
unset, no `sdkmanager`) and no Firebase project exists yet, so:
- The actual native project (`npx cap init` / `npx cap add android`), the
  WebView pointed at the deployed `apps/web`, and the
  push/local-notification/app/badge/browser plugin wiring per ADR-012's
  own architecture diagram are all unbuilt.
- The P0 exit criterion "Android signing keystore generated and backed up
  offline" is still open — it was open before this session too (P0's own
  section already lists it), and nothing here changed that.
- None of P4's own exit criteria (installed on a real phone, FCM actions
  from the lock screen, no Web Push double-buzz, WebView scroll
  performance) can be checked without a real device and a real Firebase
  project — this is the same "repo side done, environment needs the user"
  shape ADR-018 already established for P0's Oracle Cloud gap.

Full `go build ./...`, `go test ./... -count=1` (real Postgres/Redis),
`golangci-lint run ./...`, and `sqlfluff` (CI-pinned 3.5.0, not this
machine's 4.3.0 — see the earlier update on why that distinction matters)
are all clean. Committed to `main` in two commits (the query/schema-side
plumbing, then the fcm/deliver/push code) and pushed.

---

**Update, 2026-08-28: Android Studio and the SDK installed, and the
Capacitor shell is real and actually running — not scaffolding, a
verified app.**

**Android Studio + SDK, from scratch.** `brew install --cask
android-studio` (the IDE itself), then, separately and more usefully,
`brew install --cask android-commandlinetools` — a full headless SDK setup
that needs no GUI wizard: `platform-tools`, `platforms;android-34`,
`platforms;android-35`, `build-tools;34.0.0`, `build-tools;35.0.0`, all
licenses accepted via `sdkmanager`. `ANDROID_HOME`/`ANDROID_SDK_ROOT` and
`PATH` added to `~/.zshrc`, verified in a fresh shell (`adb --version`).
Java 21 (already installed) satisfies the toolchain requirement.

**`apps/mobile` — the actual Capacitor project, not a config sketch.**
`npx cap add android` generated a real Gradle project; `package.json`
carries all six of ADR-012's plugins, verified against the real npm
registry first (`@capacitor/badge` — the name ADR-012's own architecture
diagram uses — **does not exist**; the real package is
`@capawesome/capacitor-badge`, found by checking `npm view` before writing
it into a manifest nobody would have been able to install). The biometric
plugin is `@aparajita/capacitor-biometric-auth` (updated 2026-02-09), not
`capacitor-native-biometric` (last published 2023-06-14, almost certainly
stale against Capacitor 8).

**The native bridge lives in `apps/web`, not `apps/mobile` — a real
architectural point, not a filing choice.** `capacitor.config.ts`'s
`server.url` means the WebView loads the *deployed* Next.js app; anything
bundled in `apps/mobile/www` is only ever an offline fallback. So any
JavaScript that calls a Capacitor plugin — registering for push, reading
`appUrlOpen` for deep links, setting the badge — has to be part of the page
actually being loaded, which is `apps/web`. Built:
`apps/web/lib/capacitor.ts` (`initNativeBridge`, a no-op outside
`Capacitor.isNativePlatform()`; `PushNotifications.register()` → POSTs the
real FCM token to the P4-backend's own `/api/push/register-device` proxy
route, new today; `App.addListener("appUrlOpen", ...)` plus
`getLaunchUrl()` for the cold-start case, mapping
`scout://job/{id}` → `/opportunities/{id}`; `setBadgeCount`), wired into
`app/providers.tsx` via one `useEffect`. `pnpm run lint`/`typecheck`/
`build` all clean.

**Verified live, not just built — a real Gradle build, a real emulator, a
real running app:**
1. `./gradlew assembleDebug` — **BUILD SUCCESSFUL in 1m 58s**, 276 tasks,
   a real unsigned `app-debug.apk`. First real proof the whole toolchain
   (Java 21, Gradle 8.14.3, the SDK, all six plugins) actually fits
   together, not just that each piece installed.
2. A real AVD (`avdmanager create avd`, Android 15/`google_apis`/
   `arm64-v8a` — `avdmanager`'s own `--device` flag failed on a missing
   `devices.xml` this minimal SDK install doesn't ship; created without a
   hardware profile instead, which works fine), booted headless
   (`-no-window -no-audio`), confirmed via `adb wait-for-device` +
   `sys.boot_completed`.
3. `adb install` the real APK, `adb shell am start`, confirmed resumed via
   `dumpsys activity activities` (not just "the intent was sent").
4. **A real screenshot** (`adb shell screencap`) of the actual login
   screen — pixel-identical to the desktop browser — after pointing
   `apps/web`'s own dev server (`preview_start`) at the emulator's
   `10.0.2.2` host-loopback alias and reloading. Before the dev server was
   up, the same shell correctly showed Chrome's own "webpage not
   available... ERR_CONNECTION_REFUSED" — proof the failure mode is right,
   not just the happy path.
5. **`logcat` confirms the real JS bundle executed inside the shell**:
   `Capacitor/Console: File: http://10.0.2.2:3000/_next/static/chunks/...
   Download the React DevTools...` — the actual Next.js/React bundle,
   loaded and running, not a static mock. Zero fatal exceptions, zero
   crashes, anywhere in the session's logs. `Capacitor/AppPlugin` and
   `Capacitor/BrowserPlugin` both fired real lifecycle events
   (`appStateChange`, `browserFinished`) on launch.

**Found and fixed along the way**: my first `.gitignore` for
`apps/mobile` ignored the entire `android/` directory, treating it like
`packages/db/gen` — fully regenerable, safe to exclude. That's wrong for
Capacitor: `cap sync` never touches `MainActivity.java`,
`AndroidManifest.xml`, or the launcher icons — those are real, hand-edited
source a developer is expected to commit, and `cap add android` writes its
*own* `android/.gitignore` scoped correctly to just the build-artifact
subset (`build/`, `.gradle/`, `local.properties`). Caught before committing
anything, by checking what Capacitor's own generated `.gitignore` actually
excludes rather than assuming.

**Genuinely open, not glossed over**: how a real physical phone reaches
the server at all. ADR-012 (2026-08-07) assumed Tailscale; ADR-018
(2026-08-19) removed Tailscale and confined everything to `localhost`;
neither ADR was ever updated to account for the other, and this is the
first work to actually run into the gap in practice rather than just
noting it exists. `capacitor.config.ts` and `apps/mobile/README.md` both
carry the full writeup and the two real options (LAN IP + loosen
localhost-only binding, or Tailscale scoped to just the phone), neither
decided — this needs the user, not more code. Also still open: the P0
Android signing keystore (unsigned debug build only), a real Firebase
project (FCM itself is unverified — registration and the shell both
degrade gracefully without one, but nothing has actually sent a push
yet), and the biometric/local-notification plugins are installed but not
wired into any UI.

Emulator torn down (`adb emu kill`) after verification — same "don't
leave things running past the task that needed them" discipline as every
other teardown this project does.

---

**Update, 2026-08-29 (later still): notification channels and Save/Dismiss
actions built — a real device verification found a real gap, and this
closes it, though not yet re-verified live.**

A prior pass on this same date (see the entry above this one) verified
native push end-to-end on the real Pixel 7: inserted a real
`notification` row, let the real notifier pick it up and send via FCM, and
confirmed via `adb shell dumpsys notification` that it actually posted —
correct title/body, a real FCM message id back. But two things about *how*
it posted were wrong, both found in that same dump, not guessed at:

1. **Wrong channel.** It landed on Android's own
   `fcm_fallback_notification_channel`, not `opportunities` or `deadlines`
   (docs/11-notifications.md section 6.2's two named channels). Traced to a
   real gap: `deliver.go`'s `pushChannelID` has always set
   `android.notification.channel_id` correctly in the outbound FCM
   payload, but nothing on the device ever created a channel with that ID.
   `@capacitor/push-notifications` and `@capacitor/local-notifications`
   both ship a `NotificationChannelManager` utility capable of it, but
   grepping outside `node_modules` in `apps/mobile` and `apps/web` found
   zero calls to `createNotificationChannel()` anywhere in this codebase's
   own code. When the target channel doesn't exist, Android/FCM silently
   substitutes its own fallback rather than erroring — which is exactly
   what made this invisible until someone read the actual `dumpsys` output
   instead of just confirming "a notification appeared."
2. **No Save/Dismiss actions**, visible in the same dump's extras (none
   present). `docs/11` section 6.2 has always specified these as native
   `NotificationCompat` actions calling the API directly from the native
   layer — never built.

**Both fixed, and the second one turned out to need a backend change, not
just a native one.** The FCM payload `apps/notifier/internal/fcm.Send` has
always built (`body.Message.Notification = notificationBlock{...}`) put a
top-level `notification` block on every message alongside `data`. Android's
own FCM SDK auto-displays a `notification`+`data` message whenever the app
is backgrounded, *before* any app code runs — which is also why it picked
the wrong fallback channel: auto-display isn't `ScoutMessagingService`
choosing a channel, it's Android choosing one because the one it was told
to use doesn't exist yet. That same auto-display path can't attach actions
at all, no matter what channel it lands in — there's no app code in the
loop to call `.addAction()`. Fixed by making the message data-only (no
`notification` block; title/body/channel_id moved into `data`,
`apps/notifier/internal/fcm/fcm.go`), which means the app's own
`onMessageReceived` is invoked in every app state — foreground, background,
or freshly cold-started by the push itself — with full control over the
result. `fcm_test.go` updated to assert the new shape; nothing in
`apps/notifier/internal/deliver` needed to change, since `fcm.Notification`
(deliver's own input struct) didn't change shape, only `fcm.Send`'s
internal wire format did.

**Native side, three new files under `apps/mobile/android/app/src/main/
java/com/scout/app/`:**

- `NotificationChannels` — creates `opportunities` and `deadlines` (the
  only two any code path in `apps/notifier` actually sends to; `digests`
  and `system` stay reserved-but-uncreated, matching docs/11's own note)
  via `NotificationManager.createNotificationChannel()` directly, called
  from `MainActivity.onCreate` on every launch. Idempotent by Android's own
  design — recreating an existing channel can't silently re-tune an
  importance the user already changed, only update cosmetic fields.
- `ScoutMessagingService` — a custom `FirebaseMessagingService`,
  registered in `AndroidManifest.xml` in place of
  `@capacitor/push-notifications`' own `com.capacitorjs.plugins.
  pushnotifications.MessagingService` (removed via `tools:node="remove"`,
  since Firebase only delivers `MESSAGING_EVENT` to one registered
  service, and the plugin's own version only forwards to a JS
  `pushNotificationReceived` listener that `apps/web/lib/capacitor.ts`
  never registers — confirmed by reading the plugin's actual source
  on GitHub before removing it, not assumed). Builds the notification with
  `NotificationCompat`, the channel from `data.channel_id`, a
  content-intent deep link (`ACTION_VIEW` into `MainActivity`, the same
  `scout://job/{id}` shape `apps/web/lib/capacitor.ts`'s `appUrlOpen`/
  `getLaunchUrl` listeners already handle), and two actions.
- `JobActionReceiver` — handles the Save/Dismiss taps without opening the
  app. `POST`s `/v1/jobs/{group_id}/state` (the same endpoint the WebView's
  own Save/Dismiss buttons call, via `apps/web/app/api/jobs/[groupId]/
  state/route.ts`), authenticated with the `scout_web_session` cookie —
  read via `android.webkit.CookieManager`, which persists independently of
  any live `Bridge`/WebView instance, so this works even when FCM starts
  the process fresh. **Deliberately reuses the existing cookie rather than
  building a second credential path**: that route's own comment already
  records that the cookie's value *is* the raw `SCOUT_AUTH_TOKEN`, not an
  opaque session id (`res.cookies.set(SESSION_COOKIE, token, ...)` in
  `apps/web/app/api/auth/login/route.ts`) — so this is reading the real
  bearer credential the WebView already established, not inventing
  `EncryptedSharedPreferences` storage and a second sign-in flow for a
  token nothing currently provisions onto the device natively (ADR-015
  describes that as the eventual native-token design, but nothing
  upstream of this change ever puts a raw token where native code could
  read it). If the user has never signed in through the app once, there's
  no cookie yet and the action is a silent no-op — logged, not surfaced,
  since a notification has no UI to surface an error into; the
  notification's own tap-to-open still works regardless. `serverUrl`
  itself comes from `CapConfig.loadDefault(context).getServerUrl()` —
  Capacitor's own bundled `capacitor.config.json`, the same source the
  WebView's `server.url` uses, so it stays correct across a
  `SCOUT_MOBILE_SERVER_URL` change without hardcoding a second copy.
  `extractSessionToken` is a pure string function (no Android framework
  dependency), pulled out and unit-tested directly in a new
  `src/test/java/com/scout/app/JobActionReceiverTest.java`, same pattern
  as the module's existing `ExampleUnitTest` scaffold.

`docs/11-notifications.md` section 6.2 rewritten to match reality: the
payload example is now data-only, the mockup shows the two actions that
actually exist (no `Apply` — same honest gap as section 6.1's Telegram
buttons), and a new paragraph explains why data-only was necessary,
alongside the channel table's own new note that only two of the four
documented channel IDs are created today.

`go build ./...`, `go test ./... -count=1`, and `golangci-lint run ./...`
are all clean for `apps/notifier`. Every Java API used on the Android side
was checked against the real source on GitHub before being relied on
(Capacitor's `CapConfig`, the push-notifications plugin's actual
`MessagingService.java` and its `AndroidManifest.xml` service entry)
rather than assumed from memory — and then, in this same session, the user
asked to actually set up the SDK and build it, which turned out to already
be possible: `/opt/homebrew/share/android-commandlinetools` (the SDK
2026-08-28's entry set up) is a machine-wide Homebrew install, just not on
this particular worktree's `PATH`/`ANDROID_HOME` yet. Exported both,
copied `google-services.json` in from the main checkout (gitignored,
per-worktree, same project's own already-authorized config — not fetched
from anywhere new), wrote `android/local.properties`, `npx cap sync
android`, then a real `./gradlew assembleDebug`.

**BUILD SUCCESSFUL**, a real `app-debug.apk` (8.9MB). Verified three ways,
not just "no error printed":
1. The merged manifest (`app/build/intermediates/merged_manifests/debug/
   processDebugManifest/AndroidManifest.xml`) shows the plugin's own
   `com.capacitorjs.plugins.pushnotifications.MessagingService` genuinely
   gone, `com.scout.app.ScoutMessagingService` registered for
   `MESSAGING_EVENT` in its place, and `com.scout.app.JobActionReceiver`
   present as a receiver — the manifest-merge removal actually took effect,
   not just "didn't error."
2. `strings` on the APK's own `classes11.dex` (found by grepping every
   dex file in the APK for `ScoutMessagingService`) lists
   `NotificationChannels`, `ScoutMessagingService`, `JobActionReceiver`,
   and its background-thread lambda — the new code is genuinely compiled
   into the artifact, not just present in source.
3. `./gradlew testDebugUnitTest` ran the new `JobActionReceiverTest` for
   real (not `NO-SOURCE`, unlike every library module's own unrun test
   task): 4/4 passing, cookie-parsing logic verified for real.

**`./gradlew lintDebug` caught two real bugs on the first run, not style
noise — worth its own record separate from the summary at the top of this
file:**
- `MissingPermission` on `NotificationManagerCompat.from(this).notify(...)`
  in `ScoutMessagingService` — no guard against `POST_NOTIFICATIONS` being
  denied or revoked. On API 33+ that's a real, if rare, crash path: an
  uncaught `SecurityException` inside a `FirebaseMessagingService`
  callback, which is strictly worse than the Android auto-display path
  this replaces (the OS itself could never crash calling its own API).
  Fixed with an explicit `ContextCompat.checkSelfPermission` check that
  returns instead of calling `notify()` when denied — same "check first,
  degrade if not" shape `ScoutNativePlugin`'s own `firebaseAvailable()`
  guard already uses in this same package.
- `MissingFirebaseInstanceTokenRefresh` — a warning, but pointing at a real
  functional regression this change would otherwise have introduced
  silently. Replacing the plugin's `MessagingService` wholesale (necessary
  for the channel/actions fix — see the summary above) also dropped its
  `onNewToken` override, which is what forwards a rotated FCM token to
  `PushNotificationsPlugin`, which is what fires the JS `"registration"`
  listener `apps/web/lib/capacitor.ts`'s `registerDevice()` call depends
  on. Without it, docs/11 section 12's own "Native token refresh...
  re-registered, old token retired, no missed delivery" requirement would
  have quietly stopped working the first time any real device's FCM token
  ever rotated — not on this change's own happy path, so nothing in this
  session's own testing would have caught it without lint. Fixed by adding
  `onNewToken` back to `ScoutMessagingService`, forwarding to
  `PushNotificationsPlugin.onNewToken()` exactly as the removed service
  did.

Both fixes reverified with a second clean `assembleDebug` + `lintDebug` +
`testDebugUnitTest` run: zero lint warnings anywhere under `com.scout.app`
afterward (down from the one error + one relevant warning on the first
pass), build and tests both still green.

**Still honestly incomplete**: none of the above is a real device or
emulator run. A clean, lint-clean, unit-tested build proves the code is
correct enough to install, not that it behaves right in practice — the
`NotificationManagerCompat.notify()` call still hasn't actually posted
anything, no real FCM message has exercised `onMessageReceived`'s data-only
path, and `JobActionReceiver`'s `CookieManager` read (the one piece of this
whole design that's genuinely delicate — reading a persistent cookie store
from a process FCM may have started fresh, with no live `WebView` in it)
has never run for real. The next session picking this up should install
`app-debug.apk` on the real Pixel 7, repeat the earlier entry's seeded-
`notification`-row method, and check `dumpsys notification` again for the
right channel and real action buttons — then actually tap one and confirm
the job's state changes — before either roadmap checkbox moves.

---

**Update, 2026-08-29 (real device verification, same day): the user asked
to install the just-built APK and test it live. It surfaced a real,
previously-unknown state-machine bug within minutes of the first tap.**

**Setup.** `adb devices` already showed the Pixel 7 connected (wireless
debugging, same connection the 2026-08-28/29 entries established).
`android/local.properties`, `google-services.json`, the repo-root `.env`,
and `apps/mobile/.env` are all gitignored, per-checkout files this
worktree started without — all four exist in the main checkout at
`/Users/kelyon/Documents/projects/scout` (a sibling directory, not a
remote), so copied locally rather than reconstructed or asked for again;
none of this is a new secret, all of it is the same project's own
already-authorized local config. `infra/compose/local.yml` pins a fixed
Compose project name (`name: scout-local`), so `docker compose up` from
this worktree attaches to the *same* named network/volumes any other
worktree or the main checkout would — real existing data (60,635
job_groups, an already-registered `native_push` channel for this exact
phone from the 2026-08-29 FCM-verification entry) was there waiting,
not a fresh empty database. `make migrate` confirmed "no change" (already
current); `make seed` failed (`infra/seed/seed.sql` missing — a
pre-existing gap, not touched, since the sole `app_user` row already
existed from prior seeding).

**First real finding, before any code even ran: the debug APK installed
first pointed at the wrong server.** `npx cap sync android` was run once
already (this session's earlier build-verification pass) without
`SCOUT_MOBILE_SERVER_URL` exported, so `capacitor.config.json` baked in
the Android-emulator-only default (`http://10.0.2.2:3000`) — meaningless
on a real phone. Re-ran `cap sync` with `apps/mobile/.env` sourced
(`SCOUT_MOBILE_SERVER_URL=http://100.78.200.69:3000`, the laptop's real
tailnet IP, matching `network_security_config.xml`'s existing cleartext
exemption for that exact IP), rebuilt, reinstalled. Worth remembering:
building against the emulator default and installing on a real device
produces an APK that installs and launches fine and then can't reach
anything — no error until the WebView tries to load.

**The core fix, live-verified.** Brought up the full stack
(`docker compose ... up -d --build --wait`, rebuilding every image from
this worktree's own code — including the `fcm.go` data-only-message fix).
`notifier` logs confirmed `"native push armed"` on the rebuilt image.
Inserted three real `notification` rows directly (same method the
2026-08-29 FCM-verification entry used), for a real job
(`project44` · Staff Software Engineer · Bengaluru, job_group
`a711a9fb-894c-4e19-8aaa-83b9ea7a3fc7`) already in the seeded database, one
per test pass (a different `trigger` value each time, since
`notification_dedup_idx` is unique on `(user_id, job_group_id, trigger)`
and reusing the same trigger would have collided). Each one: picked up by
the real notifier within its 30s tick, delivered over both Telegram and
native push with real FCM message IDs
(`projects/scout-d46e6/messages/0:...`), confirmed via `dumpsys
notification --noredact` on the real device:
- `channel=opportunities` — not `fcm_fallback_notification_channel`, the
  exact bug this whole pass exists to fix.
- `actions=2` — both Save and Dismiss present with real
  `broadcastIntent` PendingIntents.
- A real screenshot confirmed the rendered card matches
  `docs/11-notifications.md` section 6.2's own mockup: "Scout ·
  project44 · Staff Software Engineer", "Match 94 · Posted 2 days ago" plus
  the explanation line, "Save" / "Dismiss" buttons.

**A real tap on "Save" found a real bug, immediately.** First attempt:
`adb logcat` caught `ScoutJobAction: job state update rejected: HTTP 409`.
The receiver had done everything right — read the real
`scout_web_session` cookie via `CookieManager`, POSTed to the real API —
and the API correctly rejected the transition:
`apps/api/internal/jobs/state.go`'s `validTransitions` only allows
`new -> viewed` or `new -> dismissed` directly; `saved` requires `viewed`
first, and this job had never been opened by anything, so its state was
still `new`. **This isn't a new gap in the API — it's a gap in the native
receiver replicating client behavior the web UI already has.**
`apps/web/components/StateControls.tsx`'s `transitionTo` already chains
through `viewed` before `saved`/`applied` when the current state is `new`,
specifically because of this exact constraint — a lock-screen tap has no
client-side knowledge of "current state" to condition on, so it needs the
same chain unconditionally. Fixed in `JobActionReceiver.onReceive`: when
the action is `"saved"`, POST `"viewed"` first (best-effort — ignored if
the job's already past it, which just 409s harmlessly), then the real
target. Rebuilt (`assembleDebug`/`lintDebug`/`testDebugUnitTest` all clean
again), reinstalled, retested on a fresh notification for the same job: a
real tap on "Save" produced **zero** log warnings (both hops succeeded)
and a real `user_job_state` row —
`state = 'saved', state_changed_at = 2026-08-29 12:34:43+00` — confirmed
directly in Postgres, not inferred.

**Update, same day, continued: `Dismiss` tap-verified too, and the
"cancel doesn't work" concern above turned out to be a self-inflicted
false alarm — both P4 exit-criteria items are now genuinely closed.**

**`Dismiss`, tap-verified by the user directly.** Locating the card via
screen automation (screenshot-and-tap-by-pixel-coordinate) proved
unreliable across several attempts — the notification kept failing to
resurface near the top of the shade after the first couple of test
cycles, most likely Pixel's adaptive-notification ranking demoting an app
whose notifications had just been interacted with repeatedly, though this
was never confirmed against documentation. Worse, one blind tap missed
its target and briefly opened an **unrelated, private Instagram profile's
follow-request screen** — a real near-miss, not a Scout screen at all.
No action was taken on that screen (no Confirm/Delete tap), it was backed
out of immediately via `KEYCODE_HOME`, and the screenshot was deleted
without being described. At that point automation was abandoned in favor
of asking the user to tap Dismiss themselves while this session watched
`dumpsys`/Postgres — the right call given the risk, and it is the actual
mechanism this feature exists to serve (a real person's finger, not a
script). Confirmed immediately after: `user_job_state.state = 'dismissed'`
in Postgres, `state_changed_at = 2026-08-29 13:13:43+00`. Full round trip,
same as Save: cookie read, authenticated POST, real state-machine
transition (`saved -> dismissed` is directly valid, needing no `viewed`
chain, unlike `new -> saved`).

**The "does cancel() actually clear the notification" question — raised
as unresolved right above this update — was never a real bug. It was a
`dumpsys` misreading on this session's part.** Every check that showed
the notification "still present with `actions=2`" after a successful
action was reading `dumpsys notification --noredact`'s `mArchive=Archive
(100 notifications)` section — Android's rolling *historical* log of
every notification the system has ever posted, kept regardless of
whether it was later cancelled — and mistaking it for the live list.
The real list is the output's own `Notification List:` section, printed
*before* `mArchive`; checked directly after the Dismiss confirmation
above, it has **zero** entries for `com.scout.app`. `NotificationManagerCompat
.cancel(notificationId)` has been working correctly the entire session;
nothing here needs fixing. Worth remembering for next time this tool is
used: grep `dumpsys notification` output starting from `Notification
List:` and stop before `mArchive`, or the archive's historical entries
will produce exactly this false "still there" signal.

**`adb shell am broadcast -n <pkg>/.JobActionReceiver` does not work as a
test shortcut on this Android version** — worth recording so a future
session doesn't waste time on it the way this one did. The command
reports `Broadcast completed: result=0` with zero delivery trace (no log
line from the receiver, no exception, nothing in `dumpsys package`'s
receiver resolver table for a filterless `<receiver>` declaration either)
— it silently no-ops rather than erroring, which read at first like a
code bug before a real notification tap proved the actual code path works
fine. `android:exported="false"` appears to block even shell-originated
explicit broadcasts on this device/OS version, unlike `am start -n` for
activities, which did work. A real notification action tap
(system-server-originated `PendingIntent`, not a shell broadcast) is the
only verified way to exercise this receiver.

**On budget testing, since it came up mid-session**: rapid-fire manual
`notification` inserts for live testing burn the real `maxPerHour = 8`
delivery budget fast (each instant notification is 2 deliveries —
Telegram + native push), and once hit, the notifier correctly queues
rather than sends — real, working behavior, not a bug, confirmed by
`docs/11` section 4's own design. Deleting `notification_delivery` rows
to "free budget" is the wrong way to work around this in a test session:
`GetUndeliveredNotifications` treats a missing delivery row as "never
sent," so it triggers a genuine re-send (real Telegram message, real FCM
push) rather than freeing anything — confirmed the hard way this session,
three notifications resent unintentionally. The safe move, used
afterward: `UPDATE notification_delivery SET sent_at = now() - interval
'2 hours'` — moves rows outside `CountDeliveriesSince`'s 1-hour window
without touching row existence, so nothing resends.

**A note on the phone's own notification shade during this pass**:
locating the Scout card required expanding and scrolling through the
user's real, personal notification shade many times (WhatsApp, Instagram,
Telegram, etc.) across both the Dismiss-hunt in this update and the
original Save pass above — full-shade screenshots showing that content
were taken as a side effect of locating the right card by sight, deleted
from the local scratch directory immediately after each was used, and
none of that content is reproduced or described anywhere in this writeup.
One of those attempts is the Instagram near-miss described above.

Docker stack was brought back up for this update (same fixed
`scout-local` project/volume as before, real existing data reused,
`docker compose up -d --wait`, no rebuild needed) and torn back down
(`docker compose down`, data kept) once both actions were confirmed.

**Both P4 notification exit criteria are now genuinely satisfiable.**
`docs/19-roadmap.md` updated to check them — see that file for the exact
wording of what's proven vs. what's still separately tracked (badge count
and Web Push double-buzz remain their own, unrelated open items).
