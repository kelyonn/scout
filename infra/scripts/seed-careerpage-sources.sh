#!/usr/bin/env bash
# Seed direct company career-site sources (sitemap + JSON-LD JobPosting) from
# infra/seed/careerpage_sources.sql — docs/05-source-catalog.md section 3.
# Idempotent — safe to re-run. Separate from `make seed-sources` for the same
# reason seed-gcc-sources.sh is: a different platform set, applied on demand.
set -euo pipefail

docker compose --env-file "${ENV_FILE:-.env}" -f infra/compose/local.yml \
	exec -T postgres psql -v ON_ERROR_STOP=1 -U scout -d scout \
	< infra/seed/careerpage_sources.sql

echo "seeded: 12 direct career-site sources (sitemap + JSON-LD), status=pending_review"
