# ADR-010: Passkeys with magic-link fallback

**Status:** Accepted
**Date:** 2026-08-06

## Context

Scout holds data that is genuinely sensitive to its user: a resume with personal
contact details, a full application history, interview outcomes, rejections, and
inferred career preferences. A breach is embarrassing at best and materially
harmful during an active job search.

MVP is single-user, but the auth design must not assume that, because retrofitting
multi-tenant auth is one of the more painful migrations there is.

The system is internet-facing and unattended. It will be scanned and probed
continuously from the day the domain resolves.

## Options considered

### Option A — Email and password

**For:** universally understood, works everywhere, no dependencies.
**Against:** requires correct Argon2id parameters, password reset flows (itself a
common vulnerability), breach-list checking, rate limiting against credential
stuffing, and secure storage. Every one of these is a place to get it wrong. And
the fundamental problem remains: a password is a shared secret that can be
phished, reused, and leaked.

### Option B — OAuth only (Google, GitHub)

**For:** no credential storage at all. Users already have accounts. Fast.
**Against:** hard dependency on a third party for access to your own system. If
the Google account is locked or the OAuth app is flagged, the user is locked out
of their own job search data during placement season. Also leaks the existence of
a Scout account to the identity provider.

### Option C — Magic links only

**For:** no passwords. Simple. Users understand it.
**Against:** security reduces to email account security. Slow — every login is a
context switch to an inbox. Links can be intercepted by link-scanning proxies in
corporate mail, which pre-fetch and consume single-use tokens. Fragile if email
delivery has issues.

### Option D — Passkeys (WebAuthn) with magic-link fallback

**For:**
- Phishing-resistant by construction. The credential is cryptographically bound
  to the origin, so a lookalike domain cannot use it. This is a property no
  password or OTP scheme has.
- Nothing to steal server-side. We store a public key. A full database dump
  yields no usable credential.
- Biometric UX — Face ID, Touch ID, Windows Hello — which is faster than typing a
  password and works well on the mobile-first PWA.
- Browser support is now universal across Safari 16+, Chrome 108+, and Firefox
  119+, with cross-device sync via iCloud Keychain and Google Password Manager.
- Directly addresses the ADR-009 gap: passkeys give us biometric auth in a web
  app without a native app.

**Against:**
- Device loss recovery must be handled deliberately.
- Slightly more complex to implement than a password form.
- Some older browsers need the fallback.

## Decision

**Option D.** Passkeys as primary, magic link as fallback and recovery path.

### Flows

**Registration (first user, one time):**
1. Bootstrap token generated at deploy time, printed to the server console once.
2. User visits `/setup`, enters the token.
3. WebAuthn registration ceremony, resident credential, user verification
   required.
4. Recovery email captured and verified.
5. Ten single-use recovery codes generated, displayed once, stored as Argon2id
   hashes.
6. Bootstrap token invalidated permanently.

**Login (normal):**
1. `/login` triggers a WebAuthn assertion, conditional UI so the browser offers
   the passkey inline.
2. Signature verified against the stored public key; counter checked for clone
   detection.
3. Session issued.

**Login (fallback):** magic link to the verified recovery email. Token is 32
bytes from a CSPRNG, single-use, 10-minute expiry, bound to the requesting IP's
/24 and User-Agent hash. On use, the user is prompted to register a new passkey.

**Recovery (all devices lost):** recovery code plus magic link — both factors
required. Consuming a recovery code sends a notification to every registered
channel and forces new passkey registration.

### Sessions

| Property | Value | Reasoning |
| --- | --- | --- |
| Storage | Opaque token in Postgres, not JWT | Revocable instantly. JWTs cannot be revoked without a revocation list, which is a session store with extra steps. |
| Cookie | `HttpOnly; Secure; SameSite=Lax; Path=/` | Lax, not Strict, so magic links work |
| Idle timeout | 30 days | It is a personal tool used from trusted devices |
| Absolute timeout | 180 days | Bounded exposure |
| Rotation | On privilege change and on recovery-code use | Prevents fixation |
| Concurrent sessions | Allowed, listed in settings with device and last-seen | User can revoke individually |

**Why not JWT.** The standard argument for JWT is stateless verification enabling
horizontal scale without a shared session store. We have one node and a database
every request already touches. In exchange for a benefit we cannot use, JWTs cost
us instant revocation — which for a personal data store is exactly the property we
most want.

### API authentication

| Client | Mechanism |
| --- | --- |
| Web dashboard | Session cookie + CSRF token (double-submit) |
| Telegram bot callbacks | HMAC-signed callback data with a 5-minute validity window |
| Future mobile/CLI | Bearer token, scoped, revocable, listed in settings |
| Inbound email webhook | Provider HMAC signature verification, mandatory |
| Internal service-to-service | Docker network isolation + shared secret header |

### Rate limiting

| Endpoint | Limit | On exceed |
| --- | --- | --- |
| `POST /auth/passkey/assert` | 10 / 5 min / IP | 429 + exponential backoff |
| `POST /auth/magic-link` | 3 / hour / email | 429, no enumeration signal |
| `POST /auth/recovery-code` | 5 / hour / IP | 429 + alert to all channels |
| Authenticated API | 300 / min / session | 429 with `Retry-After` |

All auth responses are constant-time and identical regardless of whether the
account exists, to prevent enumeration.

### Multi-tenant readiness

Every table that holds user data carries `user_id` from the first migration.
Every query is scoped by it. Row-Level Security policies are defined now and
enabled now, even with one user — because an RLS policy that has never been
enabled has never been tested, and enabling it later is when you discover which
queries were relying on unrestricted access.

## Consequences

**Positive.** No password exists to phish, reuse, or leak. Biometric login in a
web app. A database breach yields public keys only. Sessions are instantly
revocable. Multi-tenant migration requires no auth rework.

**Negative.** WebAuthn implementation is more involved than a password form —
mitigated by using `go-webauthn/webauthn`, which is mature and well-audited.
Recovery flows must be built and tested carefully; they are the weakest link in
any passwordless system and get dedicated test coverage. Requires an email
provider for magic links and recovery.

**Neutral.** The bootstrap token flow is unusual but appropriate for a
self-hosted single-user system, and it avoids shipping a default credential —
which is how self-hosted software usually gets compromised.

## Reversal conditions

- Passkey login failure rate above 5% → strengthen the fallback path.
- Multi-tenant launch with non-technical users → add OAuth as an additional
  option, never as a replacement.
- Enterprise use → SAML or OIDC, which is a different product decision entirely.

## Migration path

Auth sits behind an `Authenticator` interface. Adding OAuth or OIDC means adding
an implementation, not changing session handling, authorization, or any
downstream code.
