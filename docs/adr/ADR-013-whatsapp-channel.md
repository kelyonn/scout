# ADR-013: WhatsApp as a notification channel

**Status:** **Rejected** — accepted 2026-08-07, withdrawn the same day before any
implementation.
**Date:** 2026-08-07

> **Why this record still exists.** Nothing was built on this decision, so there is
> no code history to explain. It is kept for two reasons: WhatsApp is an obvious
> thing to propose again in a few months, and the two constraints that killed it are
> not obvious until you go looking. Read this before re-proposing it.
>
> **One part of this ADR remains in force:** the prohibition on unofficial WhatsApp
> libraries, in the final section. That is policy regardless of whether Scout ever
> sends a WhatsApp message.

## Context

WhatsApp was requested as a notification channel, and the reasoning was sound: in
India it is the default messaging surface, notifications there get read, and it
works on every device without installing anything.

The channel was specified, then withdrawn once the setup cost was clear. This
record documents what that cost actually is, because it is the part that is easy to
underestimate.

## What killed it

**A dedicated phone number.** The number registered to a WhatsApp Business Account
cannot simultaneously be an ordinary WhatsApp account. There is no way around this —
it is how Meta partitions the platform. The user's personal number is therefore
ineligible, and the channel requires acquiring and verifying a second number that
exists solely to be Scout's sending identity.

For a one-person tool, that is a disproportionate amount of real-world setup —
acquiring a SIM or virtual number, verification, a Meta Business account, business
verification — for a channel that duplicates notifications already arriving on two
other transports.

**Template approval on the critical path.** Business-initiated messages outside a
24-hour service window must use Meta-approved templates. Approval is calendar time
we do not control, and it sits between "code complete" and "channel working." The
design mitigated this with three generic templates rather than one per trigger, but
mitigation is not elimination: the first approval still has to happen, and a
rejection means another round.

**Marginal value over what already exists.** This is the argument that actually
settles it. Native push via FCM ([ADR-012](ADR-012-native-app-shell.md)) delivers
to the lock screen with actions and a badge. Telegram delivers in 1–2 seconds with richer inline buttons than
WhatsApp templates permit, costs nothing, and needs 60 seconds of setup. WhatsApp
would have been a **third** delivery of the same notification, with worse
formatting than Telegram, at ₹50–90/month, behind the most setup friction of any
channel in the system.

The honest summary: WhatsApp was the best channel on exactly one axis — whether the
user reads it — and worse than Telegram on capability, cost, setup, and iteration
speed. When two other channels already reach the same lock screen, that one axis
stops being decisive.

## Decision

**WhatsApp is not a Scout notification channel.**

Notification channels are specified in
[`11-notifications.md`](../11-notifications.md): native push and Telegram as
primaries, Web Push as the desktop and not-installed fallback, email for digests
and deadlines, Discord optional.

## Consequences

**Positive.** No dedicated phone number, no Meta Business verification, no template
review on the critical path, no per-message cost, and no phone number to keep
registered. M0 loses an external-paperwork exit criterion and M3 loses 2–3 days of
code plus an unpredictable wait. Monthly cost drops by ₹70–150. One fewer secret
set, one fewer inbound webhook, one fewer third party seeing notification content
and a phone number.

**Negative.** Notifications no longer arrive on the surface the user checks most
reflexively. If Telegram turns out to be a place they do not look, this will need
revisiting — and the metric that would reveal it is
`scout_notification_open_ratio{channel="telegram"}`.

**Resolved since.** This ADR originally flagged that dropping WhatsApp weakened the
fallback story for an iPhone. That concern is moot — the user's phone is Android, so
the native app covers it directly and no iOS build is planned
([ADR-012](ADR-012-native-app-shell.md)).

## Reversal conditions

Revisit if any of these becomes true:

- The user reports they are not reading Telegram notifications.
- A spare phone number becomes available at no effort, removing the main friction.
- Meta introduces a personal-use tier that does not require a separate WABA number.
- Scout becomes multi-tenant, where WhatsApp's reach across Indian users would
  justify the setup cost amortized over many users.

Reversal cost is low. The channel was designed against the existing
`NotificationChannel` interface: one adapter, three template definitions, one
inbound webhook, and one enum value. Two to three days of code, plus the wait.

---

## The part that remains in force

**`whatsapp-web.js`, `baileys`, `venom-bot`, and any other library that automates
WhatsApp Web or reimplements its protocol are prohibited in this codebase.** CI
fails the build if one appears in a dependency manifest.

These libraries are the tempting shortcut precisely because they skip everything
described above — no dedicated number, no verification, no templates, no cost. That
is what makes the prohibition worth writing down even though WhatsApp is no longer
a channel: the day someone wants WhatsApp notifications in a hurry, this is the
path they will find first.

They violate WhatsApp's Terms of Service, which prohibit unauthorized or automated
use and "modified versions of WhatsApp." Meta enforces by banning the **phone
number**, and the ban lands on a real person's WhatsApp account along with every
conversation in it. During placement season, losing the number recruiters use to
reach you is a self-inflicted wound far worse than any notification gain.

This is the same analysis as [ADR-007](ADR-007-no-tos-violating-scraping.md) applied
to an outbound channel, and it is on the same footing: **not subject to reversal.**
If WhatsApp is ever revisited, it is the official Cloud API or nothing.

See [`14-legal-compliance.md`](../14-legal-compliance.md) section 3.1.
