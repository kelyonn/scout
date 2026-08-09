# Legal and Compliance — Scout

**Status:** Draft · **Owner:** Engineering · **Last updated:** 2026-08-06

> This document reflects engineering policy, not legal advice. It describes the
> constraints we build under and why. If Scout is ever commercialized, these
> positions need review by an actual lawyer.

Policy rationale is in [ADR-007](adr/ADR-007-no-tos-violating-scraping.md).

---

## 1. The governing rules

Six rules, enforced in code rather than by convention. Each maps to a test.

`SCOUT-LEGAL-001` Scout **MUST NOT** send an automated request to any host whose
robots.txt disallows the path for our user-agent.

`SCOUT-LEGAL-002` Scout **MUST NOT** send automated requests to any source whose
terms of service prohibit automated access. Such sources are marked `prohibited`
and the collector refuses them before the request is constructed.

`SCOUT-LEGAL-003` Scout **MUST** identify itself honestly in every request, with
a contact URL and email.

`SCOUT-LEGAL-004` Scout **MUST NOT** implement CAPTCHA solving, browser
fingerprint spoofing, residential proxy rotation, header randomization intended
to evade detection, or any other bot-detection circumvention. **No exceptions.**

`SCOUT-LEGAL-005` Scout **MUST** honor rate limits, `Crawl-delay`, and
`Retry-After`, and **MUST** back off on 429 and 5xx.

`SCOUT-LEGAL-006` Scout **MAY** parse email delivered to an address the user
controls, regardless of sender.

### Enforcement

Rule 002 is enforced at the politeness gate, which checks `source.legal_posture`
before every fetch:

```go
func (g *PolitenessGate) Allow(ctx context.Context, src Source) (Decision, error) {
    if src.LegalPosture == PostureProhibited || src.LegalPosture == PostureEmailOnly {
        metrics.ComplianceRefusal.WithLabelValues(string(src.Kind)).Inc()
        return Refuse("legal_posture=" + string(src.LegalPosture)), nil
    }
    // ... robots, rate limit, crawl delay, circuit breaker
}
```

There is a unit test asserting that a source marked `prohibited` produces zero
outbound requests through every code path, and an integration test that seeds a
`prohibited` source and asserts the HTTP client is never invoked.

---

## 2. robots.txt

Implemented per RFC 9309.

| Behavior | Policy |
| --- | --- |
| Fetch | Once per host, cached 24 hours |
| User-agent matching | Our `Scout` group first, then `*` |
| `Disallow` | Honored strictly, longest-match-wins per the RFC |
| `Allow` | Honored, overrides a less specific `Disallow` |
| `Crawl-delay` | **Honored**, though non-standard |
| `Sitemap` | Used for discovery |
| robots.txt returns 4xx | Treated as unrestricted, per the RFC |
| robots.txt returns 5xx | **Treated as disallowed** until it resolves |
| robots.txt unreachable | **Treated as disallowed** |
| robots.txt exceeds 500KB | Truncated at 500KB per the RFC |

**We fail closed on 5xx and network errors,** which is stricter than the RFC
requires. A site whose robots.txt is temporarily broken should not be crawled on
the assumption that it would have permitted us.

The cache is in Redis with a Postgres fallback, so a Redis flush does not cause a
stampede of robots.txt fetches against every host we monitor.

---

## 3. Per-source posture matrix

The authoritative list lives in `source.legal_posture` in the database, seeded
from `adapters/legal-postures.yaml`, which is reviewed in pull requests.

### Permitted — direct automated access

| Source | Basis |
| --- | --- |
| Greenhouse job boards | Public JSON API intended for programmatic board embedding |
| Lever postings API | Public JSON API, documented, intended for consumption |
| Ashby job board API | Public GraphQL backing public board pages |
| Workable, SmartRecruiters, Recruitee, Teamtailor, Rippling, BambooHR | Public board APIs |
| Workday public career sites | Public endpoints backing public pages; robots-permitted paths |
| Company career pages | robots.txt-permitted paths only, checked per host |
| RSS/Atom feeds | Feeds exist to be consumed programmatically |
| Sitemaps | Same |
| JSON-LD `JobPosting` | Published specifically for machine consumption |
| Hacker News | Official Firebase API, explicitly public |
| GitHub | Official REST/GraphQL API, authenticated, within rate limits |
| Reddit | Official API, OAuth, within free-tier limits |
| RemoteOK, WeWorkRemotely | Public JSON APIs the operators offer |
| Wellfound | Permitted with attribution |
| Y Combinator | Public pages, robots-permitted |
| VC portfolio pages | Public marketing pages, robots-permitted |
| Devfolio, Devpost, MLH | Public APIs |

### Email-only — never fetched directly

| Source | Reason |
| --- | --- |
| **LinkedIn** | ToS prohibits automated access; actively enforced |
| **Indeed** | ToS prohibits; publisher agreement required for programmatic use |
| **Glassdoor** | ToS prohibits |
| **Handshake** | ToS prohibits; **account tied to university identity** |
| **Naukri** | ToS restrictive |
| **Internshala** | ToS restrictive |

These sources are ingested only through job alert emails the user has subscribed
to, delivered to a Scout-controlled address. The user is the recipient; Scout is
their mail client.

### API-only

| Source | Reason |
| --- | --- |
| Crunchbase | Paid API; free tier too limited to be useful |
| X / Twitter | API v2 costs $200/month; deferred — see [22](22-open-questions.md) |
| Discord | Bot must be invited to a server; only servers the user has joined |
| Slack | Only with workspace admin permission |

### Prohibited

Any source where robots.txt disallows the path, ToS prohibits automated access,
authentication is required and not user-delegated, or a CAPTCHA or bot challenge
is present. The collector refuses these before constructing a request.

---

## 3.1 Outbound channels: the same rule applies

The posture matrix above governs how Scout *reads*. The same discipline governs how
Scout *writes*, because a messaging platform's terms are no less binding than a job
board's.

| Channel | Mechanism | Posture |
| --- | --- | --- |
| Telegram | Official Bot API | Permitted — bots are the sanctioned integration path |
| FCM | Official Firebase SDK | Permitted |
| Web Push | VAPID, an open standard | Permitted |
| Email | Resend, SPF/DKIM/DMARC aligned | Permitted |
| Discord | Official webhook | Permitted |
| **WhatsApp** | — | **Not used.** If ever revisited: official Cloud API or nothing. |

### The WhatsApp bright line

WhatsApp is not a Scout channel ([ADR-013](adr/ADR-013-whatsapp-channel.md)), but
the prohibition below stands regardless, because the tempting shortcut is exactly
what someone reaches for when they want WhatsApp notifications in a hurry.

**`whatsapp-web.js`, `baileys`, `venom-bot`, and every other library that automates
WhatsApp Web or reimplements its protocol are prohibited in this codebase.**

WhatsApp's Terms of Service prohibit unauthorized or automated use and "modified
versions of WhatsApp." Meta enforces this by banning the **phone number**, which
means the ban lands on a real human's personal WhatsApp account along with every
conversation in it.

This is the same analysis as LinkedIn in section 4, applied to an outbound channel:
the mechanism might work, and it is still not worth the account. During placement
season, losing the number that recruiters use to reach you would be considerably
worse than not having WhatsApp notifications at all.

**Enforced, not merely stated.** A CI check fails the build if any of these packages
appears in a dependency manifest, in the same way the compliance gate blocks a
prohibited fetch. A documented rule that only lives in prose gets violated by the
next contributor in a hurry.

### Consent for messaging

Not currently a live question — Telegram requires the user to initiate contact with
the bot, and push notifications require an OS-level permission grant, so consent is
structural in both cases rather than something we have to model.

It is worth stating the mechanism anyway, because it must be correct if Scout ever
serves anyone else: opt-in timestamp and method stored on the channel row, an
opt-out path on every channel, and no channel used before `verified_at` is set.

---

## 4. On the hiQ v. LinkedIn question

This case is frequently cited as establishing that public web scraping is legal.
It does not, and building on that misreading would be a mistake.

**What the Ninth Circuit actually held:** scraping publicly accessible data
likely does not violate the Computer Fraud and Abuse Act, because the CFAA's
"without authorization" language concerns circumventing access controls, not
accessing public pages.

**What it did not hold:** that scraping is permitted under contract law. That
question was separate, and on remand LinkedIn prevailed on its breach-of-contract
claim. hiQ agreed to a permanent injunction.

**The practical position for Scout:**

1. Contract law applies independently of the CFAA. Accepting terms of service
   creates a contract, and scraping in violation of it is a breach regardless of
   the CFAA analysis.
2. Even where scraping is lawful, it is not *permitted* — a platform may ban the
   account, block the IP, and send a cease-and-desist, all without any court
   involvement.
3. The user's Handshake account is tied to their university. Losing it during
   placement season is a materially worse outcome than any coverage gain.
4. Jurisdiction matters and Indian law on this is less settled than US law, which
   argues for more caution rather than less.

**Therefore:** Scout does not scrape sources that prohibit it. Not because we are
certain it is illegal, but because the downside is severe, the upside is
recoverable through legitimate means, and a system built on hostile scraping
breaks silently at the worst possible moment.

---

## 5. Email alert ingestion — the legal basis

Why parsing LinkedIn's alert emails is different from scraping LinkedIn:

| | Scraping | Email alerts |
| --- | --- | --- |
| Who initiates | Scout, unrequested | LinkedIn, to a subscriber |
| Access method | Automated request to their servers | Receiving mail they sent |
| ToS position | Explicitly prohibited | Explicitly the intended feature |
| Load on their infra | Requests we generate | Mail they chose to send |
| Analogy | Walking into a shop after hours | Reading a catalog mailed to you |

The user subscribes to job alerts. LinkedIn sends them. Scout reads the user's
mail on the user's behalf — which is what every email client does. There is no
automated access to LinkedIn's systems at any point.

**Boundaries we hold:**

- We do not follow links back into LinkedIn to fetch full descriptions. We
  resolve the tracking redirect to identify the canonical posting, then fetch the
  full version from the **company's own ATS** if we can identify it.
- We do not use the emails to reconstruct a LinkedIn database.
- We do not share or resell the extracted data.
- The user can stop it by unsubscribing, which is entirely within their control.

---

## 6. Data protection

### Indian DPDP Act 2023

The user is in India, so the Digital Personal Data Protection Act applies to
personal data Scout processes.

| Obligation | How Scout satisfies it |
| --- | --- |
| Lawful purpose | The user is the data principal and the operator; processing is at their direction |
| Notice | Data inventory and third-party disclosure in [13](13-security-privacy.md) |
| Consent | Explicit opt-in for each notification channel and each AI feature that sends resume data |
| Data minimization | Only what ranking and notification require |
| Accuracy | User can correct any profile data directly |
| Erasure | `DELETE /api/v1/me` with full cascade |
| Security safeguards | [13](13-security-privacy.md) |
| Breach notification | Incident response procedure, section 8 of [13](13-security-privacy.md) |

Single-user self-hosted operation puts Scout largely outside the Act's
significant-data-fiduciary obligations, but the controls are built anyway because
multi-tenant operation would trigger them and retrofitting is expensive.

### GDPR

Not currently in scope — no EU data subjects. Relevant if Scout opens to
European users, at which point: lawful basis (consent), DPA agreements with every
processor, a records-of-processing register, and data-subject request handling.
The export and deletion endpoints already satisfy the mechanical requirements.

### Recruiter contact data

Job descriptions sometimes contain recruiter names and emails. This is personal
data about a third party who has not consented to our processing.

**Policy:** extracted only when present in a public posting, retained 90 days,
used only to help the user follow up on their own application, never aggregated
into a contact database, never exported, never shared. Deleted when the
associated application closes.

---

## 7. Content and copyright

**Job descriptions are copyrighted** by the posting company.

| Use | Position |
| --- | --- |
| Store for the user's personal reference | Fair use / fair dealing — personal, non-commercial, transformative |
| Display to the single user who requested discovery | Same |
| Generate embeddings and derived scores | Transformative; no substantial reproduction |
| AI-generated summary | Transformative |
| Republish publicly | **Not done.** Would be infringement. |
| Sell or redistribute | **Not done.** |

Scout always links to the original posting and always attributes the source. The
apply action goes to the company's own application URL, never to a Scout-hosted
form. We are a discovery tool pointing at the original, not a mirror of it.

**If Scout ever becomes multi-tenant,** this analysis changes materially — mass
display of copyrighted job descriptions to many users is a different posture and
would need either licensed feeds or a snippet-only display model. Flagged in
[20-risks](20-risks.md).

---

## 8. Automated application submission

**Not built.** The brief lists auto-fill as future automation; this section
records why it stays future.

| Concern | Detail |
| --- | --- |
| ATS terms | Greenhouse, Lever, Workday, and most others prohibit automated submission |
| Detection | Behavioral analysis and honeypot fields are standard; detection is likely |
| Consequence | A ban propagates across **every company using that ATS**, which for Greenhouse alone is thousands |
| Accuracy | An automated application with a wrong answer is worse than none |
| Attribution | The user is accountable for what is submitted under their name |

The asymmetry is decisive: the upside is saving a few minutes per application;
the downside is being blacklisted from a large fraction of the market during the
one year that matters.

**What we build instead:** browser autofill compatibility (correct `autocomplete`
attributes and a structured profile the browser's own autofill can use), a
one-click copy of tailored answers, pre-filled application URLs where the ATS
supports query parameters as a documented feature, and a generated cover letter
the user reviews and pastes. All the time savings, none of the automation risk.

---

## 9. Compliance monitoring

| Metric | Alert |
| --- | --- |
| `scout_compliance_refusal_total` | Any spike — indicates a misconfigured source |
| `scout_robots_disallowed_total` | Rising — a site changed its policy |
| Requests to `prohibited` sources | **Any. This is a bug, treated as SEV2.** |
| 429 responses | >10/hour from one host — our rate limit is too aggressive |
| 403 responses | >5 consecutive from one host — likely blocked; quarantine |
| Abuse complaints to the contact address | Any — respond within 24 hours |

**On receiving a complaint or block request:** stop immediately, mark the source
`prohibited`, reply acknowledging within 24 hours, and record it in the source's
notes. The contact address in our User-Agent exists so that a site operator's
first move is an email rather than a legal letter, and that only works if we
actually respond.

---

## 10. Compliance review cadence

| Activity | Frequency |
| --- | --- |
| robots.txt re-check per active source | 24 hours (automatic) |
| ToS review for the top 20 sources | Quarterly |
| Outbound channel ToS review (Telegram, push providers) | Quarterly |
| Full posture matrix review | Every 6 months |
| Review of any newly added source class | Before activation, in the PR |
| Legal-posture test suite | Every CI run |

Adding a source with `legal_posture = 'permitted'` requires the pull request to
state the basis — which robots.txt path was checked and what the terms say. This
is enforced by a PR template checklist, so the reasoning is recorded at the time
the decision is made rather than reconstructed later.
