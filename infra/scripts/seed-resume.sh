#!/usr/bin/env bash
# Seeds resume.raw_text via infra/seed/resume.sql. Idempotent — safe to
# re-run. Not part of `make seed`/`make dev`; see that file's own header
# comment for why personal content isn't baked into every fresh stack
# unconditionally.
set -euo pipefail

if [ ! -f infra/seed/resume.sql ]; then
	echo "infra/seed/resume.sql not found (it's gitignored — real resume text doesn't belong in the repo)." >&2
	echo "Copy infra/seed/resume.sql.example to infra/seed/resume.sql and fill in your own resume text." >&2
	exit 1
fi

email="${SCOUT_USER_EMAIL:-dev@scout.local}"

docker compose --env-file "${ENV_FILE:-.env}" -f infra/compose/local.yml \
	exec -T postgres psql -v ON_ERROR_STOP=1 -U scout -d scout \
	-v "user_email=${email}" \
	< infra/seed/resume.sql

echo "seeded: resume.raw_text for ${email}"
