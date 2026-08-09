-- Extensions required by docs/03-data-model.md.
--
-- vector    job.embedding and resume.embedding are vector(384), with an HNSW
--           index for semantic dedup and search (ADR-004).
-- pg_trgm   trigram GIN index on company.normalized_name, for fuzzy company
--           identity resolution (docs/08).
-- citext    case-insensitive company.slug and app_user.email. Comparing emails
--           case-sensitively is a bug that surfaces as a duplicate account.
-- pgcrypto  gen_random_uuid() for every primary key.

CREATE EXTENSION IF NOT EXISTS vector;
CREATE EXTENSION IF NOT EXISTS pg_trgm;
CREATE EXTENSION IF NOT EXISTS citext;
CREATE EXTENSION IF NOT EXISTS pgcrypto;
