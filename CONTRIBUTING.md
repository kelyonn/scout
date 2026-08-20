# Contributing to Scout

---

## Setup

**Prerequisites today:** Docker and Compose, Go 1.23+, `make`. Python with `uv`
and Node with `pnpm` join when the brain (P2) and the web app (P3) do — see
[`docs/19-roadmap.md`](docs/19-roadmap.md). Nothing installs a toolchain you do
not yet need.

```bash
git clone <repo> scout && cd scout
make dev            # starts the local stack, migrated, from a clean clone
```

That is the whole setup. `make dev` copies `.env.example` to `.env` if you have
no `.env`, builds the service images, waits for everything to report healthy, and
applies migrations.

**Local development never fetches live sources.** `SCOUT_FIXTURES_ONLY=true` is
the default and the collector refuses live egress when it is set. Development is
therefore deterministic, works offline, and cannot accidentally hammer a real
company's careers page during a debugging session.

```
http://127.0.0.1:8081/health    api
postgres://scout@127.0.0.1:5433/scout
redis://127.0.0.1:6380/0
```

Ports are non-default on purpose so Scout coexists with another project's stack.
A collision here is nasty to diagnose, because `psql` connects successfully to
the wrong database.

The API requires a token on everything except `/health`
([ADR-015](docs/adr/ADR-015-single-user-auth.md)). Locally that is the
placeholder in `.env.example`:

```bash
curl -H "Authorization: Bearer local-development-token-not-a-secret-000" \
  http://127.0.0.1:8081/v1/jobs
```

### Useful commands

```bash
make dev              # full local stack, migrated
make dev-db           # database only, for running one service natively
make down             # stop, keeping data
make clean            # stop and delete the data volume
make logs             # follow local logs
make migrate          # apply pending migrations
make test             # all tests
make lint             # golangci-lint + sqlfluff
make fmt              # gofmt
make compliance       # the banned-dependency gate
make restore-drill    # restore the latest nightly into a throwaway container
make help             # everything, including the deploy and backup targets
```

`make evals` and `make fixtures` exist and deliberately fail with a pointer to
the milestone that builds them. They are in the Makefile because runbooks and
docs already reference them, and a documented command that is simply absent fails
someone at step one.

---

## How this repository works

**Specification-first.** `docs/` defines required behavior; code implements it.
This is unusual and it is deliberate — Scout runs unattended and its worst
failures are silent, so the intended behavior needs to be written down somewhere
that a test can reference.

Read [`AGENTS.md`](AGENTS.md) for the condensed rules. Read the relevant
`docs/` chapter before implementing anything non-trivial.

**Requirement IDs.** Specs tag requirements as `SCOUT-<AREA>-<NUMBER>`. Tests
reference them in their names, so spec coverage is mechanically checkable:

```go
func TestBengaluruAlwaysRanksHigher_SCOUT_RANK_013(t *testing.T) { ... }
```

**ADRs are immutable.** To change a decision, write a new ADR that supersedes the
old one. Do not edit an accepted ADR except to add a "superseded by" header. The
history of why something was reversed is usually more valuable than the reversal.

---

## Workflow

```
1. Read the relevant spec section.
2. Branch: feat/<short-description> or fix/<short-description>
3. Write a failing test first, where practical.
4. Implement.
5. make lint test        (and `make evals` once P2 builds it)
6. Update the spec if behavior changed — same PR.
7. Open a PR against main.
```

### Commits

[Conventional Commits](https://www.conventionalcommits.org/), scoped:

```
feat(collector): add Recruitee adapter
fix(dedup): filter candidates by embedding_version
perf(api): add covering index for the feed query
docs(adr): supersede ADR-003 with NATS migration
test(notifier): add backfill suppression test
chore(deps): bump pgx to 5.7
```

Types: `feat`, `fix`, `perf`, `refactor`, `docs`, `test`, `chore`, `ci`.
Scopes: `api`, `collector`, `brain`, `notifier`, `web`, `schema`, `adapters`,
`infra`, `evals`, `adr`.

### Pull requests

The template asks four things. Answer all of them:

1. **What changed?**
2. **Why?**
3. **Which spec section does this implement or modify?**
4. **What was tested?**

Plus a checklist that includes: spec updated if behavior changed, tests added,
evals passing, and — for any new source — the legal posture basis stated (which
robots.txt path was checked, what the terms say).

---

## CI gates

Every one must pass to merge:

```
lint · typecheck · unit · contract · integration · evals
e2e · a11y · lighthouse · security · migrations · catastrophe
```

Roughly 6 minutes. The unusual ones:

**evals** — golden-set quality suites. A prompt or threshold change that degrades
classification precision fails the build even though nothing crashed.

**catastrophe** — the four tests protecting against unrecoverable mistakes:
backfill notification suppression, rescore suppression, notification uniqueness
under concurrency, and the compliance gate. These run on every PR regardless of
what changed.

**migrations** — applies against a production-sized snapshot with a 30-second
ceiling, because a migration that is instant on an empty table can be a 40-minute
outage on the real one.

Overriding a gate requires the `ci-override` label and a written reason. It is
logged, and it should be rare enough that each use is memorable.

---

## Adding an adapter

The most common contribution. Roughly:

```
1. Determine the access method — feed, then JSON-LD, then sitemap,
   then HTML, in that preference order.
2. Check legality. Fetch robots.txt. Read the terms. Set legal_posture
   and state the basis in the PR.
3. adapters/<category>/<name>/
   ├── adapter.go        implements Fetch, Parse, Validate
   ├── adapter_test.go
   ├── fixtures/         recorded responses
   └── fixtures/expected/
4. Record fixtures for: standard, empty, single-item, unicode-heavy,
   missing-location, compensation-variants, malformed, and paginated.
5. Implement Validate — what does a plausible result look like for this
   source? This is the per-adapter silent-failure detector.
6. Register the kind in packages/schema.
7. Shadow-run in staging for 48 hours before activating.
```

`Parse` must be pure and deterministic. That is what makes fixture replay
possible, and fixture replay is what catches format changes before they reach
production.

---

## Adding a prompt

```
1. packages/prompts/<task>.v<N>.md with front-matter:
   tier, output schema, token budget, eval set path.
2. Add or extend the eval set in evals/<task>/.
3. Run make evals and attach the diff to the PR.
4. Pin the model version.
```

Prompt changes get the same review as code changes, because a prompt change can
silently degrade classification across the whole corpus.

---

## Adding a source

For a company already on a known ATS this is automated:

```bash
make discover-sources
```

This pulls fresh company-slug candidates from a small set of pinned,
MIT-licensed lists (`apps/collector/cmd/discover/main.go`), skips anything
already in the `company` table, verifies each new candidate live against
its real ATS API, and only inserts the ones with at least one current
software/entry-level posting (`apps/collector/internal/discovery`'s
`Assess` — the same classify logic the ingestion pipeline itself trusts,
not a separate heuristic). Everything lands `pending_review`, same as
every other source — this never auto-activates anything. Safe to run
repeatedly; each run only looks at candidates not seen before.

Meant to run on a schedule, not just by hand — e.g. weekly from cron:

```cron
0 3 * * 0 cd /path/to/scout && make discover-sources >> logs/discover.log 2>&1
```

For anything not on a known ATS, use the admin UI or:

```sql
INSERT INTO source (company_id, kind, url, url_hash, legal_posture,
                    status, base_interval_s, max_rps, notes)
VALUES (..., 'pending_review', 900, 0.5,
        'robots.txt allows /careers; ToS silent on automated access');
```

`status = 'pending_review'` means it collects but does not notify. Promote to
`active` after 48 hours of verified-correct parsing.

---

## Debugging

**A job did not appear.** Trace it backwards:

```sql
-- Was it observed?
SELECT * FROM raw_observation WHERE canonical_url_hash = sha256('<url>');
-- Was it normalized?
SELECT * FROM job WHERE canonical_url LIKE '%<fragment>%';
-- Was it merged into an existing group?
SELECT * FROM job_merge_event WHERE job_id = '<id>';
-- Was it scored?
SELECT * FROM job_score WHERE job_id = '<id>';
-- Was it suppressed?
SELECT * FROM notification WHERE job_group_id = '<gid>';
```

**A trace explains everything.** Every job has a `trace_id` that spans all four
services. Find it in the logs and open it in Tempo — the whole lifecycle is one
waterfall, including the queue hops between processes.

**Testing an adapter against a live source:**

```bash
docker compose exec collector /app/scout-cli fetch \
  --source-id <id> --dry-run --verbose
```

Prints the raw response and parse result without writing anything.

---

## Getting help

Read the spec first. Then the ADR, which usually explains why something is built
the way it is. Then [`docs/runbooks/`](docs/runbooks/) if something is broken.

If the spec is wrong or unclear, that is worth an issue on its own — an unclear
spec produces inconsistent implementations, and this repository leans on its
specs heavily enough that keeping them accurate is real work with real value.
