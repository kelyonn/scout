-- A hard eligibility gate on top of target_seniority (000007_user), which
-- until now only fed overall_match's seniority_fit term — a soft signal
-- easily outweighed by company_quality/deadline_urgency/freshness in the
-- weighted mean, so a senior/staff/director role can still out-rank an
-- internship. Toggleable rather than a permanent behavior change: the
-- graduating-in-a-year user this was added for wants it on today and off
-- once target_seniority itself widens past internship/new_grad.
ALTER TABLE user_profile
    ADD COLUMN seniority_filter_enabled BOOLEAN NOT NULL DEFAULT TRUE;
