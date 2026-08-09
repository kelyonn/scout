# ADR-015: Network-gated auth with a bearer token, not passkeys

**Status:** Accepted
**Date:** 2026-08-08
**Supersedes:** [ADR-010](ADR-010-authentication.md) (passkeys + magic link)

## Context

ADR-010 chose passkeys with a magic-link fallback, on the reasoning that there is
then no password to leak and no credential to phish. That reasoning was correct
for the system as specified at the time: a dashboard on a public domain, behind
Cloudflare, reachable by anyone who found the URL.

Two things changed.

**The system is no longer on the public internet.** Under
[ADR-014](ADR-014-zero-cost-hosting.md), Scout is reachable only over Tailscale.
To send a request to the API at all, you must already hold a device-level
Tailscale credential on the user's tailnet. Authentication is no longer the gate
that keeps strangers out — the network is. Auth is now a second factor behind
that, which is a much weaker job.

**The system is explicitly single-user and personal.** There is one account. It
is never shared, never invited to, never recovered by a support process. Account
recovery, credential rotation across devices, and the entire enrollment flow are
solving problems that do not exist here.

WebAuthn is roughly a week of work to implement correctly — registration,
attestation, challenge storage, the counter check, cross-device sync, and the
magic-link fallback path with its own token lifecycle and email dependency. That
week buys phishing resistance against an attacker who, by construction, cannot
reach the endpoint.

## Options considered

### Option A — Keep passkeys as specified in ADR-010

**For:** genuinely the best auth mechanism available; already specified in
detail; no rework.
**Against:** about a week of M0 for a threat it no longer faces. The magic-link
fallback additionally requires outbound email, which under
[ADR-014](ADR-014-zero-cost-hosting.md) means either a verified sending domain
(costs money) or Gmail SMTP (a second credential to hold). Building a fallback
path that is itself a new dependency, for an account that cannot be locked out of
its own machine, is cost with no return.

### Option B — No authentication at all, rely on Tailscale alone

**For:** zero work. The network already restricts access to the user's devices.
**Against:** one compromised or borrowed device on the tailnet gets everything,
including the application history that
[13-security-privacy.md](../13-security-privacy.md) correctly identifies as the
most under-appreciated asset in the system. It also makes any future exposure —
a Funnel enabled for one debugging session, a multi-user experiment — an instant
full breach rather than a mistake with a second line of defense. Defence in depth
is cheap here; skipping it is not a saving worth making.

### Option C — Static bearer token, network-gated

**For:** perhaps 50 lines of Go. Works identically for the browser, the Android
WebView, `curl`, and any script. No enrollment flow, no email dependency, no
recovery path needed. Rotation is one environment variable and a restart.
**Against:** it is a long-lived shared secret. If it leaks it is valid until
rotated, and it carries no user identity, so nothing distinguishes the laptop
from the phone in an audit log.

### Option D — Tailscale identity headers

`tailscale serve` can inject `Tailscale-User-Login` and related headers for
requests it proxies, identifying the tailnet user behind the request.

**For:** real identity, no secret to manage, no code beyond reading a header.
**Against:** it binds the application to one specific ingress configuration, and
the headers are trustworthy *only* if nothing can reach the service except
through that proxy — a misconfiguration that exposes the container port directly
turns a spoofable header into full access. It also does not cover non-proxied
callers.

## Decision

**A static bearer token as the authentication mechanism, with Tailscale network
membership as the outer gate, and Tailscale identity headers used as
defence-in-depth where available.**

Concretely:

- One secret, `SCOUT_AUTH_TOKEN`, generated at setup, held in the SOPS-encrypted
  environment file. Never in the repository, never in an image.
- The browser exchanges it once for a cookie: `HttpOnly`, `Secure`,
  `SameSite=Strict`, long-lived. The user types it once per device, ever.
- The Android app sends it as an `Authorization: Bearer` header, stored in
  Android's `EncryptedSharedPreferences`.
- Comparison is constant-time. A timing-safe compare is two lines and its absence
  is the classic way this mechanism is got wrong.
- Every service binds to the Docker network only. Nothing publishes a host port
  in production, so the ingress cannot be bypassed. This is what makes Option D's
  headers meaningful rather than decorative, and it is verified in the deploy
  health gate.
- Where `Tailscale-User-Login` is present, it is recorded in the audit log. It is
  **never** the sole basis for granting access.

  **Recorded as a fingerprint, not as the address.** That header is an email
  address, and AGENTS.md rule 7 says email addresses are never logged. The two
  requirements are both satisfied by logging the first four bytes of its SHA-256:
  it is stable, so it still distinguishes the laptop from the phone across log
  lines — the entire value the identity has for a system with one user — and no
  address is written anywhere. Discovered while implementing this; the rule wins,
  and the intent of this bullet survives intact.

Rotation: change the variable, restart, re-enter on three devices. Budget five
minutes. That is the entire credential lifecycle.

## Consequences

**Good:**

- Removes roughly a week from the first milestone, which
  [19-roadmap.md](../19-roadmap.md) reallocates to adapters — the thing that
  actually determines whether the product works.
- Removes the outbound-email dependency from the critical path entirely.
- No enrollment, recovery, or cross-device sync flow to build, test, or debug at
  2am.

**Bad, and accepted:**

- A leaked token is valid until noticed and rotated. Mitigated by it never
  leaving three devices, never being logged (AGENTS.md rule 7), and being cheap
  to rotate.
- No per-device identity in audit logs unless the Tailscale header is present.
  For one user this is close to worthless information anyway.
- This is not the design to ship if Scout ever becomes multi-user.

**ADR-010 is not discarded, it is deferred.** Its analysis of passkeys versus
passwords versus OAuth stands, and if Scout ever serves someone other than its
author, that ADR is the specification to implement — at which point the week it
costs is buying protection against a threat that genuinely exists. This ADR
supersedes its conclusion for the single-user, network-gated case only.

## Reversal triggers

- Scout becomes reachable from the public internet for any reason, including a
  Tailscale Funnel left on. **Implement ADR-010 before enabling Funnel**, not
  after.
- A second person gets an account.
- The token leaks, twice. Once is an accident; twice is a design problem.
