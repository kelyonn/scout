# Frontend and UX — Scout

**Status:** Draft · **Owner:** Design + Frontend · **Last updated:** 2026-08-06

Stack rationale is in [ADR-009](adr/ADR-009-frontend-stack.md). This document
covers what the product looks like and how it behaves.

---

## 1. Design intent

The brief asks for something that feels "premium, clean, elegant, and fast" and
explicitly rejects generic templates. Three commitments make that concrete rather
than aspirational:

**Density over decoration.** This is a tool for triaging dozens of opportunities
quickly. Every pixel of padding that does not aid scanning is a pixel of
information lost. Reference points are Linear and Height, not a marketing site.

**The interface should be boring and the data should be interesting.** No
gradient meshes, no glassmorphism, no animated blobs. A restrained neutral shell
with one accent color, so that the only thing competing for attention is the job
content — which is the actual product.

**Fast enough to feel local.** Sub-100ms interaction response. Optimistic updates
on every state change. No loading spinner where a skeleton or cached data will
do. Perceived speed is the single largest contributor to whether a tool feels
premium.

**How we avoid looking like default shadcn.** shadcn components are copied into
the repo, not imported, so we own them. Before any feature work, the token layer
is replaced wholesale: our own type scale, our own spacing rhythm (4px base, not
the default), tighter radii (6px, not 8px), a custom neutral ramp, and our own
motion curves. The components keep their accessible behavior and lose their
recognizable appearance.

---

## 2. Design system

### Color

A single accent on a carefully built neutral ramp. Dark mode is the default,
because the user works evenings.

```
Neutral ramp (dark)                 Neutral ramp (light)
 bg-base        #0A0B0D              #FFFFFF
 bg-surface     #121316              #F7F8F9
 bg-elevated    #1A1C20              #FFFFFF
 border-subtle  #24262B              #E8EAED
 border-strong  #34373D              #D2D5DA
 text-muted     #6E7178              #6E7178
 text-secondary #9CA0A8              #4A4D53
 text-primary   #E8EAED              #16181C

Accent (single, used sparingly)
 accent-primary #4F7CFF              interactive, focus, primary action
 accent-hover   #6B90FF
 accent-subtle  #4F7CFF14            backgrounds at 8% opacity

Semantic
 success #2FA96B    warning #D99A2B    danger #E5484D    info #4F7CFF
```

**Score colors are a deliberate exception** — the one place a full ramp is
justified, because score is the primary scanning dimension:

```
90-100  #2FA96B  exceptional
80-89   #4F7CFF  strong
70-79   #7A8390  good
60-69   #6E7178  moderate
< 60    #4A4D53  weak
```

Every foreground/background pair is verified at 4.5:1 minimum in CI by an
automated contrast check over the token file — not spot-checked by eye.

### Type

```
UI:   Inter Variable        (system fallback: -apple-system, Segoe UI)
Mono: JetBrains Mono        (code, IDs, compensation figures)

Scale (1.2 ratio, 14px base — smaller than typical, for density)
  display  28px / 34  −0.02em  600
  title    20px / 26  −0.01em  600
  heading  16px / 22  −0.01em  600
  body     14px / 20   0       400
  label    13px / 18   0       500
  caption  12px / 16   0.01em  400
  micro    11px / 14   0.02em  500  uppercase
```

Compensation and scores render in mono with tabular figures so numbers align
vertically in lists — a small thing that makes scanning a column of salaries much
faster.

### Spacing and motion

4px base scale: `1, 2, 3, 4, 6, 8, 12, 16, 24, 32, 48, 64`. Radii: 4 (inputs),
6 (cards), 8 (modals), 999 (pills).

```
instant   100ms  cubic-bezier(0.2, 0, 0, 1)   hover, focus
quick     150ms  cubic-bezier(0.2, 0, 0, 1)   toggles, small reveals
smooth    250ms  cubic-bezier(0.4, 0, 0.2, 1) panels, modals
```

Nothing animates longer than 250ms. `prefers-reduced-motion: reduce` disables all
non-essential motion, replacing transitions with instant state changes.

---

## 3. Information architecture

```
┌─ Home                    Today's opportunities, at-a-glance status
├─ Opportunities
│  ├─ Latest               Reverse-chronological
│  ├─ Top ranked           Priority-ordered (default view)
│  ├─ Bengaluru            Tier 1 only
│  ├─ Remote               Tier 3 only
│  └─ New grad             Seniority-filtered
├─ Pipeline
│  ├─ Saved
│  ├─ Applied
│  ├─ Interviewing         Interview tracker
│  ├─ Offers
│  └─ Archive              Rejected, dismissed, expired
├─ Calendar                Deadlines and interviews, month/week/agenda
├─ Companies
│  ├─ Watchlist
│  ├─ Trending             Unusual recent hiring activity
│  └─ Directory
├─ Insights
│  ├─ Analytics            Funnel, conversion, response rates
│  ├─ Skill gaps
│  ├─ Timeline             Application history
│  └─ Sources              Which sources actually produce interviews
├─ Prep
│  ├─ Resume               Versions, match scores, feedback
│  ├─ Interview questions
│  └─ Notes
└─ Settings
   ├─ Profile & preferences
   ├─ Notifications & channels
   ├─ Sources               Admin: registry health
   └─ System                Admin: queue, costs, diagnostics
```

**Navigation:** persistent left sidebar on desktop (collapsible to icons), bottom
tab bar on mobile with the five most-used destinations (Home, Opportunities,
Pipeline, Calendar, Search). Command palette on `⌘K` reaches everything.

---

## 4. Key screens

### 4.1 Home

The screen the user opens most, so it answers three questions immediately without
scrolling: what is new, what needs action today, and is the system working.

```
┌──────────────────────────────────────────────────────────────────┐
│  Good evening                              Thu 6 Aug · 21:14 IST │
│                                                                  │
│  ┌────────┐ ┌────────┐ ┌────────┐ ┌────────┐                     │
│  │   7    │ │   2    │ │   3    │ │  91%   │                     │
│  │  new   │ │closing │ │interv- │ │ system │                     │
│  │ today  │ │ ≤3days │ │ iews   │ │ health │                     │
│  └────────┘ └────────┘ └────────┘ └────────┘                     │
│                                                                  │
│  ── Needs action ────────────────────────────────────────────    │
│  ⏰  Stripe · SWE Intern · closes in 2 days      [Apply now]     │
│  📅  Razorpay · Technical round · tomorrow 15:00 [Prepare]       │
│                                                                  │
│  ── New since you last looked ───────────────────────────────    │
│  [ job card ]                                                    │
│  [ job card ]                                                    │
│  [ job card ]                                    [ See all 7 ]   │
│                                                                  │
│  ── This week ───────────────────────────────────────────────    │
│  47 discovered · 12 saved · 6 applied · 2 interviews             │
│  ▁▂▅▃▇▄▆   discovery, last 7 days                                │
└──────────────────────────────────────────────────────────────────┘
```

"Needs action" is above "new opportunities" deliberately. A deadline in 2 days on
a job already saved matters more than a new posting, and putting new things first
would train the user to chase novelty over follow-through.

### 4.2 Job card

The most-repeated component in the product, so its density budget is strict.

```
┌──────────────────────────────────────────────────────────────────┐
│ ┌────┐  Software Engineering Intern                       ╭────╮ │
│ │ CF │  Cloudflare · 1000-5000 · Public                   │ 94 │ │
│ └────┘                                                    ╰────╯ │
│                                                                  │
│  📍 Bengaluru (Hybrid)   💰 ₹85,000/mo   🕐 12 min ago           │
│                                                                  │
│  Strong backend match — Go and distributed systems are your      │
│  top two skills, and this is genuine edge infrastructure work.   │
│                                                                  │
│  ●Go  ●Kubernetes  ●Linux  ●gRPC  ○Rust                          │
│                                                                  │
│  ~5 min to apply          [ Save ]  [ Dismiss ]  [ Apply → ]     │
└──────────────────────────────────────────────────────────────────┘
```

Filled skill dots are ones the user has; hollow are gaps. The score badge is
color-coded by band and is the primary scanning target — the eye should be able
to run down the right edge of a list and find the 90s.

**States:** default, hover (subtle lift, 1px border brighten), saved (accent left
border), applied (muted with a check), dismissed (collapsed to one line, with
undo), ghost (amber "may be evergreen" badge), new since last visit (accent dot).

### 4.3 Job detail

Two columns on desktop, stacked on mobile.

```
Left (scrollable)                    Right (sticky)
─────────────────────                ─────────────────────
Title, company, location             ╭─────────────────╮
Compensation, posted, deadline       │  Priority  94   │
                                     │  ▓▓▓▓▓▓▓▓▓▓░    │
── Why this matches ──               ╰─────────────────╯
AI explanation                       Overall match   88 ▓▓▓▓▓▓▓▓░░
                                     Skill match     92 ▓▓▓▓▓▓▓▓▓░
── Skills ──                         Resume match    81 ▓▓▓▓▓▓▓▓░░
You have: Go, K8s, Linux, gRPC       Company quality 88 ▓▓▓▓▓▓▓▓░░
Missing:  Rust (nice to have)        Compensation    82 ▓▓▓▓▓▓▓▓░░
                                     Learning        79 ▓▓▓▓▓▓▓░░░
── Description ──                    Culture         85 ▓▓▓▓▓▓▓▓░░
Full sanitized JD                    Growth          77 ▓▓▓▓▓▓▓░░░
                                     Interview prob. 34 ▓▓▓░░░░░░░
── About Cloudflare ──               Competition     41 ▓▓▓▓░░░░░░
Stage, size, stack, funding          Ease of apply   85 ▓▓▓▓▓▓▓▓░░
                                     Urgency         40 ▓▓▓▓░░░░░░
── Prep ──
Likely interview questions           Bengaluru ×1.20
Skill gaps for this role             Fresh     ×1.00

── Sources (3) ──                    [   Apply →   ]
Greenhouse · career page · HN        [ Save ] [ Dismiss ]
                                     [ Cover letter ]
```

**All thirteen scores are shown, always.** A composite number without its
components is unfalsifiable, and the PRD requires every ranking to be
explainable. Showing `interview_probability: 34` alongside `priority: 94` is
honest — this is a great opportunity that is hard to get — and that is exactly the
information the user needs to allocate effort.

The multipliers are shown explicitly too, so "why did Bengaluru rank higher" has
a visible answer rather than being buried in a config file.

### 4.4 Opportunities list

Virtualized list (TanStack Virtual) so 5,000 results scroll at 60fps. Filter rail
on the left, collapsible to a filter chip row on mobile. Filters are URL state,
so any view is shareable and bookmarkable and survives a refresh.

Bulk selection with keyboard: `j`/`k` to move, `x` to select, `s` to save, `d` to
dismiss, `Enter` to open, `a` to apply. Full keyboard triage without touching the
mouse, which is how the user will actually process 40 jobs in the evening.

### 4.5 Pipeline (Kanban)

Columns: Saved → Applied → Screening → Interviewing → Offer, with Archive
collapsed. Drag to move; keyboard-accessible alternative via a state menu on
every card (required — drag-only interfaces are inaccessible).

Each card shows company, role, days in current state, and the next action. Cards
stale in a state for more than 14 days surface a nudge: "Applied 21 days ago — no
response. Follow up?"

### 4.6 Analytics

Four questions, answered directly rather than with a wall of charts:

1. **Funnel** — applied → screening → interview → offer, with conversion at each
   step, segmented by company size, role family, and location tier.
2. **What is working** — which sources, company sizes, and role families produce
   interviews. This is the loop-closing view: it tells the user where to spend
   effort and tells us which sources to prioritize.
3. **Response time** — distribution of days-to-first-response by company, so
   "have I been ghosted?" has an evidence-based answer.
4. **Effort vs. outcome** — applications sent per interview obtained, over time.

---

## 5. Interaction principles

**Optimistic everything.** Save, dismiss, and state changes apply instantly in the
UI and reconcile in the background. On failure, the change reverts with a toast
and a retry action.

**Undo over confirm.** Dismiss does not ask "are you sure?" — it dismisses, and
offers 8 seconds of undo. Confirmation dialogs interrupt a triage flow that
depends on rhythm. The only exceptions are genuinely destructive and
irreversible: deleting a resume, revoking a session.

**Loading states, in order of preference.** Cached data with a background refresh
indicator > skeleton matching the real layout > spinner. Spinners appear only for
operations over 500ms with no cached predecessor.

**Empty states do work.** "No saved jobs" is a wasted screen. "No saved jobs yet
— here are 5 from today scoring above 85" converts an empty state into the next
action.

**Errors are actionable.** Not "Something went wrong" but "Couldn't load
opportunities — Scout's API isn't responding. [Retry] · [Status]", with the trace
ID available for support.

---

## 6. Mobile

Mobile-first, as required. Roughly 60% of usage will be checking notifications on
a phone.

| Adaptation | Detail |
| --- | --- |
| Navigation | Bottom tabs, 5 items, 48px targets |
| Job card | Single column, score badge top-right, actions in a swipe menu |
| Swipe gestures | Right = save, left = dismiss, both with undo |
| Job detail | Stacked; score panel collapses to a summary that expands on tap |
| Filters | Bottom sheet, not a sidebar |
| Kanban | Horizontal snap-scroll between columns |
| Tables | Card lists, never horizontal scroll |
| Typography | Base bumps to 15px; 14px is too tight on a phone |
| Safe areas | `env(safe-area-inset-*)` respected throughout |

**Thumb reachability.** Primary actions sit in the lower 40% of the screen.
Destructive actions do not — dismiss is a deliberate swipe, not a tap near the
thumb's resting position.

---

## 7. PWA

**Manifest:** standalone display, dark theme color, maskable icons at all
required sizes, shortcuts to Latest/Saved/Search, and a share target so a job URL
shared from any app is ingested by Scout.

**Service worker (Serwist):**

| Resource | Strategy |
| --- | --- |
| App shell | Precache, cache-first |
| API — feed | Stale-while-revalidate, 60s |
| API — job detail | Network-first, cache fallback |
| API — mutations | Network-only, Background Sync queue on failure |
| Images and logos | Cache-first, 30 days, 50-entry cap |
| Fonts | Cache-first, 1 year |

**Offline capability:** the last 100 feed items, all saved and applied jobs, and
their full detail are readable offline. Actions taken offline (save, dismiss,
state change) queue via Background Sync and replay on reconnect, with a visible
"3 changes pending sync" indicator so the user is never misled about what has
been persisted.

**Install prompt** appears on the third visit, and only when the native app is not
detected. It is framed around offline reading rather than notifications, because the
Android app owns notifications — telling a user to install the PWA "for
notifications" when they already have the app would be a second, redundant ask.

---

## 7.2 Browser support

**The user's daily browser is Dia on macOS**, so that is the primary desktop target
rather than an afterthought.

| Browser | Support level | Notes |
| --- | --- | --- |
| **Dia (macOS)** | **Primary — verified manually each release** | Chromium-based; treated as Chrome for capability purposes |
| Chrome / Edge | Full | The automated proxy for Dia in CI |
| Safari | Full | Tested via WebKit in Playwright |
| Firefox | Full | Tested in Playwright |
| Android WebView | Full | This is what the app runs in |

**Dia is Chromium-based, so the capability surface is Chrome's** — Web Push via
VAPID, service workers, Background Sync, and PWA install all behave as they do in
Chrome. Playwright has no Dia driver, so **Chromium is the automated stand-in and
Dia is checked by hand** on the manual QA pass. That is the honest arrangement:
CI proves the Chromium engine works, and a human confirms the actual browser does.

Two things to verify by hand rather than assume, because AI-browser shells sometimes
diverge from upstream Chromium in exactly these areas:

- **PWA install.** Dia's install affordance may sit somewhere other than Chrome's
  omnibox icon. If it is missing entirely, desktop falls back to a normal tab, which
  is fine — the desktop PWA is a convenience, not a requirement.
- **Web Push permission and delivery.** Desktop notifications are the fallback
  channel, so a silent failure here is worth catching. If Dia does not deliver Web
  Push reliably, nothing critical breaks: the Android app and Telegram are the
  primary channels and neither depends on the desktop browser.

**No Dia-specific code.** No user-agent sniffing, no browser-specific branches. If
something is broken there it gets fixed as a standards or Chromium issue, or it is
documented as a known limitation. Feature detection, never browser detection.

---

## 7.1 Native app shell

Per [ADR-012](adr/ADR-012-native-app-shell.md), the mobile app is a Capacitor
shell whose main view is this same web application. **There is no second UI
codebase and no design divergence** — a change to the feed ships to the app
without a rebuild.

The shell exists to provide five things the web cannot:

| Capability | Why it needs native |
| --- | --- |
| FCM push | Delivery that does not depend on a browser being installed and healthy |
| Notification actions from the lock screen | Save and Dismiss without opening the app |
| Badge count on the app icon | Unread high-priority count |
| Biometric app unlock | Fingerprint or face unlock on top of the stored bearer token ([ADR-015](adr/ADR-015-single-user-auth.md)) |
| Native share sheet | Send a job link out of Scout |
| OS notification channels | Lets the user silence digests but not instant alerts, in Android settings |

**Web code detects the shell** via `Capacitor.isNativePlatform()` and adapts:

```
Running in the native shell:
  · suppress the PWA install prompt
  · suppress the Web Push permission prompt
  · route notification permission through the native plugin
  · use the native share sheet instead of navigator.share
  · show a "Devices" section in settings listing registered devices
  · respect native safe-area insets for the cutout and gesture bar
  · handle the Android hardware back button as in-app navigation
```

**Safe-area handling** is the one visual difference: the shell has no browser
chrome, so `env(safe-area-inset-*)` must be honored on the header and bottom
navigation. Getting this wrong puts the tab bar under the Android gesture bar, and
it is checked on a real device.

**The Android back button is easy to get wrong.** By default it closes the app.
It must instead pop the WebView history, and only exit when there is nothing left to
pop — otherwise a user two screens deep taps back once and finds Scout gone.

**Deep links.** `scout://job/{id}` and `https://scout.<domain>/jobs/{id}` both
resolve to the job detail screen, so a notification tap, a Telegram button, and a
link in the email digest all land in the same place.

**Performance note.** The budgets in section 9 apply unchanged inside the WebView.
If feed scrolling drops below 50fps on device, the fallback is a natively-rendered
feed list only — not a full native rewrite. That reversal condition is recorded in
ADR-012.

---

## 8. Accessibility

WCAG 2.2 Level AA, treated as a requirement rather than a checklist.

| Area | Commitment |
| --- | --- |
| Keyboard | Every action reachable. Visible 2px accent focus ring on every interactive element. Logical tab order. Skip-to-content link. |
| Screen readers | Semantic HTML first, ARIA only where semantics are insufficient. Tested with VoiceOver and NVDA. |
| Live regions | New jobs arriving via SSE announce politely; notifications announce assertively. |
| Contrast | 4.5:1 body, 3:1 large text and UI components. Verified in CI over the token file. |
| Color independence | Score is never communicated by color alone — the number is always present. Skill dots pair fill state with a text label. |
| Motion | `prefers-reduced-motion` disables all non-essential animation. |
| Targets | 44×44px minimum on touch, 24×24px minimum on pointer (WCAG 2.2 target size). |
| Forms | Every input labelled. Errors associated via `aria-describedby`. Errors described in text, never by color alone. |
| Zoom | Usable to 200% without horizontal scroll. |
| Drag alternatives | Kanban drag has a full keyboard and menu equivalent (WCAG 2.2 dragging movements). |

**CI enforcement:** `axe-core` runs against every page in Playwright tests, and
any violation fails the build. Lighthouse accessibility score must stay ≥95.

---

## 9. Performance

| Metric | Budget | Enforcement |
| --- | --- | --- |
| FCP (mid-range Android, 4G) | ≤1.2s | Lighthouse CI |
| LCP | ≤2.0s | Lighthouse CI |
| TTI | ≤2.5s | Lighthouse CI |
| CLS | ≤0.05 | Lighthouse CI |
| INP | ≤200ms | Lighthouse CI + RUM |
| Initial JS (gzipped) | ≤120KB | `size-limit` in CI |
| Route JS (gzipped) | ≤40KB | `size-limit` in CI |

**Techniques:** React Server Components for the feed (zero client JS for job
cards); route-level code splitting; `next/font` with subsetting and preload;
AVIF/WebP with explicit dimensions to prevent layout shift; virtualized lists;
prefetch on link hover; streaming SSR with Suspense so the shell paints before
scores resolve.

**Real user monitoring** via `web-vitals` reporting to our own endpoint. Synthetic
Lighthouse numbers are a floor, not a measurement — the real device on the real
network is what counts.

---

## 10. Component inventory

Built in `packages/ui`, all customized from shadcn primitives or written from
scratch:

**Primitives:** Button, IconButton, Input, Textarea, Select, Combobox, Checkbox,
Radio, Switch, Slider, Badge, Avatar, Tooltip, Popover, Dialog, Sheet, Toast,
Skeleton, Spinner, Progress, Tabs, Accordion, Separator, ScrollArea, DropdownMenu,
ContextMenu, CommandPalette.

**Domain:** JobCard, JobCardCompact, ScoreBadge, ScoreBreakdown, SkillDots,
CompanyChip, LocationChip, CompensationChip, TimeAgo, MatchExplanation,
PipelineColumn, PipelineCard, DeadlineIndicator, SourceList, FilterRail,
FilterChip, EmptyState, ErrorState, NotificationItem, DigestPreview, StatTile,
FunnelChart, TimelineChart, SkillGapList.

**Layout:** AppShell, Sidebar, BottomNav, PageHeader, TwoColumn, StickyPanel.

Every component ships with a Storybook story covering default, hover, focus,
loading, error, and empty states, plus an accessibility annotation. Storybook is
the design review surface — reviewing components in isolation catches state bugs
that never appear in a happy-path screenshot.
