#!/usr/bin/env bash
# Populates company.company_type/hq_country via infra/seed/companies.sql,
# generated from packages/taxonomy/companies.yaml. Idempotent — safe to
# re-run. Not part of `make seed`/`make dev`; see that file's own header
# comment.
set -euo pipefail

docker compose --env-file "${ENV_FILE:-.env}" -f infra/compose/local.yml \
	exec -T postgres psql -v ON_ERROR_STOP=1 -U scout -d scout \
	< infra/seed/companies.sql

echo "seeded: company_type/hq_country for the companies in packages/taxonomy/companies.yaml"
