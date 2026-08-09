# Runbook: Notifications stuck or failing

**Severity:** SEV1 · **Alert:** Notification queue depth >100 for 10 minutes, or
delivery failure rate high

Notifications are the product. An ingestion problem costs coverage; a
notification problem costs everything downstream of it.

---

## Triage (1 minute)

```sql
SELECT
  count(*) FILTER (WHERE scheduled_for <= now()
                     AND id NOT IN (SELECT notification_id FROM notification_delivery
                                    WHERE status IN ('sent','delivered'))) AS undelivered,
  count(*) FILTER (WHERE suppressed_reason IS NOT NULL)                    AS suppressed,
  count(*) FILTER (WHERE created_at > now() - interval '1 hour')           AS last_hour
FROM notification;

SELECT channel_id, status, count(*), max(error)
FROM notification_delivery
WHERE sent_at > now() - interval '1 hour' OR sent_at IS NULL
GROUP BY 1, 2;
```

| Pattern | Go to |
| --- | --- |
| Notifications created, none delivered | Step 1 — notifier or channels |
| No notifications created at all | Step 2 — trigger evaluation |
| Many suppressed | Step 3 — check the reason |
| One channel failing, others fine | Step 4 — single channel |

---

## Step 1 — Created but not delivered

```bash
docker compose ps notifier
docker compose logs --tail 200 notifier
```

**Not running:** start it. Queued notifications deliver on startup — nothing is
lost, only delayed.

```bash
docker compose up -d notifier
```

**Running but idle:** check the queue consumer.

```sql
SELECT state, count(*), min(created_at) AS oldest
FROM river_job WHERE queue = 'notify' GROUP BY 1;
```

`state = 'available'` with an old `oldest` means the consumer is not picking up
work. Restart the notifier. If it recurs, check for a stuck advisory lock or a
poisoned job:

```sql
SELECT id, attempt, max_attempts, errors FROM river_job
WHERE queue = 'notify' AND state = 'retryable'
ORDER BY created_at LIMIT 10;
```

A job cycling through retries with the same error is poisoned. Cancel it and fix
the cause:

```sql
UPDATE river_job SET state = 'cancelled' WHERE id = <id>;
```

---

## Step 2 — No notifications being created

Jobs are being scored but nothing triggers.

```sql
-- Are jobs being scored at all?
SELECT count(*) FROM job_score WHERE computed_at > now() - interval '1 hour';

-- What is the score distribution? Anything above threshold?
SELECT width_bucket(priority, 0, 100, 10) * 10 AS bucket, count(*)
FROM job_score WHERE computed_at > now() - interval '24 hours'
GROUP BY 1 ORDER BY 1;
```

| Finding | Cause |
| --- | --- |
| No scores at all | Scoring stage broken — see [ingestion-stalled](ingestion-stalled.md) step 2 |
| Scores exist, all below 78 | Thresholds too high, or a scoring regression |
| Scores exist and high, no notifications | Trigger evaluation or the backfill guard |

**Check the backfill guard.** The most likely cause of "high scores, no
notifications":

```sql
SELECT suppressed_reason, count(*) FROM notification
WHERE created_at > now() - interval '24 hours' GROUP BY 1;
```

If `suppressed_reason = 'backfill'` dominates, a backfill flag is stuck on. Find
what set it — usually a replay job that did not clear its flag, or a rescore that
is still running.

**Check for a scoring regression.** Compare today's mean priority against last
week's:

```sql
SELECT date_trunc('day', computed_at) AS day, avg(priority), count(*)
FROM job_score WHERE computed_at > now() - interval '14 days'
GROUP BY 1 ORDER BY 1;
```

A sudden drop means a weight change or a subscore returning wrong values. See
[quality-regression](quality-regression.md).

---

## Step 3 — Heavily suppressed

```sql
SELECT suppressed_reason, count(*) FROM notification
WHERE created_at > now() - interval '24 hours'
GROUP BY 1 ORDER BY 2 DESC;
```

| Reason | Correct? |
| --- | --- |
| `quiet_hours` | Yes, if it is night. They deliver at 07:30. |
| `budget` | Maybe — if it is happening daily, thresholds need raising |
| `backfill` | Only during a backfill. Otherwise a bug. |

Budget suppression more than twice a week means the notification thresholds are
miscalibrated. Raise them rather than raising the budget — the budget exists to
protect the user, and the right fix is to be more selective.

---

## Step 4 — One channel failing

```sql
SELECT c.kind, c.platform, d.status, count(*), max(d.error)
FROM notification_delivery d JOIN notification_channel c ON c.id = d.channel_id
WHERE d.sent_at > now() - interval '6 hours' OR d.sent_at IS NULL
GROUP BY 1, 2, 3;
```

**Read `skipped` as normal, not as failure.** Web Push shows `skipped` whenever a
native device covers the same target. That is correct behavior, not an incident.

| Channel | Common failure | Fix |
| --- | --- | --- |
| Native push | `UNREGISTERED` / `410` — token stale | App uninstalled or token rotated. Retire the token. If it was the only device, tell the user on another channel to reopen the app. |
| Native push (FCM) | `401` / `403` — service account rejected | Key rotated or Firebase project changed. Re-check the SOPS secret and restart. |
| Native push (FCM) | `SENDER_ID_MISMATCH` | The token was issued for a different Firebase project. Usually a debug build registering against production. Check `google-services.json` in the installed APK. |
| Native push (FCM) | `QUOTA_EXCEEDED` | Only plausible during a backfill, which should have been suppressed. Investigate the suppression flag before raising quota. |
| Native push | Delivered but never shown | Battery optimisation, or the OS notification channel was muted by the user. Check Android settings for the app; a muted `opportunities` channel looks identical to a delivery failure from the server side. |
| Telegram | `401` — bot token invalid | Rotate token in secrets, restart notifier |
| Telegram | `403` — user blocked the bot | User must unblock; alert on other channels |
| Telegram | `429` — rate limited | Respect `retry_after`; only plausible during a backfill, which should have been suppressed |
| Web Push | `410 Gone` — subscription expired | Automatic; user re-subscribes on next visit |
| Web Push | `401` — VAPID mismatch | Keys changed. **Do not rotate VAPID keys** — it invalidates every subscription. Restore the previous keypair. |
| Email | Bounce | Check SPF/DKIM/DMARC, check provider dashboard |
| Discord | `404` — webhook deleted | User must recreate it |

**Delivery succeeds if any channel succeeds.** A single channel failing is SEV3 if
the other is working, SEV1 only if both primaries are down.

### The failure that does not look like one

Native push tokens go stale silently. The provider returns success-with-error, the
notification row says `sent`, and the user receives nothing. Check specifically:

```sql
-- Devices that have never succeeded, or have not succeeded recently
SELECT id, platform, device_label, token_refreshed_at, last_success_at, failure_count
FROM notification_channel
WHERE kind = 'native_push'
ORDER BY last_success_at NULLS FIRST;
```

A device with a recent `token_refreshed_at` but a stale `last_success_at` is
registered and not delivering, which is the worst state to be in because nothing
alerts on it except `scout_push_token_invalid_total`.

### If Telegram delivers but its buttons do nothing

Not a delivery outage — a callback outage. Messages arrive, but tapping `Save` or
`Dismiss` silently does nothing, so lock-screen triage is broken while every
delivery metric looks healthy.

```bash
# Is the webhook registered and is Telegram seeing errors?
curl -s "https://api.telegram.org/bot$TG_TOKEN/getWebhookInfo" | jq

# Any inbound callback hits at all in the last day?
docker compose logs api --since 24h | rg 'hooks/telegram' | tail -20
```

`getWebhookInfo` reports `last_error_message` and `last_error_date`, which is
usually the fastest answer. The common causes are a webhook URL never re-registered
after a domain change, and the secret-token check rejecting every payload — the
latter often because the body is being parsed before verification.

---

## Verification

```bash
# Send a test notification to every configured channel
curl -X POST localhost:8080/api/v1/me/channels/test-all -H "Cookie: $SESSION"
```

Confirm arrival on the phone, not just a 200 response. Then:

```sql
SELECT count(*) FROM notification_delivery
WHERE status = 'delivered' AND sent_at > now() - interval '5 min';
```

---

## Follow-up

1. **Measure the gap.** How long were notifications delayed? Anything over an
   hour during a weekday should be treated as lost opportunity time.
2. **Check for duplicates.** If the notifier restarted mid-delivery, verify the
   unique index held:
   ```sql
   SELECT user_id, job_group_id, trigger, count(*)
   FROM notification GROUP BY 1,2,3 HAVING count(*) > 1;
   ```
   This must return zero rows. If it does not, the guarantee is broken and that
   is a SEV1 of its own.
3. **Postmortem** if the outage exceeded 2 hours.
