# ADR-020: Expand beyond software engineering — Product, Consulting, Business

**Status:** Accepted
**Date:** 2026-10-21
**Supersedes:** [docs/01-prd.md](../01-prd.md) section 4.3's prior "Non-software
internships" and "Non-technical consulting roles" non-goals

## Context

Scout was scoped to software-engineering roles only, by explicit design:
`job.is_software` has gated every feed-surfacing query since the earliest
migration, `packages/taxonomy/roles.yaml` defined only `swe.*` (17 leaves) and
`advocacy.*` (3 leaves), and the PRD's own non-goals table said plainly
"Non-software internships: out of scope by definition."

The user asked for Scout to surface a much wider range of roles: software,
product, management consulting, and general business — a real reversal of a
stated product decision, not merely a taxonomy gap, hence this ADR rather than
a quiet code change.

**A real source of latent breakage surfaced while implementing this**:
`is_software` had quietly become overloaded to mean two different things —
"this role is software engineering" and "this role should be shown to the
user" — which only worked because `advocacy.*` (not literally software
engineering either, but shown) also set it `true`. Nothing before this ADR
ever needed to tell those two concepts apart. The moment a real non-software-
but-admitted family was added, that conflation became a real bug: either
`is_software` would have to lie about Product/Consulting/Business roles
("true" despite not being software), or the feed would hide them entirely
("false" and filtered). Investigating this also surfaced a genuine
pre-existing defect: `apps/brain`'s Tier 1 (embedding-based reclassification)
revises `role_family` on a `swe.other` row without ever touching
`is_software`, so any job Tier 1 had already correctly promoted out of
`swe.other` was silently excluded from the feed regardless of this ADR —
confirmed live against the dev database, 11 real rows already stuck.

## Decision

**Two independent boolean columns, not one overloaded field:**

- `is_software` keeps its literal, honest meaning — `true` only for
  `swe.*`/`advocacy.*` families, `false` for the new `product.*`/
  `consulting.*`/`business.*` families too.
- `in_scope` (new) is the actual feed-admission gate every surfacing query
  filters on — `true` for any family Tier 0/1/2 resolves to other than the
  genuine catch-all/unmatched `swe.other` state, regardless of `is_software`.

Every query that hard-filtered on `is_software = TRUE` (`feed.sql`,
`digest.sql` ×2, `notification.sql`, `search.sql`, `watchlist.sql`,
`company.sql`) now filters on `in_scope = TRUE` instead. `retention.sql`'s
shorter-retention-window branch, which exists specifically for genuinely
excluded jobs, now keys off `in_scope = FALSE` rather than `is_software =
FALSE` — so Product/Consulting/Business jobs get the same retention
treatment as software jobs, not the truncated treatment meant for excluded
junk.

**Eight new role families**, `is_software: false`, no `requires:
technical_evidence` gate (unlike `advocacy.*`, these are non-technical by the
title itself, so Tier 0's deterministic pattern match is sufficient — Tier 2's
LLM escalation, which only `advocacy.*` ever triggers, never sees them):

- `product.management`, `product.design`, `product.analyst`
- `consulting.strategy`, `consulting.technology`
- `business.strategy`, `business.operations`, `business.general`

`consulting.technology` (advisory-not-hands-on) is deliberately distinct from
`advocacy.solutions` (hands-on technical building, gated on real evidence) —
file order in `roles.yaml` keeps a genuinely hands-on "technical consultant"
title resolving to `advocacy.solutions` first.

**What does not change:** dedup (already treats `role_family` as an opaque
grouping key), scoring code (`is_software` was never read there; the
SWE-specific subscore terms — tech-stack keywords, rare-skill bonus,
deep-domain pattern — already degrade to their neutral default for any
unmatched family, which is an acceptable v1 limitation, not a bug), and
resume matching (a software resume naturally showing low keyword overlap
against a Consulting posting is correct behavior, not a gap).

## Consequences

- Immediate unlock: every company already sourced (~2,267 seeds) has its
  Product/Business postings start reaching the feed — those postings were
  already being fetched and discarded, not newly scraped.
- The 11-row Tier 1 backfill bug fix applies to every job it affects, not
  just the new families — a real correctness improvement independent of this
  ADR's main goal.
- Scoring gives Product/Consulting/Business postings no domain-specific
  bonus or penalty yet (neutral baseline on the SWE-tuned subscores) — a
  known, documented v1 gap, not a defect. Extend those subscores' pattern
  lists the same way `swe.*`'s were extended, once real postings show the
  need, not speculatively.
- Phase 2 (separate, tracked in the implementation's own PR description):
  zero pure-strategy-consulting firms (McKinsey, BCG, Bain, ...) exist in any
  seed list today. Surfacing their roles specifically needs new source
  discovery, the same kind of per-tenant investigation
  `adapters/web/careerpage` was built for — not something this ADR's
  taxonomy/classification change produces on its own.
