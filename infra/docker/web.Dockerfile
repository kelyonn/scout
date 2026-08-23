# apps/web is a self-contained package (its own pnpm-lock.yaml, no
# cross-repo imports — verified before writing this), so the build
# context is apps/web itself, not the monorepo root the way
# go-service.Dockerfile's is. See infra/compose/{local,production}.yml
# for the `context:` that makes that true.
#
# Runtime is plain node:22-alpine, not distroless — Next.js standalone
# output still needs a real Node binary and npm's shape of a filesystem
# (server.js expects to be run by `node`, not exec'd directly), and
# distroless's nodejs variant's exact entrypoint path isn't worth
# guessing at when alpine + the image's own built-in nonroot `node` user
# gets the same "no root process" property with far less risk of a
# silent build break.

FROM node:22-alpine AS build

WORKDIR /src

RUN corepack enable

# Lockfile first so a source-only change doesn't re-resolve dependencies.
COPY package.json pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile

COPY . .

# SCOUT_API_URL/SCOUT_AUTH_TOKEN are read at request time (route handlers,
# proxy.ts), never baked into the client bundle — this app has no
# NEXT_PUBLIC_ vars by design (the auth token must never reach browser
# JS), so the build itself needs no secrets.
RUN pnpm build

FROM node:22-alpine

WORKDIR /app

# .next/standalone already contains a minimal node_modules (only what
# tracing found each route actually imports) plus a generated server.js —
# .next/static and public/ are deliberately excluded from that trace
# (they're served as static assets, not imported by server code) and
# have to be copied alongside it by hand.
COPY --from=build /src/.next/standalone ./
COPY --from=build /src/.next/static ./.next/static
COPY --from=build /src/public ./public

USER node
EXPOSE 3000
ENV PORT=3000 HOSTNAME=0.0.0.0

ENTRYPOINT ["node", "server.js"]
