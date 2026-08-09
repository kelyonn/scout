# Notifications — Scout

**Status:** Draft · **Owner:** Product · **Last updated:** 2026-08-06

The most important feature in the product, and the easiest one to ruin.

---

## 1. The core tension

A notification is the only part of Scout that reaches the user without being
asked for. That makes it the highest-value feature and the highest-risk one.

**If Scout under-notifies,** the user misses opportunities and the product has
failed at its single stated purpose.

**If Scout over-notifies,** the user mutes it within a week, and the product has
failed more completely — because a muted notification channel is worse than none
at all, since the user now believes they are covered when they are not.

Every rule in this document exists to hold that line. The governing principle
from the PRD: **interrupt rarely, and mean it.**

The practical consequence is that most discovered jobs do *not* generate a
notification. At Year 1 scale Scout will discover ~800 new jobs per day, filter
to perhaps 40 relevant ones, and notify on 5–15. The dashboard holds the rest.

---

## 2. Triggers

`SCOUT-NOTIF-001`. Seven named triggers, each with its own threshold, urgency,
and channel routing.

| Trigger | Fires when | Urgency | Threshold |
| --- | --- | --- | --- |
| `bengaluru_match` | Tier 1 location, relevant role, paid | **INSTANT** | priority ≥ 78 |
| `high_score` | Any location | **INSTANT** | priority ≥ 88 |
| `remote_high_quality` | Tier 3 remote, India-eligible | **INSTANT** | priority ≥ 82 |
| `watchlist_hiring` | A watchlisted company posts any relevant role | **INSTANT** | priority ≥ 60 |
| `prestige_opening` | Curated prestige list organization | **INSTANT** | priority ≥ 70 |
| `newgrad_match` | Seniority `new_grad`, matches target profile | BATCHED | priority ≥ 80 |
| `deadline_approaching` | Saved job at T-72h and T-24h | **INSTANT** | any saved job |
| `digest` | Daily summary of everything below threshold | DIGEST | — |

**Why `bengaluru_match` has a lower threshold than `high_score`.** The user's
stated top preference is Bengaluru. A priority-80 Bengaluru role should reach
them faster than a priority-85 role in Ohio. The location multiplier already
boosts Bengaluru scores; the lower trigger threshold compounds that, which is the
intended behavior for a stated hard preference.

**Why `watchlist_hiring` has the lowest threshold.** The user explicitly asked to
be told when this company hires. That is a direct instruction, and second-guessing
it with a quality threshold would be wrong. The only filter is role relevance.

### Trigger evaluation order

Triggers are evaluated in the order listed and **the first match wins**. One job
produces at most one notification, ever. A Bengaluru role scoring 92 fires
`bengaluru_match`, not both `bengaluru_match` and `high_score`.

---

## 3. Urgency classes

| Class | Behavior | Quiet hours |
| --- | --- | --- |
| **INSTANT** | Delivered immediately, target under 60 seconds from scoring | Queued until 07:30, **except** `bengaluru_match` with priority ≥ 92 |
| **BATCHED** | Accumulated and delivered hourly on the hour | Queued until 07:30 |
| **DIGEST** | One message daily at 08:00 IST | N/A |

**The quiet-hours exception is deliberately narrow.** Exactly one condition
breaks through: a Bengaluru role scoring 92 or above. This is the "you would want
to know at 3am" case, and it should fire perhaps once a month. Any broader
exception and quiet hours stop meaning anything.

**Queued, never dropped.** A notification suppressed by quiet hours is held with
`suppressed_reason = 'quiet_hours'` and delivered at the boundary. The user wakes
up to the overnight roles, ordered by priority, in a single grouped message rather
than eleven separate ones.

---

## 4. Budgets

`SCOUT-NOTIF-003`. The guard against a backfill, a bug, or an unusually active
day flooding the user.

| Window | Default cap | On exceed |
| --- | --- | --- |
| Per hour | 8 | Overflow demoted to BATCHED |
| Per day | 25 | Overflow demoted to DIGEST |
| Per trigger, per day | 10 | Overflow demoted to DIGEST |
| Per company, per day | 3 | Overflow grouped into one message |

**The per-company cap solves a specific real problem.** A company opening 40
internship requisitions at once — which large companies genuinely do in August —
would otherwise produce 40 notifications. Instead the user receives: "Microsoft
posted 40 internship roles — 12 match your profile" with a link to a filtered
view.

**Budget overflow is a demotion, not a drop.** Nothing is discarded. A job that
exceeds the hourly budget arrives in the next hourly batch, and one that exceeds
the daily budget arrives in tomorrow's digest. The user always eventually sees
everything relevant, which is what `US-DISC-01` requires.

**Budget exhaustion is itself alerting.** Hitting the daily cap more than twice in
a week means thresholds are miscalibrated, and that fires an operational alert.

---

## 5. Channels

| Channel | Latency | Reliability | Rich content | Role |
| --- | --- | --- | --- | --- |
| **Native push (FCM)** | ~1–3s | Excellent | Actions, badge, icon, deep link | **Primary** |
| **Telegram** | ~1–2s | Very high | Inline buttons, markdown, images | **Primary** |
| **Web Push** | ~2–5s | High on desktop | Title, body, actions | Fallback |
| **Email** | ~10–60s | Very high | Full HTML | Digest and deadlines |
| **Discord** | ~1–3s | High | Embeds | Optional |
| **In-app** | Instant via SSE | N/A | Full | Always, as the record |

**Two primaries, deliberately redundant.** Native push is the app's own
notification and behaves like every other app on the phone. Telegram is the
cheapest, richest, and most reliable transport available, needs 60 seconds of
setup, and reaches every platform. Running both in parallel costs almost nothing
and means no single provider outage is a missed opportunity.

**Native push** is specified in [ADR-012](adr/ADR-012-native-app-shell.md). FCM on
Android, delivered to the Capacitor shell. Notification actions call
the API directly from the native layer, so `Save` and `Dismiss` work from the lock
screen without opening the app.

**Web Push is a fallback, not a primary.** It serves desktop browsers and any
device without the app installed. The notifier prefers `native_push` when a device
is registered for both and suppresses the duplicate.

**WhatsApp is not a channel.** It was specified and withdrawn — the Cloud API
requires a dedicated phone number that cannot be the user's own, plus Meta
verification and template approval, for a third copy of a notification already
arriving on two transports. See [ADR-013](adr/ADR-013-whatsapp-channel.md), which
also records the standing prohibition on unofficial WhatsApp libraries.

### Routing

| Urgency | Native push | Telegram | Web Push | Email | Discord | In-app |
| --- | --- | --- | --- | --- | --- | --- |
| **INSTANT** | ✅ | ✅ | fallback | ✗ | optional | ✅ |
| **BATCHED** | ✅ | ✅ | fallback | ✗ | optional | ✅ |
| **DIGEST** | ✗ | ✅ | ✗ | ✅ | ✗ | ✅ |
| **Deadline** | ✅ | ✅ | fallback | ✅ | ✗ | ✅ |

**Native push is excluded from the digest.** A long summary belongs in a message
you sit down with, not in a lock-screen banner. Telegram and email carry it.

**Email is deliberately not used for instant notifications.** Email latency is
unpredictable, deliverability is a permanent maintenance burden, and a job alert
sitting in a promotions tab for 40 minutes defeats the purpose. Email is for
digests and deadlines, where latency does not matter and durability does.

### Fanout and failure

Channels are attempted in parallel with independent retry. One channel failing
never blocks another.

```
attempt 1 → immediate
attempt 2 → +5s
attempt 3 → +30s
attempt 4 → +5min
attempt 5 → +30min
then → mark failed, alert if the same channel fails 3 times in an hour
```

**Success is defined as any channel succeeding.** If Telegram delivers and FCM
fails, the notification succeeded — the user was informed. Per-channel failures
are tracked separately for health monitoring, and a channel failing consistently
gets disabled with a notice on the remaining channels.

**Failure is both primaries failing**, which is what pages the operator. Native
push and Telegram run on entirely separate infrastructure, so simultaneous failure
should be effectively impossible short of the user's phone being offline — in which
case both transports queue and deliver on reconnect.

---

## 6. Message formats

### 6.1 Telegram — instant

```
🎯 Bengaluru · Match 94

Cloudflare
Software Engineering Intern

📍 Bengaluru (Hybrid)
💰 ₹85,000/month · 82nd percentile
🕐 Posted 12 minutes ago
⏱ ~5 min to apply

Strong backend match — Go and distributed
systems are your top two skills, and this
is genuine edge infrastructure work.

Skills you have: Go, Kubernetes, Linux, gRPC
You're missing: Rust (nice-to-have)

[  Apply  ]  [  Save  ]  [  Dismiss  ]
[  Details  ]
```

The inline buttons write directly to `user_job_state` through an HMAC-signed
callback, so the user can triage from the lock screen without opening anything.
This matters more than it sounds: the friction between "seeing a notification"
and "acting on it" is where most job-search tools lose the user.

### 6.2 Native push (FCM)

```
┌────────────────────────────────────────────┐
│ 🔍 Scout                            now    │
│ Cloudflare · SWE Intern · Bengaluru        │
│ Match 94 · ₹85k/mo · posted 12 min ago     │
│ Go and distributed systems match your      │
│ top skills.                                │
│                                            │
│   Apply        Save        Dismiss         │
└────────────────────────────────────────────┘
```

```jsonc
{
  "notification": {
    "title": "Cloudflare · SWE Intern · Bengaluru",
    "body":  "Match 94 · ₹85k/mo · posted 12 min ago\nGo and distributed systems match your top skills."
  },
  "data": {
    "job_group_id": "0192f3a1-...",
    "deep_link": "scout://job/0192f3a1-...",
    "trigger": "bengaluru_match",
    "priority": 94
  },
  "android": {
    "collapse_key": "job-0192f3a1",        // replaces rather than stacks
    "priority": "high",                    // wakes the device from Doze
    "notification": {
      "channel_id": "opportunities",
      "click_action": "OPEN_JOB",
      "notification_count": 7              // launcher badge
    }
  }
}
```

Four details that matter:

**`collapse_key`** means a score update for the same job replaces the existing
notification instead of stacking a second one.

**`priority: high`** is what gets an instant alert through Doze and App Standby. It
is applied only to INSTANT urgency — batched and digest use normal priority, because
burning the high-priority allowance on a daily summary is how Android starts
throttling the ones that matter.

**Actions call the API from the native layer.** `Save` and `Dismiss` write to
`user_job_state` without opening the app, matching the Telegram inline buttons.

**Notification channels are split** so the user can tune each independently in
Android settings — silence digests while leaving instant alerts audible, without
touching Scout's own settings:

| Channel ID | Importance | Carries |
| --- | --- | --- |
| `opportunities` | High — sound + heads-up | Instant triggers |
| `deadlines` | High | T-72h and T-24h warnings |
| `digests` | Default — no sound | The 08:00 IST summary |
| `system` | Low | Channel failures, budget notices |

Channels are created on first launch and **cannot be changed afterwards** — Android
locks a channel's importance once the user has seen it. Getting the split right the
first time matters more than it looks; adding a fifth channel later is fine, but
re-tuning an existing one requires a new channel ID and a migration.

### 6.3 Web Push (fallback)

```
Title: 🎯 Cloudflare · SWE Intern · Bengaluru
Body:  Match 94 · ₹85k/month · Posted 12 min ago
       Go and distributed systems match your top skills.
Actions: [Apply] [Save]
Tag:    job-{group_id}      ← replaces rather than stacks
```

Used for desktop browsers and any device without the app installed. Suppressed
when the same device is reachable via `native_push`.

### 6.4 Grouped (per-company overflow)

```
🏢 Microsoft is hiring

12 roles match your profile
(40 posted in the last hour)

Top matches:
 • SWE Intern, Azure Core — Bengaluru — 89
 • SWE Intern, M365 — Hyderabad — 84
 • SWE Intern, Research — Bengaluru — 83

[ View all 12 ]
```

### 6.5 Daily digest — 08:00 IST

```
☀️ Good morning · Thursday 6 August

Overnight: 3 new · This week: 47 new

━━ New while you slept ━━━━━━━━━━━━━

🎯 Razorpay · Backend Intern · Bengaluru · 87
   ₹70k/mo · posted 02:14 · ~8 min to apply

🎯 Vercel · Platform Intern · Remote · 84
   $2,000/mo · posted 04:41 · ~5 min to apply

━━ Closing soon ━━━━━━━━━━━━━━━━━━━━

⏰ Stripe SWE Intern — 2 days left
⏰ Figma Backend Intern — 4 days left

━━ Your week ━━━━━━━━━━━━━━━━━━━━━━

Applied 6 · Interviews 2 · Pending 11
Response rate 33% (up from 21%)

━━ One thing ━━━━━━━━━━━━━━━━━━━━━━

Kubernetes appears in 23 of your top 50
opportunities. Roughly 3 weeks of work
would unlock them.

[ Open Scout ]
```

The digest is the only place Scout is allowed to be long. It is read
deliberately, at a chosen moment, so it can carry the weekly stats and the one
piece of coaching that would be intrusive in a push notification.

---

## 7. Deduplication

`SCOUT-NOTIF-002`. The absolute guarantee.

```sql
CREATE UNIQUE INDEX notification_dedup_idx
  ON notification (user_id, job_group_id, trigger)
  WHERE job_group_id IS NOT NULL;
```

Enforced by the database. The notifier attempts an insert and treats a unique
violation as "already notified, skip" — no read-then-write race is possible.

**Additional safeguards:**

| Case | Handling |
| --- | --- |
| Job re-scored higher after notification | No new notification. The in-app record updates silently. |
| Job group merges after both members notified | Earliest retained, later marked superseded, counted in `scout_late_merge_duplicate_total` |
| Job reposted by the company | Same group via dedup → no notification |
| Same company posts a genuinely different role | Different group → notifies, correctly |
| Backfill reprocesses old jobs | `backfill = true` → notifier drops before any trigger evaluation |
| Weight version change rescores everything | Rescore sets `suppress_notifications` → nothing fires |

The last two are the dangerous ones and both are enforced at the notifier — the
last possible point — rather than trusting the caller. Each has a dedicated
integration test that runs a full backfill against a seeded database and asserts
zero notifications.

---

## 8. Delivery pipeline

```
Job scored
    │
    ▼
┌─────────────────────────────────────────────┐
│ 1. Backfill guard                           │
│    backfill flag set → DROP, log, done      │
├─────────────────────────────────────────────┤
│ 2. Eligibility                              │
│    is_software · paid or prestige · not     │
│    excluded company/keyword · status open   │
├─────────────────────────────────────────────┤
│ 3. Trigger evaluation (ordered, first wins) │
├─────────────────────────────────────────────┤
│ 4. Dedup — INSERT, unique violation = skip  │
├─────────────────────────────────────────────┤
│ 5. Budget check → possible demotion         │
├─────────────────────────────────────────────┤
│ 6. Quiet hours → possible queue             │
├─────────────────────────────────────────────┤
│ 7. Render per channel                       │
├─────────────────────────────────────────────┤
│ 8. Parallel fanout with retry               │
├─────────────────────────────────────────────┤
│ 9. Record delivery + latency                │
└─────────────────────────────────────────────┘
```

Steps 1 and 4 come before any expensive work. A backfilled job is dropped in
microseconds without rendering anything.

---

## 9. Latency

`US-NOTIF-01`: p50 ≤10 minutes, p95 ≤30 minutes, from posting to delivery.

**The budget:**

| Segment | p50 | p95 | Notes |
| --- | --- | --- | --- |
| Posting → our poll | 5 min | 25 min | **Dominant.** Bounded by poll interval. |
| Poll → observation | 0.5s | 2s | |
| Observation → scored | 2s | 8s | |
| Scored → notification decided | 0.2s | 1s | |
| Decided → delivered | 2s | 10s | |
| **Total** | **~5.1 min** | **~25.4 min** | |

**Everything except the first row is rounding error.** The entire latency budget
is poll interval, which is why the adaptive scheduler in
[06](06-ingestion-pipeline.md) matters far more than any pipeline optimization.
Getting the brain 10x faster would improve p50 by about 2 seconds. Halving the
poll interval on high-yield sources improves it by minutes.

**Measurement:** `notification_delivery.latency_ms` records
`posted_at → sent_at`. Jobs with `posted_at_estimated = true` are excluded from
the SLO calculation, because measuring latency against a timestamp we invented
would make the metric meaningless.

---

## 10. Preventing fatigue

The failure mode that kills the product quietly.

### Monitored counter-metrics

| Metric | Healthy | Action if breached |
| --- | --- | --- |
| Notification open rate | ≥70% | Below 50% → raise all thresholds by 5 |
| Dismiss rate after opening | ≤25% | Above 40% → ranking review |
| Notifications per day | 5–15 | Above 20 sustained → raise thresholds |
| Time to first action | ≤2h median | Rising → relevance is degrading |
| Channel mutes | 0 | Any → immediate review |

**Automatic threshold adaptation.** If the open rate falls below 50% over a
rolling 14 days, thresholds rise by 5 points automatically and the user is told:
"You've been ignoring some notifications, so I've raised the bar. Adjust in
settings." Self-correcting, transparent, and reversible.

### User controls

Every notification carries a snooze affordance:

```
[ Fewer like this ]  → −3 to that trigger's weight for the matched attributes
[ More like this  ]  → +3
[ Mute company    ]  → excluded_companies
[ Pause 24h       ]  → everything to digest for a day
```

`Fewer like this` writes a `marked_irrelevant` feedback signal, which feeds the
learning loop in [09](09-ranking-scoring.md).

---

## 11. Channel setup

Ordered by how much setup friction each carries, because that determines what
ships first.

**Native push.** Zero setup beyond installing the app. On first launch the shell
requests notification permission, registers with FCM, and posts the device
token to `POST /api/v1/push/register-device`. Token refresh is handled by the
plugin and re-posted automatically. Multiple devices per user are supported and
each is listed in settings with its platform and last-seen time.

**Telegram.** User messages the bot, bot returns a 6-digit code, user enters it in
settings. `chat_id` stored encrypted. Verified by an immediate test message.
Roughly 60 seconds of setup.

**Web Push.** Requested on the third visit, never the first. On desktop this is
the primary browser channel. On mobile it is only offered if the native app is not
detected — if it is, native push already covers that device and a second prompt is
noise.

Subscription stored with the VAPID keypair; expiry (`410 Gone`) triggers automatic
re-subscription on next visit.

**Email.** Verified with a confirmation link. SPF, DKIM, and DMARC configured. A
plain-text alternative accompanies every HTML message.

**Discord.** Webhook URL entered in settings, validated with a test post.

**Every channel is verified before use.** An unverified channel is never used for a
real notification, because discovering a broken channel during the one
notification that mattered is unacceptable. `POST /api/v1/me/channels/test-all`
exercises every configured channel and is part of the post-deploy smoke test.

---

## 12. Testing

`SCOUT-NOTIF-QA`.

| Test | Assertion |
| --- | --- |
| Dedup under concurrency | 100 parallel notify attempts for one group → exactly 1 row |
| Backfill suppression | Full replay of 10,000 observations → 0 notifications |
| Rescore suppression | Weight version change rescoring 5,000 jobs → 0 notifications |
| Quiet hours | Notification at 02:00 IST → delivered 07:30, not dropped |
| Quiet hours exception | Bengaluru priority 94 at 02:00 → delivered immediately |
| Budget demotion | 30 eligible jobs in one hour → 8 instant, 22 demoted, 0 lost |
| Per-company grouping | 40 jobs from one company → 1 grouped message |
| Channel failover | Telegram down → native push delivers, marked success |
| Both primaries down | Native push and Telegram both fail → pages the operator |
| Late merge | Two groups notified then merged → duplicate counter increments |
| Trigger precedence | Bengaluru job at 92 → exactly one notification, `bengaluru_match` |
| **Native/Web Push dedup** | Device registered for both → exactly 1 delivery, native preferred |
| Native token refresh | FCM token rotates → re-registered, old token retired, no missed delivery |
| Multi-device | Two registered devices → both receive it, one notification row |

The first three are the ones that protect against catastrophic trust loss, and
they run on every pull request touching the notifier.

**The native/Web Push dedup test matters more than it looks.** A phone with both
the app installed and the PWA added to the home screen is registered on two
transports. Without suppression the user gets two notifications for one job, which
technically satisfies the database uniqueness constraint — one `notification` row,
two deliveries — while still feeling like a duplicate. Delivery-level dedup is a
separate concern from notification-level dedup, and both are tested.
