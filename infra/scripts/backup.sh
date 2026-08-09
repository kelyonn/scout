#!/usr/bin/env bash
# Tiered backup. Runs ON the production host, from /opt/scout, on a cron.
#
# The principle is ADR-017: back up by recoverability class, not uniformly.
# About 95% of this database can be re-derived by re-polling the internet, and a
# few kilobytes of it cannot be re-derived at all. Giving both the same treatment
# is how a backup design ends up expensive and fragile in exchange for protecting
# data that was never at risk.
#
#   irreplaceable   the user's own work — applications, interview notes, saved
#                   state, feedback labels, the profile. Hourly. Nothing in the
#                   world has a second copy of "the recruiter said X on the 3rd".
#   full            everything. Nightly at 03:00 IST. Bulk data is re-derivable,
#                   so a day of granularity is the right price.
#
# Both are zstd-compressed and age-encrypted ON THE HOST before they leave it, to
# two destinations that are free and already the user's: the MacBook over
# Tailscale, and Google Drive via rclone. The age private key lives on offline
# media, so this script can only encrypt — it cannot read its own backups, and
# neither can Google.
#
# The silent failure of this job is the real risk in the design (ADR-017), which
# is why it pings its own healthchecks.io check and alerts Telegram on failure.
#
# Usage:  ./infra/scripts/backup.sh {irreplaceable|full}

set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/../.."

# shellcheck source=infra/scripts/notify.sh
source infra/scripts/notify.sh

MODE="${1:-}"
if [[ "${MODE}" != "irreplaceable" && "${MODE}" != "full" ]]; then
	echo "usage: $0 {irreplaceable|full}" >&2
	exit 2
fi

ENV_FILE="${SCOUT_ENV_FILE:-.env}"
if [[ -f "${ENV_FILE}" ]]; then
	set -a
	# shellcheck disable=SC1090
	source "${ENV_FILE}"
	set +a
fi

COMPOSE=(docker compose --env-file "${ENV_FILE}" -f infra/compose/production.yml)

PG_USER="${SCOUT_PG_USER:-scout}"
PG_DB="${SCOUT_PG_DATABASE:-scout}"

STAGING="${SCOUT_BACKUP_DIR:-/var/lib/scout/backups}"
LOCAL_RETAIN_DAYS="${SCOUT_BACKUP_RETAIN_DAYS:-14}"

# The MacBook, over Tailscale. An independent failure domain, in a different
# country from the Oracle region, on a machine the user physically controls.
MAC_DEST="${SCOUT_BACKUP_MAC_DEST:-}"        # e.g. macbook:~/scout-backups
# Google Drive, 15 GB free on the existing account. Off-site; survives losing
# both the host and the laptop.
RCLONE_DEST="${SCOUT_BACKUP_RCLONE_DEST:-}"  # e.g. scout-drive:backups

# The age *public* key. The private half is on offline media and must never be
# on this host — a host that can decrypt its own backups turns a host compromise
# into a history compromise.
AGE_RECIPIENT="${SCOUT_AGE_RECIPIENT:-}"

HC_URL="${SCOUT_BACKUP_HEALTHCHECK_URL:-}"

# The irreplaceable set, from ADR-017. Tables that do not exist yet are skipped
# with a warning rather than failing the run: at P0 the schema has company,
# source, and raw_observation, all of which are bulk. The list is written in full
# so that adding the tables later needs no change here — and so that a table
# going missing is visible in the log rather than silently dropped from the
# backup.
IRREPLACEABLE_TABLES=(
	application
	application_event
	interview
	note
	user_profile
	feedback_label
	notification
)

fail() {
	echo "BACKUP FAILED (${MODE}): $*" >&2
	notify "Scout backup FAILED (${MODE}): $*  — see docs/runbooks/database-recovery.md"
	ping_healthcheck "${HC_URL}" "/fail"
	exit 1
}

require() {
	command -v "$1" >/dev/null 2>&1 || fail "$1 is not installed on the host"
}

# ------------------------------------------------------------------ checks ---

require zstd
require age
[[ -n "${AGE_RECIPIENT}" ]] || fail "SCOUT_AGE_RECIPIENT is not set; refusing to write an unencrypted backup"

mkdir -p "${STAGING}"

if [[ -z "${MAC_DEST}" && -z "${RCLONE_DEST}" ]]; then
	# Not fatal — a backup sitting on the host is better than none — but a
	# backup that never leaves the host does not survive losing the host, which
	# is the scenario it exists for.
	echo "warning: no off-host destination configured; this backup stays on the host" >&2
	notify "Scout backup warning: no off-host destination configured. The backup exists only on the host, which does not survive the failure it is meant to cover."
fi

# ------------------------------------------------------------------- dump ---

STAMP="$(date -u +%Y-%m-%dT%H-%M)"

psql_q() {
	"${COMPOSE[@]}" exec -T postgres psql -tAX -U "${PG_USER}" -d "${PG_DB}" -c "$1"
}

dump_irreplaceable() {
	local present=() missing=() t
	for t in "${IRREPLACEABLE_TABLES[@]}"; do
		if [[ "$(psql_q "SELECT to_regclass('public.${t}') IS NOT NULL")" == "t" ]]; then
			present+=("--table=${t}")
		else
			missing+=("${t}")
		fi
	done

	if ((${#missing[@]})); then
		# Expected until the tables land; noise worth keeping, because after they
		# land this line means a table vanished from the backup set.
		echo "note: not yet in the schema, skipped: ${missing[*]}" >&2
	fi

	if ((${#present[@]} == 0)); then
		echo "no irreplaceable tables exist yet; nothing to back up in this class" >&2
		return 1
	fi

	# Plain SQL with --data-only and --column-inserts: this dump is replayed on
	# top of a restored nightly (docs/runbooks/database-recovery.md step 6), so it
	# must not carry schema and must not care about column order.
	"${COMPOSE[@]}" exec -T postgres pg_dump -U "${PG_USER}" -d "${PG_DB}" \
		--data-only --column-inserts "${present[@]}"
}

dump_full() {
	# -Fc: the custom format, which pg_restore can read selectively and which
	# compresses better than plain SQL before zstd even sees it.
	"${COMPOSE[@]}" exec -T postgres pg_dump -U "${PG_USER}" -d "${PG_DB}" -Fc
}

case "${MODE}" in
	irreplaceable) OUT="${STAGING}/scout-irreplaceable-${STAMP}.sql.zst.age" ;;
	full)          OUT="${STAGING}/scout-full-${STAMP}.dump.zst.age" ;;
esac

echo "--- dumping (${MODE})"

# Written to a .partial name and renamed only on success. A truncated file with
# the right name is a backup that looks fine in a listing and fails at 2am.
#
# pipefail is on, so a failure anywhere in this pipeline fails the run.
if [[ "${MODE}" == "irreplaceable" ]]; then
	if ! dump_irreplaceable | zstd -q -T0 -3 | age -r "${AGE_RECIPIENT}" > "${OUT}.partial"; then
		rm -f "${OUT}.partial"
		# The "no tables yet" case is not a failure — it is P0 with no user data
		# in the schema. Distinguish it so the dead-man's switch is not tripped
		# by a correct outcome.
		if ! psql_q "SELECT to_regclass('public.application') IS NOT NULL" | grep -q '^t$'; then
			echo "skipping: the irreplaceable tables do not exist yet"
			ping_healthcheck "${HC_URL}" ""
			exit 0
		fi
		fail "pg_dump of the irreplaceable tables failed"
	fi
else
	if ! dump_full | zstd -q -T0 -3 | age -r "${AGE_RECIPIENT}" > "${OUT}.partial"; then
		rm -f "${OUT}.partial"
		fail "pg_dump of the full database failed"
	fi
fi

mv "${OUT}.partial" "${OUT}"

SIZE="$(du -h "${OUT}" | cut -f1)"
echo "ok: ${OUT} (${SIZE})"

# A zero-length or absurdly small encrypted file means the dump produced
# nothing. age still writes a valid header, so "the file exists" proves nothing.
if [[ "$(stat -c%s "${OUT}" 2>/dev/null || stat -f%z "${OUT}")" -lt 200 ]]; then
	fail "the backup is implausibly small; the dump probably produced nothing"
fi

# ---------------------------------------------------------------- shipping ---

shipped=0

if [[ -n "${MAC_DEST}" ]]; then
	echo "--- copying to the MacBook over Tailscale"
	if rsync --archive --partial --timeout=120 "${OUT}" "${MAC_DEST}/"; then
		shipped=1
	else
		# Not fatal on its own: two destinations exist so that one being
		# unreachable is survivable. The laptop is frequently asleep.
		echo "warning: copy to the MacBook failed" >&2
	fi
fi

if [[ -n "${RCLONE_DEST}" ]]; then
	echo "--- copying to Google Drive"
	if rclone copy --transfers=1 --timeout=5m "${OUT}" "${RCLONE_DEST}/"; then
		shipped=1
	else
		echo "warning: copy to Google Drive failed" >&2
	fi
fi

if [[ -n "${MAC_DEST}${RCLONE_DEST}" ]] && ((shipped == 0)); then
	fail "the backup was written but reached neither destination"
fi

# --------------------------------------------------------------- retention ---

# Local staging only. The two destinations keep their own history; this is the
# copy that would otherwise fill the host's disk, and "disk full" is a documented
# disaster scenario.
find "${STAGING}" -name 'scout-*.age' -type f -mtime "+${LOCAL_RETAIN_DAYS}" -delete 2>/dev/null || true
find "${STAGING}" -name '*.partial' -type f -mtime +1 -delete 2>/dev/null || true

# ------------------------------------------------------------------ report ---

ping_healthcheck "${HC_URL}" ""
echo
echo "BACKUP OK (${MODE}, ${SIZE})"
