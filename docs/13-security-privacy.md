# Security and Privacy — Scout

**Status:** Draft · **Owner:** Security · **Last updated:** 2026-08-08

---

## 1. What we are protecting

Scout holds data that is more sensitive than it first appears:

| Asset | Sensitivity | Impact if exposed |
| --- | --- | --- |
| Resume (name, phone, email, address, education) | **High** | Identity theft, targeted phishing, doxxing |
| Application history | **High** | Reveals which companies rejected the user. Damaging if seen by a current or prospective employer. |
| Interview outcomes and notes | **High** | Same, plus candid notes about companies |
| Rejections | **High** | Personally sensitive |
| Career preferences and salary expectations | Medium | Negotiation leverage lost |
| Notification channel credentials | Medium | Impersonation |
| Session tokens | **Critical** | Full account access |
| LLM API keys | **Critical** | Financial loss |
| Database credentials | **Critical** | Everything |

The application history is the asset most people underestimate. A list of "37
companies applied to, 31 rejected" during an active job search is genuinely
harmful if it leaks, and it is the kind of data no one thinks to protect.

---

## 2. Threat model

Using STRIDE, scoped to what actually applies.

**The scope changed materially in [ADR-014](adr/ADR-014-zero-cost-hosting.md).**
Scout is no longer internet-facing. It is reachable only over Tailscale, so an
attacker must first hold a device-level credential on the user's own tailnet.
Every threat that begins "an anonymous attacker on the internet…" is now gated
behind that, and the likelihood column reflects it.

This is not a reason to relax. It is a reason to be precise about which defences
are still doing work — because the ones that remain are the ones that would
matter if a device is lost, and because the day someone enables Tailscale Funnel
"just to test something" is the day the outer gate disappears.

| Threat | Vector | Likelihood | Mitigation |
| --- | --- | --- | --- |
| **Spoofing** | Stolen bearer token; lost/borrowed device already on the tailnet | Low | Network gate + constant-time token compare + `HttpOnly` `Secure` `SameSite=Strict` cookie ([ADR-015](adr/ADR-015-single-user-auth.md)) |
| **Tampering** | SQL injection, XSS, CSRF | **Medium** | Parameterized queries (`sqlc`), CSP, sanitization, CSRF tokens. Unchanged — the injection vector is *ingested content*, not the user, and 400 sources still feed it. |
| **Repudiation** | Disputed state changes | Low | Append-only event log on every state transition |
| **Information disclosure** | Host compromise, verbose errors, log leakage, **the public GitHub repo** | **Medium** | Encryption at rest, generic errors, PII scrubbing in logs, and section 3 |
| **Denial of service** | Resource exhaustion | Low | Container memory limits. No public surface to flood; the realistic DoS is Scout doing it to itself via a runaway backfill. |
| **Elevation of privilege** | Ingress bypass making identity headers spoofable | Low | No container publishes a host port; asserted by the deploy health gate |

**Two threats went up, not down.** Content injection is unchanged because it
arrives through the collector, which is the one component that still talks to the
whole internet. And information disclosure gained a new vector that the paid
design did not have: **the repository is public** so that CI is free
([18-cost-model.md](18-cost-model.md)). That is a real trade and it gets its own
section.

### Attack surfaces, ranked by real risk

**1. The collector's outbound requests — the largest surface.** Scout fetches
attacker-influenceable URLs (from email alerts, from links in job descriptions).
SSRF here could reach the Docker network, cloud metadata endpoints, or localhost
services. Fully specified in [06](06-ingestion-pipeline.md) section 5.

**2. Untrusted HTML from thousands of sources.** Stored XSS if sanitization
fails. Defense in depth: sanitize on write, sanitize on read, and a strict CSP so
even injected script cannot execute.

**3. Prompt injection via job descriptions.** A posting containing instructions
aimed at our classifier. Covered in [10](10-ai-features.md) section 10. The
structural defense is that model output never directly sets a score.

**4. Inbound email.** Anyone can send mail to the alert address. Ingestion is an
**IMAP poll** rather than a webhook ([ADR-014](adr/ADR-014-zero-cost-hosting.md)),
which removes the public endpoint and the HMAC verification entirely — but not
the content risk. Mail is still attacker-controlled input: strict size limits, no
HTML rendering of inbound mail, sanitize before storage, and any URL extracted
from an email is fetched only through the SSRF guard in surface 1, never
directly.

**5. ~~The public web surface.~~** Gone. Nothing is exposed to the internet
([ADR-014](adr/ADR-014-zero-cost-hosting.md)). The dashboard is reachable only
over Tailscale. Retained as a numbered entry rather than deleted, because this is
the surface that comes back the instant anyone enables Funnel — and
[ADR-015](adr/ADR-015-single-user-auth.md) requires implementing real
authentication *before* that happens, not after.

**6. Telegram inbound.** Delivered by **long-poll** (`getUpdates`) rather than a
webhook, so there is no public callback endpoint and no secret-token comparison
to get wrong. The remaining check is authorization, not authenticity: every
inbound callback must map to the one verified `chat_id`, and anything else is
dropped. Without that, anyone who finds the bot could send it callbacks.

**7. Push device registration.** `POST /push/register-device` accepts a device
token from an authenticated client. An attacker with a stolen session could
register their own device and receive the user's notifications indefinitely. Every
registration is therefore visible in settings with its platform, label, and
first-seen time, and adding a device triggers a notification on all **existing**
channels — the same pattern as adding a new passkey. A device the user does not
recognize is the signal, and it is surfaced rather than buried.

---

## 3. What must never enter the public repository

**The repository is public.** That is what makes GitHub Actions free and
therefore what makes CI exist at all ([18-cost-model.md](18-cost-model.md)). It
is a deliberate trade and it creates the one information-disclosure vector the
paid design did not have.

| Never in the repo | Where it lives instead |
| --- | --- |
| Resume, name, phone, address | Postgres only, uploaded through the app |
| Application history, interview notes, rejections | Postgres only |
| `SCOUT_AUTH_TOKEN`, API keys, Telegram bot token, FCM service account | SOPS-encrypted (`age`), decrypted at container start |
| Tailscale auth keys | Nowhere — nodes are approved interactively |
| Android signing keystore | SOPS + offline media |
| Personal seed lists ("companies I care about") | A SOPS-encrypted data file, or the database |
| Anything in `.env` | `.env.example` only, with placeholder values |

Enforced three ways, because a rule this important should not depend on
remembering it: `.gitignore` for the obvious paths, `gitleaks` pre-commit **and**
in CI, and the structural fact that all personal data lives in Postgres rather
than in files, so there is nothing to accidentally `git add`.

**The generic seed lists are fine to publish.** Company board tokens, GCC
entities, and the taxonomy are public information and arguably the most useful
part of the repository to anyone else. It is only the *personal* layer that stays
out.

If the repository ever needs to go private, the cost is 2,000 CI minutes/month
and the workflow must be trimmed to fit. That is the fallback, not a disaster.

---

## 4. Authentication and authorization

Fully specified in [ADR-015](adr/ADR-015-single-user-auth.md), which supersedes
[ADR-010](adr/ADR-010-authentication.md) for the single-user, network-gated case.
Summary:

- **Tailscale network membership is the outer gate.** Reaching the API at all
  requires a device credential on the user's tailnet.
- A single bearer token, `SCOUT_AUTH_TOKEN`, compared in constant time.
- Browser exchanges it once for an `HttpOnly; Secure; SameSite=Strict` cookie.
  The Android app sends `Authorization: Bearer`, stored in
  `EncryptedSharedPreferences`.
- CSRF via double-submit token on every state-changing request.
- `Tailscale-User-Login` recorded in the audit log where present, **never** the
  sole basis for access.
- No container publishes a host port, so the ingress cannot be bypassed. Asserted
  by the deploy health gate.

**Row-Level Security is not enabled.** ADR-010's design had it on from the first
migration; with one user and one token it protects nothing and costs query
complexity on every table. `user_id` columns remain in the schema — they are free
and already written — so RLS is a migration away if Scout ever gains a second
user. See [03-data-model.md](03-data-model.md) section 12.

**Passkeys are deferred, not rejected.** If Scout is ever exposed publicly or
gains a second user, ADR-010 is the specification to implement, and it should be
implemented *before* the exposure rather than after.

---

## 5. Data protection

### At rest

| Layer | Method |
| --- | --- |
| Disk | Oracle block-volume encryption (on by default), plus LUKS on the data volume |
| Database | Postgres on the encrypted volume; column-level `pgcrypto` for channel credentials |
| Resumes | **On the host filesystem only**, on the encrypted volume. No object store exists ([ADR-014](adr/ADR-014-zero-cost-hosting.md)). |
| Backups | `age`-encrypted on the host **before** they leave it ([ADR-017](adr/ADR-017-tiered-backup-without-object-storage.md)) |
| Secrets | SOPS with `age`, encrypted in the repo, decrypted at container start |

**Backups are encrypted before egress, not by the destination.** One of the two
backup destinations is Google Drive. Encrypting on the host with `age` means
Google holds ciphertext and never sees the application history — which section 1
identifies as the most under-appreciated asset in the system. Relying on a
provider's at-rest encryption would mean relying on the provider.

**The backup key is stored separately from the backups**, offline. A backup and
its key in the same place is not an encrypted backup.

**The resume never leaves the host at all**, and not only for storage reasons:
[ADR-016](adr/ADR-016-free-tier-llm-cascade.md) forbids sending it to any hosted
model, because free LLM tiers commonly reserve the right to train on submitted
data. Resume matching is embedding cosine plus keyword overlap and was already
entirely local. This is AGENTS.md rule 9 and it is enforced at the LLM client
boundary, not by convention.

### In transit

TLS 1.3 minimum everywhere. HSTS with `max-age=31536000; includeSubDomains;
preload`. Internal service traffic stays on the Docker bridge network and is
never exposed to a host port. Outbound requests verify certificates; there is no
`InsecureSkipVerify` anywhere in the codebase, enforced by a lint rule.

### Secrets

```
Never:  in source, in Dockerfiles, in environment variables logged by a crash
        handler, in CI logs, in error messages, in URLs
Always: SOPS-encrypted at rest, injected at container start, rotatable
```

| Secret | Rotation |
| --- | --- |
| `SCOUT_AUTH_TOKEN` | 180 days, or immediately on device loss. Five minutes: change the variable, restart, re-enter on three devices ([ADR-015](adr/ADR-015-single-user-auth.md)). |
| Database password | 90 days |
| LLM API keys (free-tier) | 90 days, or immediately on suspicion |
| Gmail app password (IMAP ingestion) | 180 days |
| VAPID keypair | Never (rotation invalidates every push subscription) |
| **FCM service account key** | 180 days |
| Telegram bot token | 180 days, or immediately on suspicion |
| **Android signing keystore** | Never (rotation breaks upgrade-in-place for installed apps) |
| **`age` backup key** | Never (would orphan existing backups) |
| Tailscale node keys | Managed by Tailscale; revoke a lost device from the admin console |

**Three of these are never-rotate, and each for a different reason.** The VAPID
keypair because rotation invalidates every push subscription, the Android keystore
because Android refuses to upgrade an app signed by a different key, and the `age`
backup key because it would orphan history. Never-rotate secrets need
correspondingly stronger storage: all three live in SOPS with `age` and are
additionally backed up offline.

**Losing the `age` key is now the worst of the three**, which is a change from the
paid design. Under [ADR-017](adr/ADR-017-tiered-backup-without-object-storage.md)
every backup — hourly, nightly, both destinations — is encrypted with it. Lose it
and every backup becomes noise simultaneously; there is no provider-held
plaintext copy to fall back on, because that was the point. It goes on offline
media at the same time it is generated, in the same session, as an exit criterion
in [19-roadmap.md](19-roadmap.md) — alongside the Android keystore, for the same
reason: both are one-shot secrets with no recovery path, and both are generated
deliberately rather than incidentally during a first build.

`gitleaks` runs pre-commit and in CI. A committed secret fails the build; the
runbook for a leaked secret is rotate-first, investigate-second.

---

## 6. Application security

### Input validation

Every external input is validated at the boundary against a schema before it
reaches business logic — API requests against the OpenAPI schema, adapter output
against the canonical job schema, LLM output against its declared output schema,
webhook payloads after signature verification.

**SQL:** `sqlc` generates type-safe queries from SQL files. There is no string
concatenation of SQL anywhere, and no ORM constructing dynamic queries. A lint
rule blocks `fmt.Sprintf` in any file under `db/`.

**Path traversal:** no user input ever reaches a filesystem path. Object storage
keys are constructed from validated UUIDs only.

### Output encoding

**Content Security Policy:**

```
default-src 'self';
script-src 'self' 'nonce-{random}';
style-src 'self' 'nonce-{random}';
img-src 'self' data: https:;
font-src 'self';
connect-src 'self';
frame-ancestors 'none';
base-uri 'self';
form-action 'self';
object-src 'none';
upgrade-insecure-requests;
```

Nonce-based, not `unsafe-inline`. Company logos come from arbitrary HTTPS origins
(`img-src https:`) which is a deliberate, bounded relaxation — images cannot
execute script, and proxying every logo would cost bandwidth and add latency for
no security gain.

**HTML sanitization** happens twice, per [07](07-normalization-taxonomy.md)
section 11. Job descriptions are rendered through a component that cannot execute
script, never via `dangerouslySetInnerHTML` on unsanitized content.

### Other headers

```
Strict-Transport-Security: max-age=31536000; includeSubDomains; preload
X-Content-Type-Options: nosniff
Referrer-Policy: strict-origin-when-cross-origin
Permissions-Policy: geolocation=(), camera=(), microphone=(), payment=()
Cross-Origin-Opener-Policy: same-origin
Cross-Origin-Resource-Policy: same-origin
```

### Dependencies

| Tool | Scope | Cadence |
| --- | --- | --- |
| `govulncheck` | Go | Every CI run |
| `pip-audit` | Python | Every CI run |
| `pnpm audit` | Node | Every CI run |
| `trivy` | Container images | Every image build |
| Dependabot | All ecosystems | Weekly, grouped |
| `gitleaks` | Secrets | Pre-commit + CI |

**Policy:** critical vulnerabilities block the build. High severity has 7 days.
Medium has 30. Dependency additions require justification in the PR description —
the supply chain is the most likely path to compromise for a project like this,
and the cheapest defense is having fewer dependencies.

---

## 7. Infrastructure security

### Network

```
User's devices ──▶ Tailscale (WireGuard) ──▶ host tailnet interface
                                                  │
                                            Caddy (tailnet-bound)
                                                  │
                                          Docker bridge network
                                          (no host port bindings)
                                                  │
                        api · web · collector · brain · notifier
                        postgres · redis · ollama · observability
```

**No inbound port is open to the internet. None.** Not 443, not 80, not SSH.
Tailscale establishes outbound connections and the host firewall default-denies
everything inbound on the public interface. SSH is reachable only over the
tailnet, key-only, no password auth, no root login.

This is a stronger position than the Cloudflare-fronted design it replaces, and
it is worth being precise about why: a WAF filters traffic that reached you,
whereas there is now no route by which anonymous traffic arrives at all. The
attacker must first be a device on the user's tailnet.

Postgres, Redis, and Ollama have no host port binding — reachable only from
inside the Docker network. The most common self-hosted compromise is an exposed
database with a weak password, and the fix is not a strong password, it is no
exposed port. The deploy health gate asserts that no container publishes one, so
a stray `ports:` entry fails the deploy.

**What this gives up:** DDoS absorption and a managed WAF, both of which are
protections against public traffic that no longer exists. And there is no CDN, so
the dashboard is served directly by the host — irrelevant for one user on a
private network.

### Container hardening

```yaml
services:
  api:
    read_only: true
    tmpfs: [/tmp]
    cap_drop: [ALL]
    security_opt: ["no-new-privileges:true"]
    user: "10001:10001"
    mem_limit: 384m
    pids_limit: 256
```

Read-only root filesystems, all capabilities dropped, non-root users, memory and
PID limits on every service. Images are distroless or Alpine-based, multi-stage
built, with no shell in the runtime layer.

### Host

`unattended-upgrades` for security patches, automatic reboot at 04:00 IST when a
kernel update requires it. `ufw` default-deny inbound. Root login disabled.
Auditd logging privileged operations.

---

## 8. Privacy

### Data inventory

| Data | Source | Purpose | Retention |
| --- | --- | --- | --- |
| Name, email, phone | User | Identity, notifications | Until deletion |
| Resume file and text | User | Matching, generation | Until deletion |
| Skills, preferences | User | Ranking | Until deletion |
| Application history | User | Tracking, learning | Until deletion |
| Interview notes | User | Preparation | Until deletion |
| Interaction events | Derived | Learning loop | Until deletion |
| Job postings | Public web | Core function | Indefinite (not personal data) |
| Raw HTML snapshots | Public web | Reprocessing | 30 days |
| Recruiter names in JDs | Public web | Contact extraction | 90 days |
| Logs | System | Debugging | 14 days |
| Traces | System | Debugging | 7 days |

### Principles

**Data minimization.** We collect what ranking and notification require and
nothing else. No analytics SDK, no advertising identifiers, no third-party
trackers, no session recording.

**Third-party exposure, stated plainly.** Resume text is sent to LLM providers
for cover letter generation and resume feedback. This is unavoidable for those
features and is disclosed in the UI at the point of use, not buried in a policy.
Mitigations: providers with a no-training-on-API-data commitment are preferred;
resume text is never sent for pipeline tasks (classification, dedup, scoring) —
only for user-initiated generation; and the feature can be disabled entirely,
leaving the rest of Scout fully functional.

**Log hygiene.** A scrubbing middleware removes resume content, email addresses,
phone numbers, session tokens, API keys, and full job descriptions before
anything is written. Logs reference `user_id` and `job_id`, never names or
content. There is a test asserting that a log line containing a seeded PII
pattern never reaches the sink.

**Right to deletion.** `DELETE /api/v1/me` cascades across every user-scoped
table, deletes resume files from the host volume, and invalidates the auth
cookie. Backups age out within 30 days; the deletion request is recorded so that
a restore from an older backup re-applies it.

**Data export.** `GET /api/v1/me/export` produces a complete JSON archive of
everything Scout holds about the user, including derived scores and feedback
events.

### Third parties

| Provider | Data sent | Necessity |
| --- | --- | --- |
| LLM providers (free tiers) | **Job descriptions only** — public documents | Classification, dedup adjudication, explanations |
| Telegram | Notification content, digest content | Primary chat channel |
| **Google (FCM)** | Notification title and body, device token | Android push ([ADR-012](adr/ADR-012-native-app-shell.md)) |
| **Google (Drive)** | Encrypted backup blobs — ciphertext only | Off-site backup ([ADR-017](adr/ADR-017-tiered-backup-without-object-storage.md)) |
| **Google (Gmail IMAP)** | Nothing sent; job-alert mail is read | Board coverage via email ingestion |
| Tailscale | Connection metadata; **no traffic content** (end-to-end WireGuard) | Network access |
| Sentry | Stack traces, scrubbed | Error tracking |
| Oracle | Everything on disk (encrypted volume) | Hosting |
| GitHub | Source, config, SOPS-encrypted secrets — **publicly** | Code hosting, free CI |

No data is sold, shared for advertising, or used to train models by us.

**Two rows deserve attention under the ₹0 design.**

**Free LLM tiers commonly reserve the right to train on submitted data.** That is
acceptable for job descriptions, which are public documents that the company
published deliberately. It is not acceptable for anything of the user's, so
AGENTS.md rule 9 forbids the resume, application history, interview notes, and
rejection records from reaching any hosted model — enforced at the LLM client
boundary, which refuses to build such a request
([ADR-016](adr/ADR-016-free-tier-llm-cascade.md)). The paid design sent resume
text to a provider for cover-letter generation; those features are cut, so the
constraint costs nothing.

**Google now appears three times**, which is more concentration than the paid
design had, and it is a genuine downside of using storage the user already owns.
It is bounded by what each one actually sees: Drive holds only `age`-encrypted
ciphertext, Gmail is read-only ingestion of mail already sent to that account,
and FCM sees only what a public job posting already said. None of them holds
plaintext application history.

**The push provider sees notification content.** FCM payloads are
transport-encrypted but not end-to-end encrypted, so Google can read the title and
body. Notification bodies therefore contain only company, role,
location, compensation, and score — all of which came from a public job posting.
**No resume content, no profile data, and no scoring rationale beyond the one-line
explanation ever enters a push payload.** Anything sensitive stays behind the
`job_group_id` in the `data` field, which requires an authenticated API call to
resolve.

**No phone number is shared with anyone.** WhatsApp was considered and dropped
([ADR-013](adr/ADR-013-whatsapp-channel.md)); no current channel requires a phone
number, so none is collected. This is the cleanest kind of privacy property — the
data does not exist rather than being protected.

---

## 9. Incident response

### Severity

| Level | Definition | Response |
| --- | --- | --- |
| **SEV1** | Data breach, credential compromise, total outage | Immediate |
| **SEV2** | Partial outage, notification failure, source-wide breakage | 4 hours |
| **SEV3** | Degraded quality, single source broken | 24 hours |
| **SEV4** | Cosmetic, minor | Next cycle |

### SEV1 procedure

```
1. CONTAIN     Revoke all sessions. Rotate affected credentials.
               If necessary, take the service offline — availability is
               less valuable than containment.
2. ASSESS      What data, over what window, by what path? Check access
               logs, auth logs, and the audit trail.
3. PRESERVE    Snapshot logs and disk before remediation destroys evidence.
4. ERADICATE   Patch the vulnerability. Verify the patch.
5. RECOVER     Restore service. Re-issue credentials.
6. REVIEW      Blameless postmortem within 72 hours, written up in
               docs/postmortems/, with concrete action items.
```

Detailed steps in [`runbooks/security-incident.md`](runbooks/security-incident.md).

### Detection

| Signal | Alert |
| --- | --- |
| Failed auth spike | >20 in 5 min from one IP |
| Recovery code used | Always — to every channel |
| Auth token exchanged on a new device | Always |
| Session from a new IP prefix | Always |
| Admin endpoint accessed | Always |
| Outbound request to a private IP | Always — indicates SSRF |
| CSP violation report | Aggregated, investigated weekly |
| Unusual LLM spend | >3× the daily average |
| Database connections from an unexpected source | Always |

---

## 10. Security testing

| Layer | Method | Cadence |
| --- | --- | --- |
| SAST | `gosec`, `bandit`, `eslint-plugin-security` | Every CI run |
| Dependency | `govulncheck`, `pip-audit`, `pnpm audit`, `trivy` | Every CI run |
| Secrets | `gitleaks` | Pre-commit + CI |
| DAST | OWASP ZAP baseline against staging | Weekly |
| Auth tests | Session fixation, CSRF, privilege escalation, enumeration | Every CI run |
| SSRF tests | Private IP, redirect chain, DNS rebinding, scheme confusion | Every CI run |
| XSS tests | Payload corpus through the sanitizer | Every CI run |
| RLS tests | Cross-user access attempts on every protected table | Every CI run |
| Prompt injection | Adversarial corpus through the classifier | Weekly |
| Restore drill | Full backup restore to a throwaway host | Monthly |

**The RLS tests matter disproportionately.** They are the check that the
multi-tenant safety property actually holds, and they must exist from the first
migration — with one user, the only way to know RLS works is to test it.

---

## 11. Deliberate accepted risks

Stated openly, because unstated accepted risk is the same as an unknown
vulnerability.

| Risk | Why accepted | Revisit when |
| --- | --- | --- |
| Single node, no HA | Multi-node costs more than the downtime it prevents for one user, and there is no budget for a second node | Ever becomes multi-user |
| Self-managed Postgres | Managed costs money; backups are tested monthly | Ever becomes multi-user |
| **A single long-lived bearer token instead of passkeys** | The network is the primary gate; WebAuthn defends against an attacker who cannot reach the endpoint ([ADR-015](adr/ADR-015-single-user-auth.md)) | **Before** any public exposure, including Tailscale Funnel |
| **No Row-Level Security** | One user, one token. RLS protects nothing here and costs query complexity everywhere | A second user exists |
| **The repository is public** | It is what makes CI free; personal data lives in Postgres, never in files (section 3) | Personal data ever needs to live in a file |
| **No point-in-time recovery** | 95% of the data is re-derivable by re-polling; the irreplaceable few kilobytes are backed up hourly ([ADR-017](adr/ADR-017-tiered-backup-without-object-storage.md)) | Irreplaceable data grows past tens of megabytes |
| **Free-tier host with no SLA** | Bounded to a rehearsed one-hour migration rather than left open-ended ([ADR-014](adr/ADR-014-zero-cost-hosting.md)) | Reclaimed more than once despite PAYG |
| **Job descriptions sent to free LLM tiers that may train on them** | They are public documents. The resume, application history, and notes never are — AGENTS.md rule 9, enforced at the client boundary | A free tier's terms extend to something we do send |
| No SOC 2 / formal audit | Not applicable to a personal tool | Commercial launch |
| Company logos loaded from arbitrary origins | Images cannot execute; proxying costs bandwidth for no security gain | CSP reporting shows abuse |
| No WAF | There is no public traffic to filter | Funnel is ever enabled |

**The middle block is new and it is the cost of ₹0.** Each row trades a defence
for a constraint that no longer applies to a private, single-user system — and
each one names the specific condition that makes it wrong again. The pattern to
notice: **almost every one of them reverts on "a second user exists" or "it is
exposed publicly."** Those two events are the security boundary of this design,
and crossing either means re-reading this table before, not after.
