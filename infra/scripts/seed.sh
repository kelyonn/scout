#!/usr/bin/env bash
# Bootstrap a fresh database with the single user and the active
# weight_version, via infra/seed/seed.sql. Idempotent — safe to re-run.
#
# The user's email is a psql variable rather than baked into a tracked SQL
# file, since infra/seed/seed.sql is committed and an email address is
# personal data. Set SCOUT_USER_EMAIL in .env; falls back to a clearly-fake
# placeholder for a from-scratch `make dev` so first boot never fails.
set -euo pipefail

email="${SCOUT_USER_EMAIL:-dev@scout.local}"

docker compose --env-file "${ENV_FILE:-.env}" -f infra/compose/local.yml \
	exec -T postgres psql -v ON_ERROR_STOP=1 -U scout -d scout \
	-v "user_email=${email}" \
	< infra/seed/seed.sql

echo "seeded: app_user (${email}), user_profile, weight_version v1-hand-tuned-2026-08"
