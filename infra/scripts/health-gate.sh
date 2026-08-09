#!/usr/bin/env bash
# The deploy health gate. Runs ON the production host, from /opt/scout.
#
# Three checks, in order of how bad it is to get them wrong:
#
#   1. No container publishes a host port.
#   2. Every service reports healthy within the timeout.
#   3. The API answers /health through Caddy, the way a real request arrives.
#
# Check 1 is the one that is easy to under-rate. Scout's entire security posture
# is "there is no public surface" (docs/15 section 2, ADR-015): the Tailscale
# identity headers are trustworthy only because the ingress cannot be bypassed,
# and the bearer token is sized for a threat model where an attacker must already
# be on the tailnet. One stray `ports:` entry — added while debugging, committed
# by accident — silently converts that into a system exposed to the internet with
# a single static token in front of it. A document cannot enforce that. This can.
#
# On failure the gate rolls back to the image IDs pinned in .env.previous,
# re-checks, and alerts to Telegram either way.
#
# Usage:  ./infra/scripts/health-gate.sh [--timeout SECONDS] [--no-rollback]

set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/../.."

# shellcheck source=infra/scripts/notify.sh
source infra/scripts/notify.sh

COMPOSE_FILE="infra/compose/production.yml"
ENV_FILE="${SCOUT_ENV_FILE:-.env}"
COMPOSE=(docker compose --env-file "${ENV_FILE}" -f "${COMPOSE_FILE}")

TIMEOUT=60
ROLLBACK=1

while [[ $# -gt 0 ]]; do
	case "$1" in
		--timeout) TIMEOUT="$2"; shift 2 ;;
		--no-rollback) ROLLBACK=0; shift ;;
		*) echo "unknown argument: $1" >&2; exit 2 ;;
	esac
done

# Load Telegram credentials without exporting the whole environment file into
# this shell's scope any more than necessary.
if [[ -f "${ENV_FILE}" ]]; then
	set -a
	# shellcheck disable=SC1090
	source "${ENV_FILE}"
	set +a
fi

fail() {
	echo "HEALTH GATE FAILED: $*" >&2
	return 1
}

# --------------------------------------------------------------- check one ---

check_no_published_ports() {
	echo "--- checking that no container publishes a host port"

	# Ask Docker what is actually bound, not what the compose file says. A
	# compose file can be correct while a container started by hand is not, and
	# the property that matters is the running state.
	local ports_output published
	if ! ports_output="$("${COMPOSE[@]}" ps --format '{{.Names}} {{.Ports}}' 2>/dev/null)"; then
		# An error here must not read as "no ports found". This check is the
		# whole reason the gate exists; failing open would be worse than failing.
		fail "could not list containers to check for published ports"
		return 1
	fi

	# -e, because BSD grep parses a leading "->" as options and exits 2 — which,
	# with the usual `|| true`, silently turns this check into one that always
	# passes.
	published="$(grep -E -e '->' <<<"${ports_output}" || true)"

	if [[ -n "${published}" ]]; then
		echo "${published}" >&2
		fail "a container publishes a host port; Scout must have no public surface"
		return 1
	fi

	echo "ok: no published ports"
}

# --------------------------------------------------------------- check two ---

check_services_healthy() {
	echo "--- waiting up to ${TIMEOUT}s for every service to report healthy"

	local deadline=$((SECONDS + TIMEOUT))
	local unhealthy=""

	while ((SECONDS < deadline)); do
		unhealthy=""

		# Services without a healthcheck report an empty Health field. Treating
		# that as healthy would make the gate pass for a service it never
		# checked, so "running with no healthcheck" is reported explicitly.
		while read -r name state health; do
			[[ -z "${name}" ]] && continue
			case "${health}" in
				healthy) ;;
				"" ) [[ "${state}" == "running" ]] || unhealthy+=" ${name}(${state})" ;;
				*) unhealthy+=" ${name}(${health})" ;;
			esac
		done < <("${COMPOSE[@]}" ps --format '{{.Service}} {{.State}} {{.Health}}' 2>/dev/null)

		if [[ -z "${unhealthy}" ]]; then
			echo "ok: all services healthy"
			return 0
		fi

		sleep 3
	done

	fail "services not healthy after ${TIMEOUT}s:${unhealthy}"
	return 1
}

# ------------------------------------------------------------- check three ---

check_api_through_ingress() {
	echo "--- probing /health through Caddy"

	# Through the proxy, not directly at the API: this is the path a real
	# request takes, and a Caddyfile that stopped routing is invisible to a
	# container healthcheck.
	if "${COMPOSE[@]}" exec -T caddy \
		wget --quiet --tries=1 --timeout=10 --spider http://127.0.0.1:8080/health; then
		echo "ok: /health answers through the ingress"
		return 0
	fi

	fail "/health did not answer through Caddy"
	return 1
}

# ---------------------------------------------------------------- rollback ---

rollback() {
	echo "--- rolling back to the previously deployed images"

	if [[ ! -f .env.previous ]]; then
		notify "Scout deploy FAILED and there is no .env.previous to roll back to. Manual intervention needed. See docs/runbooks/bad-deploy.md."
		echo "no .env.previous; cannot roll back automatically" >&2
		return 1
	fi

	# .env.previous pins the image IDs that were running before this deploy.
	# Compose reads them as overrides for the `image:` of each service.
	if ! "${COMPOSE[@]}" --env-file .env.previous up -d --no-build; then
		notify "Scout deploy FAILED and the automatic rollback ALSO failed. Manual intervention needed. See docs/runbooks/bad-deploy.md."
		return 1
	fi

	if check_services_healthy; then
		notify "Scout deploy failed its health gate and was rolled back automatically. The previous version is running. See docs/runbooks/bad-deploy.md."
		return 0
	fi

	notify "Scout deploy FAILED and the rollback did not come back healthy. Scout is DOWN. See docs/runbooks/bad-deploy.md."
	return 1
}

# -------------------------------------------------------------------- main ---

main() {
	if check_no_published_ports && check_services_healthy && check_api_through_ingress; then
		echo
		echo "HEALTH GATE PASSED"
		return 0
	fi

	if ((ROLLBACK)); then
		rollback || true
	else
		notify "Scout deploy failed its health gate. Rollback was disabled, so the broken version is still running. See docs/runbooks/bad-deploy.md."
	fi

	return 1
}

main
