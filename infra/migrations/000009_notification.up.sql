-- Notifications. See docs/03-data-model.md section 7 and
-- docs/11-notifications.md.
--
-- notification_dedup_idx is THE guarantee behind "never notify twice"
-- (AGENTS.md rule 2, SCOUT-NOTIF-002). Do not add an ON CONFLICT DO UPDATE
-- that circumvents it and do not add a code path that inserts around it.

CREATE TABLE notification_channel (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id              UUID NOT NULL REFERENCES app_user (id) ON DELETE CASCADE,
    kind                 notification_channel_kind NOT NULL,
    config               JSONB NOT NULL,     -- encrypted: chat_id, push subscription, ...
    enabled              BOOLEAN NOT NULL DEFAULT TRUE,
    verified_at          TIMESTAMPTZ,
    last_success_at      TIMESTAMPTZ,
    last_failure_at      TIMESTAMPTZ,
    failure_count        SMALLINT NOT NULL DEFAULT 0,

    -- native_push only: one row per physical device (ADR-012).
    platform             device_platform,
    device_token         TEXT,             -- FCM registration token
    device_label         TEXT,             -- "Pixel 8", shown in settings
    app_version          TEXT,
    token_refreshed_at   TIMESTAMPTZ,

    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT native_push_needs_device CHECK (
        kind <> 'native_push' OR (platform IS NOT NULL AND device_token IS NOT NULL)
    )
);

-- One channel row per device token; re-registering the same token updates in place.
CREATE UNIQUE INDEX notification_channel_device_idx
ON notification_channel (user_id, device_token)
WHERE kind = 'native_push';

-- A user has at most one Telegram, email, or Discord channel, but many devices.
CREATE UNIQUE INDEX notification_channel_single_idx
ON notification_channel (user_id, kind)
WHERE kind IN ('telegram', 'email', 'discord');

CREATE TABLE notification (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id              UUID NOT NULL REFERENCES app_user (id) ON DELETE CASCADE,
    job_group_id         UUID REFERENCES job_group (id) ON DELETE CASCADE,
    trigger              notification_trigger NOT NULL,
    urgency              TEXT NOT NULL CHECK (urgency IN ('instant', 'batched', 'digest')),
    payload              JSONB NOT NULL,
    priority_at_send     SMALLINT,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    scheduled_for        TIMESTAMPTZ NOT NULL DEFAULT now(),
    suppressed_reason    TEXT     -- 'quiet_hours'|'budget'|'backfill'|null
);

-- THE guarantee: never notify twice for the same opportunity (SCOUT-NOTIF-002).
CREATE UNIQUE INDEX notification_dedup_idx
ON notification (user_id, job_group_id, trigger)
WHERE job_group_id IS NOT NULL;

CREATE TABLE notification_delivery (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    notification_id   UUID NOT NULL REFERENCES notification (id) ON DELETE CASCADE,
    channel_id        UUID NOT NULL REFERENCES notification_channel (id) ON DELETE CASCADE,
    status            TEXT NOT NULL CHECK (
        status IN ('pending', 'sent', 'delivered', 'failed', 'skipped')
    ),
    attempts          SMALLINT NOT NULL DEFAULT 0,
    error             TEXT,
    sent_at           TIMESTAMPTZ,
    opened_at         TIMESTAMPTZ,
    latency_ms        INTEGER,        -- posted_at -> sent_at, the SLO measurement
    provider_msg_id   TEXT,           -- for reconciling delivery receipts
    UNIQUE (notification_id, channel_id)
);
