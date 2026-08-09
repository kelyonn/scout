# API Design — Scout

**Status:** Draft · **Owner:** Backend · **Last updated:** 2026-08-06

REST over HTTPS, JSON, with an OpenAPI 3.1 document generated from Go code and
used to generate the TypeScript client. The contract is checked in and CI fails
if it drifts from the implementation.

---

## 1. Conventions

**Base URL:** `https://<domain>/api/v1`

**Versioning** is in the path. A breaking change means `/v2` running alongside
`/v1` for at least 90 days. Additive changes — new fields, new optional
parameters, new endpoints — are not breaking and ship into the current version.

**Resource naming:** plural nouns, kebab-case paths, `snake_case` JSON fields.
`snake_case` because it matches the database and avoids a translation layer; the
TypeScript client generator handles the camelCase conversion at the boundary if
we ever want it.

**Methods:** `GET` read, `POST` create or action, `PATCH` partial update, `PUT`
full replace (rare), `DELETE` remove. `PATCH` uses merge-patch semantics: absent
means unchanged, `null` means clear.

**Idempotency:** every non-`GET` endpoint accepts an `Idempotency-Key` header.
Keys are stored for 24 hours; a repeat returns the original response. Required
for anything that triggers a notification or an external side effect.

**Timestamps:** RFC 3339 in UTC with a `Z` suffix, always. `2026-08-06T11:04:32Z`.

**Money:** an object, never a bare float.
`{"amount": "80000.00", "currency": "INR", "period": "month"}`. String amounts to
avoid float precision loss.

**IDs:** UUIDv7 in canonical string form. v7 rather than v4 because it is
time-sortable, which gives us better index locality on insert-heavy tables.

---

## 2. Errors

One error shape everywhere, based on RFC 9457 Problem Details:

```json
{
  "type": "https://scout.dev/errors/validation-failed",
  "title": "Validation failed",
  "status": 422,
  "detail": "location_tier must be between 1 and 4",
  "instance": "/api/v1/jobs?location_tier=7",
  "trace_id": "4bf92f3577b34da6a3ce929d0e0e4736",
  "errors": [
    { "field": "location_tier", "code": "out_of_range", "message": "must be 1-4" }
  ]
}
```

`trace_id` is always present and always matches the trace in Tempo. When
something goes wrong, the user reports the trace ID and we find the exact request
across all four services.

| Status | Type slug | When |
| --- | --- | --- |
| 400 | `malformed-request` | Unparseable body or parameters |
| 401 | `unauthenticated` | Missing or invalid session |
| 403 | `forbidden` | Authenticated but not permitted |
| 404 | `not-found` | Resource absent, or hidden by RLS |
| 409 | `conflict` | State transition invalid, unique violation |
| 410 | `gone` | Job expired or deleted |
| 422 | `validation-failed` | Well-formed but semantically invalid |
| 429 | `rate-limited` | With `Retry-After` |
| 500 | `internal-error` | Bug. Never leaks internals; trace_id only. |
| 503 | `dependency-unavailable` | Degraded mode; `Retry-After` set |

**Rule: a 404 and a 403 are indistinguishable for resources the user cannot
access.** Returning 403 confirms existence, which is an information leak.

---

## 3. Pagination, filtering, sorting

**Cursor-based, always.** Offset pagination on a feed that receives new rows
continuously causes items to shift between pages, which for a job feed means the
user genuinely misses postings while scrolling. That is the exact failure Scout
exists to prevent, so offset pagination is not acceptable here.

```
GET /api/v1/jobs?limit=25&cursor=eyJwIjo5MSwiaWQiOiIwMTkyLi4uIn0
```

```json
{
  "data": [ ... ],
  "page": {
    "next_cursor": "eyJwIjo4NCwiaWQiOiIwMTkzLi4uIn0",
    "has_more": true,
    "estimated_total": 1247
  }
}
```

The cursor encodes the sort key and a tiebreaker ID, base64url'd. It is opaque to
clients. `estimated_total` is an estimate from the planner rather than an exact
`COUNT(*)`, which is deliberately cheap.

**Filtering** uses explicit parameters, not a query DSL. A DSL is a security
surface and a support burden; a fixed parameter set is documentable and
cacheable.

| Parameter | Type | Notes |
| --- | --- | --- |
| `q` | string | Full-text plus semantic hybrid |
| `role_family` | enum, repeatable | `swe.backend`, `swe.ml`, … |
| `seniority` | enum, repeatable | |
| `location_tier` | int, repeatable | 1–4 |
| `city` | string, repeatable | |
| `country` | ISO-3166-1 alpha-2, repeatable | |
| `work_mode` | enum, repeatable | |
| `company_id` | uuid, repeatable | |
| `company_size` | enum, repeatable | |
| `min_priority` | int 0–100 | |
| `paid` | enum | Defaults to `paid` |
| `posted_after` | RFC3339 | |
| `deadline_before` | RFC3339 | |
| `state` | enum, repeatable | User's tracking state |
| `has_deadline` | bool | |

Repeated parameters are OR'd within a field and AND'd across fields.

**Sorting:** `sort=priority` (default), `posted_at`, `deadline_at`,
`company_quality`, `compensation`. Prefix with `-` to reverse.

---

## 4. Endpoints

### 4.1 Jobs

```http
GET    /api/v1/jobs                     # ranked feed, filtered
GET    /api/v1/jobs/{group_id}          # full detail with all sources
GET    /api/v1/jobs/{group_id}/similar  # semantic neighbours
GET    /api/v1/jobs/{group_id}/score    # subscore breakdown + explanation
GET    /api/v1/jobs/{group_id}/sources  # every observation backing this group
POST   /api/v1/jobs/{group_id}/state    # state transition
POST   /api/v1/jobs/{group_id}/feedback # explicit relevance signal
GET    /api/v1/jobs/{group_id}/prep     # AI: questions, gaps, summary
POST   /api/v1/jobs/{group_id}/cover-letter  # AI generation, async
```

Feed item shape:

```json
{
  "job_group_id": "0192f3a1-...",
  "title": "Software Engineering Intern",
  "company": {
    "id": "0192b7c4-...",
    "name": "Cloudflare",
    "logo_url": "https://...",
    "size_bucket": "1001-5000",
    "stage": "public",
    "quality_score": 88
  },
  "location": {
    "raw": "Remote - India",
    "city": null,
    "country": "IN",
    "tier": 3,
    "work_mode": "remote"
  },
  "compensation": {
    "min": { "amount": "80000.00", "currency": "INR", "period": "month" },
    "max": null,
    "normalized_inr_month": "80000.00",
    "confidence": 0.92,
    "paid": "paid"
  },
  "role_family": "swe.backend",
  "seniority": "internship",
  "skills": ["go", "rust", "distributed-systems", "networking"],
  "posted_at": "2026-08-06T10:52:00Z",
  "deadline_at": null,
  "first_seen_at": "2026-08-06T11:04:32Z",
  "score": {
    "priority": 91,
    "overall_match": 88,
    "explanation": "Strong backend match: Go and distributed systems align with your top skills. Remote with an India-based team, paid at ₹80k/month, and Cloudflare's intern program has a high conversion rate."
  },
  "state": "new",
  "source_count": 3,
  "apply_url": "https://boards.greenhouse.io/cloudflare/jobs/5847392"
}
```

**State transitions** are validated server-side against an explicit state
machine; an invalid transition is a 409, not a silent accept:

```
new ──▶ viewed ──┬──▶ saved ────▶ applied ──▶ screening ──▶ interviewing
                 │                    │                          │
                 └──▶ dismissed       └──▶ rejected     ┌─────────┼─────────┐
                                                        ▼         ▼         ▼
                                                     offer   rejected  withdrawn
                                                        │
                                                        ▼
                                                    accepted
```

### 4.2 Search

```http
GET  /api/v1/search?q=backend+intern+bangalore&mode=hybrid
GET  /api/v1/search/suggest?q=cloudf
```

`mode` is `keyword`, `semantic`, or `hybrid` (default). Hybrid fuses the two
ranked lists with Reciprocal Rank Fusion — see
[ADR-004](adr/ADR-004-search-strategy.md).

### 4.3 Realtime

```http
GET /api/v1/stream            # Server-Sent Events
```

```
event: job.new
data: {"job_group_id":"...","priority":91,"title":"..."}

event: job.score_updated
data: {"job_group_id":"...","priority":94}

event: notification.sent
data: {"notification_id":"...","trigger":"bengaluru_match"}

event: heartbeat
data: {"ts":"2026-08-06T11:05:00Z"}
```

Heartbeat every 30 seconds keeps proxies from closing the connection. Clients
reconnect with `Last-Event-ID` and receive anything missed, so a dropped
connection does not mean a missed job.

### 4.4 Profile and preferences

```http
GET    /api/v1/me
PATCH  /api/v1/me/profile
GET    /api/v1/me/preferences
PATCH  /api/v1/me/preferences
POST   /api/v1/me/resumes
GET    /api/v1/me/resumes
DELETE /api/v1/me/resumes/{id}
POST   /api/v1/me/resumes/{id}/default
GET    /api/v1/me/skill-gaps
```

Changing preferences that affect ranking enqueues a rescore of open jobs and
returns `202 Accepted` with a job handle the client can poll.

### 4.5 Companies and watchlists

```http
GET    /api/v1/companies
GET    /api/v1/companies/{id}
GET    /api/v1/companies/{id}/jobs
GET    /api/v1/companies/trending          # unusual recent hiring activity
GET    /api/v1/me/watchlist
POST   /api/v1/me/watchlist                # {company_id, notify_any_role}
DELETE /api/v1/me/watchlist/{company_id}
```

### 4.6 Applications, interviews, analytics

```http
GET    /api/v1/applications                     # ?state=applied
GET    /api/v1/applications/{group_id}/timeline
POST   /api/v1/interviews
PATCH  /api/v1/interviews/{id}
GET    /api/v1/interviews/upcoming
GET    /api/v1/calendar.ics                     # signed-token iCal feed
GET    /api/v1/analytics/funnel                 # ?group_by=company_size
GET    /api/v1/analytics/timeline
GET    /api/v1/analytics/sources                # which sources produce results
```

`analytics/sources` closes the loop: it reports which discovery sources actually
led to interviews, which is the input for reprioritizing the source registry.

### 4.7 Notifications

```http
GET    /api/v1/notifications
POST   /api/v1/notifications/{id}/read
GET    /api/v1/me/channels
POST   /api/v1/me/channels                 # add telegram/discord/email
POST   /api/v1/me/channels/{id}/test
POST   /api/v1/me/channels/test-all        # exercise every channel; smoke test
DELETE /api/v1/me/channels/{id}

POST   /api/v1/push/register-device        # native: FCM token — ADR-012
DELETE /api/v1/push/devices/{id}
POST   /api/v1/push/subscribe              # Web Push subscription (fallback)
GET    /api/v1/push/vapid-public-key
```

**`POST /push/register-device`** is called by the Capacitor shell on every launch.
It is idempotent on `(user_id, device_token)` — the same token re-registers in
place rather than accumulating rows, which is what makes token rotation safe.

```jsonc
// POST /api/v1/push/register-device
{
  "platform": "android",              // enum; only "android" ships today
  "device_token": "fMEp0X3...",
  "device_label": "Pixel 8",          // shown in settings
  "app_version": "1.2.0"
}
// 200 — created or refreshed
{ "channel_id": "0192...", "kind": "native_push", "verified_at": "2026-08-07T..." }
```

Registering a native device causes Web Push for that same device to be marked
`skipped` on future deliveries rather than deleted, so uninstalling the app falls
back to Web Push automatically.

### 4.7.1 Inbound channel webhooks

```http
POST   /api/v1/hooks/telegram              # inline button callbacks
```

Records inline-button actions (`Save`, `Dismiss`, `Not relevant`) against
`user_job_state`, so triage works from a Telegram message without opening the app.
The handler verifies the secret token set at webhook registration **before parsing
the body** — see [`13-security-privacy.md`](13-security-privacy.md).

### 4.8 Admin and operations

Same-origin, session-authenticated, not documented publicly.

```http
GET    /api/v1/admin/sources               # ?status=quarantined
POST   /api/v1/admin/sources               # register manually
PATCH  /api/v1/admin/sources/{id}
POST   /api/v1/admin/sources/{id}/poll     # force immediate poll
GET    /api/v1/admin/sources/{id}/history
GET    /api/v1/admin/queue                 # depth per queue
GET    /api/v1/admin/costs                 # LLM spend by task
POST   /api/v1/admin/rescore               # {weight_version, dry_run}
POST   /api/v1/admin/groups/{id}/unmerge   # fix a bad merge
GET    /api/v1/health
GET    /api/v1/health/deep                 # checks DB, Redis, R2, providers
```

### 4.9 Webhooks (inbound)

```http
POST   /api/v1/hooks/email                 # inbound job-alert mail
POST   /api/v1/hooks/telegram              # bot callbacks
```

Both verify an HMAC signature before parsing a single byte of body. Unsigned
requests are rejected at the middleware layer and logged as a security event.

---

## 5. Authentication

Detail in [ADR-015](adr/ADR-015-single-user-auth.md), which supersedes
[ADR-010](adr/ADR-010-authentication.md) for the single-user, network-gated case.

**The outer gate is the network.** Reaching this API at all requires a device on
the user's tailnet ([ADR-014](adr/ADR-014-zero-cost-hosting.md)). The endpoints
below are the inner gate.

```http
POST   /api/v1/auth/exchange     # bearer token → session cookie, once per device
POST   /api/v1/auth/logout       # clears the cookie
```

That is the entire authentication surface. The eleven passkey, magic-link,
recovery, and session-management endpoints the earlier version specified are
gone — there is one account, one token, and no enrollment or recovery flow to
have. `GET /auth/sessions` and its `DELETE` are gone with them: with a single
shared token there is nothing per-session to revoke, and rotation is changing one
environment variable and restarting.

**Requests may authenticate two ways:**

| Client | Mechanism |
| --- | --- |
| Browser | Session cookie set by `/auth/exchange`: `HttpOnly; Secure; SameSite=Strict` |
| Android app, `curl`, scripts | `Authorization: Bearer <SCOUT_AUTH_TOKEN>` |

Token comparison is constant-time. CSRF via double-submit token on all
state-changing requests — still required, because a cookie-authenticated browser
is still a browser.

Where Caddy forwards a `Tailscale-User-Login` header it is recorded in the audit
log. It is **never** the sole basis for granting access
([ADR-015](adr/ADR-015-single-user-auth.md)).

---

## 6. Rate limiting

Token bucket in Redis, keyed by token.

**This is self-protection, not abuse prevention.** There is no anonymous traffic
to throttle — the network gate means every request already comes from one of
three devices. What the limiter actually catches is a runaway script or a
mis-written client loop hammering the API, which is the realistic denial-of-
service in this design ([13](13-security-privacy.md) section 2).

| Scope | Limit | Burst |
| --- | --- | --- |
| Authenticated, read | 300/min | 50 |
| Authenticated, write | 60/min | 10 |
| AI generation | 20/hour | 3 |
| Unauthenticated | 20/min | 5 |
| Auth endpoints | See [ADR-010](adr/ADR-010-authentication.md) | |

Every response carries `RateLimit-Limit`, `RateLimit-Remaining`, and
`RateLimit-Reset` per the IETF draft.

---

## 7. Caching

| Endpoint class | Strategy | TTL |
| --- | --- | --- |
| `GET /jobs` (feed) | `private, max-age=30` + ETag | 30s |
| `GET /jobs/{id}` | `private, max-age=300` + ETag | 5 min |
| `GET /companies/{id}` | `private, max-age=3600` | 1 hour |
| `GET /analytics/*` | `private, max-age=600` | 10 min |
| Static assets | `public, max-age=31536000, immutable` | 1 year |
| Anything user-mutable | `no-store` | — |

ETags are computed from a content hash, so a conditional request that has not
changed costs one index lookup and returns 304.

---

## 8. Long-running operations

Anything over ~2 seconds — cover letter generation, resume analysis, bulk
rescoring — returns `202 Accepted` with an operation handle rather than holding
the connection.

```json
{
  "operation_id": "0192f4b2-...",
  "status": "pending",
  "poll_url": "/api/v1/operations/0192f4b2-...",
  "estimated_seconds": 12
}
```

The client polls, or listens for the completion event on the SSE stream. The SSE
path is preferred and is what the web client does; polling exists for clients
without a stream open.

---

## 9. OpenAPI and client generation

The spec at `packages/schema/openapi.yaml` is generated from Go handler
annotations by `swaggo`, and the TypeScript client is generated from that spec by
`openapi-typescript` plus `openapi-fetch`.

CI enforces three things:

1. The committed spec matches what the code generates. Drift fails the build.
2. The generated client compiles against the spec.
3. A breaking change to the spec — removed field, narrowed type, removed
   endpoint — fails unless the PR carries a `breaking-api` label and bumps the
   version.

This means the frontend cannot call an endpoint that does not exist, and a
backend field rename breaks the build rather than production.

---

## 10. Design decisions worth stating

**No GraphQL.** One client, known access patterns, one developer. GraphQL's
benefit is client-driven flexibility across many consumers; its costs are N+1
resolution, cache complexity, and query-depth attack surface. Wrong trade here.

**No gRPC externally.** Browser gRPC needs a proxy. The internal service boundary
is the database, not RPC, so there is no internal gRPC either.

**Cursor pagination is mandatory, not optional.** Explained in section 3 — with a
continuously-updating feed, offset pagination causes real missed jobs.

**Explanations ship with scores, not on demand.** Fetching a score without its
explanation would let the UI render an unjustified number, which principle 3 in
the PRD forbids. They are one payload so they cannot separate.

**Notification identity is the job group, not the job.** Every user-facing
endpoint keys on `job_group_id`. Individual `job` rows are an implementation
detail exposed only through `/jobs/{group_id}/sources`, so the API cannot
accidentally surface the same opportunity twice.
