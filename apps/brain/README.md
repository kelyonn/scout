# apps/brain

Python 3.12 service: embeddings, classification, dedup adjudication, scoring
explanations. See [`docs/02-architecture.md`](../../docs/02-architecture.md).

**Not implemented yet.** Scaffolding lands in its own module. Per
[ADR-001](../../docs/adr/ADR-001-monorepo-and-language-split.md), Go and Python
never call each other synchronously — they communicate through Postgres and the
job queue.
