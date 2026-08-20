# apps/brain

Python 3.12 service: embeddings, classification, dedup adjudication, scoring
explanations. See [`docs/02-architecture.md`](../../docs/02-architecture.md).

Per [ADR-001](../../docs/adr/ADR-001-monorepo-and-language-split.md), Go and
Python never call each other synchronously — they communicate through
Postgres and River (`packages/queue` on the Go side enqueues,
`packages/riverpy` here consumes; results are written back as plain
Postgres columns, never a reply queue).

## What's here

- `scout_brain/embeddings.py` — local embeddings via `fastembed`
  (`bge-small-en-v1.5`, 384-dim, ONNX, no network call once the model is
  cached).
- `scout_brain/embed_consumer.py` — consumes the `embed` queue: computes and
  writes `job.embedding`/`job.embedding_version`.
- `scout_brain/worker.py` — the entrypoint; one long-running process.

## Running it

```bash
uv run python -m scout_brain.worker
```

Requires `SCOUT_DATABASE_URL`. Dockerized via
`infra/docker/brain.Dockerfile`, wired into `infra/compose/local.yml` as the
`brain` service.

## Testing

```bash
uv run pytest
uv run ruff check .
```

DB-backed tests skip (not fail) without a reachable Postgres — set
`SCOUT_TEST_DATABASE_URL` or run against the local dev stack.
