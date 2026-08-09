# packages/schema

Canonical JSON Schema definitions — the single source of truth for types shared
across Go, Python, and TypeScript. Codegen produces Go structs, Pydantic models,
and TypeScript types; CI fails if generated code drifts from the schema. See
[ADR-001](../../docs/adr/ADR-001-monorepo-and-language-split.md).

**Not implemented yet.** This is module 2.

Note that `packages/db` (sqlc output) is a different thing and deliberately so:
sqlc generates Go types from the *database* schema, while this package defines
the types that cross *service* boundaries. They overlap but are not the same set.
