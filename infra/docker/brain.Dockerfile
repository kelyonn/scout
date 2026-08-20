# apps/brain's Python service. Separate from go-service.Dockerfile since the
# toolchain has nothing in common — see ADR-001's language split.
#
# Built for linux/arm64 only, matching go-service.Dockerfile's own reasoning
# (ADR-014): the Oracle A1 host and the MacBook are both ARM64.

FROM ghcr.io/astral-sh/uv:python3.12-bookworm-slim

WORKDIR /src

# packages/riverpy is a local path dependency (pyproject.toml's
# [tool.uv.sources]), so it needs to be in the build context — same reason
# go-service.Dockerfile's context is the repo root, not the individual
# service directory. packages/taxonomy is not a dependency in the Python
# packaging sense, but role_taxonomy.py reads roles.yaml straight off disk
# (deliberately — see that file's own comment on why Go and Python share
# one taxonomy file rather than duplicating it), so it has to be present at
# the same relative path the repo itself uses.
COPY packages/riverpy /src/packages/riverpy
COPY packages/taxonomy /src/packages/taxonomy
COPY apps/brain /src/apps/brain

# scout_brain/config.py's DEFAULT_LLM_PROVIDERS_PATH resolves to
# /src/infra/config/llm_providers.yaml at this WORKDIR — this was missing
# from every COPY above until 2026-08-20, so the file never existed in
# the built image at all. load_llm_config() degrades a missing file to
# local-Ollama-only silently (by design, so a host with no hosted
# provider signed up still runs) — which meant ADR-016's whole hosted
# cascade had never actually been reachable from a real container, ever,
# regardless of which API keys were configured. Only this one file, not
# all of infra/config/ — nothing else under it is a Python runtime
# dependency.
COPY infra/config/llm_providers.yaml /src/infra/config/llm_providers.yaml

WORKDIR /src/apps/brain

# --frozen: fail the build rather than silently re-resolving if uv.lock is
# out of date with pyproject.toml — the same "CI fails if generated code is
# out of date" posture ADR-001 states for the schema codegen pipeline.
RUN --mount=type=cache,target=/root/.cache/uv \
    uv sync --frozen --no-dev

# fastembed's bge-small-en-v1.5 download (~130MB) happens on first run, not
# at build time — no HF token is available in CI, and baking a specific
# model version into every image layer would need rebuilding the image on
# every model bump. Cached under this volume across restarts.
VOLUME /root/.cache/fastembed

ENTRYPOINT ["uv", "run", "--frozen", "python", "-m", "scout_brain.worker"]
