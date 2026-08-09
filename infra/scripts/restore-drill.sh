#!/usr/bin/env bash
# The monthly restore drill. Runs on the MacBook, first Sunday.
#
# A backup that has never been restored is a hypothesis. This is what converts it
# into a backup: take the latest nightly, decrypt it, restore it into a throwaway
# container, verify it, destroy the container. Roughly ten minutes, no provider
# involved, nothing touched that production depends on.
#
# It deliberately uses a throwaway container on a non-default port rather than
# the local development stack. Restoring production data over the local database
# would destroy the fixtures the drill is not supposed to care about, and — worse
# — would put real application history on a laptop's development database, where
# docs/13 section 3 says it does not belong.
#
# Usage:  make restore-drill
#         SCOUT_DRILL_DUMP=path/to/dump.zst.age make restore-drill

set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/../.."

BACKUP_DIR="${SCOUT_LOCAL_BACKUP_DIR:-${HOME}/scout-backups}"
AGE_KEY="${SCOUT_AGE_KEY_FILE:-${HOME}/keys/scout-backup.age}"
CONTAINER="scout-restore-drill"
PORT="${SCOUT_DRILL_PORT:-5434}"
PG_PASSWORD="drill_only_$$"
IMAGE="pgvector/pgvector:pg16"

WORK="$(mktemp -d)"
cleanup() {
	echo "--- cleaning up"
	docker rm -f "${CONTAINER}" >/dev/null 2>&1 || true
	# The decrypted dump contains the application history in plaintext. It must
	# not outlive the drill.
	rm -rf "${WORK}"
}
trap cleanup EXIT

die() { echo "DRILL FAILED: $*" >&2; exit 1; }

# ------------------------------------------------------------- find a dump ---

command -v age >/dev/null 2>&1 || die "age is not installed: brew install age"
command -v zstd >/dev/null 2>&1 || die "zstd is not installed: brew install zstd"

DUMP="${SCOUT_DRILL_DUMP:-}"
if [[ -z "${DUMP}" ]]; then
	[[ -d "${BACKUP_DIR}" ]] || die "no backup directory at ${BACKUP_DIR}. Set SCOUT_LOCAL_BACKUP_DIR or SCOUT_DRILL_DUMP."
	DUMP="$(find "${BACKUP_DIR}" -name 'scout-full-*.dump.zst.age' -type f \
		| sort | tail -1)"
fi

[[ -n "${DUMP}" && -f "${DUMP}" ]] || die "no nightly dump found in ${BACKUP_DIR}"
[[ -f "${AGE_KEY}" ]] || die "age key not found at ${AGE_KEY}. It lives on offline media — mount it."

# The age of the dump is part of what the drill verifies. A drill that passes
# against a three-week-old dump has proved the restore path and missed that the
# backup job stopped, which is the actual risk ADR-017 names.
DUMP_AGE_DAYS=$(( ($(date +%s) - $(stat -f%m "${DUMP}" 2>/dev/null || stat -c%Y "${DUMP}")) / 86400 ))
echo "--- dump: $(basename "${DUMP}") (${DUMP_AGE_DAYS} days old)"
if ((DUMP_AGE_DAYS > 2)); then
	echo "WARNING: the newest nightly is ${DUMP_AGE_DAYS} days old. The backup job" >&2
	echo "may have silently stopped — check the backup healthchecks.io check." >&2
fi

# ---------------------------------------------------------------- decrypt ---

echo "--- decrypting"
age -d -i "${AGE_KEY}" "${DUMP}" | zstd -d > "${WORK}/full.dump" \
	|| die "decrypt or decompress failed. Wrong key, or a truncated backup."

echo "    $(du -h "${WORK}/full.dump" | cut -f1) decrypted"

# --------------------------------------------------- throwaway container ---

echo "--- starting a throwaway Postgres on port ${PORT}"
docker rm -f "${CONTAINER}" >/dev/null 2>&1 || true
docker run -d --name "${CONTAINER}" \
	-e POSTGRES_USER=scout \
	-e POSTGRES_PASSWORD="${PG_PASSWORD}" \
	-e POSTGRES_DB=scout \
	-e POSTGRES_INITDB_ARGS="--encoding=UTF8 --locale=C" \
	-p "127.0.0.1:${PORT}:5432" \
	"${IMAGE}" >/dev/null

echo -n "    waiting for it to accept connections"
for _ in $(seq 1 60); do
	if docker exec "${CONTAINER}" pg_isready -U scout -d scout >/dev/null 2>&1; then
		echo " ok"
		break
	fi
	echo -n "."
	sleep 1
done
docker exec "${CONTAINER}" pg_isready -U scout -d scout >/dev/null 2>&1 \
	|| die "the throwaway Postgres never became ready"

# ---------------------------------------------------------------- restore ---

echo "--- restoring"
# --exit-on-error, because pg_restore's default is to report errors and carry on,
# which produces a partial database and a zero exit status. That is precisely the
# failure this drill exists to catch.
docker exec -i "${CONTAINER}" pg_restore -U scout -d scout \
	--clean --if-exists --no-owner --exit-on-error \
	< "${WORK}/full.dump" \
	|| die "pg_restore failed. The backup is not restorable — this is the drill working."

# ------------------------------------------------------------------ verify ---

echo "--- verifying"

q() { docker exec "${CONTAINER}" psql -tAX -U scout -d scout -c "$1"; }

# Row counts on the tables that exist. The list grows with the schema; a table
# that is in the database but not here is simply not asserted on, which is why
# the extension and index checks below matter as a structural backstop.
for table in company company_alias source raw_observation; do
	if [[ "$(q "SELECT to_regclass('public.${table}') IS NOT NULL")" != "t" ]]; then
		die "table ${table} is missing after the restore"
	fi
	echo "    ${table}: $(q "SELECT count(*) FROM ${table}") rows"
done

# Extensions restore separately from data and are a classic silent omission —
# a database that restores "successfully" without pgvector will fail on the first
# embedding query, hours later, looking like an application bug.
for ext in vector pg_trgm citext pgcrypto; do
	[[ "$(q "SELECT count(*) FROM pg_extension WHERE extname = '${ext}'")" == "1" ]] \
		|| die "extension ${ext} is missing after the restore"
done
echo "    extensions: vector, pg_trgm, citext, pgcrypto all present"

# The partitions of raw_observation are where a restore most plausibly loses
# structure while appearing to succeed.
PARTITIONS="$(q "SELECT count(*) FROM pg_inherits WHERE inhparent = 'raw_observation'::regclass")"
[[ "${PARTITIONS}" -ge 1 ]] || die "raw_observation has no partitions after the restore"
echo "    raw_observation partitions: ${PARTITIONS}"

# The DEFAULT partition must be empty at all times — a non-empty one means the
# partition maintenance job stopped running. Asserted here as well as in CI,
# because a restore is exactly when you want to know.
DEFAULT_ROWS="$(q "SELECT count(*) FROM raw_observation_default")"
[[ "${DEFAULT_ROWS}" == "0" ]] || die "the DEFAULT partition has ${DEFAULT_ROWS} rows"
echo "    default partition: empty"

# A checksum of the job table is specified in ADR-017. The table arrives with the
# normalization work at P1; until then this is stated rather than silently
# skipped, so its absence is visible in the drill output.
if [[ "$(q "SELECT to_regclass('public.job') IS NOT NULL")" == "t" ]]; then
	echo "    job checksum: $(q "SELECT md5(string_agg(id::text, ',' ORDER BY id)) FROM job")"
else
	echo "    job checksum: skipped, the job table lands at P1"
fi

echo
echo "DRILL PASSED — restored $(basename "${DUMP}") and verified it."
echo "Record the result in docs/runbooks/ per ADR-017."
