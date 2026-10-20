#!/usr/bin/env bash
# One-time bootstrap of a HOSTED Postgres (Neon, ADR-019): apply every
# migration, then seed the single user, the starter source boards, company
# metadata and the weight_version — the same things `make dev` does locally,
# pointed at SCOUT_DATABASE_URL instead of the Compose container.
#
#   SCOUT_DATABASE_URL='postgres://…neon.tech/neondb?sslmode=require' \
#   SCOUT_USER_EMAIL='you@example.com' \
#   ./infra/scripts/bootstrap-hosted-db.sh
#
# Idempotent: migrate is a no-op when current, and the seeds are written to be
# re-runnable. Deliberately does NOT copy the local jobs: the collector
# repopulates them, and the local database (raw_observation, job.embedding) is
# already larger than Neon's free storage (docs/20-risks.md). The resume is
# personal data and is never part of this script (docs/13 section 3) — upload it
# once through the web app's Resume page after the first deploy.
#
# Needs Docker only for the pinned migrate and psql client images; nothing runs
# on the host besides this script. Set SCOUT_BOOTSTRAP_HOST_NET=1 when the target
# is a database on this machine (testing).
set -euo pipefail
cd "$(dirname "$0")/../.."

: "${SCOUT_DATABASE_URL:?set SCOUT_DATABASE_URL to the hosted Postgres connection string}"
email="${SCOUT_USER_EMAIL:-dev@scout.local}"
net=()
[ "${SCOUT_BOOTSTRAP_HOST_NET:-}" = "1" ] && net=(--add-host=host.docker.internal:host-gateway)

psql_run() {
	docker run --rm -i "${net[@]}" -e PGCONNECT_TIMEOUT=30 pgvector/pgvector:pg16 \
		psql "$SCOUT_DATABASE_URL" -v ON_ERROR_STOP=1 -q "$@"
}

echo "1/5 migrations"
docker run --rm "${net[@]}" -v "$PWD/infra/migrations:/migrations:ro" migrate/migrate:v4.18.1 \
	-path=/migrations -database="$SCOUT_DATABASE_URL" up

echo "2/5 user + weight_version"
psql_run -v "user_email=${email}" < infra/seed/seed.sql

echo "3/5 starter sources (Greenhouse/Lever/Ashby)"
psql_run < infra/seed/sources.sql

echo "4/5 GCC/enterprise sources (Workday/SmartRecruiters)"
psql_run < infra/seed/gcc_sources.sql

echo "5/5 company metadata"
psql_run < infra/seed/companies.sql

echo "done: hosted database is migrated and seeded (sources are pending_review; the collector promotes them)"
