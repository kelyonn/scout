# apps/web

The Scout dashboard — a Next.js 16 (App Router, React 19, Tailwind 4) app
that is the only user interface Scout has. `apps/mobile` wraps this same
deployed app in an Android WebView; it is not a second UI
([ADR-012](../../docs/adr/ADR-012-native-app-shell.md)).

The behavior this app implements is specified in
[`docs/12-frontend-ux.md`](../../docs/12-frontend-ux.md). Read that before
changing a screen.

## How it talks to the backend

The browser never calls the Go API directly. Every request goes to a
same-origin route under `app/api/*`, which forwards it to `SCOUT_API_URL`
with the bearer token attached server-side (`lib/proxy.ts`). That is what
keeps `SCOUT_AUTH_TOKEN` out of browser JavaScript
([ADR-015](../../docs/adr/ADR-015-single-user-auth.md)). There are no
`NEXT_PUBLIC_` variables, and there must not be.

`proxy.ts` (Next 16's name for middleware) gates every page and API route
on the session cookie; `/login`, its API, `/api/health`, and static assets
are the only public paths.

```
browser ──cookie──▶ proxy.ts ──▶ app/api/* ──bearer──▶ Go API (apps/api)
                                     │
                                lib/proxy.ts
```

## Running it

With the whole stack (recommended):

```bash
make dev            # from the repo root — web is at http://127.0.0.1:3000
```

Natively, against `make dev`'s API, for fast iteration:

```bash
make web-dev        # copies .env.example → .env.local on first run, then `next dev`
```

`.env.local` must agree with the repo root's `.env` on two values:
`SCOUT_API_URL` (the host-mapped `SCOUT_API_PORT`) and `SCOUT_AUTH_TOKEN`.
See [`.env.example`](.env.example).

Sign in with the value of `SCOUT_AUTH_TOKEN` — locally, the placeholder in
the root `.env.example`.

## Checks

```bash
pnpm run lint        # eslint (next/core-web-vitals + typescript)
pnpm run typecheck   # tsc --noEmit — needs .next/types from a prior dev/build
pnpm run test        # vitest, lib/** unit tests
pnpm run build       # production build; what infra/docker/web.Dockerfile ships
```

All four run in CI (`.github/workflows/ci.yml`, job `web`), and
`make test` / `make lint` at the repo root include them.

## Layout

```
app/
  (app)/            Authenticated screens; (app)/layout.tsx mounts the Shell once
  api/              Same-origin proxy routes → Go API (see lib/proxy.ts)
  login/            The one public page
  error.tsx, not-found.tsx, global-error.tsx
  manifest.ts, icon.svg
components/         Shell (sidebar + mobile tabs), JobCard, StateControls,
                    Toast, FilterBar, ui.tsx (PageHeader/Skeleton/EmptyState/ErrorNotice)
lib/
  api.ts            Typed client + ApiError; 401 → /login, human error messages
  proxy.ts          Server-side forwarder used by every app/api route
  auth.ts           Token comparison + cookie attributes (server-only)
  rateLimit.ts      In-memory limiter for the login route
  safeRedirect.ts   ?next= sanitizer
  stateMachine.ts   Frontend mirror of apps/api/internal/jobs/state.go
  taxonomy.ts       Display labels for enum values
  capacitor.ts      Native bridge (no-op outside apps/mobile's WebView)
  types.ts          Hand-mirrored response shapes from apps/api
```

`lib/types.ts` and `lib/stateMachine.ts` mirror Go code by hand; a change on
either side has to be made on both (the vitest suite pins the transition
table so a one-sided edit fails loudly).
