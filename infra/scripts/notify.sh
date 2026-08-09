#!/usr/bin/env bash
# Send an operational alert to Telegram.
#
# Sourced by the deploy, health-gate, and backup scripts. Telegram is the only
# alert channel at ₹0 that reaches a phone without a domain, a sending
# reputation, or a paid tier (docs/15 section 8, ADR-014).
#
# It is deliberately best-effort. An alert that cannot be delivered must never
# fail the operation it was reporting on: a backup that succeeded and then
# exited non-zero because Telegram was unreachable would trip the dead-man's
# switch and page the user about a backup that worked.
#
# Usage:  notify "message"
#
# Requires SCOUT_TELEGRAM_BOT_TOKEN and SCOUT_TELEGRAM_CHAT_ID. Without them it
# prints to stderr and returns success, which is the right behaviour on a laptop
# and a loud one on a host — see the warning it emits.

notify() {
	local message="$1"

	if [[ -z "${SCOUT_TELEGRAM_BOT_TOKEN:-}" || -z "${SCOUT_TELEGRAM_CHAT_ID:-}" ]]; then
		echo "ALERT (telegram not configured): ${message}" >&2
		return 0
	fi

	# --max-time so a hung Telegram cannot hang a deploy or a cron job.
	# The token is in the URL, so the URL never gets echoed — AGENTS.md rule 7.
	curl --silent --show-error --max-time 15 \
		--output /dev/null \
		--data-urlencode "chat_id=${SCOUT_TELEGRAM_CHAT_ID}" \
		--data-urlencode "text=${message}" \
		"https://api.telegram.org/bot${SCOUT_TELEGRAM_BOT_TOKEN}/sendMessage" \
		|| echo "warning: telegram alert failed to send: ${message}" >&2

	return 0
}

# ping_healthcheck <url> [suffix]
#
# Reports to a healthchecks.io check. Suffix is empty for success or "/fail".
# Same best-effort contract as notify, for the same reason.
ping_healthcheck() {
	local url="${1:-}"
	local suffix="${2:-}"

	[[ -z "${url}" ]] && return 0

	curl --silent --show-error --max-time 15 --retry 2 \
		--output /dev/null \
		-X POST "${url}${suffix}" \
		|| echo "warning: healthcheck ping failed" >&2

	return 0
}
