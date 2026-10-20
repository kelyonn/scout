# Hosted setup checklist (ADR-019)

Everything below needs **you**: it means creating accounts or pasting secrets.
All code, workflows, the Render blueprint and the bootstrap script already
exist. Do the steps in order; each takes a few minutes. Never paste secrets into
chat, commits or issues — only into the dashboards named here.

## 1. Accounts (free, no card)

| Service | For | Notes |
| --- | --- | --- |
| Neon | Postgres | New project, region closest to you (e.g. Singapore). Copy the **pooled** connection string; it must end in `?sslmode=require`. Free storage is ~0.5 GB. |
| Upstash | Redis | New Redis database, same region. Copy the `rediss://` URL. |
| Render | API | "New → Blueprint", point it at the GitHub repo; it reads `render.yaml`. |
| Vercel | Web | Import the repo, **Root Directory = `apps/web`**. |

## 2. Bootstrap the database (once, from your laptop)

```bash
SCOUT_DATABASE_URL='<neon connection string>' \
SCOUT_USER_EMAIL='<your email>' \
./infra/scripts/bootstrap-hosted-db.sh
```

Applies all migrations and seeds the user, ~1,250 source boards and company
metadata. It deliberately does not copy local jobs (the collector refills them,
and the local database is already bigger than Neon's free tier).

## 3. GitHub repository secrets (Settings → Secrets and variables → Actions)

Secrets: `SCOUT_DATABASE_URL`, `SCOUT_REDIS_URL`,
`SCOUT_COLLECTOR_OPERATOR_EMAIL` (the address that goes in the User-Agent),
`SCOUT_NOTIFIER_TELEGRAM_BOT_TOKEN` (create a bot with @BotFather).
Optional: `SCOUT_FCM_PROJECT_ID`, `SCOUT_FCM_SERVICE_ACCOUNT_JSON` (phone push),
`SCOUT_BRAIN_GEMINI_API_KEY`, `SCOUT_BRAIN_GROQ_API_KEY`,
`SCOUT_BRAIN_OPENROUTER_API_KEY` (free-tier model keys).

**Variable** (same page, Variables tab): `SCOUT_FIXTURES_ONLY` = `false`.
Without it the workflow runs but fetches nothing — that is the safe default.

Then Actions → ingest → "Run workflow" once and watch it go green.

## 4. Render (API)

In the service's Environment tab fill the `sync: false` values:
`SCOUT_DATABASE_URL`, `SCOUT_REDIS_URL`, `SCOUT_AUTH_TOKEN` (pick a long random
string — this is your login token), `SCOUT_USER_EMAIL` (same as step 2).
Note the service URL, e.g. `https://scout-api.onrender.com`.

## 5. Vercel (web)

Environment variables: `SCOUT_API_URL` = the Render URL, `SCOUT_AUTH_TOKEN` = the
same token as Render. Deploy. Open the Vercel URL and sign in with that token.

## 6. Upload your resume

The bootstrap does not load it (personal data). Use the Resume page in the app.

## 7. Phones (ask Claude, or do it yourself)

The Android app is a shell around the web app, so the only build-time setting is
the URL. HTTPS needs no network-security-config change.

```bash
cd apps/mobile
SCOUT_MOBILE_SERVER_URL='https://<your-vercel-url>' pnpm exec cap sync android
cd android && ./gradlew assembleRelease   # needs keystore.properties
adb install -r app/build/outputs/apk/release/app-release.apk
```

Uninstall the debug build first if `install -r` reports a signature mismatch
(this clears the app's local data only).

## Neon's 0.5 GB: retention

The local database was 1.2 GB after one live day (`job` 805 MB, `raw_observation`
204 MB), so the hosted one would have hit Neon's limit within days. Bounded
retention now runs on every collector maintenance pass (each `-once` cycle),
and settled at ~200 MB for ~38k jobs in measurement — see
[03-data-model.md](03-data-model.md) section 11 and risk R39 in
[20-risks.md](20-risks.md).

What it does: cold jobs (closed, non-software, >45 days old) have descriptions,
embeddings and raw payloads cleared but keep their identity and group; payloads
of old raw observations are scrubbed (rows kept); finished and stale queue rows
are deleted. Anything you saved, applied to, noted, or were notified about is
never touched.

Things to know:

- **Bound the first sweep.** The first run ingests tens of thousands of postings.
  If one `-once` cycle can ingest more than ~100 MB before its retention pass,
  Neon can reject writes mid-cycle. Cap sources per cycle (`RunUntilIdle` is
  unbounded) until the backlog is drained.
- **Watch the log line** `retention` (sizes in MB) and the warning
  `database size is near the free-tier limit`. Above 380 MB the aggressive
  windows engage automatically.
- **Dead space.** Plain vacuum reuses freed space but does not shrink files. If
  the size warning persists after a run, reclaim it once (connect with `psql`):
  `VACUUM FULL job; VACUUM FULL job_score;` (takes a lock; run when idle).
- The steady-state rate of new software postings is unmeasured (the local data
  was one burst). Re-check `select pg_size_pretty(pg_database_size(current_database()))`
  after a week.
