# ADR-007: No scraping of sources that prohibit it

**Status:** Accepted
**Date:** 2026-08-06
**Note:** This decision departs from the original brief, which listed LinkedIn
Jobs, Indeed, Glassdoor, and Handshake as sources to monitor.

## Context

The brief asks Scout to monitor LinkedIn Jobs, Indeed, Glassdoor, and Handshake.
It also says, in the Discovery Strategy section, to respect "robots.txt, terms of
service, and applicable policies."

These two instructions are in direct conflict. All four platforms explicitly
prohibit automated access in their terms of service, and all four disallow job
search paths in robots.txt. This ADR resolves the conflict and proposes a route
that recovers most of the coverage legitimately.

### What the terms actually say

| Platform | Position | Enforcement history |
| --- | --- | --- |
| LinkedIn | Prohibits scraping, bots, and automated access | Actively litigates. *hiQ v. LinkedIn* is often cited as permitting scraping — it held only that scraping *public* data likely does not violate the CFAA. It did **not** hold that it is permitted under contract law, and LinkedIn subsequently won on breach-of-contract grounds. Accounts are banned aggressively. |
| Indeed | Prohibits automated access; requires a publisher agreement for programmatic use | Aggressive bot detection; IP-level blocking |
| Glassdoor | Prohibits scraping | Blocks aggressively; content largely behind a contribution wall |
| Handshake | Prohibits scraping; **account is tied to university identity** | Violation can mean loss of university career-services access |

Handshake deserves separate emphasis. That account is issued by the user's
university and connects to their student record. Losing it means losing access to
campus placement infrastructure. The downside is not "Scout has less coverage" —
it is "the user is locked out of their university's career services during
placement season." No amount of coverage justifies that risk.

### Why "everyone does it" is not a reason

Detection is not the only risk, or even the main one:

- **Account bans** are permanent and can extend to the user's real, personal
  account, not just a throwaway.
- **IP bans** on a residential or VPS IP block everything else running there.
- **Cease-and-desist** letters are cheap for the sender and expensive for the
  recipient.
- **Fragility.** Anti-bot measures change constantly. A scraper against a hostile
  target is not a feature you build once; it is a maintenance obligation that
  breaks at the worst moment — like during peak hiring season.

That last point is the engineering argument, independent of ethics: a system
built on hostile scraping is a system that silently stops working, and silent
failure is exactly the outcome Scout exists to prevent.

## Options considered

### Option A — Scrape anyway, with rotating proxies and headless browsers

**For:** maximum short-term coverage.
**Against:** everything above. Also requires residential proxies (~$50–300/month),
headless browser infrastructure (heavy, slow, expensive), and continuous
maintenance against an adversary with a much larger engineering budget. Costs
more than every other part of Scout combined and delivers the least reliable
component.

### Option B — Omit these platforms entirely

**For:** clean, safe, simple.
**Against:** LinkedIn and Indeed genuinely carry postings that appear nowhere
else, particularly from mid-size Indian companies that use LinkedIn as their
primary channel. Straight omission is a real coverage loss.

### Option C — Email alert ingestion

Every one of these platforms offers **job alert emails**. The user subscribes to
alerts using a Scout-owned address (`alerts@scout.<domain>`), configured with
their own search criteria. Scout receives that mail and parses it.

**This is not a workaround; it is the platforms' own intended distribution
channel.** They built the alert feature specifically to push jobs to users. The
user is the subscriber. The mail is addressed to them. Scout is acting as their
mail client — which is precisely what an email client does.

**For:**
- Fully legitimate. No ToS violation, no robots.txt question, no ban risk.
- Zero anti-bot friction. Email does not have a CAPTCHA.
- Cheap: inbound webhook, no proxies, no browsers.
- Platform-blessed and therefore stable. Alert emails change format rarely, and
  when they do it is a parser update, not an arms race.
- Latency is good — most platforms send alerts within 15–60 minutes of matching
  postings, and several offer near-real-time alerts.

**Against:**
- Coverage is bounded by what the alert criteria match. Mitigated by configuring
  many overlapping alerts (per role family × per location tier).
- Alert emails sometimes truncate descriptions, so the record is thinner. We
  fetch the canonical posting from the *company's* ATS when we can resolve it,
  which usually restores full detail.
- Latency is worse than direct polling (minutes to an hour rather than seconds).
  Acceptable, because these platforms are a secondary discovery channel — most
  postings also appear on the company's own ATS, which we poll directly and
  faster.

### Option D — Official APIs and partner programs

Indeed has a publisher program. LinkedIn has partner APIs. Both are aimed at
commercial job boards and are unlikely to approve a personal project, but they
cost nothing to apply for.

**For:** the cleanest possible access if granted.
**Against:** approval is unlikely and slow. Not a plan, but worth a submitted
application.

## Decision

**Option C as the primary route, Option D pursued opportunistically, Option A
never.**

### The policy, stated as rules

1. **Scout MUST NOT** send automated requests to any host whose robots.txt
   disallows the path, whose ToS prohibits automated access, or which requires
   authentication that the user has not explicitly and knowingly delegated.
2. **Scout MUST NOT** implement CAPTCHA solving, browser fingerprint spoofing,
   residential proxy rotation, or any other bot-detection evasion. This is a
   bright line with no exceptions.
3. **Scout MUST** identify itself honestly in every request:
   `Scout/1.0 (+https://<domain>/bot; personal job discovery agent)`.
4. **Scout MAY** parse email delivered to an address the user controls,
   regardless of who sent it.
5. **Every source** in the registry carries a `legal_posture` field —
   `permitted`, `email_only`, `api_only`, or `prohibited` — and the collector
   refuses to fetch anything marked `prohibited`. This is enforced in code, not
   convention: the politeness gate checks it before every request and the check
   is unit-tested.

### Coverage impact, honestly assessed

Direct polling of the company's own ATS is faster and richer than any aggregator,
and the overwhelming majority of postings on LinkedIn and Indeed originate from an
ATS we already poll. The aggregators are mostly a *rediscovery* channel.

Estimated residual loss after email ingestion: **5–10% of postings**, concentrated
in small companies that post only to LinkedIn and never adopt an ATS. Partially
recovered by the company discovery pipeline, which finds those companies through
funding announcements and their own websites, and then monitors them directly.

Meanwhile, we gain: no ban risk, no proxy costs, no headless browser
infrastructure, no maintenance arms race, and no chance of the user losing their
Handshake account in October.

## Consequences

**Positive.** The system can run unattended for years. No legal exposure. No
proxy or browser infrastructure — likely $100–300/month saved. Sources that
cannot break in an arms race. The user's university account is safe.

**Negative.** 5–10% coverage loss on aggregator-exclusive postings. Email
ingestion adds a mail-receiving component and a parser per platform. Higher
latency on that channel. Requires the user to do a one-time setup of alert
subscriptions — roughly 20 minutes, documented in the setup runbook.

**Neutral.** Forces investment in direct company monitoring, which is a better
channel anyway: faster, richer, and it surfaces the small-company long tail that
the brief explicitly wants and that aggregators bury.

## Reversal conditions

- A platform grants API access → build a proper adapter immediately.
- A platform publishes a public feed or relaxes its terms → re-evaluate.
- Settled law changes materially in a relevant jurisdiction → re-evaluate with
  actual legal advice, not an ADR.

Rules 1–3 are **not** subject to reversal on convenience grounds.

## Migration path

`legal_posture` is a column on the source registry from the first migration.
Flipping a source from `email_only` to `permitted` is a one-row update plus an
adapter. No structural change required.
