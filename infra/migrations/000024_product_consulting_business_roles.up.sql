-- ADR-020: Scout no longer excludes non-software roles by definition
-- (superseding docs/01-prd.md section 4.3's prior "Non-software
-- internships" / "Non-technical consulting roles" non-goals). Adds eight
-- new role_family leaves — product.management, product.design,
-- product.analyst, consulting.strategy, consulting.technology,
-- business.strategy, business.operations, business.general — matching
-- packages/taxonomy/roles.yaml's new entries. None require the
-- advocacy.*-only technical-evidence gate; packages/schema/job.go carries
-- the corresponding typed constants.
--
-- ALTER TYPE ... ADD VALUE cannot run inside the same transaction as a
-- later statement that uses the new value (a real Postgres restriction,
-- not a style choice) — harmless here since this migration does nothing
-- but add values, same as 000021/000022's precedent.
ALTER TYPE role_family ADD VALUE 'product.management';
ALTER TYPE role_family ADD VALUE 'product.design';
ALTER TYPE role_family ADD VALUE 'product.analyst';
ALTER TYPE role_family ADD VALUE 'consulting.strategy';
ALTER TYPE role_family ADD VALUE 'consulting.technology';
ALTER TYPE role_family ADD VALUE 'business.strategy';
ALTER TYPE role_family ADD VALUE 'business.operations';
ALTER TYPE role_family ADD VALUE 'business.general';
