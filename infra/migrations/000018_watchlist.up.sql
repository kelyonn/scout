-- The watchlist_hiring notification trigger (docs/11-notifications.md
-- section 2: "A watchlisted company posts any relevant role" — the
-- lowest threshold of any trigger, priority >= 60, because the user
-- explicitly asked to be told, and second-guessing a direct instruction
-- with a quality filter would be wrong). Schema exactly as
-- docs/03-data-model.md section 6 already specified; this migration is
-- the first thing to actually create it.
--
-- notify_any_role exists in the row today but is not read by the
-- trigger yet — docs/11 defines only one watchlist behavior ("any
-- relevant role"), so there is nothing for a false value to mean until a
-- second, narrower mode is designed. Kept because the API contract
-- (docs/04-api-design.md section 4.5: POST /v1/me/watchlist accepts
-- {company_id, notify_any_role}) already commits to the field.
CREATE TABLE watchlist (
    user_id      UUID NOT NULL REFERENCES app_user (id) ON DELETE CASCADE,
    company_id   UUID NOT NULL REFERENCES company (id) ON DELETE CASCADE,
    notify_any_role BOOLEAN NOT NULL DEFAULT TRUE,
    added_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, company_id)
);
