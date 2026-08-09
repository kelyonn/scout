-- Company identity. See docs/03-data-model.md section 3.1.
--
-- Identity resolution is the foundation: dedup filters by company_id, so
-- getting this wrong breaks everything downstream.

CREATE TABLE company (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    slug                CITEXT NOT NULL UNIQUE,

    -- Identity signals, in confidence order
    canonical_name      TEXT NOT NULL,
    legal_name          TEXT,
    registered_domain   CITEXT UNIQUE,          -- strongest signal
    normalized_name     CITEXT NOT NULL,        -- lowercased, suffixes stripped

    -- Enrichment
    description         TEXT,
    logo_url            TEXT,
    website_url         TEXT,
    linkedin_url        TEXT,
    github_org          TEXT,
    crunchbase_slug     TEXT,

    -- Classification. NOTE: none of these may feed company_quality — see
    -- docs/09-ranking-scoring.md. They drive adapter choice, scheduling,
    -- competition estimation, and per-category coverage auditing only.
    company_type        TEXT NOT NULL DEFAULT 'unknown' CHECK (company_type IN (
        'product',
        'services_it', 'services_consulting', 'services_engineering',
        'gcc',
        'core_bfsi', 'core_manufacturing', 'core_energy', 'core_telecom',
        'core_retail_cpg', 'core_healthcare', 'core_aerospace_def',
        'core_logistics',
        'research', 'public_sector', 'nonprofit',
        'unknown'
    )),
    company_type_source TEXT CHECK (company_type_source IN (
        'registry', 'heuristic', 'llm'
    )),

    -- GCCs and subsidiaries: "Target in India" -> "Target Corporation".
    -- Deliberately NOT a merge; see docs/08-dedup-identity.md. Merging them
    -- would collapse a Minneapolis role and a Bengaluru role into one group,
    -- and location tier is the strongest signal in the whole ranking.
    parent_company_id   UUID REFERENCES company (id) ON DELETE SET NULL,

    size_bucket         TEXT CHECK (size_bucket IN (
        '1-10', '11-50', '51-200', '201-500', '501-1000',
        '1001-5000', '5001-10000', '10000+'
    )),
    founded_year        SMALLINT,
    stage               TEXT CHECK (stage IN (
        'pre_seed', 'seed', 'series_a', 'series_b', 'series_c',
        'series_d_plus', 'public', 'private', 'bootstrapped',
        'nonprofit', 'government', 'academic'
    )),
    total_funding_usd   BIGINT,
    last_funding_at     DATE,
    industries          TEXT [] NOT NULL DEFAULT '{}',
    tech_stack          TEXT [] NOT NULL DEFAULT '{}',
    hq_country          CHAR(2),
    hq_city             TEXT,

    -- Scoring inputs (0-100), computed nightly
    quality_score       SMALLINT CHECK (quality_score BETWEEN 0 AND 100),
    engineering_score   SMALLINT CHECK (engineering_score BETWEEN 0 AND 100),
    quality_computed_at TIMESTAMPTZ,

    -- Discovery provenance
    discovered_via      TEXT NOT NULL,   -- 'seed'|'ats_enumeration'|'funding'|...
    discovered_at       TIMESTAMPTZ NOT NULL DEFAULT now(),

    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at          TIMESTAMPTZ
);

CREATE INDEX company_normalized_name_trgm ON company
USING gin (normalized_name gin_trgm_ops);
CREATE INDEX company_quality_idx ON company (quality_score DESC NULLS LAST)
WHERE deleted_at IS NULL;
CREATE INDEX company_industries_idx ON company USING gin (industries);
CREATE INDEX company_type_idx ON company (company_type) WHERE deleted_at IS NULL;
CREATE INDEX company_parent_idx ON company (parent_company_id)
WHERE parent_company_id IS NOT NULL;

-- Aliases: "Meta" = "Facebook" = "Meta Platforms Inc."
CREATE TABLE company_alias (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    company_id    UUID NOT NULL REFERENCES company (id) ON DELETE CASCADE,
    alias         CITEXT NOT NULL,
    alias_kind    TEXT NOT NULL CHECK (alias_kind IN (
        'former_name', 'abbreviation', 'ats_token', 'domain',
        'legal_entity', 'misspelling', 'subsidiary'
    )),
    confidence    REAL NOT NULL DEFAULT 1.0 CHECK (confidence BETWEEN 0 AND 1),
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (alias, alias_kind)
);

CREATE INDEX company_alias_lookup ON company_alias (alias);
