#!/usr/bin/env bash
# Seed the starter list of real, verified source boards (Greenhouse, Lever,
# Ashby) from infra/seed/sources.sql. Idempotent — safe to re-run. Separate
# from `make seed` on purpose; see that file's header comment.
set -euo pipefail

docker compose --env-file "${ENV_FILE:-.env}" -f infra/compose/local.yml \
	exec -T postgres psql -v ON_ERROR_STOP=1 -U scout -d scout \
	< infra/seed/sources.sql

echo "seeded: 73 real source boards across Greenhouse/Lever/Ashby, status=pending_review"
