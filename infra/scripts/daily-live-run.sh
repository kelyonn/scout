#!/usr/bin/env bash
# ADR-018's "on demand, typically once a day" live run, automated: flips
# SCOUT_FIXTURES_ONLY on for the collector for one bounded window, then back
# off. Meant to be invoked by a launchd agent (see infra/scripts/
# install-daily-schedule.sh) so the laptop doesn't need to be babysat, but
# runs the exact same way by hand: `make live-run`.
#
# Deliberately does NOT edit .env. The override is passed on the `docker
# compose up` command line only, for one invocation — .env's own
# SCOUT_FIXTURES_ONLY stays whatever safe default it already is (see
# .env.example), so a Docker Desktop restart or a manual `docker compose up`
# outside this script can never silently resume live fetching just because a
# previous run happened to leave it on.
set -euo pipefail
cd "$(dirname "$0")/../.."

DURATION_SECONDS="${SCOUT_DAILY_RUN_DURATION_SECONDS:-7200}" # 2h default
ENV_FILE="${ENV_FILE:-.env}"
COMPOSE="docker compose --env-file $ENV_FILE -f infra/compose/local.yml"

if ! docker info >/dev/null 2>&1; then
	echo "$(date -Iseconds): docker is not running — skipping today's live run" >&2
	exit 1
fi

echo "$(date -Iseconds): starting live run, will stop after ${DURATION_SECONDS}s"
# postgres/redis/collector/brain/notifier only — the ingest -> classify ->
# dedup -> score -> notify loop this window exists for. api/web are for a
# human browsing the result and aren't part of that loop; start those
# yourself (`make dev`) if you want to look while it runs.
SCOUT_FIXTURES_ONLY=false $COMPOSE up -d --wait postgres redis collector brain notifier
echo "$(date -Iseconds): collector is live"

sleep "$DURATION_SECONDS"

# Full teardown, not just `stop collector` — this used to leave
# postgres/redis/brain/notifier running indefinitely after every window
# closed (brain in particular holds an embedding model resident in
# memory and polls the queue every tick), a real, unbounded background
# resource cost between runs that had nothing to do with "on demand."
# `down` stops and removes the containers; the named volumes (postgres
# data, the brain model cache) are untouched, so the next run starts
# with a warm cache and no data loss — same guarantee `docker compose
# down` without `-v` always gives.
$COMPOSE down postgres redis collector brain notifier
echo "$(date -Iseconds): live window closed, stack torn down"
