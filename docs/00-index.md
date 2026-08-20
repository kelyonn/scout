# Scout — Documentation Index

Every document in this set, what it covers, and who it is for.

**Reading order for review:** if you only read three documents, read
[01-prd](01-prd.md), [02-architecture](02-architecture.md), and
[19-roadmap](19-roadmap.md). Then read [22-open-questions](22-open-questions.md),
which is the only document that asks you for decisions.

**Picking up an existing session?** Read [`../HANDOFF.md`](../HANDOFF.md)
first — it tracks what's actually built right now, which moves faster than
this spec set and faster than the roadmap's checkboxes.

---

## Product

| # | Document | Covers |
| --- | --- | --- |
| 01 | [Product Requirements](01-prd.md) | Problem, users, scope, user stories, success metrics, non-goals |
| 21 | [Glossary](21-glossary.md) | Every domain term used across the docs |
| 22 | [Open Questions](22-open-questions.md) | Decisions that need your input before or during build |

## Architecture

| # | Document | Covers |
| --- | --- | --- |
| 02 | [System Architecture](02-architecture.md) | Components, data flow, failure domains, scaling path |
| — | [Decision Records](adr/) | Eighteen ADRs recording every significant technical choice, including the five (014–018) forced by the ₹0 budget |
| 03 | [Data Model](03-data-model.md) | Entity model, full DDL, indexes, partitioning, migrations |
| 04 | [API Design](04-api-design.md) | REST surface, conventions, errors, pagination, realtime, auth |

## Discovery and ingestion

| # | Document | Covers |
| --- | --- | --- |
| 05 | [Source Catalog](05-source-catalog.md) | Every source, access method, legality, freshness, cost, tier |
| 06 | [Ingestion Pipeline](06-ingestion-pipeline.md) | Scheduling, fetching, change detection, politeness, retries |
| 07 | [Normalization and Taxonomy](07-normalization-taxonomy.md) | Canonical schema, role/location/skill taxonomies, comp parsing |
| 08 | [Deduplication and Identity](08-dedup-identity.md) | Company resolution, three-stage job dedup, group merging |

## Intelligence

| # | Document | Covers |
| --- | --- | --- |
| 09 | [Ranking and Scoring](09-ranking-scoring.md) | All thirteen scores, formulas, weights, calibration, learning loop |
| 10 | [AI Features](10-ai-features.md) | Model cascade, prompts, RAG, embeddings, cost control, evals |

## Experience

| # | Document | Covers |
| --- | --- | --- |
| 11 | [Notifications](11-notifications.md) | Triggers, channels, routing, rate limits, quiet hours, templates |
| 12 | [Frontend and UX](12-frontend-ux.md) | Information architecture, screens, design system, PWA, native shell, a11y |

## Operations

| # | Document | Covers |
| --- | --- | --- |
| 13 | [Security and Privacy](13-security-privacy.md) | Threat model, authn/z, secrets, PII, data retention |
| 14 | [Legal and Compliance](14-legal-compliance.md) | robots.txt policy, ToS matrix per source, scraping ethics |
| 15 | [Infrastructure and Deployment](15-infrastructure-deployment.md) | Environments, IaC, CI/CD, releases, rollback |
| 16 | [Observability](16-observability.md) | Logs, metrics, traces, SLOs, alert routing, dashboards |
| 17 | [Testing and QA](17-testing-qa.md) | Test strategy, fixtures, golden datasets, eval harness, CI gates |
| — | [Runbooks](runbooks/) | Step-by-step operational procedures for on-call |

## Planning

| # | Document | Covers |
| --- | --- | --- |
| 18 | [Cost Model](18-cost-model.md) | **₹0** — what is free, free-tier headroom, and what zero actually costs |
| 19 | [Roadmap and Milestones](19-roadmap.md) | P0–P5, objectives, exit criteria, honest estimates |
| 20 | [Risks and Mitigations](20-risks.md) | Technical, legal, product, and operational risk register |

---

## Document conventions

**Requirement keywords** follow RFC 2119. **MUST** is a hard requirement whose
violation is a bug. **SHOULD** is a strong default that may be overridden with a
recorded reason. **MAY** is optional.

**Every requirement is tagged** with a stable identifier of the form
`SCOUT-<AREA>-<NUMBER>` — for example `SCOUT-DEDUP-004`. Tests reference these
identifiers so that coverage of the specification is mechanically checkable.

**Status markers** appear at the top of each document:
`Draft` (under review) · `Accepted` (approved, implementable) ·
`Superseded` (replaced, kept for history).

Every document in this set is currently **Draft**.
