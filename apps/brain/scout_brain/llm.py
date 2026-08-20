"""LLM clients. `OllamaClient` is the local, always-available tier —
Model: qwen2.5:3b-instruct, picked for real structured-JSON output quality
at this size (verified directly against the dedup-adjudication prompt
shape before committing to it) while fitting comfortably alongside
Postgres/Redis/the Go services in 16GB of host RAM.

`CascadeClient` (below) is ADR-016's full cascade — rotated free hosted
providers (Google AI Studio/Groq/OpenRouter) with `OllamaClient` as the
last-resort local fallback. It is used only for tasks whose input is a
public job posting (classification, summarization, dedup adjudication).

**Anything that touches resume, application history, interview notes, or
rejection records must construct its own `OllamaClient` directly and never
go through `CascadeClient`** — AGENTS.md rule 9 / ADR-016's data rule:
those never leave the host. This is enforced structurally, by which client
type a call site is given (see scout_brain.worker's construction of
`explain`'s consumer), not by inspecting prompt text for resume-likeness,
which cannot be done reliably.
"""

from __future__ import annotations

import json
import logging
import time
from collections.abc import Callable, Sequence
from dataclasses import dataclass
from typing import TYPE_CHECKING, Any, Protocol

import httpx

from scout_brain.metrics import (
    LLM_BUDGET_USED_RATIO,
    LLM_CALLS_TOTAL,
    LLM_ERRORS_TOTAL,
    LLM_LATENCY_SECONDS,
)

if TYPE_CHECKING:
    from scout_brain.config import LLMConfig, ProviderConfig

logger = logging.getLogger(__name__)

DEFAULT_MODEL = "qwen2.5:3b-instruct"


class LLMUnavailableError(Exception):
    """Raised when Ollama cannot be reached or never returns valid JSON
    after a retry — the caller's fallback (template output, or treating an
    uncertain dedup pair as distinct, per ADR-016's degrade posture) is the
    correct response, not a crash.
    """


@dataclass(frozen=True)
class StructuredResponse:
    data: dict[str, Any]


class LLMClient(Protocol):
    """The `generate_json`/`model` surface OllamaClient and CascadeClient
    both expose. Consumers that only need public-data inference (dedup
    adjudication, Tier 2 classification, summaries) take this instead of
    the concrete OllamaClient, so either client works. explain.py's
    ExplainConsumer deliberately does NOT use this — it reads job_score's
    resume-derived match data, and AGENTS.md rule 9 requires that stay
    local, so it's pinned to the concrete OllamaClient type specifically to
    make passing a CascadeClient there a type error, not just a code-review
    catch.
    """

    @property
    def model(self) -> str: ...

    def generate_json(self, prompt: str, task: str = "") -> StructuredResponse: ...


class OllamaClient:
    def __init__(
        self, host: str, model: str = DEFAULT_MODEL, timeout_seconds: float = 30.0
    ) -> None:
        self._host = host.rstrip("/")
        self._model = model
        self._client = httpx.Client(timeout=timeout_seconds)

    @property
    def model(self) -> str:
        """The model actually in use — callers that persist an
        explanation/summary alongside its provenance (job.ai_summary_model
        and friends) read this rather than hardcoding DEFAULT_MODEL, so
        the recorded value stays correct if this client is ever
        constructed with an override.
        """
        return self._model

    def generate_json(self, prompt: str, task: str = "") -> StructuredResponse:
        """Sends prompt with Ollama's JSON output mode, retrying once on a
        response that isn't valid JSON (a real, if uncommon, small-model
        failure mode) before giving up — three behaviours ADR-016 specifies
        for the cascade's last tier: try, retry once, then degrade rather
        than loop or crash.

        task identifies the caller (e.g. "explain", "dedup_stage3") for
        docs/16-observability.md's per-task metrics — optional and
        defaulted rather than required, since a handful of call sites in
        this codebase predate this instrumentation and an empty label is a
        harmless "unknown" bucket, not a broken one.
        """
        start = time.monotonic()
        last_error: Exception | None = None
        for attempt in range(2):
            try:
                raw = self._call(prompt)
            except httpx.HTTPError as exc:
                LLM_ERRORS_TOTAL.labels(
                    provider="local", error_class="http_error"
                ).inc()
                raise LLMUnavailableError(f"ollama unreachable: {exc}") from exc

            try:
                result = StructuredResponse(data=json.loads(raw))
                LLM_CALLS_TOTAL.labels(task=task, provider="local").inc()
                LLM_LATENCY_SECONDS.labels(task=task, provider="local").observe(
                    time.monotonic() - start
                )
                return result
            except json.JSONDecodeError as exc:
                last_error = exc
                logger.warning(
                    "ollama returned non-JSON response (attempt %d): %r",
                    attempt + 1,
                    raw[:200],
                )

        LLM_ERRORS_TOTAL.labels(provider="local", error_class="invalid_json").inc()
        raise LLMUnavailableError(f"ollama never returned valid JSON: {last_error}")

    def _call(self, prompt: str) -> str:
        resp = self._client.post(
            f"{self._host}/api/generate",
            json={
                "model": self._model,
                "prompt": prompt,
                "format": "json",
                "stream": False,
                # Classification/adjudication calls want a consistent answer
                # for the same input, not creative variance — Ollama's
                # non-zero default temperature was observed to flip the same
                # prompt between two valid role families across runs.
                "options": {"temperature": 0},
            },
        )
        resp.raise_for_status()
        text: str = resp.json()["response"]
        return text


class RateLimitExceeded(Exception):
    """Raised internally when a provider's daily allowance (rpd) is
    exhausted, or the provider itself returned HTTP 429. Distinct from an
    rpm (per-minute) limit, which `_RateLimiter` waits out instead of
    raising — ADR-016's "wait, do not degrade" for a window that refills
    in under a minute. `CascadeClient` catches this and rotates to the
    next provider; callers of `generate_json` never see it.
    """


class _RateLimiter:
    """A sliding-window request counter for one provider's rpm and rpd
    budgets (ADR-016's "budgets are request-shaped"). `clock`/`sleep` are
    injected so tests exercise real rotation/wait logic without a wall
    clock or an actual sleep.
    """

    def __init__(
        self,
        rpm: int | None,
        rpd: int | None,
        clock: Callable[[], float] = time.monotonic,
        sleep: Callable[[float], None] = time.sleep,
    ) -> None:
        self._rpm = rpm
        self._rpd = rpd
        self._clock = clock
        self._sleep = sleep
        self._minute_timestamps: list[float] = []
        self._day_timestamps: list[float] = []

    def acquire(self) -> None:
        """Blocks until a request may be sent, or raises RateLimitExceeded
        if the daily allowance is already spent. Call once per request,
        immediately before sending it.
        """
        now = self._clock()
        self._day_timestamps = [t for t in self._day_timestamps if now - t < 86400]
        if self._rpd is not None and len(self._day_timestamps) >= self._rpd:
            raise RateLimitExceeded("daily allowance exhausted")

        while True:
            now = self._clock()
            self._minute_timestamps = [
                t for t in self._minute_timestamps if now - t < 60
            ]
            if self._rpm is None or len(self._minute_timestamps) < self._rpm:
                break
            wait_seconds = 60 - (now - self._minute_timestamps[0])
            logger.info("cascade: rpm limit hit, waiting %.1fs", wait_seconds)
            self._sleep(max(wait_seconds, 0.1))

        now = self._clock()
        self._minute_timestamps.append(now)
        self._day_timestamps.append(now)

    @property
    def budget_used_ratio(self) -> float:
        """Fraction of today's rpd allowance already spent — 0.0 for a
        provider with no daily cap (rpd=None; nothing to be a fraction of).
        Reads the same _day_timestamps acquire() maintains rather than a
        separate counter, so this can never drift from what actually gates
        a request.
        """
        if self._rpd is None:
            return 0.0
        now = self._clock()
        recent = [t for t in self._day_timestamps if now - t < 86400]
        return min(len(recent) / self._rpd, 1.0)


class GeminiProvider:
    """Google AI Studio's free tier. Distinct request/response shape from
    the OpenAI-compatible providers below, hence its own class rather than
    a `base_url` variation of `OpenAICompatibleProvider`.
    """

    def __init__(self, api_key: str, model: str, timeout_seconds: float = 30.0) -> None:
        self._api_key = api_key
        self._model = model
        self._client = httpx.Client(timeout=timeout_seconds)

    def send(self, prompt: str) -> str:
        resp = self._client.post(
            f"https://generativelanguage.googleapis.com/v1beta/models/{self._model}:generateContent",
            params={"key": self._api_key},
            json={
                "contents": [{"parts": [{"text": prompt}]}],
                "generationConfig": {
                    "responseMimeType": "application/json",
                    "temperature": 0,
                },
            },
        )
        resp.raise_for_status()
        text: str = resp.json()["candidates"][0]["content"]["parts"][0]["text"]
        return text


class OpenAICompatibleProvider:
    """Groq and OpenRouter's free-tier models both speak the OpenAI chat-
    completions shape, so one class serves both — only `base_url`, the
    key, and the model differ, all supplied by `infra/config/llm_providers.yaml`.
    """

    def __init__(
        self, base_url: str, api_key: str, model: str, timeout_seconds: float = 30.0
    ) -> None:
        self._base_url = base_url.rstrip("/")
        self._model = model
        self._client = httpx.Client(
            timeout=timeout_seconds, headers={"Authorization": f"Bearer {api_key}"}
        )

    def send(self, prompt: str) -> str:
        resp = self._client.post(
            f"{self._base_url}/chat/completions",
            json={
                "model": self._model,
                "messages": [{"role": "user", "content": prompt}],
                "response_format": {"type": "json_object"},
                "temperature": 0,
            },
        )
        resp.raise_for_status()
        text: str = resp.json()["choices"][0]["message"]["content"]
        return text


class Provider(Protocol):
    """The name/model/call surface CascadeClient needs from each cascade
    entry — RateLimitedProvider in production, a bare test double in
    tests/test_llm.py's cascade-routing tests, which have no need for real
    rate limiting or HTTP.
    """

    name: str
    model: str

    def call(self, prompt: str) -> str: ...


class RateLimitedProvider:
    """One cascade entry: a hosted provider's `send`, wrapped with its own
    rpm/rpd budget. `CascadeClient` holds an ordered list of these.
    """

    def __init__(
        self,
        name: str,
        model: str,
        send: Callable[[str], str],
        rpm: int | None,
        rpd: int | None,
    ) -> None:
        self.name = name
        self.model = model
        self._send = send
        self._limiter = _RateLimiter(rpm, rpd)

    def call(self, prompt: str) -> str:
        self._limiter.acquire()
        LLM_BUDGET_USED_RATIO.labels(provider=self.name).set(
            self._limiter.budget_used_ratio
        )
        try:
            return self._send(prompt)
        except httpx.HTTPStatusError as exc:
            if exc.response.status_code == 429:
                raise RateLimitExceeded(f"{self.name} returned 429") from exc
            raise


class CascadeClient:
    """ADR-016's cascade: walk the configured hosted providers in order,
    rotating past one on daily exhaustion, a 429, a request failure, or a
    non-JSON response, and fall back to a local `OllamaClient` only once
    every hosted provider is exhausted — "degrade only when everything is
    exhausted," exactly as the ADR specifies. An rpm limit is waited out
    inside `RateLimitedProvider.call` rather than treated as exhaustion,
    per the same ADR: a per-minute window refills in under a minute and
    scoring is off the notification critical path.

    Exposes the same `generate_json`/`model` surface as `OllamaClient` —
    `model` is mutated after each call to record which tier actually
    answered, exactly as `OllamaClient.model` already does, so callers that
    persist provenance (`job.ai_summary_model` and friends) need no
    changes to work with either client.
    """

    def __init__(self, providers: Sequence[Provider], local: OllamaClient) -> None:
        self._providers = providers
        self._local = local
        self._model = local.model

    @property
    def model(self) -> str:
        return self._model

    def generate_json(self, prompt: str, task: str = "") -> StructuredResponse:
        for provider in self._providers:
            start = time.monotonic()
            try:
                raw = provider.call(prompt)
            except RateLimitExceeded as exc:
                logger.info("cascade: %s exhausted (%s), rotating", provider.name, exc)
                LLM_ERRORS_TOTAL.labels(
                    provider=provider.name, error_class="rate_limited"
                ).inc()
                continue
            except httpx.HTTPError as exc:
                logger.warning(
                    "cascade: %s request failed (%s), rotating", provider.name, exc
                )
                LLM_ERRORS_TOTAL.labels(
                    provider=provider.name, error_class="http_error"
                ).inc()
                continue

            try:
                data = json.loads(raw)
            except json.JSONDecodeError:
                logger.warning(
                    "cascade: %s returned non-JSON response, rotating", provider.name
                )
                LLM_ERRORS_TOTAL.labels(
                    provider=provider.name, error_class="invalid_json"
                ).inc()
                continue

            self._model = provider.model
            LLM_CALLS_TOTAL.labels(task=task, provider=provider.name).inc()
            LLM_LATENCY_SECONDS.labels(task=task, provider=provider.name).observe(
                time.monotonic() - start
            )
            return StructuredResponse(data=data)

        logger.info(
            "cascade: every hosted provider exhausted or failed, falling back to local Ollama"
        )
        result = self._local.generate_json(prompt, task=task)
        self._model = self._local.model
        return result


def build_cascade_client(
    llm_config: LLMConfig, ollama_host: str
) -> OllamaClient | CascadeClient:
    """Constructs the client for public-data tasks (classification,
    summarization, dedup adjudication) from config.load()'s LLMConfig. With
    no hosted providers configured — no API keys set on this host — this
    returns a plain OllamaClient rather than a one-provider CascadeClient,
    so a dev machine with zero signups behaves exactly as it did before the
    cascade existed, not as a cascade of one that always falls through.
    """
    local = OllamaClient(ollama_host, model=llm_config.local_model)
    if not llm_config.providers:
        return local

    providers = [_build_provider(p) for p in llm_config.providers]
    return CascadeClient(providers, local)


def _build_provider(cfg: ProviderConfig) -> RateLimitedProvider:
    if cfg.kind == "gemini":
        send = GeminiProvider(cfg.api_key, cfg.model).send
    elif cfg.kind == "openai_compatible":
        if not cfg.base_url:
            raise ValueError(
                f"llm provider {cfg.name!r}: openai_compatible requires base_url"
            )
        send = OpenAICompatibleProvider(cfg.base_url, cfg.api_key, cfg.model).send
    else:
        raise ValueError(f"llm provider {cfg.name!r}: unknown kind {cfg.kind!r}")
    return RateLimitedProvider(cfg.name, cfg.model, send, cfg.rpm, cfg.rpd)
