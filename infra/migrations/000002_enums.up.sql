-- Enumerated types. See docs/03-data-model.md sections 3 and 7.
--
-- Enums rather than lookup tables because these vocabularies change with code,
-- not with data — a new source kind arrives with the adapter that reads it. The
-- tradeoff is that adding a value needs a migration, which is the correct
-- friction: an unrecognised role family should fail loudly, not silently insert.

CREATE TYPE source_kind AS ENUM (
    'ats_greenhouse', 'ats_lever', 'ats_ashby', 'ats_workday',
    'ats_smartrecruiters', 'ats_bamboohr', 'ats_rippling', 'ats_icims',
    'ats_jobvite', 'ats_successfactors', 'ats_workable', 'ats_recruitee',
    'ats_teamtailor',
    'rss', 'atom', 'json_feed', 'sitemap', 'html_page',
    'hackernews', 'github_repo', 'github_discussions', 'reddit', 'x_account',
    'discord', 'telegram_channel',
    'email_alert', 'api_partner', 'hackathon', 'vc_portfolio', 'research_lab'
);

CREATE TYPE legal_posture AS ENUM (
    'permitted',    -- robots.txt and ToS allow automated access
    'email_only',   -- prohibited to fetch; ingest via the user's alert email
    'api_only',     -- only via an authorized API
    'prohibited'    -- do not touch; the collector refuses
);

CREATE TYPE source_status AS ENUM (
    'active', 'paused', 'quarantined', 'failed', 'retired', 'pending_review'
);

-- Role families. The tree lives in docs/07-normalization-taxonomy.md; this enum
-- is its storage form. Intermediate nodes ('swe.ml', 'swe.infra') are
-- assignable because a title can be confidently placed in a family without
-- being placeable in a leaf.
--
-- The advocacy.* families are developer-facing engineering roles. They are
-- admitted only with concrete technical evidence in the description, which is
-- what keeps developer marketing and quota-carrying sales roles out of the feed.
CREATE TYPE role_family AS ENUM (
    'swe.general', 'swe.backend', 'swe.frontend', 'swe.fullstack', 'swe.mobile',
    'swe.ml', 'swe.ml.research', 'swe.data', 'swe.infra', 'swe.infra.sre',
    'swe.infra.devops', 'swe.infra.platform', 'swe.infra.cloud', 'swe.systems',
    'swe.security', 'swe.embedded', 'swe.qa', 'swe.research',
    'advocacy.devrel', 'advocacy.devex', 'advocacy.solutions',
    'swe.other'
);

CREATE TYPE seniority AS ENUM (
    'internship', 'apprenticeship', 'new_grad', 'entry', 'mid', 'senior',
    'staff', 'unknown'
);

-- 'unknown' is NOT 'unpaid'. Most postings state no compensation, and excluding
-- them would discard most of the market. See docs/07 section 7.
CREATE TYPE paid_signal AS ENUM ('paid', 'unpaid', 'unknown');

CREATE TYPE work_mode AS ENUM ('onsite', 'hybrid', 'remote', 'unknown');

CREATE TYPE job_status AS ENUM ('open', 'closed', 'expired', 'filled', 'unknown');

CREATE TYPE application_state AS ENUM (
    'new', 'viewed', 'saved', 'dismissed', 'applied', 'screening',
    'interviewing', 'offer', 'accepted', 'rejected', 'withdrawn', 'expired'
);

CREATE TYPE notification_trigger AS ENUM (
    'bengaluru_match', 'high_score', 'remote_high_quality', 'watchlist_hiring',
    'deadline_approaching', 'prestige_opening', 'newgrad_match', 'digest'
);

CREATE TYPE notification_channel_kind AS ENUM (
    'native_push',   -- FCM via the Capacitor shell (ADR-012)
    'telegram',
    'web_push',      -- desktop and not-installed fallback
    'email', 'discord', 'in_app'
);

-- Enum rather than a boolean: only Android ships, but ADR-012 keeps iOS
-- reachable without a migration.
CREATE TYPE device_platform AS ENUM ('android', 'ios');
