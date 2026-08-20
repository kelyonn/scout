#!/usr/bin/env bash
# Seed GCC and enterprise source boards (Workday, SmartRecruiters) from
# infra/seed/gcc_sources.sql — docs/05-source-catalog.md section 5.2.
# Idempotent — safe to re-run. Separate from `make seed-sources` because
# it targets a different platform set and landed in a later milestone
# (P3); see that script's own header for why source seeding is kept out
# of `make seed`/`make dev` entirely.
set -euo pipefail

docker compose --env-file "${ENV_FILE:-.env}" -f infra/compose/local.yml \
	exec -T postgres psql -v ON_ERROR_STOP=1 -U scout -d scout \
	< infra/seed/gcc_sources.sql

echo "seeded: 21 real GCC/enterprise source boards across Workday/SmartRecruiters, status=pending_review"
