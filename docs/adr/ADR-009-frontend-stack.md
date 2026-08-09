# ADR-009: Next.js PWA, no native mobile app

**Status:** **Partially superseded by [ADR-012](ADR-012-native-app-shell.md)**
**Date:** 2026-08-06
**Note:** This decision partially departed from the brief, which listed "Mobile
Push Notifications" — usually read as implying a native app.

> **Superseded portion.** The "no native app" conclusion was reversed on
> 2026-08-07. [ADR-012](ADR-012-native-app-shell.md) adopts a Capacitor shell
> wrapping this same web application, using FCM and APNs as the primary
> notification transport. Two arguments made below did not survive review: the
> app-store review cost does not apply to a sideloaded personal app, and the
> "second codebase" cost assumed a React Native UI rather than a WebView shell.
>
> **Everything else in this ADR stands** — Next.js 15 with the App Router, the
> stack table, the shadcn customization approach, the PWA and offline strategy,
> and the performance budgets. Web Push is retained as the desktop and
> not-installed fallback rather than as the primary mobile transport.

## Context

The dashboard must be fast, feel premium rather than templated, work well on a
phone, and deliver push notifications reliably. The brief asks for React,
Next.js, and PWA support, plus mobile push.

The open question is whether mobile push requires a native app.

## Options considered

### Framework

**Next.js 15 (App Router).** React Server Components matter here specifically:
the feed view renders 50 job cards each with 13 scores and an explanation. As an
RSC this is one server round-trip with zero client-side data fetching and no
loading spinner. As a client-side SPA it is a request waterfall. Streaming SSR
with Suspense lets the shell paint while scores resolve.

**Remix** — excellent, and its nested-route data loading is arguably cleaner. But
RSC gives us a materially smaller client bundle for a data-heavy read-mostly app,
and Next's ecosystem for PWA tooling is deeper.

**SvelteKit** — smaller bundles, genuinely pleasant. Rejected on ecosystem depth:
shadcn/ui, TanStack Query, and the React chart libraries we want have weaker
Svelte equivalents, and this is a one-developer project where ecosystem leverage
matters more than a 30KB bundle difference.

**Vite SPA** — simplest, but everything is client-rendered, which is the wrong
default for a feed that is mostly server data.

### Mobile push

**Option A — Native app (React Native / Expo).**
**For:** the most reliable push on iOS, home screen presence, background
capabilities, biometric auth.
**Against:** two additional build pipelines, an Apple Developer account
($99/year), app review latency on every release, and a second codebase surface —
for one user. Push via FCM/APNs is more reliable on iOS than Web Push, but the
gap has narrowed considerably, and we have a fallback that closes it entirely.

**Option B — PWA with Web Push (VAPID).**
**For:** one codebase. Works on Android (Chrome, Firefox, Samsung Internet) and
on iOS 16.4+ **for PWAs added to the home screen** — that caveat is real and the
onboarding flow must walk the user through installing. Free. No app store.
Instant deploys.
**Against:** iOS requires the install step. iOS push can be throttled if the user
rarely opens the app. Notification presentation is less rich than native.

**Option C — PWA + Telegram bot as the primary push channel.**
**For:** Telegram's native app delivers push with the reliability of a native app,
because it *is* one. Delivery is typically under 2 seconds. Rich formatting,
inline buttons ("Save", "Dismiss", "Open") that call back into our API, full
message history, and it works identically on iOS, Android, and desktop. It costs
nothing and takes about 50 lines to implement.
**Against:** requires the user to have Telegram. (The user does.)

## Decision

**Next.js 15 PWA, with Telegram as the primary notification channel and Web Push
as the secondary. No native app.**

The reasoning in one line: Telegram gives us native-grade push reliability for
roughly 1% of the effort of building a native app, and it does so on every
platform at once.

### Stack

| Concern | Choice | Why |
| --- | --- | --- |
| Framework | Next.js 15, App Router | RSC for the data-heavy feed |
| Language | TypeScript, strict | Non-negotiable |
| Styling | Tailwind CSS v4 | Colocated, no naming overhead, tiny production CSS |
| Components | shadcn/ui, **heavily customized** | Copy-in, not a dependency — we own and restyle the code. Addresses the "no generic templates" requirement directly: default shadcn is recognizable, so the design system in [12](../12-frontend-ux.md) replaces its tokens, radii, spacing scale, and motion. |
| Server state | TanStack Query v5 | Caching, background refetch, optimistic updates |
| Client state | Zustand | Minimal; most state is server state |
| Forms | React Hook Form + Zod | Zod schemas shared with the API contract |
| Charts | Recharts | Sufficient, small, composable |
| Tables | TanStack Table | Headless, so it inherits our design system |
| Service worker | Serwist | Maintained successor to Workbox, first-class Next support |
| Push | Web Push, VAPID | No third-party push service needed |
| Realtime | SSE via `EventSource` | One-directional; see [02](../02-architecture.md) |
| Animation | CSS transitions + Motion for the few cases that need it | Bundle discipline |
| Testing | Vitest, Testing Library, Playwright | Unit, component, E2E |

### PWA specifics

- **Offline shell.** Saved jobs, applied jobs, and the last 100 feed items are
  cached and readable offline. Actions taken offline queue via Background Sync
  and replay on reconnect.
- **Install prompt.** Shown on the third visit, never on the first. On iOS,
  detect Safari-not-installed and show explicit "Share → Add to Home Screen"
  instructions, since iOS gives no programmatic install prompt and this is the
  step that gates push notifications entirely.
- **App shortcuts** in the manifest for Latest, Saved, and Search.
- **Share target** so a job URL shared from any app is ingested by Scout.
- **Caching strategy:** app shell cache-first, API responses
  stale-while-revalidate with a 60-second TTL, job detail network-first with
  cache fallback.

### Performance budget, enforced in CI

| Metric | Budget |
| --- | --- |
| First Contentful Paint (mid-range Android, 4G) | ≤1.2s |
| Time to Interactive | ≤2.5s |
| Largest Contentful Paint | ≤2.0s |
| Cumulative Layout Shift | ≤0.05 |
| Interaction to Next Paint | ≤200ms |
| Initial JS bundle (gzipped) | ≤120KB |
| Lighthouse Performance (mobile) | ≥90 |
| Lighthouse Accessibility | ≥95 |

Lighthouse CI runs on every pull request and fails the build on regression.

## Consequences

**Positive.** One codebase, one deploy pipeline, instant releases with no review
queue. Telegram gives better push reliability than a hastily built native app
would. Zero app store cost. RSC keeps the client bundle small on a data-heavy app.

**Negative.** iOS Web Push requires the user to install the PWA — a real
onboarding step that must be handled well. No biometric unlock (passkeys mitigate
this: WebAuthn uses Face ID and Touch ID in the browser). No background
geolocation or other native capabilities — none of which we need. Notification
presentation is less rich than native, though Telegram's inline buttons are
arguably richer than a standard iOS notification.

**Neutral.** If a native app is ever wanted, the API is already client-agnostic
and a React Native shell could reuse the type-safe generated client.

## Reversal conditions

- Web Push delivery reliability on iOS measured below 90% over a month.
- A genuine need for a native capability (background location, native share
  extensions, offline-first sync with conflict resolution).
- Multi-tenant launch where app store presence becomes a distribution channel
  rather than an engineering cost.

## Migration path

`apps/mobile` with Expo, reusing `packages/clients` and the design tokens from
`packages/ui`. The API needs no change. Estimated 3–4 weeks, which is precisely
why it is not in the MVP.
