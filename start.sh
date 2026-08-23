#!/usr/bin/env bash
# Starts the whole Scout stack, live (ADR-018) — postgres, redis, api,
# collector, brain, notifier, web, migrated and seeded. Same as `make dev`
# but with real fetching turned on for this run only (see Makefile and
# docs/15-infrastructure-deployment.md section 1 for why .env itself stays
# fixtures-only at rest). Stop with ./stop.sh.
set -euo pipefail
cd "$(dirname "$0")"

SCOUT_FIXTURES_ONLY=false make dev

echo
echo "Scout is running, live."
echo "Stop with ./stop.sh"
