#!/usr/bin/env bash
# Deploy to production. Runs FROM the MacBook, over Tailscale SSH.
#
# CI validates; a human deploys. That is deliberate (docs/15 section 3): an
# automated deploy from GitHub Actions would need a Tailscale auth key stored as
# a GitHub secret, which puts a credential for the user's entire private network
# into a third-party system, to save typing eight characters on a project that
# deploys a few times a week.
#
# Building on the host from a git pull also removes the need for a container
# registry entirely, which is one fewer free tier that can be withdrawn.
#
# The sequence:
#
#   1. Refuse to deploy a dirty or unpushed tree.
#   2. Work out which services actually changed.
#   3. Pin the currently-running image IDs into .env.previous, for rollback.
#   4. Pull, build, migrate, restart only what changed.
#   5. Run the health gate, which rolls back on its own if anything is wrong.
#
# Step 2 matters more than it looks: a web change must not restart the collector
# mid-crawl or the brain mid-batch.
#
# Usage:  make deploy   (or)   SCOUT_HOST=scout-host ./infra/scripts/deploy.sh

set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/../.."

# shellcheck source=infra/scripts/notify.sh
source infra/scripts/notify.sh

SCOUT_HOST="${SCOUT_HOST:-scout-host}"
REMOTE_DIR="${SCOUT_REMOTE_DIR:-/opt/scout}"
COMPOSE_FILE="infra/compose/production.yml"
BRANCH="${SCOUT_DEPLOY_BRANCH:-main}"

# Every service that can be rebuilt from this repository. Used when a shared
# change means "restart everything".
ALL_SERVICES="api collector caddy"

remote() {
	# shellcheck disable=SC2029  # deliberate client-side expansion of REMOTE_DIR
	ssh "${SCOUT_HOST}" "set -euo pipefail; cd ${REMOTE_DIR}; $*"
}

# ------------------------------------------------------- preflight, locally ---

preflight() {
	echo "--- preflight"

	if [[ -n "$(git status --porcelain)" ]]; then
		echo "working tree is dirty. Commit or stash first: the host deploys from" >&2
		echo "a git pull, so uncommitted work would not be deployed and the build" >&2
		echo "you tested is not the build that would run." >&2
		exit 1
	fi

	local local_head remote_head
	local_head="$(git rev-parse HEAD)"
	remote_head="$(git rev-parse "origin/${BRANCH}" 2>/dev/null || echo "")"

	if [[ "${local_head}" != "${remote_head}" ]]; then
		echo "HEAD is not origin/${BRANCH}. Push first — the host pulls from the" >&2
		echo "remote, so anything unpushed simply will not be there." >&2
		exit 1
	fi

	if ! ssh -o ConnectTimeout=10 "${SCOUT_HOST}" true 2>/dev/null; then
		echo "cannot reach ${SCOUT_HOST} over SSH. Is Tailscale up on both ends?" >&2
		echo "  tailscale status" >&2
		exit 1
	fi

	echo "ok: clean tree at ${local_head:0:12}, ${SCOUT_HOST} reachable"
}

# --------------------------------------------------- what actually changed ---

changed_services() {
	local deployed
	deployed="$(remote "git rev-parse HEAD" 2>/dev/null || echo "")"

	if [[ -z "${deployed}" ]]; then
		echo "${ALL_SERVICES}"
		return
	fi

	# A host ahead of, or diverged from, local means someone deployed something
	# else. Rebuilding everything is the safe answer; guessing a diff is not.
	if ! git merge-base --is-ancestor "${deployed}" HEAD 2>/dev/null; then
		echo "${ALL_SERVICES}"
		return
	fi

	local files services=""
	files="$(git diff --name-only "${deployed}" HEAD)"

	[[ -z "${files}" ]] && return

	# Anything shared rebuilds everything: go.mod, the Dockerfile, and the
	# compose file all change every image.
	if grep -qE '^(go\.(mod|sum)|infra/docker/|infra/compose/)' <<<"${files}"; then
		echo "${ALL_SERVICES}"
		return
	fi

	grep -qE '^apps/api/'       <<<"${files}" && services+=" api"
	grep -qE '^apps/collector/' <<<"${files}" && services+=" collector"
	grep -qE '^infra/caddy/'    <<<"${files}" && services+=" caddy"

	echo "${services# }"
}

# -------------------------------------------------------------------- main ---

main() {
	preflight

	echo "--- working out what changed"
	local services
	services="$(changed_services)"

	# Migrations are applied whether or not a service image changed: a migration
	# with no accompanying code change is normal and must still be applied.
	local migrations_pending=0
	local deployed
	deployed="$(remote "git rev-parse HEAD" 2>/dev/null || echo "")"
	if [[ -z "${deployed}" ]] || git diff --name-only "${deployed}" HEAD 2>/dev/null \
		| grep -qE '^infra/migrations/'; then
		migrations_pending=1
	fi

	if [[ -z "${services}" ]] && ((migrations_pending == 0)); then
		echo "nothing to deploy: no service or migration changed since ${deployed:0:12}"
		exit 0
	fi

	echo "    services: ${services:-<none>}"
	echo "    migrations: $((migrations_pending)) pending-check"

	echo "--- pinning current image IDs for rollback"
	# Written before anything changes. This file is what health-gate.sh reads
	# when it has to put the previous version back.
	remote "docker compose -f ${COMPOSE_FILE} config --images >/dev/null 2>&1 || true
		{
			for svc in ${ALL_SERVICES}; do
				id=\$(docker compose -f ${COMPOSE_FILE} images -q \"\$svc\" 2>/dev/null || true)
				[ -n \"\$id\" ] && echo \"SCOUT_IMAGE_\$(echo \$svc | tr a-z A-Z)=\$id\"
			done
		} > .env.previous.tmp && mv .env.previous.tmp .env.previous"

	echo "--- pulling ${BRANCH} on ${SCOUT_HOST}"
	remote "git fetch origin ${BRANCH} && git merge --ff-only origin/${BRANCH}"

	echo "--- building"
	if [[ -n "${services}" ]]; then
		# shellcheck disable=SC2086  # word splitting is the point
		remote "docker compose -f ${COMPOSE_FILE} build ${services}"
	fi

	echo "--- applying migrations"
	# Before the new code starts. Migrations are additive within a single deploy
	# (docs/15 section 3), so the currently-running old code tolerates the new
	# schema for the seconds between these two steps.
	remote "docker compose -f ${COMPOSE_FILE} --profile tools run --rm migrate up"

	if [[ -n "${services}" ]]; then
		echo "--- restarting: ${services}"
		# --no-deps so restarting the api does not also restart Postgres.
		# shellcheck disable=SC2086
		remote "docker compose -f ${COMPOSE_FILE} up -d --no-deps --no-build ${services}"
	fi

	echo "--- health gate"
	if remote "./infra/scripts/health-gate.sh --timeout 90"; then
		notify "Scout deployed: ${services:-migrations only} at $(git rev-parse --short HEAD)."
		echo
		echo "DEPLOYED"
		exit 0
	fi

	# health-gate.sh has already rolled back and alerted; this is just the exit
	# status the human at the terminal needs to see.
	echo
	echo "DEPLOY FAILED — see the health gate output above and docs/runbooks/bad-deploy.md" >&2
	exit 1
}

main
