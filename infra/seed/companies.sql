-- Populates company.company_type/hq_country from packages/taxonomy/companies.yaml —
-- generated from that file, not hand-duplicated, so the two never drift. Re-run
-- after editing companies.yaml. Idempotent (plain UPDATE by slug, safe to re-run).
--
-- well_known is deliberately NOT a column here — it's read directly from the
-- embedded companies.yaml by apps/collector/internal/scoring (competition_estimate's
-- brand-recognition proxy only), the same way skills.yaml/roles.yaml are consumed
-- without a database mirror.
--
-- Run via `make seed-companies`. Not part of `make seed`/`make dev` — same reasoning
-- as seed-sources.sh: a deliberate, reviewed action rather than something that runs
-- silently on every fresh local stack.

update company set company_type = 'product', company_type_source = 'registry', hq_country = 'US' where slug = 'affirm';
update company set company_type = 'product', company_type_source = 'registry', hq_country = 'US' where slug = 'airbyte';
update company set company_type = 'product', company_type_source = 'registry', hq_country = 'US' where slug = 'airtable';
update company set company_type = 'product', company_type_source = 'registry', hq_country = 'US' where slug = 'amplitude';
update company set company_type = 'product', company_type_source = 'registry', hq_country = 'US' where slug = 'asana';
update company set company_type = 'product', company_type_source = 'registry', hq_country = 'US' where slug = 'attio';
update company set company_type = 'product', company_type_source = 'registry', hq_country = 'US' where slug = 'block';
update company set company_type = 'product', company_type_source = 'registry', hq_country = 'US' where slug = 'brex';
update company set company_type = 'product', company_type_source = 'registry', hq_country = 'US' where slug = 'calendly';
update company set company_type = 'product', company_type_source = 'registry', hq_country = 'US' where slug = 'carta';
update company set company_type = 'product', company_type_source = 'registry', hq_country = 'US' where slug = 'checkr';
update company set company_type = 'product', company_type_source = 'registry', hq_country = 'US' where slug = 'clickhouse';
update company set company_type = 'product', company_type_source = 'registry', hq_country = 'US' where slug = 'cloudflare';
update company set company_type = 'product', company_type_source = 'registry', hq_country = 'US' where slug = 'coinbase';
update company set company_type = 'product', company_type_source = 'registry', hq_country = 'US' where slug = 'column';
update company set company_type = 'product', company_type_source = 'registry', hq_country = 'US' where slug = 'coursera';
update company set company_type = 'product', company_type_source = 'registry', hq_country = 'IN' where slug = 'cred';
update company set company_type = 'product', company_type_source = 'registry', hq_country = 'US' where slug = 'cursor';
update company set company_type = 'product', company_type_source = 'registry', hq_country = 'US' where slug = 'databricks';
update company set company_type = 'product', company_type_source = 'registry', hq_country = 'US' where slug = 'datadog';
update company set company_type = 'product', company_type_source = 'registry', hq_country = 'US' where slug = 'discord';
update company set company_type = 'product', company_type_source = 'registry', hq_country = 'US' where slug = 'doximity';
update company set company_type = 'product', company_type_source = 'registry', hq_country = 'US' where slug = 'dropbox';
update company set company_type = 'product', company_type_source = 'registry', hq_country = 'US' where slug = 'druva';
update company set company_type = 'product', company_type_source = 'registry', hq_country = 'US' where slug = 'duolingo';
update company set company_type = 'product', company_type_source = 'registry', hq_country = 'US' where slug = 'elastic';
update company set company_type = 'product', company_type_source = 'registry', hq_country = 'US' where slug = 'figma';
update company set company_type = 'product', company_type_source = 'registry', hq_country = 'US' where slug = 'fireworks';
update company set company_type = 'product', company_type_source = 'registry', hq_country = 'US' where slug = 'fivetran';
update company set company_type = 'product', company_type_source = 'registry', hq_country = 'US' where slug = 'flexport';
update company set company_type = 'product', company_type_source = 'registry', hq_country = 'US' where slug = 'gitlab';
update company set company_type = 'product', company_type_source = 'registry', hq_country = 'IN' where slug = 'glance';
update company set company_type = 'product', company_type_source = 'registry', hq_country = 'US' where slug = 'grafanalabs';
update company set company_type = 'product', company_type_source = 'registry', hq_country = 'US' where slug = 'gusto';
update company set company_type = 'product', company_type_source = 'registry', hq_country = 'US' where slug = 'hex';
update company set company_type = 'product', company_type_source = 'registry', hq_country = 'US' where slug = 'highradius';
update company set company_type = 'product', company_type_source = 'registry', hq_country = 'US' where slug = 'imprint';
update company set company_type = 'product', company_type_source = 'registry', hq_country = 'US' where slug = 'instacart';
update company set company_type = 'product', company_type_source = 'registry', hq_country = 'US' where slug = 'instawork';
update company set company_type = 'product', company_type_source = 'registry', hq_country = 'US' where slug = 'linear';
update company set company_type = 'product', company_type_source = 'registry', hq_country = 'US' where slug = 'lyft';
update company set company_type = 'product', company_type_source = 'registry', hq_country = 'IN' where slug = 'meesho';
update company set company_type = 'product', company_type_source = 'registry', hq_country = 'US' where slug = 'mercury';
update company set company_type = 'product', company_type_source = 'registry', hq_country = 'US' where slug = 'minio';
update company set company_type = 'product', company_type_source = 'registry', hq_country = 'US' where slug = 'mixpanel';
update company set company_type = 'product', company_type_source = 'registry', hq_country = 'US' where slug = 'modal';
update company set company_type = 'product', company_type_source = 'registry', hq_country = 'US' where slug = 'mongodb';
update company set company_type = 'product', company_type_source = 'registry', hq_country = 'US' where slug = 'notion';
update company set company_type = 'product', company_type_source = 'registry', hq_country = 'US' where slug = 'openai';
update company set company_type = 'product', company_type_source = 'registry', hq_country = 'IN' where slug = 'paytm';
update company set company_type = 'product', company_type_source = 'registry', hq_country = 'US' where slug = 'pinterest';
update company set company_type = 'product', company_type_source = 'registry', hq_country = 'US' where slug = 'posthog';
update company set company_type = 'product', company_type_source = 'registry', hq_country = 'US' where slug = 'postman';
update company set company_type = 'product', company_type_source = 'registry', hq_country = 'US' where slug = 'ramp';
update company set company_type = 'product', company_type_source = 'registry', hq_country = 'US' where slug = 'reddit';
update company set company_type = 'product', company_type_source = 'registry', hq_country = 'US' where slug = 'render';
update company set company_type = 'product', company_type_source = 'registry', hq_country = 'US' where slug = 'replit';
update company set company_type = 'product', company_type_source = 'registry', hq_country = 'US' where slug = 'resend';
update company set company_type = 'product', company_type_source = 'registry', hq_country = 'US' where slug = 'robinhood';
update company set company_type = 'product', company_type_source = 'registry', hq_country = 'US' where slug = 'sardine';
update company set company_type = 'product', company_type_source = 'registry', hq_country = 'IN' where slug = 'slice';
update company set company_type = 'product', company_type_source = 'registry', hq_country = 'US' where slug = 'stripe';
update company set company_type = 'product', company_type_source = 'registry', hq_country = 'US' where slug = 'stytch';
update company set company_type = 'product', company_type_source = 'registry', hq_country = 'US' where slug = 'substack';
update company set company_type = 'product', company_type_source = 'registry', hq_country = 'US' where slug = 'supabase';
update company set company_type = 'product', company_type_source = 'registry', hq_country = 'US' where slug = 'temporal';
update company set company_type = 'services_engineering', company_type_source = 'registry', hq_country = 'US' where slug = 'thoughtworks';
update company set company_type = 'product', company_type_source = 'registry', hq_country = 'US' where slug = 'twilio';
update company set company_type = 'product', company_type_source = 'registry', hq_country = 'US' where slug = 'udemy';
update company set company_type = 'product', company_type_source = 'registry', hq_country = 'US' where slug = 'warp';
update company set company_type = 'product', company_type_source = 'registry', hq_country = 'US' where slug = 'watershed';
update company set company_type = 'product', company_type_source = 'registry', hq_country = 'US' where slug = 'webflow';
update company set company_type = 'product', company_type_source = 'registry', hq_country = 'US' where slug = 'zenoti';
update company set company_type = 'product', company_type_source = 'registry', hq_country = 'US' where slug = 'zocdoc';
