# One Dockerfile for all three Go services.
#
# They differ only in which package under apps/*/cmd gets built, so a file per
# service would be three copies of the same twenty lines drifting apart. The
# service name comes in as a build argument; see infra/compose/production.yml.
#
# Built for linux/arm64 only. The Oracle A1 host and the MacBook are both ARM64,
# so local and production are the same architecture and there is no cross-build
# to get wrong (ADR-014).

ARG GO_VERSION=1.23

FROM golang:${GO_VERSION}-alpine AS build

# Which service to build: api, collector, or notifier.
ARG SERVICE

WORKDIR /src

# There are no third-party dependencies yet, so there is nothing to pre-fetch in
# a separate layer. When go.sum appears, add a `COPY go.mod go.sum ./` +
# `go mod download` layer above this one so a source-only change stops
# re-resolving the module graph.
COPY . .

# CGO off so the binary is static and can run on `scratch`-class base images.
# -trimpath keeps build-host paths out of the binary; -s -w drop the symbol and
# DWARF tables, which is a few MB per image and nothing we can use in production
# anyway, since panics still carry a stack trace.
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    test -n "${SERVICE}" && \
    CGO_ENABLED=0 GOOS=linux go build \
      -trimpath \
      -ldflags="-s -w" \
      -o /out/app \
      ./apps/${SERVICE}/cmd

# distroless/static: no shell, no package manager, no libc, and a nonroot user
# baked in. It carries CA certificates, which the collector needs for the
# dead-man's switch and will need for every source fetch.
#
# The absence of a shell is the point. A container with no /bin/sh is one an
# attacker with code execution cannot pivot from with the usual toolkit, and it
# costs nothing here because these binaries are static and self-contained.
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /out/app /app/app

# 65532 is the `nonroot` user in the base image. Stated explicitly so that
# `docker inspect` shows it and a future base-image change cannot silently
# promote these to root.
USER 65532:65532

ENTRYPOINT ["/app/app"]
