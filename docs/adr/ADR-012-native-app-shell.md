# ADR-012: Native app shell via Capacitor for real OS notifications

**Status:** Accepted
**Date:** 2026-08-07
**Supersedes:** [ADR-009](ADR-009-frontend-stack.md) in part — the "no native app"
decision only. ADR-009's framework and design-system choices stand.

## Context

[ADR-009](ADR-009-frontend-stack.md) chose a PWA with Web Push plus a Telegram
bot, and rejected a native app. On review, the user asked for notifications that
behave like a normal app's notifications, and that request is reasonable. Two of
the three arguments in ADR-009 do not survive scrutiny:

**The app-store review argument was wrong for this product.** ADR-009 cited "app
review latency on every release" as a cost. For a single-user personal app there
is no app store — Android installs a direct APK, iOS installs via Xcode or
TestFlight. Review latency is zero because there is no review. This was a
generic-SaaS argument applied to a personal tool, and it does not hold.

**The "two more build pipelines" argument was overstated.** It assumed a separate
React Native UI codebase. A WebView shell around the existing Next.js app is one
thin pipeline that reuses 100% of the UI.

**The remaining argument still holds partially.** Telegram genuinely does deliver
native-grade push for ~50 lines of code, and it stays in the design as a channel.
It is just not a *substitute* for the app's own notifications.

There is also a capability argument, though it is worth being precise about how
strong it actually is on the target platform:

| | Android Web Push | FCM via native shell |
| --- | --- | --- |
| Delivery reliability | Good | Excellent |
| Depends on the browser being installed and healthy | **Yes** | No |
| Notification channels tunable in OS settings | No | Yes |
| Badge count on the app icon | No | Yes |
| Notification actions | Basic | Full, with API calls from the native layer |
| Appears as a real app in the launcher and recents | Only if the PWA is installed | Yes |
| Biometric unlock | No | Yes, via plugin |

**Being honest about the gap.** On Android, Web Push is genuinely decent — this
table is not the landslide the equivalent iOS comparison would be. The reliability
difference is real but modest. The decisive arguments are that the user asked for
notifications that behave like a normal app's, that a native shell decouples
notifications from whichever browser happens to be installed, and that it costs 5–7
days rather than weeks. Overstating the technical case would be the same error
ADR-009 made in the opposite direction.

## Options considered

### Option A — Keep PWA Web Push only (status quo)

**For:** zero additional work, zero additional cost.
**Against:** the notification path — the product's core value — depends on a browser
being installed, permitted, and healthy. No badge, no OS-level notification
channels, weaker actions, and no app in the launcher. It does not deliver what was
asked for.

### Option B — Full React Native / Expo application

A second UI codebase in React Native.

**For:** best native feel, full platform capability, best performance.
**Against:** the entire dashboard rebuilt in RN — feed, filters, job detail with
thirteen scores, Kanban, charts. Three to four weeks minimum, then permanently
two UIs to keep in sync. For a single user, that maintenance tax is paid forever
in exchange for polish nobody else sees.

### Option C — Capacitor shell wrapping the existing web app

A native binary whose main view is a WebView pointed at the Next.js app, with
native plugins for push, badge, biometrics, and share. Android now; iOS available
from the same codebase if ever needed.

**For:**
- **Real OS notifications via FCM.** Correct app icon, notification actions, badge
  counts, OS-level notification channels, background delivery independent of any
  browser.
- **One UI codebase.** The shell is roughly 300 lines of configuration plus a
  handful of plugin calls. Every dashboard improvement ships to the app
  automatically because the app *is* the web app.
- **Instant releases for UI changes.** Web content updates without rebuilding the
  binary. Only shell or plugin changes need a new build.
- **Native biometric unlock** via a plugin, complementing the passkey flow.
- **Free and immediate on Android** — build an APK, install it, done. No fee, no
  store, no review.
- **iOS stays reachable** without a rewrite if the platform question ever changes.

**Against:**
- WebView performance is slightly below native. For a data-list app hitting a
  local-network API, this is not perceptible; the performance budgets in
  [ADR-009](ADR-009-frontend-stack.md) already assume mobile web.
- One more build pipeline, and a Gradle/Android SDK toolchain in CI.
- App stores are sometimes hostile to WebView wrappers. Irrelevant while this is
  a personal sideloaded app; relevant if Scout is ever published, and noted as a
  reversal condition.

### Option D — Capacitor shell with a native-rendered feed

Hybrid: WebView for most screens, a natively-rendered feed list.

**For:** best scrolling performance on the most-used screen.
**Against:** reintroduces a partial second UI codebase, which is the cost Option B
was rejected for. Not justified unless WebView scrolling measurably disappoints.

## Decision

**Option C, Android only.** A Capacitor shell in `apps/mobile`, wrapping the
existing Next.js application, with FCM as the primary notification transport.

### Architecture

```
┌─────────────────────────────────────────────────────────┐
│  apps/mobile  (Capacitor)                               │
│                                                         │
│  ┌───────────────────────────────────────────────────┐  │
│  │  WebView → the deployed Next.js app               │  │
│  │  (identical UI to the browser, no fork)           │  │
│  └───────────────────────────────────────────────────┘  │
│                                                         │
│  Native plugins:                                        │
│   · @capacitor/push-notifications   FCM                 │
│   · @capacitor/local-notifications  scheduled reminders │
│   · @capacitor/app                  deep links, state   │
│   · @capacitor/badge                unread count        │
│   · @capacitor/browser              in-app apply flow   │
│   · biometric plugin                app unlock          │
└───────────────────────┬─────────────────────────────────┘
                        │ device token registration
                        ▼
              POST /api/v1/push/register-device
                        │
                        ▼
        notification_channel (kind = 'native_push')
                        │
                        ▼
              Notifier → FCM
```

### Transport selection

The notifier picks the best available native transport per device:

| Platform | Transport | Reliability |
| --- | --- | --- |
| **Android, app installed** | **FCM** | **Excellent — the shipping target** |
| Desktop browser (macOS) | Web Push (VAPID) | Good |
| iOS, app installed | APNs | Not built — see below |

**Web Push is not removed.** It remains the transport for desktop browsers and
for any device without the app installed. `native_push` and `web_push` are
separate channel kinds, and the notifier deduplicates so a device registered for
both receives one notification, preferring the native transport.

### Deep linking

Notifications carry `scout://job/{job_group_id}`, and the shell maps that to the
web route. Notification actions (`Save`, `Dismiss`, `Apply`) call the API
directly from the native layer using the stored session, so triage works from the
lock screen without opening the app — matching what the Telegram inline buttons
already provide.

### Release model

| Change | Requires a new binary? |
| --- | --- |
| Any UI change | **No** — the WebView loads the deployed app |
| API change | No |
| New notification content | No |
| Native plugin added or updated | Yes |
| Capacitor or OS SDK upgrade | Yes |
| Permission change | Yes |

Expected binary release cadence: a handful of times per year. This is the property
that makes the shell cheap to own.

### Distribution

**Android only.** Signed APK built in CI, published as a GitHub release artifact,
installed directly. No fee, no review, no store, no Apple tooling.

**iOS is not built.** The user's phone is Android, so APNs, the $99/year Apple
Developer Program, provisioning profiles, and the macOS build lane are all out of
scope. This removes the single largest cost the mobile decision would have added
(~₹725/month, more than the server) and one of the two never-rotate signing secrets.

The **iOS path stays available at low cost** if that ever changes. Capacitor targets
both platforms from the same shell, the user already has a MacBook — so no new
hardware is needed — and the work would be an Apple enrolment, a `.p8` auth key, an
APNs sender in the notifier, and a macOS CI lane. Estimated 2–3 days on top of the
existing Android shell, versus the 5–7 days for the shell itself.

**Nothing in the design assumes one platform.** `device_platform` remains an enum
rather than a boolean, the notifier dispatches per transport rather than
per-if-statement, and the delivery pipeline is provider-agnostic. Adding APNs later
is an additive change, not a refactor. That is the whole point of choosing a
cross-platform shell even when only one platform ships.

## Consequences

**Positive.** Notifications behave like every other app on the phone — correct
icon, actions, badge count, and OS-level notification channels the user can tune.
Delivery no longer depends on a browser being installed and healthy. One UI
codebase preserved, so every dashboard change ships to the app for free. Native
biometric unlock. **Total added cost: ₹0.** FCM is free at any volume and Android
distribution needs no fee, no store, and no review.

**Negative.** One more build pipeline and an Android toolchain in CI. WebView
performance is marginally below native. FCM credentials and the Android signing
keystore become secrets to manage — the keystore is never-rotate and must be backed
up offline, because losing it forces an uninstall and reinstall on every device.
Push registration adds a third transport to keep straight (`native_push`,
`web_push`, Telegram), which is more delivery-path complexity than before.

**Neutral.** Capacitor is well maintained with a clear upgrade path. If the WebView
approach ever disappoints, Option D remains available for the feed screen alone
without discarding the shell.

## Reversal conditions

- WebView feed scrolling measurably below 50fps on the target device → adopt
  Option D for the feed only.
- FCM delivery reliability measured below 95% over a month → investigate; this
  would indicate a configuration fault rather than a platform limit.
- Scout published publicly → app stores may reject a WebView wrapper, at which
  point Option B becomes necessary for store presence.
- Capacitor abandoned upstream → migrate the shell to Expo with
  `react-native-webview`, which is a near-identical architecture.
- **The user switches to an iPhone** → enable the iOS target. Apple enrolment, a
  `.p8` key, an APNs sender, and a macOS CI lane. 2–3 days, no rewrite.

## Migration path

The shell is additive. `apps/web` is unchanged, `apps/mobile` is new, and the API
gains one endpoint (`/push/register-device`) and one channel kind. Removing the
shell later means deleting a directory and disabling a channel — nothing else
depends on it.

Estimated effort: **5–7 days** for Android, versus 3–4 weeks for Option B.
Scheduled into M3 alongside the rest of the notification work.
